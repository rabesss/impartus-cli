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
	"time"

	"github.com/google/uuid"

	"github.com/rabesss/impartus-cli/internal/client"
)

// StartPlayServer starts a temporary local HTTP server to stream and decrypt HLS segments on the fly.
// It returns the URL to the master playlist, a cleanup function to shut down the server, and any error.
func (d *Downloader) StartPlayServer(ctx context.Context, playlist client.ParsedPlaylist) (string, func(), error) {
	if !d.hasPlayableViews(playlist) {
		return "", nil, fmt.Errorf("no playable views available for lecture %d", playlist.SeqNo)
	}

	decryptionKey, err := d.fetchDecryptionKey(ctx, playlist.KeyURL)
	if err != nil {
		return "", nil, fmt.Errorf("failed to fetch decryption key: %w", err)
	}

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
	mux.HandleFunc(fmt.Sprintf("/%s/segment/", sessionToken), d.handleSegment(playlist, decryptionKey))

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		_ = server.Serve(listener) //nolint:errcheck
	}()

	cleanup := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx) //nolint:errcheck
	}

	masterURL := fmt.Sprintf("http://127.0.0.1:%d/%s/master.m3u8", port, sessionToken)
	return masterURL, cleanup, nil
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
	hasFirst := d.config.Views != "right" && len(playlist.FirstViewURLs) > 0
	hasSecond := d.config.Views != "left" && len(playlist.SecondViewURLs) > 0 && playlist.HasMultipleViews
	return hasFirst, hasSecond
}

func (d *Downloader) hasPlayableViews(playlist client.ParsedPlaylist) bool {
	hasFirst, hasSecond := d.playableViews(playlist)
	return hasFirst || hasSecond
}

func (d *Downloader) handleLeft(playlist client.ParsedPlaylist, port int, token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		playlistStr := buildLocalM3U8("left", playlist.FirstViewURLs, playlist.FirstDurations, port, token)
		_, _ = w.Write([]byte(playlistStr)) //nolint:errcheck
	}
}

func (d *Downloader) handleRight(playlist client.ParsedPlaylist, port int, token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		playlistStr := buildLocalM3U8("right", playlist.SecondViewURLs, playlist.SecondDurations, port, token)
		_, _ = w.Write([]byte(playlistStr)) //nolint:errcheck
	}
}

func (d *Downloader) handleSegment(playlist client.ParsedPlaylist, decryptionKey []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Expecting path like /<token>/segment/<view>/<idx>
		path := strings.TrimPrefix(r.URL.Path, "/")
		parts := strings.Split(path, "/")
		if len(parts) != 4 {
			http.Error(w, "invalid segment path", http.StatusBadRequest)
			return
		}
		view := parts[2]
		idxStr := parts[3]
		idx, err := strconv.Atoi(idxStr)
		if err != nil {
			http.Error(w, "invalid segment index", http.StatusBadRequest)
			return
		}

		var urls []string
		switch view {
		case "left":
			urls = playlist.FirstViewURLs
		case "right":
			urls = playlist.SecondViewURLs
		default:
			http.Error(w, "invalid view name", http.StatusBadRequest)
			return
		}

		if idx < 0 || idx >= len(urls) {
			http.Error(w, "segment index out of range", http.StatusNotFound)
			return
		}

		realURL := urls[idx]

		if waitErr := d.rateLimiter.WaitForDownload(r.Context()); waitErr != nil {
			http.Error(w, fmt.Sprintf("rate limit wait failed: %v", waitErr), http.StatusInternalServerError)
			return
		}

		resp, err := d.client.GetAuthorizedWithToken(r.Context(), realURL, d.config.Token)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to fetch segment: %v", err), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close() //nolint:errcheck

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
			http.Error(w, fmt.Sprintf("failed to read segment bytes: %v", err), http.StatusBadGateway)
			return
		}

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
