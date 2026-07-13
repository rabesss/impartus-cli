package downloader

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

func TestCopyWithLimit(t *testing.T) {
	t.Run("exact limit", func(t *testing.T) {
		var dst bytes.Buffer
		written, err := copyWithLimit(&dst, bytes.NewBufferString("12345678"), 8)
		if err != nil || written != 8 || dst.String() != "12345678" {
			t.Fatalf("copyWithLimit() = (%d, %v, %q), want exact success", written, err, dst.String())
		}
	})

	t.Run("one byte over limit", func(t *testing.T) {
		var dst bytes.Buffer
		written, err := copyWithLimit(&dst, bytes.NewBufferString("123456789"), 8)
		if !errors.Is(err, errDownloadSizeLimit) || written != 9 {
			t.Fatalf("copyWithLimit() = (%d, %v), want size-limit error after 9 bytes", written, err)
		}
	})
}

func TestDownloadURLWithLimit(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "exact limit succeeds",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, "12345678") //nolint:errcheck
			},
		},
		{
			name: "declared oversize is rejected",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Length", "9")
				_, _ = io.WriteString(w, "123456789") //nolint:errcheck
			},
			wantErr: true,
		},
		{
			name: "chunked oversize is rejected",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				flusher, ok := w.(http.Flusher)
				if !ok {
					http.Error(w, "streaming unsupported", http.StatusInternalServerError)
					return
				}
				flusher.Flush()
				_, _ = io.WriteString(w, "123456789") //nolint:errcheck
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()
			tempDir := t.TempDir()
			d := testLimitDownloader(tempDir, client.New(server.Client(), nil))

			path, _, _, err := d.doDownloadChunkWithLimit(context.Background(), server.URL+"/chunk.ts", 1, 0, "left", false, 8)
			if tt.wantErr {
				if !errors.Is(err, errDownloadSizeLimit) {
					t.Fatalf("doDownloadChunkWithLimit() error = %v, want size-limit error", err)
				}
				assertNoChunkPartial(t, tempDir)
				return
			}
			if err != nil {
				t.Fatalf("doDownloadChunkWithLimit() unexpected error: %v", err)
			}
			contents, readErr := os.ReadFile(path)
			if readErr != nil || string(contents) != "12345678" {
				t.Fatalf("downloaded chunk = %q, %v", contents, readErr)
			}
		})
	}
}

func TestDownloadURLWithLimitRemovesInterruptedPartial(t *testing.T) {
	tempDir := t.TempDir()
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			ContentLength: -1,
			Body:          io.NopCloser(&failingChunkReader{}),
			Header:        make(http.Header),
		}, nil
	})}
	d := testLimitDownloader(tempDir, client.New(httpClient, nil))

	path, data, written, err := d.doDownloadChunkWithLimit(context.Background(), "https://media.placeholder.test/chunk.ts", 1, 0, "left", false, 8)
	if err == nil || errors.Is(err, errDownloadSizeLimit) {
		t.Fatalf("doDownloadChunkWithLimit() error = %v, want interrupted-read error", err)
	}
	if path != "" || data != nil || written != 0 {
		t.Fatalf("failed download returned path=%q data=%v written=%d", path, data, written)
	}
	assertNoChunkPartial(t, tempDir)
}

func testLimitDownloader(tempDir string, apiClient *client.Client) *Downloader {
	return &Downloader{
		config:      &config.Config{TempDirLocation: tempDir, Token: "placeholder-token"},
		client:      apiClient,
		rateLimiter: NewRateLimiter(100.0, 50.0, false),
	}
}

func assertNoChunkPartial(t *testing.T, tempDir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(tempDir, "*.temp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("partial chunk files remain: %v", matches)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

type failingChunkReader struct {
	read bool
}

func (r *failingChunkReader) Read(p []byte) (int, error) {
	if r.read {
		return 0, errors.New("synthetic interruption")
	}
	r.read = true
	return copy(p, "1234"), nil
}
