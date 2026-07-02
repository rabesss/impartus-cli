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

func TestHealthHandler(t *testing.T) {
	s := newAPIServer(validServerConfig())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	contentType := rec.Header().Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Fatalf("expected JSON content type, got %s", contentType)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if success, ok := resp["success"].(bool); !ok || !success {
		t.Error("expected success to be true")
	}
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatal("expected data object in response")
	}
	// Overall status may be "ok" or "degraded" depending on upstream
	// reachability and FFmpeg availability (environment-dependent).
	if data["status"] != "ok" && data["status"] != "degraded" {
		t.Errorf("expected status 'ok' or 'degraded', got %v", data["status"])
	}
	meta, ok := resp["meta"].(map[string]any)
	if !ok {
		t.Fatal("expected meta object in response")
	}
	if meta["command"] != healthCommand {
		t.Errorf("expected meta.command 'health', got %v", meta["command"])
	}
	if meta["mode"] != "api" {
		t.Errorf("expected meta.mode 'api', got %v", meta["mode"])
	}
}

func TestLoginHandler(t *testing.T) {
	s := newAPIServer(validServerConfig())

	// Valid login
	reqBody := `{"username":"user","password":"pass"}`
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/auth/login", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if success, ok := resp["success"].(bool); !ok || !success {
		t.Error("expected success=true")
	}
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatal("expected data in response")
	}
	if data["token"] == nil || data["token"] == "" {
		t.Error("expected token in response")
	}
	if data["expires"] == nil {
		t.Error("expected expires in response")
	}
}

func TestLoginHandlerInvalidCredentials(t *testing.T) {
	s := newAPIServer(validServerConfig())

	reqBody := `{"username":"wrong","password":"wrong"}`
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/auth/login", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if success, ok := resp["success"].(bool); !ok || success {
		t.Error("expected success=false")
	}
}

func TestLoginHandlerInvalidJSON(t *testing.T) {
	s := newAPIServer(validServerConfig())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/auth/login", strings.NewReader("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestLoginHandlerOptionsRequest(t *testing.T) {
	s := newAPIServer(validServerConfig())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodOptions, "/api/v1/auth/login", nil)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for OPTIONS, got %d", rec.Code)
	}
}

func TestListJobsHandler(t *testing.T) {
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

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/jobs", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}

	contentType := rec.Header().Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Fatalf("expected JSON content type, got %s", contentType)
	}
}

func TestGetJobHandlerJobNotFound(t *testing.T) {
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

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/jobs/nonexistent", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestGetJobHandlerSuccess(t *testing.T) {
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

	// Create a job directly
	cfg := &config.Config{DownloadLocation: "./downloads"}
	job := s.jobStore.CreateJob(1, 1, 1, 1, cfg)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/jobs/"+job.ID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	// getJobHandler returns the job wrapped in envelope
	if success, ok := resp["success"].(bool); !ok || !success {
		t.Error("expected success to be true")
	}
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatal("expected data object in response")
	}
	if data["id"] != job.ID {
		t.Errorf("expected job id %s, got %v", job.ID, data["id"])
	}
	meta, ok := resp["meta"].(map[string]any)
	if !ok {
		t.Fatal("expected meta object in response")
	}
	if meta["command"] != "getJob" {
		t.Errorf("expected meta.command 'getJob', got %v", meta["command"])
	}
}

func TestDeleteJobHandlerCancel(t *testing.T) {
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

	// Create a job directly
	cfg := &config.Config{DownloadLocation: "./downloads"}
	job := s.jobStore.CreateJob(1, 1, 1, 1, cfg)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "/api/v1/jobs/"+job.ID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}

	// Verify job is canceled
	canceled, _ := s.jobStore.GetJob(job.ID)
	if canceled.Status != StatusCanceled {
		t.Errorf("expected status 'canceled', got %s", canceled.Status)
	}
}

func TestDeleteJobHandlerNotFound(t *testing.T) {
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

	req := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "/api/v1/jobs/nonexistent", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestDeleteJobHandlerTerminalState(t *testing.T) {
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

	// Create and complete a job
	cfg := &config.Config{DownloadLocation: "./downloads"}
	job := s.jobStore.CreateJob(1, 1, 1, 1, cfg)
	s.jobStore.UpdateJob(job.ID, "completed", 100.0, "")

	req := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "/api/v1/jobs/"+job.ID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "JOB_CANNOT_CANCEL") {
		t.Fatalf("expected JOB_CANNOT_CANCEL error, got body: %s", rec.Body.String())
	}
}

// ============================================================================
// Utility Function Tests
// ============================================================================
