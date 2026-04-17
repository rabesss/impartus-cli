package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/config"
)

// ============================================================================
// JobStore Tests
// ============================================================================

func TestJobStoreCreateJob(t *testing.T) {
	js := NewJobStore()
	cfg := &config.Config{
		Username:         "user",
		Password:         "pass",
		DownloadLocation: "./downloads",
	}

	job := js.CreateJob(123, 456, 1, 5, cfg)

	if job == nil {
		t.Fatal("expected non-nil job")
	}
	if job.ID == "" {
		t.Error("expected job ID to be set")
	}
	if job.SubjectID != 123 {
		t.Errorf("expected subjectID 123, got %d", job.SubjectID)
	}
	if job.SessionID != 456 {
		t.Errorf("expected sessionID 456, got %d", job.SessionID)
	}
	if job.StartIndex != 1 {
		t.Errorf("expected startIndex 1, got %d", job.StartIndex)
	}
	if job.EndIndex != 5 {
		t.Errorf("expected endIndex 5, got %d", job.EndIndex)
	}
	if job.Status != "pending" {
		t.Errorf("expected status 'pending', got %s", job.Status)
	}
	if job.Progress != 0 {
		t.Errorf("expected progress 0, got %f", job.Progress)
	}
}

func TestJobStoreGetJob(t *testing.T) {
	js := NewJobStore()
	cfg := &config.Config{DownloadLocation: "./downloads"}

	// Get non-existent job
	_, ok := js.GetJob("nonexistent")
	if ok {
		t.Error("expected GetJob to return false for nonexistent job")
	}

	// Create and get job
	job := js.CreateJob(1, 1, 1, 1, cfg)
	retrieved, ok := js.GetJob(job.ID)
	if !ok {
		t.Fatal("expected GetJob to return true for created job")
	}
	if retrieved.ID != job.ID {
		t.Errorf("expected job ID %s, got %s", job.ID, retrieved.ID)
	}
}

func TestJobStoreListJobs(t *testing.T) {
	js := NewJobStore()
	cfg := &config.Config{DownloadLocation: "./downloads"}

	// Empty store
	jobs := js.ListJobs()
	if len(jobs) != 0 {
		t.Errorf("expected 0 jobs, got %d", len(jobs))
	}

	// Add jobs
	js.CreateJob(1, 1, 1, 1, cfg)
	js.CreateJob(2, 2, 1, 1, cfg)
	js.CreateJob(3, 3, 1, 1, cfg)

	jobs = js.ListJobs()
	if len(jobs) != 3 {
		t.Errorf("expected 3 jobs, got %d", len(jobs))
	}
}

func TestJobStoreUpdateJob(t *testing.T) {
	js := NewJobStore()
	cfg := &config.Config{DownloadLocation: "./downloads"}

	job := js.CreateJob(1, 1, 1, 1, cfg)
	js.UpdateJob(job.ID, "running", 50.0, "")

	updated, ok := js.GetJob(job.ID)
	if !ok {
		t.Fatal("expected job to exist")
	}
	if updated.Status != "running" {
		t.Errorf("expected status 'running', got %s", updated.Status)
	}
	if updated.Progress != 50.0 {
		t.Errorf("expected progress 50.0, got %f", updated.Progress)
	}

	// Update with error
	js.UpdateJob(job.ID, "failed", 0.0, "download error")
	updated, _ = js.GetJob(job.ID)
	if updated.Error != "download error" {
		t.Errorf("expected error 'download error', got %s", updated.Error)
	}

	// Update non-existent job (should not panic)
	js.UpdateJob("nonexistent-id", "running", 10.0, "")
}

func TestJobStoreSetLectureProgress(t *testing.T) {
	js := NewJobStore()
	cfg := &config.Config{DownloadLocation: "./downloads"}

	job := js.CreateJob(1, 1, 1, 3, cfg)
	js.SetLectureProgress(job.ID, 2, 3)

	updated, ok := js.GetJob(job.ID)
	if !ok {
		t.Fatal("expected job to exist")
	}
	if updated.CompletedLectures != 2 {
		t.Errorf("expected completed lectures 2, got %d", updated.CompletedLectures)
	}
	if updated.TotalLectures != 3 {
		t.Errorf("expected total lectures 3, got %d", updated.TotalLectures)
	}

	// Set progress on non-existent job (should not panic)
	js.SetLectureProgress("nonexistent-id", 1, 1)
}

func TestJobStoreSetOutputs(t *testing.T) {
	js := NewJobStore()
	cfg := &config.Config{DownloadLocation: "./downloads"}

	job := js.CreateJob(1, 1, 1, 1, cfg)
	outputs := []string{"output1.mp4", "output2.mp4"}
	js.SetOutputs(job.ID, outputs)

	updated, ok := js.GetJob(job.ID)
	if !ok {
		t.Fatal("expected job to exist")
	}
	if len(updated.Outputs) != 2 {
		t.Errorf("expected 2 outputs, got %d", len(updated.Outputs))
	}
	if updated.Outputs[0] != "output1.mp4" {
		t.Errorf("expected first output 'output1.mp4', got %s", updated.Outputs[0])
	}

	// Verify original slice is not modified
	outputs[0] = "modified"
	if updated.Outputs[0] == "modified" {
		t.Error("outputs slice should be copied, not referenced")
	}

	// Set outputs on non-existent job (should not panic)
	js.SetOutputs("nonexistent-id", []string{"out.mp4"})
}

func TestJobStoreCancelJob(t *testing.T) {
	js := NewJobStore()
	cfg := &config.Config{DownloadLocation: "./downloads"}

	// Cancel non-existent job
	_, err := js.CancelJob("nonexistent")
	if err == nil {
		t.Error("expected error when canceling nonexistent job")
	}
	if err.Error() != "not_found" {
		t.Errorf("expected 'not_found' error, got %v", err)
	}

	// Cancel pending job
	job := js.CreateJob(1, 1, 1, 1, cfg)
	canceled, err := js.CancelJob(job.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if canceled.Status != StatusCanceled {
		t.Errorf("expected status 'canceled', got %s", canceled.Status)
	}

	// Verify job is updated
	updated, _ := js.GetJob(job.ID)
	if updated.Status != StatusCanceled {
		t.Errorf("expected status 'canceled', got %s", updated.Status)
	}

	// Try to cancel already canceled job (terminal state)
	_, err = js.CancelJob(job.ID)
	if err == nil {
		t.Error("expected error when canceling already canceled job")
	}
	if !strings.HasPrefix(err.Error(), "terminal:") {
		t.Errorf("expected 'terminal:' prefix in error, got %v", err)
	}

	// Create completed job and try to cancel
	completedJob := js.CreateJob(2, 2, 1, 1, cfg)
	js.UpdateJob(completedJob.ID, "completed", 100.0, "")
	_, err = js.CancelJob(completedJob.ID)
	if err == nil {
		t.Error("expected error when canceling completed job")
	}

	// Create failed job and try to cancel
	failedJob := js.CreateJob(3, 3, 1, 1, cfg)
	js.UpdateJob(failedJob.ID, "failed", 0.0, "error")
	_, err = js.CancelJob(failedJob.ID)
	if err == nil {
		t.Error("expected error when canceling failed job")
	}
}

func TestJobStoreCancelJobTriggersContextCancellation(t *testing.T) {
	js := NewJobStore()
	cfg := &config.Config{DownloadLocation: "./downloads"}

	job := js.CreateJob(1, 1, 1, 1, cfg)

	// Verify context can be canceled
	select {
	case <-job.ctx.Done():
		t.Error("context should not be done before cancel")
	default:
	}

	_, err := js.CancelJob(job.ID)
	if err != nil {
		t.Fatalf("CancelJob failed: %v", err)
	}

	// Context should now be done
	select {
	case <-job.ctx.Done():
		// expected
	default:
		t.Error("context should be done after cancel")
	}
}

func TestJobStoreConcurrentAccess(t *testing.T) {
	js := NewJobStore()
	cfg := &config.Config{DownloadLocation: "./downloads"}

	// Create initial job
	initialJob := js.CreateJob(1, 1, 1, 1, cfg)

	var wg sync.WaitGroup
	numGoroutines := 50

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			js.GetJob(initialJob.ID)
			js.ListJobs()
		}()
	}

	// Concurrent writes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			js.CreateJob(n, n, 1, 1, cfg)
		}(i)
	}

	// Concurrent updates
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			js.UpdateJob(initialJob.ID, "running", 50.0, "")
		}()
	}

	wg.Wait()
}

// ============================================================================
// Auth Middleware Tests
// ============================================================================

func TestAuthMiddlewareMissingAuthorizationHeader(t *testing.T) {
	s := NewAPIServer("8080", validServerConfig())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/courses", nil)
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
	s := NewAPIServer("8080", validServerConfig())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/courses", nil)
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
	s := NewAPIServer("8080", validServerConfig())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/courses", nil)
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
	s := NewAPIServer("8080", validServerConfig())

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

	req := httptest.NewRequest(http.MethodGet, "/api/v1/courses", nil)
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
	s := NewAPIServer("8080", validServerConfig())

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/courses", nil)
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

func TestHealthHandler(t *testing.T) {
	s := NewAPIServer("8080", validServerConfig())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
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
	if data["status"] != "ok" {
		t.Errorf("expected status 'ok', got %v", data["status"])
	}
	meta, ok := resp["meta"].(map[string]any)
	if !ok {
		t.Fatal("expected meta object in response")
	}
	if meta["command"] != "health" {
		t.Errorf("expected meta.command 'health', got %v", meta["command"])
	}
	if meta["mode"] != "api" {
		t.Errorf("expected meta.mode 'api', got %v", meta["mode"])
	}
}

func TestLoginHandler(t *testing.T) {
	s := NewAPIServer("8080", validServerConfig())

	// Valid login
	reqBody := `{"username":"user","password":"pass"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(reqBody))
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
	s := NewAPIServer("8080", validServerConfig())

	reqBody := `{"username":"wrong","password":"wrong"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(reqBody))
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
	s := NewAPIServer("8080", validServerConfig())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestLoginHandlerOptionsRequest(t *testing.T) {
	s := NewAPIServer("8080", validServerConfig())

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/auth/login", nil)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for OPTIONS, got %d", rec.Code)
	}
}

func TestListJobsHandler(t *testing.T) {
	s := NewAPIServer("8080", validServerConfig())

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

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs", nil)
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
	s := NewAPIServer("8080", validServerConfig())

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

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/nonexistent", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestGetJobHandlerSuccess(t *testing.T) {
	s := NewAPIServer("8080", validServerConfig())

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

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/"+job.ID, nil)
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
	s := NewAPIServer("8080", validServerConfig())

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

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/jobs/"+job.ID, nil)
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
	s := NewAPIServer("8080", validServerConfig())

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

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/jobs/nonexistent", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestDeleteJobHandlerTerminalState(t *testing.T) {
	s := NewAPIServer("8080", validServerConfig())

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

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/jobs/"+job.ID, nil)
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

func TestRuntimeConfigFrom(t *testing.T) {
	cfg := &config.Config{
		Quality:                   "720",
		Views:                     "first",
		AudioOnly:                 true,
		AudioFormat:               "mp3",
		DownloadLocation:          "./downloads",
		EnablePipeline:            true,
		NumWorkers:                5,
		DownloadWorkersPerLecture: 2,
		DecryptWorkersPerLecture:  1,
		Slides:                    true,
	}

	runtime := runtimeConfigFrom(cfg)

	if runtime.Quality != "720" {
		t.Errorf("expected quality '720', got %s", runtime.Quality)
	}
	if runtime.Views != "first" {
		t.Errorf("expected views 'first', got %s", runtime.Views)
	}
	if !runtime.AudioOnly {
		t.Error("expected audioOnly=true")
	}
	if runtime.AudioFormat != "mp3" {
		t.Errorf("expected audioFormat 'mp3', got %s", runtime.AudioFormat)
	}
	if runtime.OutputPath != "./downloads" {
		t.Errorf("expected outputPath './downloads', got %s", runtime.OutputPath)
	}
	if !runtime.EnablePipeline {
		t.Error("expected enablePipeline=true")
	}
	if runtime.NumWorkers != 5 {
		t.Errorf("expected numWorkers 5, got %d", runtime.NumWorkers)
	}
	if runtime.DownloadWorkersPerLecture != 2 {
		t.Errorf("expected downloadWorkersPerLecture 2, got %d", runtime.DownloadWorkersPerLecture)
	}
	if runtime.DecryptWorkersPerLecture != 1 {
		t.Errorf("expected decryptWorkersPerLecture 1, got %d", runtime.DecryptWorkersPerLecture)
	}
	if !runtime.Slides {
		t.Error("expected slides=true")
	}
}

func TestRuntimeConfigFromNil(t *testing.T) {
	runtime := runtimeConfigFrom(nil)

	if runtime.Quality != "" {
		t.Errorf("expected empty quality, got %s", runtime.Quality)
	}
}

func TestCloneConfig(t *testing.T) {
	cfg := &config.Config{
		Username:         "user",
		Password:         "pass",
		BaseURL:          "https://example.com",
		DownloadLocation: "./downloads",
	}

	clone := cloneConfig(cfg)

	if clone == cfg {
		t.Error("clone should be a different pointer")
	}
	if clone.Username != cfg.Username {
		t.Errorf("expected username %s, got %s", cfg.Username, clone.Username)
	}
	if clone.Password != cfg.Password {
		t.Errorf("expected password %s, got %s", cfg.Password, clone.Password)
	}

	// Verify modification doesn't affect original
	clone.Username = "modified"
	if cfg.Username == "modified" {
		t.Error("modifying clone should not affect original")
	}
}

func TestCloneConfigWithBaseURL(t *testing.T) {
	cfg := &config.Config{
		BaseURL: "https://example.com",
	}

	clone := cloneConfig(cfg)

	if clone.BaseURL != cfg.BaseURL {
		t.Errorf("expected baseURL %s, got %s", cfg.BaseURL, clone.BaseURL)
	}
}

func TestCloneConfigNil(t *testing.T) {
	clone := cloneConfig(nil)
	if clone != nil {
		t.Error("cloning nil should return nil")
	}
}

func TestApplyJobConfigOverrides(t *testing.T) {
	cfg := &config.Config{
		Quality:                   "450",
		Views:                     "both",
		AudioOnly:                 false,
		AudioFormat:               "mp3",
		DownloadLocation:          "./downloads",
		EnablePipeline:            false,
		NumWorkers:                3,
		DownloadWorkersPerLecture: 1,
		DecryptWorkersPerLecture:  1,
	}

	opts := &JobConfigOptions{
		Quality:                   strPtr("720"),
		Views:                     strPtr("first"),
		AudioOnly:                 boolPtr(true),
		AudioFormat:               strPtr("aac"),
		OutputPath:                strPtr("./custom-output"),
		EnablePipeline:            boolPtr(true),
		NumWorkers:                intPtr(8),
		DownloadWorkersPerLecture: intPtr(4),
		DecryptWorkersPerLecture:  intPtr(2),
	}

	applyJobConfigOverrides(cfg, opts)

	if cfg.Quality != "720" {
		t.Errorf("expected quality '720', got %s", cfg.Quality)
	}
	if cfg.Views != "first" {
		t.Errorf("expected views 'first', got %s", cfg.Views)
	}
	if !cfg.AudioOnly {
		t.Error("expected audioOnly=true")
	}
	if cfg.AudioFormat != "aac" {
		t.Errorf("expected audioFormat 'aac', got %s", cfg.AudioFormat)
	}
	if cfg.DownloadLocation != "./custom-output" {
		t.Errorf("expected downloadLocation './custom-output', got %s", cfg.DownloadLocation)
	}
	if !cfg.EnablePipeline {
		t.Error("expected enablePipeline=true")
	}
	if cfg.NumWorkers != 8 {
		t.Errorf("expected numWorkers 8, got %d", cfg.NumWorkers)
	}
	if cfg.DownloadWorkersPerLecture != 4 {
		t.Errorf("expected downloadWorkersPerLecture 4, got %d", cfg.DownloadWorkersPerLecture)
	}
	if cfg.DecryptWorkersPerLecture != 2 {
		t.Errorf("expected decryptWorkersPerLecture 2, got %d", cfg.DecryptWorkersPerLecture)
	}
}

func TestApplyJobConfigOverridesNilOpts(t *testing.T) {
	cfg := &config.Config{
		Quality:    "450",
		NumWorkers: 3,
	}

	applyJobConfigOverrides(cfg, nil)

	// Original values should remain unchanged
	if cfg.Quality != "450" {
		t.Errorf("expected quality '450', got %s", cfg.Quality)
	}
	if cfg.NumWorkers != 3 {
		t.Errorf("expected numWorkers 3, got %d", cfg.NumWorkers)
	}
}

func TestGetRequestID(t *testing.T) {
	// Test with no request ID in context
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	id := GetRequestID(req)
	if id != "" {
		t.Errorf("expected empty string when no request ID, got %s", id)
	}

	// Test with request ID in context
	ctx := context.WithValue(req.Context(), requestIDKey{}, "test-request-id")
	req = req.WithContext(ctx)
	id = GetRequestID(req)
	if id != "test-request-id" {
		t.Errorf("expected 'test-request-id', got %s", id)
	}
}

func TestEffectiveJobConfigWithJobConfig(t *testing.T) {
	opts := &JobConfigOptions{
		Quality: strPtr("720"),
	}

	req := createJobRequest{
		SubjectID:  1,
		SessionID:  1,
		StartIndex: 1,
		EndIndex:   1,
		JobConfig:  opts,
	}

	// Top-level fields should be ignored when JobConfig is set
	effective := req.effectiveJobConfig()
	if effective == nil {
		t.Fatal("expected non-nil effective config")
	}
	if effective.Quality == nil || *effective.Quality != "720" {
		t.Errorf("expected quality '720', got %v", effective.Quality)
	}
}

func TestEffectiveJobConfigWithTopLevelFields(t *testing.T) {
	quality := "450"
	req := createJobRequest{
		SubjectID:       1,
		SessionID:       1,
		StartIndex:      1,
		EndIndex:        1,
		JobConfigOptions: &JobConfigOptions{
			Quality: &quality,
		},
	}

	effective := req.effectiveJobConfig()
	if effective == nil {
		t.Fatal("expected non-nil effective config")
	}
	if effective.Quality == nil || *effective.Quality != "450" {
		t.Errorf("expected quality '450', got %v", effective.Quality)
	}
}

func TestEffectiveJobConfigWithNeither(t *testing.T) {
	req := createJobRequest{
		SubjectID:  1,
		SessionID:  1,
		StartIndex: 1,
		EndIndex:   1,
	}

	effective := req.effectiveJobConfig()
	if effective != nil {
		t.Errorf("expected nil effective config, got %+v", effective)
	}
}

func TestCreateJobHandlerValidation(t *testing.T) {
	s := NewAPIServer("8080", validServerConfig())

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

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantErr    string
	}{
		{
			name:       "invalid json",
			body:       `invalid`,
			wantStatus: http.StatusBadRequest,
			wantErr:    "INVALID_REQUEST",
		},
		{
			name:       "missing subjectId",
			body:       `{"sessionId":1,"startIndex":1,"endIndex":1}`,
			wantStatus: http.StatusBadRequest,
			wantErr:    "MISSING_PARAMETER",
		},
		{
			name:       "zero subjectId",
			body:       `{"subjectId":0,"sessionId":1,"startIndex":1,"endIndex":1}`,
			wantStatus: http.StatusBadRequest,
			wantErr:    "MISSING_PARAMETER",
		},
		{
			name:       "zero sessionId",
			body:       `{"subjectId":1,"sessionId":0,"startIndex":1,"endIndex":1}`,
			wantStatus: http.StatusBadRequest,
			wantErr:    "MISSING_PARAMETER",
		},
		{
			name:       "zero startIndex",
			body:       `{"subjectId":1,"sessionId":1,"startIndex":0,"endIndex":1}`,
			wantStatus: http.StatusBadRequest,
			wantErr:    "INVALID_REQUEST",
		},
		{
			name:       "endIndex less than startIndex",
			body:       `{"subjectId":1,"sessionId":1,"startIndex":3,"endIndex":1}`,
			wantStatus: http.StatusBadRequest,
			wantErr:    "INVALID_REQUEST",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs", strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			s.router.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Errorf("expected status %d, got %d, body: %s", tc.wantStatus, rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.wantErr) {
				t.Errorf("expected error containing %q, got body: %s", tc.wantErr, rec.Body.String())
			}
		})
	}
}

func TestLecturesHandlerValidation(t *testing.T) {
	s := NewAPIServer("8080", validServerConfig())

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

	tests := []struct {
		name       string
		query      string
		wantStatus int
		wantErr    string
	}{
		{
			name:       "missing subject_id",
			query:      "?session_id=1",
			wantStatus: http.StatusBadRequest,
			wantErr:    "MISSING_PARAMETER",
		},
		{
			name:       "missing session_id",
			query:      "?subject_id=1",
			wantStatus: http.StatusBadRequest,
			wantErr:    "MISSING_PARAMETER",
		},
		{
			name:       "invalid subject_id",
			query:      "?subject_id=abc&session_id=1",
			wantStatus: http.StatusBadRequest,
			wantErr:    "INVALID_REQUEST",
		},
		{
			name:       "invalid session_id",
			query:      "?subject_id=1&session_id=xyz",
			wantStatus: http.StatusBadRequest,
			wantErr:    "INVALID_REQUEST",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/lectures"+tc.query, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			s.router.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Errorf("expected status %d, got %d, body: %s", tc.wantStatus, rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.wantErr) {
				t.Errorf("expected error containing %q, got body: %s", tc.wantErr, rec.Body.String())
			}
		})
	}
}

func TestMergeConfigWithJobOptionsEmptyOutputPath(t *testing.T) {
	cfg := validServerConfig()
	opts := &JobConfigOptions{
		OutputPath: strPtr(""),
	}

	_, err := mergeConfigWithJobOptions(cfg, opts)
	if err == nil {
		t.Error("expected error for empty output path")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected 'empty' in error, got %v", err)
	}
}

func TestMergeConfigWithJobOptionsInvalidQuality(t *testing.T) {
	cfg := validServerConfig()
	opts := &JobConfigOptions{
		Quality: strPtr("invalid"),
	}

	_, err := mergeConfigWithJobOptions(cfg, opts)
	if err == nil {
		t.Error("expected error for invalid quality")
	}
}

func TestMergeConfigWithJobOptionsInvalidWorkers(t *testing.T) {
	cfg := validServerConfig()
	opts := &JobConfigOptions{
		NumWorkers: intPtr(0),
	}

	_, err := mergeConfigWithJobOptions(cfg, opts)
	if err == nil {
		t.Error("expected error for zero workers")
	}
}

func TestNewAPIServerWithNilConfig(t *testing.T) {
	s := NewAPIServer("8080", nil)

	if s == nil {
		t.Fatal("expected non-nil server")
	}
	if s.cfg == nil {
		t.Error("expected config to be initialized")
	}
	// Default values should be set
	if s.cfg.DownloadLocation != "./downloads" {
		t.Errorf("expected default download location './downloads', got %s", s.cfg.DownloadLocation)
	}
	if s.cfg.TempDirLocation != "./temp" {
		t.Errorf("expected default temp dir './temp', got %s", s.cfg.TempDirLocation)
	}
}

func TestNewAPIServerWithPartialConfig(t *testing.T) {
	cfg := &config.Config{
		Username: "user",
		Password: "pass",
	}
	s := NewAPIServer("8080", cfg)

	if s.cfg.DownloadLocation != "./downloads" {
		t.Errorf("expected default download location, got %s", s.cfg.DownloadLocation)
	}
	if s.cfg.TempDirLocation != "./temp" {
		t.Errorf("expected default temp dir, got %s", s.cfg.TempDirLocation)
	}
}

// ============================================================================
// WSHub Tests
// ============================================================================

func TestNewWSHub(t *testing.T) {
	hub := NewWSHub()
	if hub == nil {
		t.Fatal("expected non-nil hub")
	}
	if len(hub.clients) != 0 {
		t.Errorf("expected 0 clients, got %d", len(hub.clients))
	}
}

func TestWSHubBroadcastNoClients(t *testing.T) {
	hub := NewWSHub()
	err := hub.Broadcast(map[string]string{"type": "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBroadcastEventNoHub(t *testing.T) {
	hub := NewWSHub()
	// Should not panic
	broadcastEvent(hub, map[string]string{"type": "test"})
}

func TestWSHubBroadcastMarshalError(t *testing.T) {
	hub := NewWSHub()
	// Channels can't be marshaled to JSON
	err := hub.Broadcast(make(chan int))
	if err == nil {
		t.Error("expected error for unmarshallable type")
	}
}
