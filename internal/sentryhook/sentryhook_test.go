package sentryhook

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConfig_Init(t *testing.T) {
	cfg := Config{
		DSN:         "https://example@sentry.io/123",
		Environment: "production",
		Release:     "impartus-cli@v1.0.0",
		Debug:       true,
	}

	if cfg.DSN != "https://example@sentry.io/123" {
		t.Errorf("expected DSN to be set, got %q", cfg.DSN)
	}
	if cfg.Environment != "production" {
		t.Errorf("expected Environment to be production, got %q", cfg.Environment)
	}
	if cfg.Release != "impartus-cli@v1.0.0" {
		t.Errorf("expected Release to be set, got %q", cfg.Release)
	}
	if !cfg.Debug {
		t.Error("expected Debug to be true")
	}
}

func TestConfig_Default(t *testing.T) {
	cfg := Config{}

	if cfg.DSN != "" {
		t.Errorf("expected empty DSN, got %q", cfg.DSN)
	}
	if cfg.Environment != "" {
		t.Errorf("expected empty Environment, got %q", cfg.Environment)
	}
	if cfg.Release != "" {
		t.Errorf("expected empty Release, got %q", cfg.Release)
	}
	if cfg.Debug {
		t.Error("expected Debug to be false")
	}
}

func TestCaptureError_NotEnabled(t *testing.T) {
	// Without SENTRY_DSN set, Sentry is not initialized
	result := CaptureError(errors.New("test error"))
	if result != nil {
		t.Error("expected nil when Sentry is not enabled")
	}
}

func TestCaptureMessage_NotEnabled(t *testing.T) {
	result := CaptureMessage("test message")
	if result != nil {
		t.Error("expected nil when Sentry is not enabled")
	}
}

func TestCaptureErrorWithContext_NotEnabled(t *testing.T) {
	result := CaptureErrorWithContext(
		errors.New("test error"),
		map[string]string{"key": "value"},
		map[string]any{"ctx": "data"},
	)
	if result != nil {
		t.Error("expected nil when Sentry is not enabled")
	}
}

func TestIsEnabled_NotInitialized(t *testing.T) {
	// Without SENTRY_DSN, Sentry should not be enabled
	if IsEnabled() {
		t.Error("expected Sentry to be disabled without DSN")
	}
}

func TestInit_NoDSN(t *testing.T) {
	// Init without DSN should succeed without initializing Sentry
	err := Init()
	if err != nil {
		t.Fatalf("Init without DSN should not error: %v", err)
	}
}

func TestWithRecovery(t *testing.T) {
	called := false
	WithRecovery(func() {
		called = true
	})
	if !called {
		t.Error("expected function to be called")
	}
}

func TestWithRecovery_Panic(t *testing.T) {
	// Should not panic even when inner function panics
	WithRecovery(func() {
		panic("test panic")
	})
	// If we get here, recovery worked
}

func TestWithRecovery_PanicError(t *testing.T) {
	WithRecovery(func() {
		panic(errors.New("error panic"))
	})
}

func TestWithRecovery_PanicInt(t *testing.T) {
	WithRecovery(func() {
		panic(42)
	})
}

func TestMiddleware_NoPanic(t *testing.T) {
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestMiddleware_WithHeaders(t *testing.T) {
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/test?key=val", nil)
	req.Header.Set("X-Request-ID", "req-123")
	req.Header.Set("X-User-ID", "user-456")
	req.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestSetUser_NoPanic(t *testing.T) {
	// Should not panic even when Sentry is not initialized
	SetUser("user-1", "test@example.com")
}

func TestSetRequestID_NoPanic(t *testing.T) {
	SetRequestID("req-123")
}

func TestSetTag_NoPanic(t *testing.T) {
	SetTag("key", "value")
}

func TestSetContext_NoPanic(t *testing.T) {
	SetContext("test", map[string]any{"key": "value"})
}

func TestSanitizeHeaders(t *testing.T) {
	tests := []struct {
		name           string
		inputHeaders   http.Header
		expectRedacted []string
		expectPresent  map[string]string
	}{
		{
			name: "authorization header redacted",
			inputHeaders: http.Header{
				"Authorization": []string{"Bearer testval"},
				"Content-Type":  []string{"application/json"},
				"X-Request-ID":  []string{"123-456"},
			},
			expectRedacted: []string{"Authorization"},
			expectPresent: map[string]string{
				"Content-Type": "application/json",
				"X-Request-ID": "123-456",
			},
		},
		{
			name: "cookie header redacted",
			inputHeaders: http.Header{
				"Cookie":     []string{"session=testval"},
				"User-Agent": []string{"Mozilla/5.0"},
			},
			expectRedacted: []string{"Cookie"},
			expectPresent: map[string]string{
				"User-Agent": "Mozilla/5.0",
			},
		},
		{
			name: "x-api-key header redacted",
			inputHeaders: http.Header{
				"X-Api-Key": []string{"sk-test-xyz"},
				"Accept":    []string{"application/json"},
			},
			expectRedacted: []string{"X-Api-Key"},
			expectPresent: map[string]string{
				"Accept": "application/json",
			},
		},
		{
			name: "set-cookie header redacted",
			inputHeaders: http.Header{
				"Set-Cookie":    []string{"session=testval; HttpOnly"},
				"Cache-Control": []string{"no-cache"},
			},
			expectRedacted: []string{"Set-Cookie"},
			expectPresent: map[string]string{
				"Cache-Control": "no-cache",
			},
		},
		{
			name: "case insensitive sensitive headers",
			inputHeaders: http.Header{
				"AUTHORIZATION": []string{"Bearer testval"},
				"content-type":  []string{"text/html"},
				"X-Request-Id":  []string{"abc"},
			},
			expectRedacted: []string{"AUTHORIZATION"},
			expectPresent: map[string]string{
				"content-type": "text/html",
				"X-Request-Id": "abc",
			},
		},
		{
			name: "empty headers",
			inputHeaders: http.Header{
				"X-Empty": []string{},
			},
			expectRedacted: []string{},
			expectPresent:  map[string]string{},
		},
		{
			name:           "nil headers",
			inputHeaders:   nil,
			expectRedacted: []string{},
			expectPresent:  map[string]string{},
		},
		{
			name: "multiple values only first taken",
			inputHeaders: http.Header{
				"Accept": []string{"application/json", "text/plain"},
			},
			expectRedacted: []string{},
			expectPresent: map[string]string{
				"Accept": "application/json",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeHeaders(tt.inputHeaders)

			// Check redacted headers
			for _, header := range tt.expectRedacted {
				if val, ok := result[header]; !ok {
					t.Errorf("expected header %q to be present in result", header)
				} else if val != "***REDACTED***" {
					t.Errorf("expected header %q to be redacted, got %q", header, val)
				}
			}

			// Check present headers
			for header, expectedVal := range tt.expectPresent {
				if val, ok := result[header]; !ok {
					t.Errorf("expected header %q to be present in result", header)
				} else if val != expectedVal {
					t.Errorf("expected header %q to be %q, got %q", header, expectedVal, val)
				}
			}
		})
	}
}

func TestSanitizeHeaders_AllSensitiveHeaders(t *testing.T) {
	headers := http.Header{
		"Authorization": []string{"redact1"},
		"Cookie":        []string{"redact2"},
		"Set-Cookie":    []string{"redact3"},
		"X-Api-Key":     []string{"redact4"},
		"Content-Type":  []string{"application/json"},
		"Accept":        []string{"*/*"},
	}

	result := sanitizeHeaders(headers)

	// Verify sensitive headers are redacted
	if result["Authorization"] != "***REDACTED***" {
		t.Errorf("expected Authorization to be redacted, got %q", result["Authorization"])
	}
	if result["Cookie"] != "***REDACTED***" {
		t.Errorf("expected Cookie to be redacted, got %q", result["Cookie"])
	}
	if result["Set-Cookie"] != "***REDACTED***" {
		t.Errorf("expected Set-Cookie to be redacted, got %q", result["Set-Cookie"])
	}
	if result["X-Api-Key"] != "***REDACTED***" {
		t.Errorf("expected X-Api-Key to be redacted, got %q", result["X-Api-Key"])
	}

	// Verify non-sensitive headers are preserved
	if result["Content-Type"] != "application/json" {
		t.Errorf("expected Content-Type to be preserved, got %q", result["Content-Type"])
	}
	if result["Accept"] != "*/*" {
		t.Errorf("expected Accept to be preserved, got %q", result["Accept"])
	}
}
