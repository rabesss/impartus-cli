package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/config"
)

func TestHealthEndpointReturnsStructuredStatus(t *testing.T) {
	s := newAPIServer(validServerConfig())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Check success field
	if resp["success"] != true {
		t.Errorf("expected success=true, got %v", resp["success"])
	}

	// Check meta field
	meta, metaOK := resp["meta"].(map[string]any)
	if !metaOK {
		t.Fatal("expected meta field in response")
	}
	if meta["command"] != healthCommand {
		t.Errorf("expected command=health, got %v", meta["command"])
	}
	if meta["mode"] != "api" {
		t.Errorf("expected mode=api, got %v", meta["mode"])
	}

	// Check data field
	data, dataOK := resp["data"].(map[string]any)
	if !dataOK {
		t.Fatal("expected data field in response")
	}

	// Check top-level status field
	if _, statusOK := data["status"]; !statusOK {
		t.Fatal("expected status field in data")
	}

	// Check config field
	config, configOK := data["config"].(map[string]any)
	if !configOK {
		t.Fatal("expected config field in data")
	}
	if _, csOK := config["status"]; !csOK {
		t.Fatal("expected config.status field")
	}
	if _, cmOK := config["missing"]; cmOK {
		t.Fatal("config must not expose field-level 'missing' recon data")
	}

	// Check upstream field
	upstream, upstreamOK := data["upstream"].(map[string]any)
	if !upstreamOK {
		t.Fatal("expected upstream field in data")
	}
	if _, usOK := upstream["status"]; !usOK {
		t.Fatal("expected upstream.status field")
	}

	// Check ffmpeg field
	ffmpeg, ffmpegOK := data["ffmpeg"].(map[string]any)
	if !ffmpegOK {
		t.Fatal("expected ffmpeg field in data")
	}
	if _, fsOK := ffmpeg["status"]; !fsOK {
		t.Fatal("expected ffmpeg.status field")
	}
}

func TestHealthConfigStatusOkWithValidConfig(t *testing.T) {
	s := newAPIServer(validServerConfig())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	data := assertMapField(t, resp, "data")
	config := assertMapField(t, data, "config")

	if config["status"] != "ok" {
		t.Errorf("expected config.status=ok, got %v", config["status"])
	}
	for _, field := range []string{"username", "password", "baseUrl", "missing", "configured"} {
		if _, leaked := config[field]; leaked {
			t.Errorf("config must not expose %q recon data, got %v", field, config[field])
		}
	}
}

func TestHealthConfigStatusMisconfiguredWithMissingFields(t *testing.T) {
	s := newAPIServer(&config.Config{
		Username:         "", // missing
		Password:         "pass",
		BaseURL:          "https://example.com",
		DownloadLocation: "./downloads",
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	data := assertMapField(t, resp, "data")
	config := assertMapField(t, data, "config")

	if config["status"] != "misconfigured" {
		t.Errorf("expected config.status=misconfigured, got %v", config["status"])
	}
	for _, field := range []string{"username", "password", "baseUrl"} {
		if _, leaked := config[field]; leaked {
			t.Errorf("config must not expose %q recon data, got %v", field, config[field])
		}
	}
}

func TestHealthConfigStatusMisconfiguredWithAllMissingFields(t *testing.T) {
	s := newAPIServer(&config.Config{
		Username:         "",
		Password:         "",
		BaseURL:          "",
		DownloadLocation: "./downloads",
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	data := assertMapField(t, resp, "data")
	config := assertMapField(t, data, "config")

	if config["status"] != "misconfigured" {
		t.Errorf("expected config.status=misconfigured, got %v", config["status"])
	}
}

func TestHealthConfigStatusMisconfiguredWithNilConfig(t *testing.T) {
	s := newAPIServer(nil)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	data := assertMapField(t, resp, "data")
	config := assertMapField(t, data, "config")

	if config["status"] != "misconfigured" {
		t.Errorf("expected config.status=misconfigured, got %v", config["status"])
	}
}

func TestHealthUpstreamStatusWithValidBaseUrl(t *testing.T) {
	s := newAPIServer(&config.Config{
		Username:         "user",
		Password:         "pass",
		BaseURL:          "https://example.com",
		DownloadLocation: "./downloads",
	})
	// Seed the upstream cache so the HTTP probe authenticates against a stub
	// instead of hitting the real network. Deterministic: stub returns 200.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/profile" || r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	s.cfg.BaseURL = srv.URL
	s.upstreamCacheMu.Lock()
	s.upstreamCache = &upstreamCacheEntry{token: "test-token", expiresAt: time.Now().Add(time.Hour)}
	s.upstreamCacheMu.Unlock()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	data := assertMapField(t, resp, "data")
	upstream := assertMapField(t, data, "upstream")
	if upstream["status"] != "reachable" {
		t.Errorf("expected upstream.status=reachable (stub returned 200), got %v", upstream["status"])
	}
}

// TestCheckUpstreamStatus_HTTPRejectionBeatsTCPReachable is the regression guard
// for the TCP-fallback bug: when a cached-token HTTP probe gets an explicit
// non-2xx from an upstream whose TCP port is open, the status must be
// "unreachable" — the TCP probe must NOT flip it back to "reachable".
func TestCheckUpstreamStatus_HTTPRejectionBeatsTCPReachable(t *testing.T) {
	// TCP port is open (server is listening) but it returns 401 for the probe.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	s := newAPIServer(&config.Config{
		Username:         "user",
		Password:         "pass",
		BaseURL:          srv.URL,
		DownloadLocation: "./downloads",
	})
	s.upstreamCacheMu.Lock()
	s.upstreamCache = &upstreamCacheEntry{token: "test-token", expiresAt: time.Now().Add(time.Hour)}
	s.upstreamCacheMu.Unlock()

	if got := s.checkUpstreamStatus(context.Background()).Status; got != "unreachable" {
		t.Errorf("expected unreachable (HTTP 401 must beat an open TCP port), got %q", got)
	}
}

func TestHealthUpstreamStatusWithUnreachableBaseUrl(t *testing.T) {
	s := newAPIServer(&config.Config{
		Username:         "user",
		Password:         "pass",
		BaseURL:          "https://127.0.0.1:1", // closed port -> connection refused
		DownloadLocation: "./downloads",
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	data := assertMapField(t, resp, "data")
	upstream := assertMapField(t, data, "upstream")

	if upstream["status"] != "unreachable" {
		t.Errorf("expected upstream.status 'unreachable', got %v", upstream["status"])
	}
}

func TestHealthFFmpegStatus(t *testing.T) {
	s := newAPIServer(validServerConfig())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	data := assertMapField(t, resp, "data")
	ffmpeg := assertMapField(t, data, "ffmpeg")

	// FFmpeg should be either "available" or "not_found" depending on system
	if ffmpeg["status"] != "available" && ffmpeg["status"] != "not_found" {
		t.Errorf("expected ffmpeg.status to be 'available' or 'not_found', got %v", ffmpeg["status"])
	}
}

func TestHealthOverallStatusOk(t *testing.T) {
	s := newAPIServer(validServerConfig())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	data := assertMapField(t, resp, "data")

	// Overall status should be "ok" if all components are healthy
	// It might be "degraded" if ffmpeg is not found or upstream is unreachable
	// This test just verifies the field exists and has a valid value
	if data["status"] != "ok" && data["status"] != "degraded" {
		t.Errorf("expected overall status to be 'ok' or 'degraded', got %v", data["status"])
	}
}

func TestHealthOverallStatusDegradedWithMisconfiguredConfig(t *testing.T) {
	s := newAPIServer(&config.Config{
		Username:         "", // missing - causes misconfiguration
		Password:         "pass",
		BaseURL:          "https://example.com",
		DownloadLocation: "./downloads",
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	data := assertMapField(t, resp, "data")
	config := assertMapField(t, data, "config")

	// When config is misconfigured, overall status should be "degraded"
	if config["status"] != "misconfigured" {
		t.Errorf("expected config.status=misconfigured, got %v", config["status"])
	}
	if data["status"] != "degraded" {
		t.Errorf("expected overall status=degraded when config is misconfigured, got %v", data["status"])
	}
}

func TestHealthNoAuthRequired(t *testing.T) {
	s := newAPIServer(validServerConfig())

	// Request without Authorization header
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	// Should return 200, not 401
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200 for health endpoint without auth, got %d", rec.Code)
	}

	// Should not contain auth error
	body := rec.Body.String()
	if strings.Contains(body, "MISSING_TOKEN") || strings.Contains(body, "INVALID_TOKEN") {
		t.Error("health endpoint should not require authentication")
	}
}

// ============================================================================
// Idempotency Key Tests
// ============================================================================

// setupAuth creates an auth token for the given server and returns it.
