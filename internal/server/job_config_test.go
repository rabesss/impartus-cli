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
	if runtime.Views != "left" {
		t.Errorf("expected views 'left', got %s", runtime.Views)
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

	if err := applyJobConfigOverrides(cfg, opts); err != nil {
		t.Fatalf("applyJobConfigOverrides: %v", err)
	}

	if cfg.Quality != "720" {
		t.Errorf("expected quality '720', got %s", cfg.Quality)
	}
	if cfg.Views != "left" {
		t.Errorf("expected views 'left', got %s", cfg.Views)
	}
	if !cfg.AudioOnly {
		t.Error("expected audioOnly=true")
	}
	if cfg.AudioFormat != "aac" {
		t.Errorf("expected audioFormat 'aac', got %s", cfg.AudioFormat)
	}
	if cfg.DownloadLocation != "custom-output" {
		t.Errorf("expected downloadLocation 'custom-output', got %s", cfg.DownloadLocation)
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

	if err := applyJobConfigOverrides(cfg, nil); err != nil {
		t.Fatalf("applyJobConfigOverrides(nil): %v", err)
	}

	// Original values should remain unchanged
	if cfg.Quality != "450" {
		t.Errorf("expected quality '450', got %s", cfg.Quality)
	}
	if cfg.NumWorkers != 3 {
		t.Errorf("expected numWorkers 3, got %d", cfg.NumWorkers)
	}
}

func TestRequestIDFrom(t *testing.T) {
	// Test with no request ID in context
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/test", nil)
	id := requestIDFrom(req)
	if id != "" {
		t.Errorf("expected empty string when no request ID, got %s", id)
	}

	// Test with request ID in context
	ctx := context.WithValue(req.Context(), requestIDKey{}, "test-request-id")
	req = req.WithContext(ctx)
	id = requestIDFrom(req)
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

func TestEffectiveJobConfigWithLegacyTopLevelFields(t *testing.T) {
	var req createJobRequest
	if err := json.Unmarshal([]byte(`{
		"subjectId": 1,
		"sessionId": 1,
		"startIndex": 1,
		"endIndex": 1,
		"quality": "450"
	}`), &req); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	effective := req.effectiveJobConfig()
	if effective == nil {
		t.Fatal("expected non-nil effective config")
	}
	if effective.Quality == nil || *effective.Quality != "450" {
		t.Errorf("expected quality '450', got %v", effective.Quality)
	}
}

func TestCreateJobRequestPrefersCanonicalJobConfig(t *testing.T) {
	var req createJobRequest
	if err := json.Unmarshal([]byte(`{
		"subjectId": 1,
		"sessionId": 1,
		"startIndex": 1,
		"endIndex": 1,
		"quality": "450",
		"jobConfig": {
			"quality": "720"
		}
	}`), &req); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	effective := req.effectiveJobConfig()
	if effective == nil {
		t.Fatal("expected non-nil effective config")
	}
	if effective.Quality == nil || *effective.Quality != "720" {
		t.Errorf("expected quality '720', got %v", effective.Quality)
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
			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/jobs", strings.NewReader(tc.body))
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
			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/lectures"+tc.query, nil)
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
	// Empty output path override should be skipped, leaving the default download location
	cfg := &config.Config{
		Username: "user",
		Password: "pass",
		BaseURL:  "https://example.com",
		Quality:  "450",
		// DownloadLocation intentionally empty — will get default via ApplyDefaults
	}
	opts := &JobConfigOptions{
		OutputPath: strPtr(""),
	}

	result, err := mergeConfigWithJobOptions(cfg, opts)
	if err != nil {
		t.Fatalf("expected no error for empty output path override, got: %v", err)
	}
	if result.DownloadLocation == "" {
		t.Error("expected downloadLocation to be set to default")
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
	s := newAPIServer(nil)

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
	s := newAPIServer(cfg)

	if s.cfg.DownloadLocation != "./downloads" {
		t.Errorf("expected default download location, got %s", s.cfg.DownloadLocation)
	}
	if s.cfg.TempDirLocation != "./temp" {
		t.Errorf("expected default temp dir, got %s", s.cfg.TempDirLocation)
	}
}

// ============================================================================
// WSHub Tests (see hub_test.go)
// ============================================================================
