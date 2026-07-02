package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/config"
)

func newTestRequest(method string) *http.Request {
	return httptest.NewRequestWithContext(context.Background(), method, "/", nil)
}

func TestRequestIDMiddlewareGeneratesID(t *testing.T) {
	var seen string
	h := requestIDMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = requestIDFrom(r)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, newTestRequest(http.MethodGet))

	if seen == "" {
		t.Error("expected a generated request ID in context")
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("expected X-Request-ID response header")
	}
}

func TestRequestIDMiddlewarePreservesProvidedID(t *testing.T) {
	var seen string
	h := requestIDMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = requestIDFrom(r)
	}))
	req := newTestRequest(http.MethodGet)
	req.Header.Set("X-Request-ID", "abc-123")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if seen != "abc-123" {
		t.Errorf("requestIDFrom = %q, want abc-123", seen)
	}
}

func TestRequestIDFromMissing(t *testing.T) {
	if id := requestIDFrom(newTestRequest(http.MethodGet)); id != "" {
		t.Errorf("expected empty request ID, got %q", id)
	}
}
func TestCorsMiddleware(t *testing.T) {
	s := newAPIServer(nil)
	h := s.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, newTestRequest(http.MethodGet))
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("expected CORS allow-origin header for loopback bind")
	}

	optRec := httptest.NewRecorder()
	h.ServeHTTP(optRec, newTestRequest(http.MethodOptions))
	if optRec.Code != http.StatusOK {
		t.Errorf("OPTIONS preflight status = %d, want 200", optRec.Code)
	}
}

func TestCorsMiddlewareNonLoopbackRejectsForeignOrigin(t *testing.T) {
	s := newAPIServer(&config.Config{
		Username:         "u",
		Password:         "p",
		BaseURL:          "https://example.com",
		ListenAddr:       "0.0.0.0",
		DownloadLocation: "./downloads",
	})
	if s.loopback {
		t.Fatal("expected non-loopback server")
	}
	h := s.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// A foreign origin on an exposed server must not receive a permissive ACAO.
	rec := httptest.NewRecorder()
	req := newTestRequest(http.MethodGet)
	req.Host = "localhost:8080"
	req.Header.Set("Origin", "https://evil.example")
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no CORS allow-origin for foreign origin on exposed server, got %q", got)
	}
	// Vary: Origin must be emitted even on the non-matching branch, otherwise a
	// shared cache could reuse this response for a different origin.
	if vary := rec.Header().Get("Vary"); !strings.Contains(vary, "Origin") {
		t.Errorf("expected Vary to contain Origin on the foreign-origin branch, got %q", vary)
	}
}

func TestCorsMiddlewareNonLoopbackAllowsSameOrigin(t *testing.T) {
	s := newAPIServer(&config.Config{
		Username:         "u",
		Password:         "p",
		BaseURL:          "https://example.com",
		ListenAddr:       "0.0.0.0",
		DownloadLocation: "./downloads",
	})
	h := s.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// A matching Origin/Host on an exposed server must reflect ACAO and always
	// emit Vary: Origin (the allow decision depends on it).
	rec := httptest.NewRecorder()
	req := newTestRequest(http.MethodGet)
	req.Host = "app.local:8080"
	req.Header.Set("Origin", "http://app.local:8080")
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://app.local:8080" {
		t.Errorf("same-origin ACAO = %q, want http://app.local:8080", got)
	}
	if vary := rec.Header().Get("Vary"); !strings.Contains(vary, "Origin") {
		t.Errorf("expected Vary to contain Origin, got %q", vary)
	}
}

func TestCorsMiddlewareNonLoopbackDefaultPortNormalization(t *testing.T) {
	s := newAPIServer(&config.Config{
		Username: "u", Password: "p", BaseURL: "https://example.com",
		ListenAddr: "0.0.0.0", DownloadLocation: "./downloads",
	})
	h := s.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Origin omits the default https port; Host includes it. They must match.
	rec := httptest.NewRecorder()
	req := newTestRequest(http.MethodGet)
	req.Host = "app.local:443"
	req.Header.Set("Origin", "https://app.local")
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got == "" {
		t.Errorf("expected same-origin ACAO after default-port normalization, got %q", got)
	}
}

func TestOriginAllowed(t *testing.T) {
	loopback := newAPIServer(nil)
	exposed := newAPIServer(&config.Config{
		Username: "u", Password: "p", BaseURL: "https://example.com",
		ListenAddr: "0.0.0.0", DownloadLocation: "./downloads",
	})
	if exposed.loopback {
		t.Fatal("expected non-loopback server")
	}

	cases := []struct {
		name   string
		server *APIServer
		origin string
		host   string
		want   bool
	}{
		{"loopback accepts any origin", loopback, "https://evil.example", "localhost:8080", true},
		{"exposed accepts no-origin (non-browser)", exposed, "", "app.local:8080", true},
		{"exposed accepts same-origin", exposed, "http://app.local:8080", "app.local:8080", true},
		{"exposed rejects mismatched origin", exposed, "https://evil.example", "app.local:8080", false},
		{"exposed rejects default-port mismatch", exposed, "http://app.local:8080", "app.local:80", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := newTestRequest(http.MethodGet)
			req.Host = tc.host
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if got := tc.server.originAllowed(req); got != tc.want {
				t.Errorf("originAllowed() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNonLoopbackBindError(t *testing.T) {
	if err := nonLoopbackBindError("0.0.0.0:8080", true); err != nil {
		t.Errorf("allowRemote=true should permit the bind, got error: %v", err)
	}
	err := nonLoopbackBindError("0.0.0.0:8080", false)
	if err == nil {
		t.Fatal("allowRemote=false should refuse the bind")
	}
	if !strings.Contains(err.Error(), "refusing to bind non-loopback address 0.0.0.0:8080") {
		t.Errorf("unexpected refusal message: %v", err)
	}
	if !strings.Contains(err.Error(), "IMPARTUS_ALLOW_REMOTE_ACCESS") {
		t.Errorf("refusal should mention the opt-in env var, got: %v", err)
	}
}
