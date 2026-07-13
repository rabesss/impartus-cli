package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

func TestDownloadLectureSlideLimitAndAtomicReplacement(t *testing.T) {
	tests := []struct {
		name        string
		handler     http.HandlerFunc
		wantErr     bool
		wantLimit   bool
		wantContent string
		wantMode    os.FileMode
	}{
		{
			name: "exact limit replaces final atomically",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, "12345678") //nolint:errcheck
			},
			wantContent: "12345678",
			wantMode:    0o600,
		},
		{
			name: "declared oversize preserves final",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Length", "9")
				_, _ = io.WriteString(w, "123456789") //nolint:errcheck
			},
			wantErr:   true,
			wantLimit: true,
		},
		{
			name: "chunked oversize preserves final",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				flusher, ok := w.(http.Flusher)
				if !ok {
					http.Error(w, "streaming unsupported", http.StatusInternalServerError)
					return
				}
				flusher.Flush()
				_, _ = io.WriteString(w, "123456789") //nolint:errcheck
			},
			wantErr:   true,
			wantLimit: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()
			downloadDir := t.TempDir()
			finalPath := filepath.Join(downloadDir, "LEC 001 Lecture.pdf")
			if err := os.WriteFile(finalPath, []byte("existing"), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg := &config.Config{BaseURL: server.URL, DownloadLocation: downloadDir, Token: "placeholder-token"}
			lecture := client.Lecture{VideoID: 10, SeqNo: 1, Topic: "Lecture"}

			err := downloadLectureSlideWithLimit(context.Background(), client.New(server.Client(), nil), cfg, lecture, 8)
			if tt.wantErr {
				if err == nil || tt.wantLimit && !errors.Is(err, errSlideSizeLimit) {
					t.Fatalf("downloadLectureSlideWithLimit() error = %v", err)
				}
			} else if err != nil {
				t.Fatalf("downloadLectureSlideWithLimit() unexpected error: %v", err)
			}

			contents, readErr := os.ReadFile(finalPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			wantContent := tt.wantContent
			if tt.wantErr {
				wantContent = "existing"
			}
			if string(contents) != wantContent {
				t.Fatalf("final slide = %q, want %q", contents, wantContent)
			}
			if runtime.GOOS != "windows" && tt.wantMode != 0 {
				info, statErr := os.Stat(finalPath)
				if statErr != nil {
					t.Fatal(statErr)
				}
				if got := info.Mode().Perm(); got != tt.wantMode {
					t.Fatalf("final slide mode = %04o, want %04o", got, tt.wantMode)
				}
			}
			assertNoSlideParts(t, downloadDir)
		})
	}
}

func TestDownloadLectureSlideNewFileUsesReadableMode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "slide") //nolint:errcheck
	}))
	defer server.Close()
	downloadDir := t.TempDir()
	cfg := &config.Config{BaseURL: server.URL, DownloadLocation: downloadDir, Token: "placeholder-token"}
	lecture := client.Lecture{VideoID: 10, SeqNo: 1, Topic: "Lecture"}

	if err := downloadLectureSlideWithLimit(context.Background(), client.New(server.Client(), nil), cfg, lecture, 8); err != nil {
		t.Fatalf("downloadLectureSlideWithLimit() unexpected error: %v", err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(downloadDir, "LEC 001 Lecture.pdf"))
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o644 {
			t.Fatalf("final slide mode = %04o, want 0644", got)
		}
	}
	assertNoSlideParts(t, downloadDir)
}

func TestDownloadLectureSlideInterruptedReadPreservesFinal(t *testing.T) {
	testDownloadLectureSlideTransportFailure(t, io.NopCloser(&failingSlideReader{}), "interrupted-read")
}

func TestDownloadLectureSlideResponseCloseFailurePreservesFinal(t *testing.T) {
	testDownloadLectureSlideTransportFailure(t, &closeErrorSlideBody{Reader: io.NopCloser(&fixedSlideReader{})}, "close-response")
}

func testDownloadLectureSlideTransportFailure(t *testing.T, body io.ReadCloser, operation string) {
	t.Helper()
	downloadDir := t.TempDir()
	finalPath := filepath.Join(downloadDir, "LEC 001 Lecture.pdf")
	if err := os.WriteFile(finalPath, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	httpClient := &http.Client{Transport: slideRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			ContentLength: -1,
			Body:          body,
			Header:        make(http.Header),
		}, nil
	})}
	cfg := &config.Config{BaseURL: "https://api.placeholder.test", DownloadLocation: downloadDir, Token: "placeholder-token"}
	lecture := client.Lecture{VideoID: 10, SeqNo: 1, Topic: "Lecture"}

	err := downloadLectureSlideWithLimit(context.Background(), client.New(httpClient, nil), cfg, lecture, 8)
	if err == nil || errors.Is(err, errSlideSizeLimit) {
		t.Fatalf("downloadLectureSlideWithLimit() error = %v, want %s error", err, operation)
	}
	contents, readErr := os.ReadFile(finalPath)
	if readErr != nil || string(contents) != "existing" {
		t.Fatalf("final slide = %q, %v; want preserved content", contents, readErr)
	}
	assertNoSlideParts(t, downloadDir)
}

func assertNoSlideParts(t *testing.T, downloadDir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(downloadDir, ".slide-*.part"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("partial slide files remain: %v", matches)
	}
}

type slideRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn slideRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

type failingSlideReader struct {
	read bool
}

type fixedSlideReader struct {
	read bool
}

func (r *fixedSlideReader) Read(p []byte) (int, error) {
	if r.read {
		return 0, io.EOF
	}
	r.read = true
	return copy(p, "12345678"), nil
}

type closeErrorSlideBody struct {
	Reader io.ReadCloser
}

func (b *closeErrorSlideBody) Read(p []byte) (int, error) {
	return b.Reader.Read(p)
}

func (b *closeErrorSlideBody) Close() error {
	_ = b.Reader.Close() //nolint:errcheck
	return errors.New("synthetic close failure")
}

func (r *failingSlideReader) Read(p []byte) (int, error) {
	if r.read {
		return 0, errors.New("synthetic interruption")
	}
	r.read = true
	return copy(p, "1234"), nil
}
