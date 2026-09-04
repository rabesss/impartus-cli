package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestDownloadLectureSlideUsesCanonicalEndpointPath(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		if r.Header.Get("Authorization") != "Bearer slide-token" {
			t.Errorf("Authorization = %q, want bearer token", r.Header.Get("Authorization"))
		}
		_, _ = io.WriteString(w, "slide") //nolint:errcheck
	}))
	defer server.Close()

	err := downloadLectureSlideWithLimit(context.Background(), client.New(server.Client(), nil), &config.Config{
		BaseURL:          server.URL + "/api///",
		DownloadLocation: t.TempDir(),
		Token:            "slide-token",
	}, client.Lecture{VideoID: 10, SeqNo: 1, Topic: "Lecture"}, 8)
	if err != nil {
		t.Fatalf("downloadLectureSlideWithLimit() error = %v", err)
	}
	if gotPath != "/api/videos/10/auto-generated-pdf" || gotQuery != "" {
		t.Fatalf("slide request URL = %q?%s, want canonical /api/videos/10/auto-generated-pdf", gotPath, gotQuery)
	}
}

func TestDownloadLectureSlideUnauthorizedReturnsAuthenticationError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	err := downloadLectureSlideWithLimit(context.Background(), client.New(server.Client(), nil), &config.Config{
		BaseURL:          server.URL,
		DownloadLocation: t.TempDir(),
		Token:            "slide-token",
	}, client.Lecture{VideoID: 10, SeqNo: 1, Topic: "Lecture"}, 8)
	if err == nil {
		t.Fatal("downloadLectureSlideWithLimit() error = nil, want AuthenticationError")
	}
	var authErr *client.AuthenticationError
	if !errors.As(err, &authErr) {
		t.Fatalf("error = %T %v, want *client.AuthenticationError", err, err)
	}
	if authErr.Operation != "slide download" || authErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("AuthenticationError = %+v, want slide download/401", authErr)
	}
	if !errors.Is(err, client.ErrAuthentication) {
		t.Fatalf("error = %v, want ErrAuthentication classification", err)
	}
}

func TestDownloadLectureSlideSanitizesNonUnauthorizedResponseBody(t *testing.T) {
	const body = "Authorization: Bearer slide-body-bearer-secret\n" +
		"URL: https://media.example.test/chunk.ts?token=slide-body-query-secret\n" +
		"URL: https://user:slide-body-userinfo-secret@media.example.test/chunk.ts\n" +
		"Cookie: session=slide-body-cookie-secret\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, body) //nolint:errcheck
	}))
	defer server.Close()

	err := downloadLectureSlideWithLimit(context.Background(), client.New(server.Client(), nil), &config.Config{
		BaseURL:          server.URL,
		DownloadLocation: t.TempDir(),
		Token:            "slide-token",
	}, client.Lecture{VideoID: 10, SeqNo: 1, Topic: "Lecture"}, 8)
	if err == nil {
		t.Fatal("downloadLectureSlideWithLimit() error = nil, want upstream failure")
	}
	for _, secret := range []string{
		"slide-body-bearer-secret",
		"slide-body-query-secret",
		"slide-body-userinfo-secret",
		"slide-body-cookie-secret",
	} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("slide error leaked %q: %v", secret, err)
		}
	}
	if !strings.Contains(err.Error(), "REDACTED") {
		t.Fatalf("slide error = %v, want sanitized body marker", err)
	}
}

func TestDownloadLectureSlideOmitsOversizedErrorBodyBeforeRedaction(t *testing.T) {
	const token = "slide-oversized-unknown-token"
	body := strings.Repeat("prefix ", 70) + "unknown=" + token + " trailing diagnostic"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, body) //nolint:errcheck
	}))
	defer server.Close()

	err := downloadLectureSlideWithLimit(context.Background(), client.New(server.Client(), nil), &config.Config{
		BaseURL:          server.URL,
		DownloadLocation: t.TempDir(),
		Token:            token,
	}, client.Lecture{VideoID: 10, SeqNo: 1, Topic: "Lecture"}, 8)
	if err == nil {
		t.Fatal("downloadLectureSlideWithLimit() error = nil, want upstream failure")
	}
	if strings.Contains(err.Error(), "unknown=") || strings.Contains(err.Error(), token[:12]) {
		t.Fatalf("slide error exposed an oversized body prefix: %v", err)
	}
	if !strings.Contains(err.Error(), slideErrorBodyOmitted) {
		t.Fatalf("slide error = %v, want bounded omission", err)
	}
}

func TestDownloadLectureSlideOmitsTokenPrefixWithinBodyCap(t *testing.T) {
	token := strings.Repeat("long-token-", 60)
	prefix := token[:maxSlideErrorBodySize-1]
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, prefix) //nolint:errcheck
	}))
	defer server.Close()

	err := downloadLectureSlideWithLimit(context.Background(), client.New(server.Client(), nil), &config.Config{
		BaseURL:          server.URL,
		DownloadLocation: t.TempDir(),
		Token:            token,
	}, client.Lecture{VideoID: 10, SeqNo: 1, Topic: "Lecture"}, 8)
	if err == nil {
		t.Fatal("downloadLectureSlideWithLimit() error = nil, want upstream failure")
	}
	if strings.Contains(err.Error(), prefix) || !strings.Contains(err.Error(), slideErrorBodyOmitted) {
		t.Fatalf("slide error exposed token prefix: %v", err)
	}
}

func TestDownloadLectureSlideSanitizesSuccessfulBodyCopyError(t *testing.T) {
	const token = "slide-copy-secret"
	httpClient := &http.Client{Transport: slideRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(slideTokenErrorReader{token: token}),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})}
	err := downloadLectureSlideWithLimit(context.Background(), client.New(httpClient, nil), &config.Config{
		BaseURL:          "https://api.example.test",
		DownloadLocation: t.TempDir(),
		Token:            token,
	}, client.Lecture{VideoID: 10, SeqNo: 1, Topic: "Lecture"}, 8)
	if err == nil {
		t.Fatal("downloadLectureSlideWithLimit() error = nil, want body copy failure")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("slide copy error leaked token: %v", err)
	}
	if !strings.Contains(err.Error(), "REDACTED") {
		t.Fatalf("slide copy error = %v, want redaction marker", err)
	}
}

func TestDownloadLectureSlideInterruptedReadPreservesFinal(t *testing.T) {
	testDownloadLectureSlideTransportFailure(t, io.NopCloser(&failingSlideReader{}), "interrupted-read")
}

func TestDownloadLectureSlideResponseCloseFailurePreservesFinal(t *testing.T) {
	testDownloadLectureSlideTransportFailure(t, &closeErrorSlideBody{Reader: io.NopCloser(&fixedSlideReader{})}, "close-response")
}

func TestDownloadLectureSlideResponseCloseErrorSanitizesToken(t *testing.T) {
	const token = "slide-close-secret"
	downloadDir := t.TempDir()
	httpClient := &http.Client{Transport: slideRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			ContentLength: -1,
			Body: &closeErrorSlideBody{
				Reader: io.NopCloser(&fixedSlideReader{}),
				Err:    fmt.Errorf("close failed unknown=%s", token),
			},
			Header: make(http.Header),
		}, nil
	})}
	err := downloadLectureSlideWithLimit(context.Background(), client.New(httpClient, nil), &config.Config{
		BaseURL:          "https://api.placeholder.test",
		DownloadLocation: downloadDir,
		Token:            token,
	}, client.Lecture{VideoID: 10, SeqNo: 1, Topic: "Lecture"}, 8)
	if err == nil {
		t.Fatal("downloadLectureSlideWithLimit() error = nil, want response close failure")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("slide response close error leaked token: %v", err)
	}
	if !strings.Contains(err.Error(), "REDACTED") {
		t.Fatalf("slide response close error = %v, want redaction marker", err)
	}
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

type slideTokenErrorReader struct {
	token string
}

func (r slideTokenErrorReader) Read([]byte) (int, error) {
	return 0, fmt.Errorf("copy failed unknown=%s", r.token)
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
	Err    error
}

func (b *closeErrorSlideBody) Read(p []byte) (int, error) {
	return b.Reader.Read(p)
}

func (b *closeErrorSlideBody) Close() error {
	_ = b.Reader.Close() //nolint:errcheck
	if b.Err != nil {
		return b.Err
	}
	return errors.New("synthetic close failure")
}

func (r *failingSlideReader) Read(p []byte) (int, error) {
	if r.read {
		return 0, errors.New("synthetic interruption")
	}
	r.read = true
	return copy(p, "1234"), nil
}
