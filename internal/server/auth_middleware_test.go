package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAuthMiddlewareMissingAuthorizationHeader(t *testing.T) {
	s := newAPIServer(validServerConfig())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/courses", nil)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "MISSING_TOKEN") {
		t.Fatalf("expected MISSING_TOKEN error, got body: %s", rec.Body.String())
	}
}

func TestAuthMiddlewareInvalidTokenFormat(t *testing.T) {
	s := newAPIServer(validServerConfig())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/courses", nil)
	req.Header.Set("Authorization", "InvalidFormat token123")
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "INVALID_TOKEN_FORMAT") {
		t.Fatalf("expected INVALID_TOKEN_FORMAT error, got body: %s", rec.Body.String())
	}
}

func TestAuthMiddlewareInvalidToken(t *testing.T) {
	s := newAPIServer(validServerConfig())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/courses", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "INVALID_TOKEN") {
		t.Fatalf("expected INVALID_TOKEN error, got body: %s", rec.Body.String())
	}
}

func TestAuthMiddlewareValidToken(t *testing.T) {
	s := newAPIServer(validServerConfig())

	// Create a valid token
	token, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}
	s.tokenStore.Store(token, TokenInfo{
		Username:  "user",
		Expiry:    time.Now().Add(1 * time.Hour),
		CreatedAt: time.Now(),
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/courses", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	// The courses handler will fail since we don't have a real client,
	// but auth should pass
	s.router.ServeHTTP(rec, req)

	// We expect an error from the courses handler (login failed), not auth
	// If auth failed, we'd get 401
	if rec.Code == http.StatusUnauthorized {
		t.Fatal("auth should have passed with valid token")
	}
}

func TestAuthMiddlewareOptionsRequest(t *testing.T) {
	s := newAPIServer(validServerConfig())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodOptions, "/api/v1/courses", nil)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	// OPTIONS should return 200 without auth check
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for OPTIONS, got %d", rec.Code)
	}
}

// ============================================================================
// Handler Tests
// ============================================================================
