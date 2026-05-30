package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
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
	h := corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, newTestRequest(http.MethodGet))
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("expected CORS allow-origin header")
	}

	optRec := httptest.NewRecorder()
	h.ServeHTTP(optRec, newTestRequest(http.MethodOptions))
	if optRec.Code != http.StatusOK {
		t.Errorf("OPTIONS preflight status = %d, want 200", optRec.Code)
	}
}
