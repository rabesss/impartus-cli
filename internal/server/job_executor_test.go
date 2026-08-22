package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/downloader"
)

func TestSanitizeUpstreamErr(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		want          string
		secretMarkers []string
	}{
		{"nil", nil, "", nil},
		{"canceled", context.Canceled, "job was canceled or timed out", nil},
		{"no media outputs", downloader.ErrNoMediaOutputs, "no media outputs available for selected lectures", nil},
		{"deadline", context.DeadlineExceeded, "job was canceled or timed out", nil},
		{"dns", &net.DNSError{Err: "no such host"}, "upstream connection failed", nil},
		{"http status", fmt.Errorf("request failed with status 503"), "upstream API returned HTTP 503", nil},
		{"untyped http 401 remains status", fmt.Errorf("request failed with status 401"), "upstream API returned HTTP 401", nil},
		{
			"typed authentication",
			&client.AuthenticationError{Operation: "login", StatusCode: 401},
			"upstream authentication failed",
			nil,
		},
		{
			"wrapped typed authentication",
			fmt.Errorf("request failed with response-marker-secret: %w", &client.AuthenticationError{Operation: "subjects", StatusCode: 401}),
			"upstream authentication failed",
			[]string{"response-marker-secret"},
		},
		{
			"joined typed authentication",
			errors.Join(errors.New("joined-body-secret"), &client.AuthenticationError{Operation: "playlist", StatusCode: 401}),
			"upstream authentication failed",
			[]string{"joined-body-secret"},
		},
		{"auth scrubs token value", fmt.Errorf("invalid token abc123secret"), "upstream authentication failed", []string{"abc123secret"}},
		{"generic", errors.New("something broke"), "upstream API error", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeUpstreamErr(tt.err)
			if got != tt.want {
				t.Errorf("sanitizeUpstreamErr(%v) = %q, want %q", tt.err, got, tt.want)
			}
			for _, marker := range tt.secretMarkers {
				if strings.Contains(got, marker) {
					t.Errorf("sanitized message leaked sensitive data %q: %q", marker, got)
				}
			}
		})
	}
}

func TestSanitizeUpstreamErrPreservesAggregatedChunkAuthenticationFailure(t *testing.T) {
	const (
		bodyMarker = "aggregated-chunk-response-secret"
		urlMarker  = "aggregated-chunk-url-secret"
	)
	keyResponse := append([]byte{0, 0}, []byte("fedcba9876543210")...)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/key" {
			if _, err := w.Write(keyResponse); err != nil {
				t.Errorf("write key response: %v", err)
			}
			return
		}
		http.Error(w, bodyMarker, http.StatusUnauthorized)
	}))
	defer upstream.Close()

	cfg := &config.Config{
		Token:                     "request-token",
		TempDirLocation:           t.TempDir(),
		Views:                     "left",
		EnablePipeline:            true,
		DownloadWorkersPerLecture: 1,
		DecryptWorkersPerLecture:  1,
		RateLimit:                 100,
		APIRateLimit:              100,
	}
	d := downloader.NewWithDiagnosticWriter(cfg, client.New(upstream.Client(), nil), io.Discard)
	_, err := d.DownloadPlaylist(t.Context(), client.ParsedPlaylist{
		ID:            42,
		SeqNo:         1,
		KeyURL:        upstream.URL + "/key",
		FirstViewURLs: []string{upstream.URL + "/chunk?access_token=" + urlMarker},
	}, nil, nil)
	if !errors.Is(err, client.ErrAuthentication) {
		t.Fatalf("DownloadPlaylist error = %v, want ErrAuthentication", err)
	}
	if got := sanitizeUpstreamErr(err); got != "upstream authentication failed" {
		t.Fatalf("sanitizeUpstreamErr(aggregated chunk 401) = %q", got)
	}
	if strings.Contains(err.Error(), bodyMarker) || strings.Contains(err.Error(), urlMarker) {
		t.Fatalf("aggregated chunk authentication error leaked upstream marker: %v", err)
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestSanitizeUpstreamErrNetworkTimeout(t *testing.T) {
	err := &net.OpError{Op: "dial", Err: timeoutErr{}}
	if got := sanitizeUpstreamErr(err); got != "upstream connection failed" {
		t.Errorf("sanitizeUpstreamErr(timeout) = %q, want %q", got, "upstream connection failed")
	}
}

func TestContainsAny(t *testing.T) {
	if !containsAny("Has TOKEN here", []string{"token"}) {
		t.Error("expected case-insensitive substring match")
	}
	if containsAny("clean message", []string{"token", "auth"}) {
		t.Error("expected no match for clean message")
	}
}
