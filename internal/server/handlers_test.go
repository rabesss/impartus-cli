package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
			s := newAPIServer("8080", validServerConfig())
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
	s := newAPIServer("8080", validServerConfig())
	// No upstream cache set
	result := s.probeUpstreamHTTP()
	if result {
		t.Error("expected false when no upstream cache")
	}
}

func TestProbeUpstreamHTTP_EmptyToken(t *testing.T) {
	s := newAPIServer("8080", validServerConfig())
	s.upstreamCacheMu.Lock()
	s.upstreamCache = &upstreamCacheEntry{token: ""}
	s.upstreamCacheMu.Unlock()

	result := s.probeUpstreamHTTP()
	if result {
		t.Error("expected false when token is empty")
	}
}

func TestProbeUpstreamHTTP_InvalidURL(t *testing.T) {
	s := newAPIServer("8080", &config.Config{
		Username:         "user",
		Password:         "pass",
		BaseURL:          "://invalid",
		DownloadLocation: "./downloads",
	})
	s.upstreamCacheMu.Lock()
	s.upstreamCache = &upstreamCacheEntry{token: "some-token"}
	s.upstreamCacheMu.Unlock()

	result := s.probeUpstreamHTTP()
	if result {
		t.Error("expected false for invalid URL")
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
	s := newAPIServer("8080", &config.Config{
		Username:         "user",
		Password:         "pass",
		BaseURL:          baseURL,
		DownloadLocation: "./downloads",
	})
	s.upstreamCacheMu.Lock()
	s.upstreamCache = &upstreamCacheEntry{token: "valid-token"}
	s.upstreamCacheMu.Unlock()

	result := s.probeUpstreamHTTP()
	if !result {
		t.Error("expected true for reachable upstream")
	}
}

func TestProbeUpstreamHTTP_ServerReturnsError(t *testing.T) {
	// Create a server that immediately closes connections
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return 500 to simulate error
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	s := newAPIServer("8080", &config.Config{
		Username:         "user",
		Password:         "pass",
		BaseURL:          ts.URL,
		DownloadLocation: "./downloads",
	})
	s.upstreamCacheMu.Lock()
	s.upstreamCache = &upstreamCacheEntry{token: "valid-token"}
	s.upstreamCacheMu.Unlock()

	// Server returns 500 but connection succeeds, so probe should return true
	// (it only checks connectivity, not status code)
	result := s.probeUpstreamHTTP()
	if !result {
		t.Error("expected true for reachable upstream (HTTP 500 still means connected)")
	}
}

// ============================================================================
// probeUpstreamTCP Tests
// ============================================================================

func TestProbeUpstreamTCP_InvalidURL(t *testing.T) {
	s := newAPIServer("8080", &config.Config{
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
	s := newAPIServer("8080", &config.Config{
		Username:         "user",
		Password:         "pass",
		BaseURL:          "https://192.0.2.1:1", // RFC 5737 TEST-NET, should be unreachable
		DownloadLocation: "./downloads",
	})

	// This will timeout or fail quickly
	result := s.probeUpstreamTCP()
	// Can't assert false definitively (network-dependent), just ensure no panic
	_ = result
}

func TestProbeUpstreamTCP_ReachableServer(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()

	s := newAPIServer("8080", &config.Config{
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
	s := newAPIServer("8080", nil)
	result := s.checkUpstreamStatus()
	if result.Status != "not_configured" {
		t.Errorf("expected not_configured, got %s", result.Status)
	}
}

func TestCheckUpstreamStatus_EmptyBaseURL(t *testing.T) {
	s := newAPIServer("8080", &config.Config{
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

	s := newAPIServer("8080", &config.Config{
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
	s := newAPIServer("8080", validServerConfig())
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

	req := httptest.NewRequest(http.MethodGet, "/api/v1/courses", nil)
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

	req := httptest.NewRequest(http.MethodGet, "/api/v1/lectures?subject_id=1&session_id=1", nil)
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
	req := httptest.NewRequest(http.MethodGet, "/api/v1/lectures?subjectId=1&sessionId=1", nil)
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

	counter := &mockLoginCallCounter{}
	s := NewAPIServerWithLogin("8080", &config.Config{
		Username:         "user",
		Password:         "pass",
		BaseURL:          ts.URL,
		DownloadLocation: t.TempDir(),
		Quality:          "450",
	}, mockUpstreamLogin(counter))

	token := setupAuth(t, s)

	body := `{"subjectId":1,"sessionId":1,"startIndex":1,"endIndex":1}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs", strings.NewReader(body))
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

	req := httptest.NewRequest(http.MethodGet, "/api/v1/lectures?subject_id=1&session_id=1", nil)
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

	req := httptest.NewRequest(http.MethodGet, "/api/v1/courses", nil)
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
			result := extractJoinOutputs(tt.result)
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
	s := newAPIServer("8080", validServerConfig())
	token := setupAuth(t, s)

	cfg := &config.Config{DownloadLocation: "./downloads"}
	job := s.jobStore.CreateJob(1, 1, 1, 5, cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/"+job.ID, nil)
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
	s := newAPIServer("8080", validServerConfig())
	token := setupAuth(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/nonexistent", nil)
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
	s := newAPIServer("8080", validServerConfig())
	token := setupAuth(t, s)

	cfg := &config.Config{DownloadLocation: "./downloads"}
	job := s.jobStore.CreateJob(1, 1, 1, 5, cfg)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/jobs/"+job.ID, nil)
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
	s := newAPIServer("8080", validServerConfig())
	token := setupAuth(t, s)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/jobs/nonexistent", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req = mux.SetURLVars(req, map[string]string{"id": "nonexistent"})
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d, body: %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteJobHandler_TerminalJob(t *testing.T) {
	s := newAPIServer("8080", validServerConfig())
	token := setupAuth(t, s)

	cfg := &config.Config{DownloadLocation: "./downloads"}
	job := s.jobStore.CreateJob(1, 1, 1, 5, cfg)
	s.jobStore.UpdateJob(job.ID, StatusCompleted, 100, "")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/jobs/"+job.ID, nil)
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
	js := NewJobStoreWithPersistence(filepath.Join(tmpDir, "jobs.json"))
	cfg := &config.Config{DownloadLocation: "./downloads"}
	js.CreateJob(1, 1, 1, 5, cfg)
	// Corrupt the persistence path to trigger an error
	js.persistence.path = "/nonexistent/dir/jobs.json"
	// Should log a warning but not panic
	js.saveToDisk()
}
