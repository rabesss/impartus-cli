package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/downloader"
)

// ============================================================================
// ensureScheme Tests
// ============================================================================

func TestEnsureScheme(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"https already", "https://example.com", "https://example.com"},
		{"http already", "http://example.com", "http://example.com"},
		{"no scheme", "example.com", "https://example.com"},
		{"no scheme with path", "example.com/api/v1", "https://example.com/api/v1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newAPIServer(validServerConfig())
			got := s.ensureScheme(tt.raw)
			if got != tt.want {
				t.Errorf("ensureScheme(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

// ============================================================================
// probeUpstreamHTTP Tests
// ============================================================================

func TestProbeUpstreamHTTP_NoCache(t *testing.T) {
	s := newAPIServer(validServerConfig())
	// No upstream cache set: nothing to authenticate with, so not probed.
	reachable, probed := s.probeUpstreamHTTP()
	if probed {
		t.Error("expected not probed when no upstream cache")
	}
	if reachable {
		t.Error("expected unreachable when no upstream cache")
	}
}

func TestProbeUpstreamHTTP_EmptyToken(t *testing.T) {
	s := newAPIServer(validServerConfig())
	s.upstreamCacheMu.Lock()
	s.upstreamCache = &upstreamCacheEntry{token: ""}
	s.upstreamCacheMu.Unlock()

	reachable, probed := s.probeUpstreamHTTP()
	if probed {
		t.Error("expected not probed when token is empty")
	}
	if reachable {
		t.Error("expected unreachable when token is empty")
	}
}

func TestProbeUpstreamHTTP_InvalidURL(t *testing.T) {
	s := newAPIServer(&config.Config{
		Username:         "user",
		Password:         "pass",
		BaseURL:          "://invalid",
		DownloadLocation: "./downloads",
	})
	s.upstreamCacheMu.Lock()
	s.upstreamCache = &upstreamCacheEntry{token: "some-token"}
	s.upstreamCacheMu.Unlock()

	// A token exists so the probe is attempted, but the invalid URL means it
	// cannot reach: probed=true, reachable=false (no TCP fallback).
	reachable, probed := s.probeUpstreamHTTP()
	if !probed {
		t.Error("expected probed despite invalid URL")
	}
	if reachable {
		t.Error("expected unreachable for invalid URL")
	}
}

func TestProbeUpstreamHTTP_SuccessfulProbe(t *testing.T) {
	// Create a test server that responds to /user/profile
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user/profile" {
			w.WriteHeader(http.StatusOK)
			if _, err := w.Write([]byte(`{"name":"test"}`)); err != nil {
				t.Fatalf("Write() failed: %v", err)
			}
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	baseURL := ts.URL
	s := newAPIServer(&config.Config{
		Username:         "user",
		Password:         "pass",
		BaseURL:          baseURL,
		DownloadLocation: "./downloads",
	})
	s.upstreamCacheMu.Lock()
	s.upstreamCache = &upstreamCacheEntry{token: "valid-token"}
	s.upstreamCacheMu.Unlock()

	reachable, probed := s.probeUpstreamHTTP()
	if !probed {
		t.Error("expected probe to be issued against reachable upstream")
	}
	if !reachable {
		t.Error("expected reachable for 2xx upstream")
	}
}

func TestProbeUpstreamHTTP_ServerReturnsError(t *testing.T) {
	// Create a server that immediately closes connections
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return 500 to simulate error
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	s := newAPIServer(&config.Config{
		Username:         "user",
		Password:         "pass",
		BaseURL:          ts.URL,
		DownloadLocation: "./downloads",
	})
	s.upstreamCacheMu.Lock()
	s.upstreamCache = &upstreamCacheEntry{token: "valid-token"}
	s.upstreamCacheMu.Unlock()

	// A 5xx response indicates an unhealthy upstream; the probe must report it
	// as not reachable (probed=true) rather than letting a TCP probe flip it.
	reachable, probed := s.probeUpstreamHTTP()
	if !probed {
		t.Error("expected probe to be issued against 500 upstream")
	}
	if reachable {
		t.Error("expected unreachable for upstream returning HTTP 500")
	}
}

// ============================================================================
// probeUpstreamTCP Tests
// ============================================================================

func TestProbeUpstreamTCP_InvalidURL(t *testing.T) {
	s := newAPIServer(&config.Config{
		Username:         "user",
		Password:         "pass",
		BaseURL:          "://invalid",
		DownloadLocation: "./downloads",
	})

	result := s.probeUpstreamTCP()
	if result {
		t.Error("expected false for invalid URL")
	}
}

func TestProbeUpstreamTCP_UnreachableHost(t *testing.T) {
	s := newAPIServer(&config.Config{
		Username:         "user",
		Password:         "pass",
		BaseURL:          "https://127.0.0.1:1", // closed port -> connection refused
		DownloadLocation: "./downloads",
	})

	if s.probeUpstreamTCP() {
		t.Error("expected probeUpstreamTCP to return false for a refused connection")
	}
}

func TestProbeUpstreamTCP_ReachableServer(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()

	s := newAPIServer(&config.Config{
		Username:         "user",
		Password:         "pass",
		BaseURL:          ts.URL,
		DownloadLocation: "./downloads",
	})

	result := s.probeUpstreamTCP()
	if !result {
		t.Error("expected true for reachable local test server")
	}
}

// ============================================================================
// checkUpstreamStatus Tests
// ============================================================================

func TestCheckUpstreamStatus_NilConfig(t *testing.T) {
	s := newAPIServer(nil)
	result := s.checkUpstreamStatus()
	if result.Status != "not_configured" {
		t.Errorf("expected not_configured, got %s", result.Status)
	}
}

func TestCheckUpstreamStatus_EmptyBaseURL(t *testing.T) {
	s := newAPIServer(&config.Config{
		Username:         "user",
		Password:         "pass",
		BaseURL:          "",
		DownloadLocation: "./downloads",
	})
	result := s.checkUpstreamStatus()
	if result.Status != "not_configured" {
		t.Errorf("expected not_configured, got %s", result.Status)
	}
}

func TestCheckUpstreamStatus_Reachable(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()

	s := newAPIServer(&config.Config{
		Username:         "user",
		Password:         "pass",
		BaseURL:          ts.URL,
		DownloadLocation: "./downloads",
	})

	result := s.checkUpstreamStatus()
	if result.Status != "reachable" {
		t.Errorf("expected reachable, got %s", result.Status)
	}
}

// ============================================================================
// checkFFmpegStatus Tests
// ============================================================================

func TestCheckFFmpegStatus(t *testing.T) {
	s := newAPIServer(validServerConfig())
	result := s.checkFFmpegStatus()
	// Can be either "available" or "not_found" depending on system
	if result.Status != "available" && result.Status != "not_found" {
		t.Errorf("expected available or not_found, got %s", result.Status)
	}
}

// ============================================================================
// coursesHandler Tests (with upstream mock)
// ============================================================================

func TestCoursesHandler_WithMockedUpstream(t *testing.T) {
	// Create a test upstream server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`[{"sessionId":1,"subjectName":"Math","professorName":"Prof X"}]`)); err != nil {
			t.Fatalf("Write() failed: %v", err)
		}
	}))
	defer ts.Close()

	counter := &mockLoginCallCounter{}
	s := NewAPIServerWithLogin("8080", &config.Config{
		Username:         "user",
		Password:         "pass",
		BaseURL:          ts.URL,
		DownloadLocation: "./downloads",
	}, mockUpstreamLogin(counter))

	token := setupAuth(t, s)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/courses", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if success, ok := resp["success"].(bool); !ok || !success {
		t.Error("expected success=true")
	}
}

// ============================================================================
// lecturesHandler Tests (with upstream mock)
// ============================================================================

func TestLecturesHandler_WithMockedUpstream(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`[{"ttid":1,"topic":"Intro","seqNo":1}]`)); err != nil {
			t.Fatalf("Write() failed: %v", err)
		}
	}))
	defer ts.Close()

	counter := &mockLoginCallCounter{}
	s := NewAPIServerWithLogin("8080", &config.Config{
		Username:         "user",
		Password:         "pass",
		BaseURL:          ts.URL,
		DownloadLocation: "./downloads",
	}, mockUpstreamLogin(counter))

	token := setupAuth(t, s)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/lectures?subject_id=1&session_id=1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
}

func TestLecturesHandler_CamelCaseParams(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`[{"ttid":1,"topic":"Intro","seqNo":1}]`)); err != nil {
			t.Fatalf("Write() failed: %v", err)
		}
	}))
	defer ts.Close()

	counter := &mockLoginCallCounter{}
	s := NewAPIServerWithLogin("8080", &config.Config{
		Username:         "user",
		Password:         "pass",
		BaseURL:          ts.URL,
		DownloadLocation: "./downloads",
	}, mockUpstreamLogin(counter))

	token := setupAuth(t, s)

	// Test camelCase query params
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/lectures?subjectId=1&sessionId=1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for camelCase params, got %d, body: %s", rec.Code, rec.Body.String())
	}
}

// ============================================================================
// createJobHandler Tests (with upstream mock)
// ============================================================================

func TestCreateJobHandler_SuccessWithUpstream(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`[{"ttid":1,"topic":"Intro","seqNo":1}]`)); err != nil {
			t.Fatalf("Write() failed: %v", err)
		}
	}))
	defer ts.Close()

	// Use os.MkdirTemp instead of t.TempDir() to avoid a race condition:
	// createJobHandler starts executeJob in a background goroutine that may
	// outlive the test. t.TempDir() auto-cleans on test exit, causing the
	// goroutine to encounter a missing directory. os.MkdirTemp avoids this
	// by not tying cleanup to test lifecycle.
	tmpDir, err := os.MkdirTemp("", "impartus-test-createjob-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	counter := &mockLoginCallCounter{}
	s := NewAPIServerWithLogin("8080", &config.Config{
		Username:         "user",
		Password:         "pass",
		BaseURL:          ts.URL,
		DownloadLocation: tmpDir,
		Quality:          "450",
	}, mockUpstreamLogin(counter))

	token := setupAuth(t, s)

	body := `{"subjectId":1,"sessionId":1,"startIndex":1,"endIndex":1}`
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/jobs", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d, body: %s", rec.Code, rec.Body.String())
	}
}

// ============================================================================
// WSHub Tests
// ============================================================================

func TestWSHubRegisterAndBroadcast(t *testing.T) {
	hub := NewWSHub()

	// Broadcast with no connections should not panic
	evt := newWSEvent("test.event", "job-1")
	evt.Status = StatusCompleted
	broadcastEvent(hub, evt)
}

func TestWSHubRegisterUnregister(t *testing.T) {
	hub := NewWSHub()
	// Register nil conn is fine for testing — hub just stores it
	hub.Register(nil)
	hub.Unregister(nil) // Should not panic
}

// ============================================================================
// broadcastEvent Tests
// ============================================================================

func TestBroadcastEventWithDetails(t *testing.T) {
	hub := NewWSHub()

	evt := wsEvent{
		Type:      "job.progress",
		JobID:     "job-123",
		Status:    StatusRunning,
		Progress:  0.5,
		Phase:     "downloading",
		Timestamp: time.Now().Unix(),
		Details:   map[string]any{"chunk": 5, "total": 10},
	}
	broadcastEvent(hub, evt)
	// No panic = success
}

// ============================================================================
// buildinfo coverage (imported from server)
// ============================================================================

func TestDefaultUpstreamLogin_NilConfig(t *testing.T) {
	_, _, err := defaultUpstreamLogin(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil config in defaultUpstreamLogin")
	}
}

// ============================================================================
// selectJobLectures additional edge cases
// ============================================================================

func TestSelectJobLectures_EndIndexClamped(t *testing.T) {
	lectures := client.Lectures{
		client.Lecture{SeqNo: 1, Topic: "L1"},
		client.Lecture{SeqNo: 2, Topic: "L2"},
	}

	// endIndex > len(lectures) — SelectRange returns error for out-of-range
	job := &Job{StartIndex: 1, EndIndex: 10}
	_, _, err := selectJobLectures(job, lectures)
	if err == nil {
		t.Fatal("expected error for out-of-range end index")
	}
}

// ============================================================================
// lecturesHandler with upstream failure
// ============================================================================

func TestLecturesHandler_UpstreamLoginFails(t *testing.T) {
	s := NewAPIServerWithLogin("8080", &config.Config{
		Username:         "user",
		Password:         "pass",
		BaseURL:          "https://example.com",
		DownloadLocation: "./downloads",
	}, mockFailingLogin())

	token := setupAuth(t, s)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/lectures?subject_id=1&session_id=1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d, body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "LOGIN_FAILED") {
		t.Errorf("expected LOGIN_FAILED error, got: %s", rec.Body.String())
	}
}

func TestCoursesHandler_UpstreamLoginFails(t *testing.T) {
	s := NewAPIServerWithLogin("8080", &config.Config{
		Username:         "user",
		Password:         "pass",
		BaseURL:          "https://example.com",
		DownloadLocation: "./downloads",
	}, mockFailingLogin())

	token := setupAuth(t, s)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/courses", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d, body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "LOGIN_FAILED") {
		t.Errorf("expected LOGIN_FAILED error, got: %s", rec.Body.String())
	}
}

// ============================================================================
// extractJoinOutputs Tests
// ============================================================================

func TestExtractJoinOutputs(t *testing.T) {
	tests := []struct {
		name    string
		result  downloader.JoinResult
		wantLen int
	}{
		{"empty", downloader.JoinResult{}, 0},
		{"left only", downloader.JoinResult{LeftOutput: "/path/left.mp4"}, 1},
		{"left and right", downloader.JoinResult{LeftOutput: "/path/left.mp4", RightOutput: "/path/right.mp4"}, 2},
		{"both only", downloader.JoinResult{BothOutput: "/path/both.mp4"}, 1},
		{"all three", downloader.JoinResult{LeftOutput: "/path/left.mp4", RightOutput: "/path/right.mp4", BothOutput: "/path/both.mp4"}, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.result.OutputPaths()
			if len(result) != tt.wantLen {
				t.Errorf("got %d outputs, want %d", len(result), tt.wantLen)
			}
		})
	}
}

// ============================================================================
// getJobHandler Tests
// ============================================================================

func TestGetJobHandler_Success(t *testing.T) {
	s := newAPIServer(validServerConfig())
	token := setupAuth(t, s)

	cfg := &config.Config{DownloadLocation: "./downloads"}
	job := s.jobStore.CreateJob(1, 1, 1, 5, cfg)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/jobs/"+job.ID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req = mux.SetURLVars(req, map[string]string{"id": job.ID})
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if success, ok := resp["success"].(bool); !ok || !success {
		t.Error("expected success=true")
	}
}

func TestGetJobHandler_NotFound(t *testing.T) {
	s := newAPIServer(validServerConfig())
	token := setupAuth(t, s)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/jobs/nonexistent", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req = mux.SetURLVars(req, map[string]string{"id": "nonexistent"})
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d, body: %s", rec.Code, rec.Body.String())
	}
}

// ============================================================================
// deleteJobHandler Tests
// ============================================================================

func TestDeleteJobHandler_Success(t *testing.T) {
	s := newAPIServer(validServerConfig())
	token := setupAuth(t, s)

	cfg := &config.Config{DownloadLocation: "./downloads"}
	job := s.jobStore.CreateJob(1, 1, 1, 5, cfg)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "/api/v1/jobs/"+job.ID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req = mux.SetURLVars(req, map[string]string{"id": job.ID})
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "cancel") {
		t.Errorf("expected cancel status, got: %s", rec.Body.String())
	}
}

func TestDeleteJobHandler_NotFound(t *testing.T) {
	s := newAPIServer(validServerConfig())
	token := setupAuth(t, s)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "/api/v1/jobs/nonexistent", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req = mux.SetURLVars(req, map[string]string{"id": "nonexistent"})
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d, body: %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteJobHandler_TerminalJob(t *testing.T) {
	s := newAPIServer(validServerConfig())
	token := setupAuth(t, s)

	cfg := &config.Config{DownloadLocation: "./downloads"}
	job := s.jobStore.CreateJob(1, 1, 1, 5, cfg)
	s.jobStore.UpdateJob(job.ID, StatusCompleted, 100, "")

	req := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "/api/v1/jobs/"+job.ID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req = mux.SetURLVars(req, map[string]string{"id": job.ID})
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d, body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "CANNOT_CANCEL") {
		t.Errorf("expected CANNOT_CANCEL error, got: %s", rec.Body.String())
	}
}

// ============================================================================
// store.saveToDisk Tests
// ============================================================================

func TestSaveToDisk_NoPersistence(t *testing.T) {
	js := NewJobStore()
	cfg := &config.Config{DownloadLocation: "./downloads"}
	js.CreateJob(1, 1, 1, 5, cfg)
	// Should not panic when no persistence is configured
	js.saveToDisk()
}

func TestSaveToDisk_WithBadPath(t *testing.T) {
	tmpDir := t.TempDir()
	persistencePath := filepath.Join(tmpDir, "jobs.json")
	js := NewJobStoreWithPersistence(persistencePath)
	t.Cleanup(func() {
		js.persistence.path = persistencePath
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := js.Close(ctx); err != nil {
			t.Errorf("close persistent job store: %v", err)
		}
	})
	cfg := &config.Config{DownloadLocation: "./downloads"}
	js.CreateJob(1, 1, 1, 5, cfg)
	// Corrupt the persistence path to trigger an error
	js.persistence.path = "/nonexistent/dir/jobs.json"
	// Should log a warning but not panic
	js.saveToDisk()
}
