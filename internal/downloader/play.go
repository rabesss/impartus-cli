package downloader

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rabesss/impartus-cli/internal/client"
)

// StartPlayServer starts a temporary local HTTP server to stream and decrypt HLS segments on the fly.
// It returns the URL to the master playlist, a cleanup function to shut down the server, and any error.
func (d *Downloader) StartPlayServer(ctx context.Context, playlist client.ParsedPlaylist) (string, func(), error) {
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

	mux := http.NewServeMux()
	mux.HandleFunc("/master.m3u8", d.handleMaster(playlist, port))
	mux.HandleFunc("/left.m3u8", d.handleLeft(playlist, port))
	mux.HandleFunc("/right.m3u8", d.handleRight(playlist, port))
	mux.HandleFunc("/segment/", d.handleSegment(playlist, decryptionKey))

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
		_ = server.Close()   //nolint:errcheck
		_ = listener.Close() //nolint:errcheck
	}

	masterURL := fmt.Sprintf("http://127.0.0.1:%d/master.m3u8", port)
	return masterURL, cleanup, nil
}

func (d *Downloader) handleMaster(playlist client.ParsedPlaylist, port int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")

		var sb strings.Builder
		sb.WriteString("#EXTM3U\n")

		hasFirst := d.config.Views != "right" && len(playlist.FirstViewURLs) > 0
		hasSecond := d.config.Views != "left" && len(playlist.SecondViewURLs) > 0 && playlist.HasMultipleViews

		if hasFirst {
			_, _ = fmt.Fprintf(&sb, "#EXT-X-STREAM-INF:BANDWIDTH=1000000,RESOLUTION=1280x720,NAME=\"Left View\"\nhttp://127.0.0.1:%d/left.m3u8\n", port)
		}
		if hasSecond {
			_, _ = fmt.Fprintf(&sb, "#EXT-X-STREAM-INF:BANDWIDTH=1000000,RESOLUTION=1280x720,NAME=\"Right View\"\nhttp://127.0.0.1:%d/right.m3u8\n", port)
		}

		if !hasFirst && !hasSecond {
			_, _ = fmt.Fprintf(&sb, "#EXT-X-STREAM-INF:BANDWIDTH=1000000,RESOLUTION=1280x720,NAME=\"Left View\"\nhttp://127.0.0.1:%d/left.m3u8\n", port)
		}

		_, _ = w.Write([]byte(sb.String())) //nolint:errcheck
	}
}

func (d *Downloader) handleLeft(playlist client.ParsedPlaylist, port int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		playlistStr := buildLocalM3U8("left", playlist.FirstViewURLs, port)
		_, _ = w.Write([]byte(playlistStr)) //nolint:errcheck
	}
}

func (d *Downloader) handleRight(playlist client.ParsedPlaylist, port int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		playlistStr := buildLocalM3U8("right", playlist.SecondViewURLs, port)
		_, _ = w.Write([]byte(playlistStr)) //nolint:errcheck
	}
}

func (d *Downloader) handleSegment(playlist client.ParsedPlaylist, decryptionKey []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/segment/"), "/")
		if len(parts) != 2 {
			http.Error(w, "invalid segment path", http.StatusBadRequest)
			return
		}
		view := parts[0]
		idxStr := parts[1]
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

		encryptedBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to read segment bytes: %v", err), http.StatusInternalServerError)
			return
		}

		decryptedBytes, err := decryptInMemory(encryptedBytes, decryptionKey)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to decrypt segment: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "video/MP2T")
		w.Header().Set("Content-Length", strconv.Itoa(len(decryptedBytes)))
		_, _ = w.Write(decryptedBytes) //nolint:errcheck
	}
}

func decryptInMemory(encrypted []byte, key []byte) ([]byte, error) {
	if len(key) != 16 && len(key) != 24 && len(key) != 32 {
		return nil, fmt.Errorf("invalid AES key length: %d", len(key))
	}
	if len(encrypted) == 0 || len(encrypted)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("ciphertext length %d is not a multiple of block size %d", len(encrypted), aes.BlockSize)
	}
	iv := make([]byte, 16)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	mode := cipher.NewCBCDecrypter(block, iv)
	plainText := make([]byte, len(encrypted))
	mode.CryptBlocks(plainText, encrypted)
	return removePKCS7Padding(plainText), nil
}

func buildLocalM3U8(view string, urls []string, port int) string {
	var sb strings.Builder
	sb.WriteString("#EXTM3U\n")
	sb.WriteString("#EXT-X-VERSION:3\n")
	sb.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")
	sb.WriteString("#EXT-X-ALLOW-CACHE:YES\n")
	sb.WriteString("#EXT-X-TARGETDURATION:11\n")
	for i := range urls {
		_, _ = fmt.Fprintf(&sb, "#EXTINF:11.0,\nhttp://127.0.0.1:%d/segment/%s/%d\n", port, view, i)
	}
	sb.WriteString("#EXT-X-ENDLIST\n")
	return sb.String()
}
