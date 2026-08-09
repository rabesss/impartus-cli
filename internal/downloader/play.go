package downloader

import (
	"context"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/secrets"
)

type playbackKey struct {
	mutex    sync.Mutex
	material []byte
	closed   bool
}

func newPlaybackKey(material []byte) *playbackKey {
	return &playbackKey{material: material}
}

func (key *playbackKey) acquire() ([]byte, bool) {
	key.mutex.Lock()
	defer key.mutex.Unlock()
	if key.closed {
		return nil, false
	}
	return append([]byte(nil), key.material...), true
}

func (key *playbackKey) close() {
	key.mutex.Lock()
	defer key.mutex.Unlock()
	if key.closed {
		return
	}
	key.closed = true
	zeroKey(key.material)
}

// StartPlayServer starts a temporary local HTTP server to stream and decrypt HLS segments on the fly.
// It returns the URL to the master playlist, a cleanup function to shut down the server, and any error.
func (d *Downloader) StartPlayServer(ctx context.Context, playlist client.ParsedPlaylist) (string, func(), error) {
	if !d.hasPlayableViews(playlist) {
		return "", nil, fmt.Errorf("no playable views available for lecture %d", playlist.SeqNo)
	}

	decryptionKey, err := d.fetchDecryptionKey(ctx, playlist.KeyURL)
	if err != nil {
		return "", nil, fmt.Errorf("failed to fetch decryption key: %s", secrets.ScrubError(err))
	}
	keyStore := newPlaybackKey(decryptionKey)
	keyOwned := true
	defer func() {
		if keyOwned {
			keyStore.close()
		}
	}()

	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create listener: %w", err)
	}

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		_ = listener.Close() //nolint:errcheck
		return "", nil, fmt.Errorf("failed to assert net.Addr to *net.TCPAddr")
	}
	port := tcpAddr.Port

	sessionToken := uuid.New().String()
	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/%s/master.m3u8", sessionToken), d.handleMaster(playlist, port, sessionToken))
	mux.HandleFunc(fmt.Sprintf("/%s/left.m3u8", sessionToken), d.handleLeft(playlist, port, sessionToken))
	mux.HandleFunc(fmt.Sprintf("/%s/right.m3u8", sessionToken), d.handleRight(playlist, port, sessionToken))
	mux.HandleFunc(fmt.Sprintf("/%s/segment/", sessionToken), d.handleSegment(playlist, keyStore, sessionToken))
	expectedHost := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))

	server := &http.Server{
		Handler:           securePlayHandler(expectedHost, mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		_ = server.Serve(listener) //nolint:errcheck
	}()

	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdownCtx) //nolint:errcheck
			_ = listener.Close()             //nolint:errcheck
			keyStore.close()
		})
	}

	masterURL := fmt.Sprintf("http://127.0.0.1:%d/%s/master.m3u8", port, sessionToken)
	keyOwned = false
	return masterURL, cleanup, nil
}

func securePlayHandler(expectedHost string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Host != expectedHost {
			http.Error(w, "unexpected host", http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (d *Downloader) handleMaster(playlist client.ParsedPlaylist, port int, token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")

		var sb strings.Builder
		sb.WriteString("#EXTM3U\n")

		hasFirst, hasSecond := d.playableViews(playlist)
		if !hasFirst && !hasSecond {
			http.Error(w, "no playable views available", http.StatusNotFound)
			return
		}
		bandwidth, resolution := hlsVariantMetadata(d.config.Quality)

		if hasFirst {
			_, _ = fmt.Fprintf(&sb, "#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%s,NAME=\"Left View\"\nhttp://127.0.0.1:%d/%s/left.m3u8\n", bandwidth, resolution, port, token)
		}
		if hasSecond {
			_, _ = fmt.Fprintf(&sb, "#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%s,NAME=\"Right View\"\nhttp://127.0.0.1:%d/%s/right.m3u8\n", bandwidth, resolution, port, token)
		}

		_, _ = w.Write([]byte(sb.String())) //nolint:errcheck
	}
}

func (d *Downloader) playableViews(playlist client.ParsedPlaylist) (bool, bool) {
	hasFirst := d.config.IncludesLeft() && len(playlist.FirstViewURLs) > 0
	hasSecond := d.config.IncludesRight() && len(playlist.SecondViewURLs) > 0 && playlist.HasMultipleViews
	return hasFirst, hasSecond
}

func (d *Downloader) hasPlayableViews(playlist client.ParsedPlaylist) bool {
	hasFirst, hasSecond := d.playableViews(playlist)
	return hasFirst || hasSecond
}

func (d *Downloader) handleLeft(playlist client.ParsedPlaylist, port int, token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = w.Write([]byte(buildLocalM3U8("left", playlist.FirstViewURLs, playlist.FirstDurations, port, token))) //nolint:errcheck
	}
}

func (d *Downloader) handleRight(playlist client.ParsedPlaylist, port int, token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = w.Write([]byte(buildLocalM3U8("right", playlist.SecondViewURLs, playlist.SecondDurations, port, token))) //nolint:errcheck
	}
}

func (d *Downloader) handleSegment(playlist client.ParsedPlaylist, keyStore *playbackKey, token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		realURL, status, message := resolveSegmentSource(playlist, token, r.URL.Path)
		if status != 0 {
			http.Error(w, message, status)
			return
		}

		if waitErr := d.rateLimiter.WaitForDownload(r.Context()); waitErr != nil {
			http.Error(w, fmt.Sprintf("rate limit wait failed: %v", waitErr), http.StatusInternalServerError)
			return
		}

		resp, err := d.client.GetAuthorizedWithToken(r.Context(), realURL, d.config.Token)
		if err != nil {
			http.Error(w, "failed to fetch upstream segment", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close() //nolint:errcheck

		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			http.Error(w, "upstream authorization failed", http.StatusBadGateway)
			return
		}
		if resp.StatusCode != http.StatusOK {
			http.Error(w, fmt.Sprintf("segment fetch returned status %d", resp.StatusCode), http.StatusBadGateway)
			return
		}

		const maxSegmentSize = 50 * 1024 * 1024 // 50 MB
		if resp.ContentLength > maxSegmentSize {
			http.Error(w, fmt.Sprintf("segment exceeds maximum size of %d bytes", maxSegmentSize), http.StatusBadGateway)
			return
		}

		encryptedBytes, err := readSegmentBytes(resp.Body, maxSegmentSize)
		if err != nil {
			http.Error(w, "failed to read upstream segment", http.StatusBadGateway)
			return
		}

		decryptionKey, acquired := keyStore.acquire()
		if !acquired {
			http.Error(w, "playback is shutting down", http.StatusServiceUnavailable)
			return
		}
		defer zeroKey(decryptionKey)
		decryptedBytes, err := DecryptAESInPlace(encryptedBytes, decryptionKey)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to decrypt segment: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "video/MP2T")
		w.Header().Set("Content-Length", strconv.Itoa(len(decryptedBytes)))
		_, _ = w.Write(decryptedBytes) //nolint:errcheck
	}
}

func resolveSegmentSource(playlist client.ParsedPlaylist, token, requestPath string) (string, int, string) {
	// Expecting path like /<token>/segment/<view>/<idx>.
	parts := strings.Split(strings.TrimPrefix(requestPath, "/"), "/")
	if len(parts) != 4 {
		return "", http.StatusBadRequest, "invalid segment path"
	}
	if parts[0] != token || parts[1] != "segment" {
		return "", http.StatusNotFound, "invalid segment path"
	}
	index, err := strconv.Atoi(parts[3])
	if err != nil {
		return "", http.StatusBadRequest, "invalid segment index"
	}
	var urls []string
	switch parts[2] {
	case "left":
		urls = playlist.FirstViewURLs
	case "right":
		urls = playlist.SecondViewURLs
	default:
		return "", http.StatusBadRequest, "invalid view name"
	}
	if index < 0 || index >= len(urls) {
		return "", http.StatusNotFound, "segment index out of range"
	}
	return urls[index], 0, ""
}

func readSegmentBytes(r io.Reader, maxBytes int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("segment exceeds maximum size of %d bytes", maxBytes)
	}
	return data, nil
}

func buildLocalM3U8(view string, urls []string, durations []float64, port int, token string) string {
	var sb strings.Builder
	sb.WriteString("#EXTM3U\n")
	sb.WriteString("#EXT-X-VERSION:3\n")
	sb.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")
	sb.WriteString("#EXT-X-ALLOW-CACHE:YES\n")
	_, _ = fmt.Fprintf(&sb, "#EXT-X-TARGETDURATION:%d\n", targetDuration(durations, len(urls)))
	sb.WriteString("#EXT-X-KEY:METHOD=NONE\n")
	for i := range urls {
		_, _ = fmt.Fprintf(&sb, "#EXTINF:%.3f,\nhttp://127.0.0.1:%d/%s/segment/%s/%d\n", segmentDuration(durations, i), port, token, view, i)
	}
	sb.WriteString("#EXT-X-ENDLIST\n")
	return sb.String()
}

func targetDuration(durations []float64, segmentCount int) int {
	maxDuration := 0.0
	for i := 0; i < segmentCount; i++ {
		maxDuration = math.Max(maxDuration, segmentDuration(durations, i))
	}
	if maxDuration <= 0 {
		maxDuration = 11.0
	}
	return int(math.Ceil(maxDuration))
}

func segmentDuration(durations []float64, index int) float64 {
	if index >= 0 && index < len(durations) && durations[index] > 0 {
		return durations[index]
	}
	return 11.0
}

func hlsVariantMetadata(quality string) (int, string) {
	switch strings.TrimSpace(quality) {
	case "144":
		return 256000, "256x144"
	case "450":
		return 800000, "800x450"
	default:
		return 1500000, "1280x720"
	}
}
