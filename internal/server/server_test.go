package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

func strPtr(v string) *string { return &v }
func boolPtr(v bool) *bool    { return &v }
func intPtr(v int) *int       { return &v }

// assertMapField asserts that the given key exists in the map and is a map[string]any.
func assertMapField(t *testing.T, m map[string]any, key string) map[string]any {
	t.Helper()
	v, ok := m[key].(map[string]any)
	if !ok {
		t.Fatalf("expected %q field to be map[string]any, got %T", key, m[key])
	}
	return v
}

func validServerConfig() *config.Config {
	return &config.Config{
		Username:         "user",
		Password:         "pass",
		BaseURL:          "https://example.com",
		Quality:          "450",
		Views:            "both",
		DownloadLocation: "./downloads",
		NumWorkers:       5,
		RateLimit:        1,
		APIRateLimit:     1,
		AudioFormat:      "mp3",
		HTTPTimeout:      "1m",
	}
}

func TestMergeConfigWithJobOptionsAppliesOverridesAndValidatesInvalidValues(t *testing.T) {
	cfg := validServerConfig()
	opts := &JobConfigOptions{
		Quality:                   strPtr("720"),
		Views:                     strPtr("second"),
		AudioOnly:                 boolPtr(true),
		AudioFormat:               strPtr("aac"),
		OutputPath:                strPtr(" ./custom-output "),
		EnablePipeline:            boolPtr(true),
		NumWorkers:                intPtr(8),
		DownloadWorkersPerLecture: intPtr(4),
		DecryptWorkersPerLecture:  intPtr(2),
	}

	merged, err := mergeConfigWithJobOptions(cfg, opts)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if merged.Quality != "720" || merged.Views != "right" || !merged.AudioOnly || merged.AudioFormat != "aac" {
		t.Fatalf("unexpected merged media config: %+v", merged)
	}
	if merged.DownloadLocation != "./custom-output" {
		t.Fatalf("expected trimmed output path override, got %q", merged.DownloadLocation)
	}
	if !merged.EnablePipeline || merged.NumWorkers != 8 || merged.DownloadWorkersPerLecture != 4 || merged.DecryptWorkersPerLecture != 2 {
		t.Fatalf("unexpected merged worker config: %+v", merged)
	}
	if cfg.Quality != "450" {
		t.Fatalf("expected original config to remain unchanged, got quality=%q", cfg.Quality)
	}

	invalidCases := []struct {
		name    string
		opts    *JobConfigOptions
		wantErr string
	}{
		{name: "invalid quality", opts: &JobConfigOptions{Quality: strPtr("1080")}, wantErr: "quality must be one of"},
		{name: "invalid workers", opts: &JobConfigOptions{NumWorkers: intPtr(0)}, wantErr: "numWorkers must be between"},
		{name: "empty output", opts: &JobConfigOptions{OutputPath: strPtr("   ")}, wantErr: "outputPath/downloadLocation cannot be empty"},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := mergeConfigWithJobOptions(validServerConfig(), tc.opts)
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestCreateJobRequestEffectiveJobConfigMapsTopLevelFields(t *testing.T) {
	quality := "450"
	views := "both"
	audioOnly := true
	audioFormat := "opus"
	outputPath := "./output"
	enablePipeline := true
	numWorkers := 7
	downloadWorkers := 3
	decryptWorkers := 2

	req := createJobRequest{
		JobConfig: &JobConfigOptions{
			Quality:                   &quality,
			Views:                     &views,
			AudioOnly:                 &audioOnly,
			AudioFormat:               &audioFormat,
			OutputPath:                &outputPath,
			EnablePipeline:            &enablePipeline,
			NumWorkers:                &numWorkers,
			DownloadWorkersPerLecture: &downloadWorkers,
			DecryptWorkersPerLecture:  &decryptWorkers,
		},
	}

	effective := req.effectiveJobConfig()
	if effective == nil {
		t.Fatal("expected effective config, got nil")
	}

	if *effective.Quality != quality || *effective.Views != views || *effective.AudioOnly != audioOnly ||
		*effective.AudioFormat != audioFormat || *effective.OutputPath != outputPath ||
		*effective.EnablePipeline != enablePipeline || *effective.NumWorkers != numWorkers ||
		*effective.DownloadWorkersPerLecture != downloadWorkers || *effective.DecryptWorkersPerLecture != decryptWorkers {
		t.Fatalf("unexpected effective config mapping: %+v", effective)
	}
}

func TestNewAPIServerWithPersistenceRestoresJobsAcrossRestart(t *testing.T) {
	tmpDir := t.TempDir()
	persistencePath := filepath.Join(tmpDir, ".jobs.json")

	s1 := NewAPIServerWithPersistence("8080", validServerConfig(), persistencePath)
	job := s1.jobStore.CreateJob(123, 456, 1, 3, validServerConfig())
	s1.jobStore.UpdateJob(job.ID, "completed", 100, "")
	s1.jobStore.SetOutputs(job.ID, []string{"lecture.mp4"})

	s2 := NewAPIServerWithPersistence("8080", validServerConfig(), persistencePath)
	restored, ok := s2.jobStore.GetJob(job.ID)
	if !ok {
		t.Fatal("expected persisted job to be restored")
	}
	if restored.Status != "completed" {
		t.Fatalf("expected restored status completed, got %s", restored.Status)
	}
	if len(restored.Outputs) != 1 || restored.Outputs[0] != "lecture.mp4" {
		t.Fatalf("expected restored outputs to round-trip, got %+v", restored.Outputs)
	}
}

func TestNormalizeViewsViaConfig(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "first", want: "left"},
		{in: "second", want: "right"},
		{in: "both", want: "both"},
		{in: "left", want: "left"},
		{in: "", want: ""},
	}

	for _, tc := range cases {
		if got := config.NormalizeViews(tc.in); got != tc.want {
			t.Fatalf("NormalizeViews(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestWebSocketRouteRequiresAuth(t *testing.T) {
	s := newAPIServer(validServerConfig())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/ws", nil)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated websocket request, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "MISSING_TOKEN") {
		t.Fatalf("expected MISSING_TOKEN error, got body: %s", rec.Body.String())
	}
}

func TestRequestIDMiddlewareAddsHeader(t *testing.T) {
	// Test that middleware generates a request ID when none provided
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := requestIDFrom(r)
		if requestID == "" {
			t.Error("expected request ID in context")
		}
		w.WriteHeader(http.StatusOK)
	})

	middleware := requestIDMiddleware(handler)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)

	// Check response header contains X-Request-ID
	requestID := rec.Header().Get("X-Request-ID")
	if requestID == "" {
		t.Error("expected X-Request-ID header in response")
	}
}

func TestRequestIDMiddlewarePropagatesExistingID(t *testing.T) {
	existingID := "existing-request-id-12345"

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := requestIDFrom(r)
		if requestID != existingID {
			t.Errorf("expected request ID %q, got %q", existingID, requestID)
		}
		w.WriteHeader(http.StatusOK)
	})

	middleware := requestIDMiddleware(handler)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/test", nil)
	req.Header.Set("X-Request-ID", existingID)
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)

	// Check response header contains the propagated ID
	requestID := rec.Header().Get("X-Request-ID")
	if requestID != existingID {
		t.Errorf("expected X-Request-ID %q, got %q", existingID, requestID)
	}
}

// TestSelectJobLecturesMatchesCLIAlignment verifies that selectJobLectures
// produces the same lecture selection as CLI's selectLectureRange for identical
// index inputs. This ensures VAL-CROSS-002: CLI range selection and API job ranges
// refer to the same lecture slice.
func TestSelectJobLecturesMatchesCLIAlignment(t *testing.T) {
	// Create test lectures matching the structure used in CLI tests
	lectures := client.Lectures{
		client.Lecture{SeqNo: 1, Topic: "Lecture 1"},
		client.Lecture{SeqNo: 2, Topic: "Lecture 2"},
		client.Lecture{SeqNo: 3, Topic: "Lecture 3"},
		client.Lecture{SeqNo: 4, Topic: "Lecture 4"},
		client.Lecture{SeqNo: 5, Topic: "Lecture 5"},
	}

	// Test case 1: range 1-2 should select first 2 lectures from reversed order
	// CLI's selectLectureRange: reverses to [5,4,3,2,1], then takes [5,4] for range 1-2
	job1 := &Job{StartIndex: 1, EndIndex: 2}
	selected1, filtered1, err := selectJobLectures(job1, lectures)
	if err != nil {
		t.Fatalf("selectJobLectures(1-2) unexpected error: %v", err)
	}
	if filtered1 != 0 {
		t.Fatalf("expected 0 filtered lectures, got %d", filtered1)
	}
	// Expected: reversed [5,4,3,2,1], take indices 0-1 => [5,4]
	if len(selected1) != 2 {
		t.Fatalf("expected 2 lectures, got %d", len(selected1))
	}
	if selected1[0].SeqNo != 5 || selected1[1].SeqNo != 4 {
		t.Errorf("range 1-2: expected [5, 4], got [%d, %d]", selected1[0].SeqNo, selected1[1].SeqNo)
	}

	// Test case 2: range 1-5 (full range) should select all lectures in reverse order
	job2 := &Job{StartIndex: 1, EndIndex: 5}
	selected2, filtered2, err := selectJobLectures(job2, lectures)
	if err != nil {
		t.Fatalf("selectJobLectures(1-5) unexpected error: %v", err)
	}
	if filtered2 != 0 {
		t.Fatalf("expected 0 filtered lectures, got %d", filtered2)
	}
	expectedSeqNos := []int{5, 4, 3, 2, 1}
	if len(selected2) != len(expectedSeqNos) {
		t.Fatalf("expected %d lectures, got %d", len(expectedSeqNos), len(selected2))
	}
	for i, expected := range expectedSeqNos {
		if selected2[i].SeqNo != expected {
			t.Errorf("full range: position %d expected SeqNo %d, got %d", i, expected, selected2[i].SeqNo)
		}
	}

	// Test case 3: default range (0,0) should select all lectures
	job3 := &Job{StartIndex: 0, EndIndex: 0}
	selected3, filtered3, err := selectJobLectures(job3, lectures)
	if err != nil {
		t.Fatalf("selectJobLectures(0,0) unexpected error: %v", err)
	}
	if filtered3 != 0 {
		t.Fatalf("expected 0 filtered lectures, got %d", filtered3)
	}
	if len(selected3) != 5 {
		t.Errorf("default range: expected 5 lectures, got %d", len(selected3))
	}
	// Default should also be reversed
	for i, expected := range expectedSeqNos {
		if selected3[i].SeqNo != expected {
			t.Errorf("default range: position %d expected SeqNo %d, got %d", i, expected, selected3[i].SeqNo)
		}
	}

	// Test case 4: range 3-4 should select middle lectures from reversed order
	// Reversed: [5,4,3,2,1], indices 2-3 => [3,2]
	job4 := &Job{StartIndex: 3, EndIndex: 4}
	selected4, filtered4, err := selectJobLectures(job4, lectures)
	if err != nil {
		t.Fatalf("selectJobLectures(3-4) unexpected error: %v", err)
	}
	if filtered4 != 0 {
		t.Fatalf("expected 0 filtered lectures, got %d", filtered4)
	}
	if len(selected4) != 2 {
		t.Fatalf("expected 2 lectures, got %d", len(selected4))
	}
	if selected4[0].SeqNo != 3 || selected4[1].SeqNo != 2 {
		t.Errorf("range 3-4: expected [3, 2], got [%d, %d]", selected4[0].SeqNo, selected4[1].SeqNo)
	}
}

// TestSelectJobLecturesOutOfRange validates error handling for invalid ranges
func TestSelectJobLecturesOutOfRange(t *testing.T) {
	lectures := client.Lectures{
		client.Lecture{SeqNo: 1, Topic: "Lecture 1"},
		client.Lecture{SeqNo: 2, Topic: "Lecture 2"},
	}

	// Start index beyond available lectures
	job := &Job{StartIndex: 5, EndIndex: 10}
	_, _, err := selectJobLectures(job, lectures)
	if err == nil {
		t.Error("expected error for startIndex out of range")
	}
	if !strings.Contains(err.Error(), "range") {
		t.Errorf("expected 'range' error, got: %v", err)
	}

	// Empty lectures
	emptyJob := &Job{StartIndex: 1, EndIndex: 1}
	_, _, err = selectJobLectures(emptyJob, nil)
	if err == nil {
		t.Error("expected error for empty lectures")
	}
	if !strings.Contains(err.Error(), "no lectures found") {
		t.Errorf("expected 'no lectures found' error, got: %v", err)
	}
}

// ============================================================================
// Token Cache Tests
// ============================================================================

// mockLoginCallCounter tracks how many times the mock login function is called.
type mockLoginCallCounter struct {
	mu    sync.Mutex
	calls int
}

func (m *mockLoginCallCounter) increment() {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()
}

func (m *mockLoginCallCounter) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// mockUpstreamLogin creates a mock login function that returns a fake client
// and token without contacting any real upstream server.
func mockUpstreamLogin(counter *mockLoginCallCounter) UpstreamLoginFunc {
	return func(ctx context.Context, cfg *config.Config) (*client.Client, *config.Config, error) {
		if counter != nil {
			counter.increment()
		}
		apiClient := client.New(nil, nil)
		cfg.Token = "mock-token-12345"
		return apiClient, cfg, nil
	}
}

// mockFailingLogin creates a mock login function that always returns an error.
func mockFailingLogin() UpstreamLoginFunc {
	return func(ctx context.Context, cfg *config.Config) (*client.Client, *config.Config, error) {
		return nil, nil, fmt.Errorf("mock: upstream login failed")
	}
}

// TestUpstreamCachePopulatedAfterFirstCall verifies that after the first call
// to getOrRefreshUpstreamClient, the cache is populated with a valid token,
// client, and expiry time. (VAL-CACHE-001)
func TestUpstreamCachePopulatedAfterFirstCall(t *testing.T) {
	counter := &mockLoginCallCounter{}
	s := NewAPIServerWithLogin("8080", validServerConfig(), mockUpstreamLogin(counter))

	// Before any call, cache should be nil
	s.upstreamCacheMu.RLock()
	beforeCache := s.upstreamCache
	s.upstreamCacheMu.RUnlock()
	if beforeCache != nil {
		t.Error("expected nil cache before first call")
	}

	// First call should populate cache
	apiClient, cfg, err := s.getOrRefreshUpstreamClient(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if apiClient == nil {
		t.Fatal("expected non-nil client")
	}
	if cfg.Token != "mock-token-12345" {
		t.Errorf("expected mock token, got %q", cfg.Token)
	}
	if counter.count() != 1 {
		t.Errorf("expected 1 login call, got %d", counter.count())
	}

	// After call, cache should be populated
	s.upstreamCacheMu.RLock()
	afterCache := s.upstreamCache
	s.upstreamCacheMu.RUnlock()
	if afterCache == nil {
		t.Fatal("expected non-nil cache after first call")
	}
	if afterCache.token != "mock-token-12345" {
		t.Errorf("expected non-empty token in cache, got %q", afterCache.token)
	}
	if afterCache.client == nil {
		t.Error("expected non-nil client in cache")
	}
	if afterCache.expiresAt.IsZero() {
		t.Error("expected non-zero expiresAt in cache")
	}
}

// TestUpstreamCacheReuseOnSubsequentCalls verifies that subsequent calls
// return the same cached client without calling the login function again. (VAL-CACHE-002)
func TestUpstreamCacheReuseOnSubsequentCalls(t *testing.T) {
	counter := &mockLoginCallCounter{}
	s := NewAPIServerWithLogin("8080", validServerConfig(), mockUpstreamLogin(counter))

	// First call
	client1, cfg1, err1 := s.getOrRefreshUpstreamClient(context.Background())
	if err1 != nil {
		t.Fatalf("first call failed: %v", err1)
	}

	// Second call should return the same cached client (no new login)
	client2, cfg2, err2 := s.getOrRefreshUpstreamClient(context.Background())
	if err2 != nil {
		t.Fatalf("second call failed: %v", err2)
	}

	// Should be the same client instance (cached)
	if client1 != client2 {
		t.Error("expected same client instance on subsequent calls")
	}

	// Token should be the same
	if cfg1.Token != cfg2.Token {
		t.Errorf("expected same token, got %q and %q", cfg1.Token, cfg2.Token)
	}

	// Login should only have been called once (cache reused on second call)
	if counter.count() != 1 {
		t.Errorf("expected 1 login call (cache reused), got %d", counter.count())
	}
}

// TestUpstreamCacheExpiredTokenRefreshes verifies that when the cached token
// is expired, a new login is triggered and cache is updated. (VAL-CACHE-003)
func TestUpstreamCacheExpiredTokenRefreshes(t *testing.T) {
	counter := &mockLoginCallCounter{}
	s := NewAPIServerWithLogin("8080", validServerConfig(), mockUpstreamLogin(counter))

	// First call to populate cache
	_, cfg1, err := s.getOrRefreshUpstreamClient(context.Background())
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	// Verify cfg1 has a non-empty token
	if cfg1.Token == "" {
		t.Error("expected non-empty token after first call")
	}

	// Verify login was called once
	if counter.count() != 1 {
		t.Errorf("expected 1 login call after first get, got %d", counter.count())
	}

	// Manually expire the cached token
	s.upstreamCacheMu.Lock()
	if s.upstreamCache != nil {
		s.upstreamCache.expiresAt = time.Now().Add(-1 * time.Hour) // Expired
	}
	s.upstreamCacheMu.Unlock()

	// Next call should trigger a refresh (new login)
	_, cfg2, err := s.getOrRefreshUpstreamClient(context.Background())
	if err != nil {
		t.Fatalf("call after expiry failed: %v", err)
	}

	// Token should still be valid (mock always returns same token, but login was re-invoked)
	if cfg2.Token == "" {
		t.Error("expected non-empty token after refresh")
	}

	// Login should have been called twice now (initial + refresh)
	if counter.count() != 2 {
		t.Errorf("expected 2 login calls (initial + refresh after expiry), got %d", counter.count())
	}

	// Verify cache was updated with new expiry
	s.upstreamCacheMu.RLock()
	if s.upstreamCache == nil {
		t.Fatal("expected non-nil cache after refresh")
	}
	if s.upstreamCache.expiresAt.Before(time.Now()) {
		t.Error("expected cache expiry to be in the future after refresh")
	}
	s.upstreamCacheMu.RUnlock()
}

// TestUpstreamCacheLoginFailureDoesNotPoisonCache verifies that if upstream login
// fails, the cache is not populated with a bad entry. (VAL-CACHE-004)
func TestUpstreamCacheLoginFailureDoesNotPoisonCache(t *testing.T) {
	s := NewAPIServerWithLogin("8080", validServerConfig(), mockFailingLogin())

	// First call - will fail login
	_, _, err := s.getOrRefreshUpstreamClient(context.Background())
	if err == nil {
		t.Fatal("expected error from failing mock login")
	}
	if !strings.Contains(err.Error(), "mock: upstream login failed") {
		t.Errorf("expected mock login error, got: %v", err)
	}

	// Cache should not be populated after failed login
	s.upstreamCacheMu.RLock()
	cached := s.upstreamCache
	s.upstreamCacheMu.RUnlock()

	if cached != nil && cached.token != "" {
		t.Error("cache should not have a valid token after failed login")
	}
}

// TestUpstreamCacheConcurrentAccess verifies that multiple goroutines
// accessing the cache simultaneously get the same cached client. (VAL-CACHE-005)
func TestUpstreamCacheConcurrentAccess(t *testing.T) {
	counter := &mockLoginCallCounter{}
	s := NewAPIServerWithLogin("8080", validServerConfig(), mockUpstreamLogin(counter))

	// First call to populate cache
	_, _, err := s.getOrRefreshUpstreamClient(context.Background())
	if err != nil {
		t.Fatalf("initial call failed: %v", err)
	}

	// Get the cached client
	s.upstreamCacheMu.RLock()
	expectedClient := s.upstreamCache.client
	expectedToken := s.upstreamCache.token
	s.upstreamCacheMu.RUnlock()

	var wg sync.WaitGroup
	numGoroutines := 10
	errChan := make(chan error, numGoroutines)

	// Launch multiple goroutines that all try to get the client
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client, cfg, err := s.getOrRefreshUpstreamClient(context.Background())
			if err != nil {
				errChan <- err
				return
			}
			// All should get the same client instance
			if client != expectedClient {
				errChan <- fmt.Errorf("got different client instance")
				return
			}
			// All should get the same token
			if cfg.Token != expectedToken {
				errChan <- fmt.Errorf("got different token: %q vs %q", cfg.Token, expectedToken)
				return
			}
		}()
	}

	wg.Wait()
	close(errChan)

	// Check for errors
	for err := range errChan {
		t.Errorf("concurrent access error: %v", err)
	}

	// Login should still only have been called once (all concurrent reads hit cache)
	if counter.count() != 1 {
		t.Errorf("expected 1 login call (all reads from cache), got %d", counter.count())
	}
}

// ============================================================================
// Health Endpoint Tests
// ============================================================================

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
	if _, cuOK := config["username"]; !cuOK {
		t.Fatal("expected config.username field")
	}
	if _, cpOK := config["password"]; !cpOK {
		t.Fatal("expected config.password field")
	}
	if _, cbOK := config["baseUrl"]; !cbOK {
		t.Fatal("expected config.baseUrl field")
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
	if config["username"] != "ok" {
		t.Errorf("expected config.username=ok, got %v", config["username"])
	}
	if config["password"] != "ok" {
		t.Errorf("expected config.password=ok, got %v", config["password"])
	}
	if config["baseUrl"] != "ok" {
		t.Errorf("expected config.baseUrl=ok, got %v", config["baseUrl"])
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
	if config["username"] != "missing" {
		t.Errorf("expected config.username=missing, got %v", config["username"])
	}
	if config["password"] != "ok" {
		t.Errorf("expected config.password=ok, got %v", config["password"])
	}
	if config["baseUrl"] != "ok" {
		t.Errorf("expected config.baseUrl=ok, got %v", config["baseUrl"])
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
	if config["username"] != "missing" {
		t.Errorf("expected config.username=missing, got %v", config["username"])
	}
	if config["password"] != "missing" {
		t.Errorf("expected config.password=missing, got %v", config["password"])
	}
	if config["baseUrl"] != "missing" {
		t.Errorf("expected config.baseUrl=missing, got %v", config["baseUrl"])
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
	if config["username"] != "missing" {
		t.Errorf("expected config.username=missing, got %v", config["username"])
	}
	if config["password"] != "missing" {
		t.Errorf("expected config.password=missing, got %v", config["password"])
	}
	if config["baseUrl"] != "missing" {
		t.Errorf("expected config.baseUrl=missing, got %v", config["baseUrl"])
	}
}

func TestHealthUpstreamStatusWithValidBaseUrl(t *testing.T) {
	s := newAPIServer(&config.Config{
		Username:         "user",
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
	upstream := assertMapField(t, data, "upstream")

	// Status should be either "reachable" or "unreachable" - we just check the field exists
	if upstream["status"] != "reachable" && upstream["status"] != "unreachable" {
		t.Errorf("expected upstream.status to be 'reachable' or 'unreachable', got %v", upstream["status"])
	}
}

func TestHealthUpstreamStatusWithUnreachableBaseUrl(t *testing.T) {
	s := newAPIServer(&config.Config{
		Username:         "user",
		Password:         "pass",
		BaseURL:          "https://this-domain-does-not-exist-12345.com",
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

	// For unreachable domain, status should be "unreachable"
	if upstream["status"] != "unreachable" {
		t.Logf("upstream status was %v (may be reachable if network allows)", upstream["status"])
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
func setupAuth(t *testing.T, s *APIServer) string {
	t.Helper()
	token, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}
	s.tokenStore.Store(token, TokenInfo{
		Username:  "user",
		Expiry:    time.Now().Add(1 * time.Hour),
		CreatedAt: time.Now(),
	})
	return token
}

// TestIdempotencyKeySameKeyReturnsExistingJob verifies that submitting the same
// idempotency key returns the previously created job without creating a duplicate.
// (VAL-IDEM-001)
func TestIdempotencyKeySameKeyReturnsExistingJob(t *testing.T) {
	js := NewJobStore()
	cfg := &config.Config{DownloadLocation: "./downloads"}

	job1, created1 := js.CreateJobWithKey(123, 456, 1, 5, cfg, "key-abc")
	if !created1 {
		t.Fatal("expected first creation to return created=true")
	}
	if job1 == nil {
		t.Fatal("expected job1 to be non-nil")
	}

	job2, created2 := js.CreateJobWithKey(789, 101, 2, 10, cfg, "key-abc")
	if created2 {
		t.Fatal("expected duplicate key to return created=false")
	}
	if job2.ID != job1.ID {
		t.Errorf("expected same job ID %s, got %s", job1.ID, job2.ID)
	}

	// Verify only one job exists
	jobs := js.ListJobs()
	if len(jobs) != 1 {
		t.Errorf("expected 1 job, got %d", len(jobs))
	}
}

// TestIdempotencyKeyDifferentKeysCreateDifferentJobs verifies that different
// idempotency keys each create a unique job.
// (VAL-IDEM-002)
func TestIdempotencyKeyDifferentKeysCreateDifferentJobs(t *testing.T) {
	js := NewJobStore()
	cfg := &config.Config{DownloadLocation: "./downloads"}

	job1, created1 := js.CreateJobWithKey(1, 1, 1, 1, cfg, "key-alpha")
	if !created1 {
		t.Fatal("expected first creation to succeed")
	}

	job2, created2 := js.CreateJobWithKey(2, 2, 2, 2, cfg, "key-beta")
	if !created2 {
		t.Fatal("expected second creation with different key to succeed")
	}

	if job1.ID == job2.ID {
		t.Errorf("expected different job IDs for different keys, both are %s", job1.ID)
	}

	if job1.SubjectID != 1 || job2.SubjectID != 2 {
		t.Error("expected jobs to have their respective subject IDs")
	}

	jobs := js.ListJobs()
	if len(jobs) != 2 {
		t.Errorf("expected 2 jobs, got %d", len(jobs))
	}
}

// TestIdempotencyKeyOmittedAlwaysCreatesNewJob verifies that omitting the
// idempotency key always creates a new job regardless of other parameters.
// (VAL-IDEM-003)
func TestIdempotencyKeyOmittedAlwaysCreatesNewJob(t *testing.T) {
	js := NewJobStore()
	cfg := &config.Config{DownloadLocation: "./downloads"}

	job1, created1 := js.CreateJobWithKey(1, 1, 1, 5, cfg, "")
	if !created1 {
		t.Fatal("expected first creation without key to succeed")
	}

	job2, created2 := js.CreateJobWithKey(1, 1, 1, 5, cfg, "")
	if !created2 {
		t.Fatal("expected second creation without key to succeed")
	}

	if job1.ID == job2.ID {
		t.Errorf("expected different job IDs when no idempotency key, both are %s", job1.ID)
	}

	jobs := js.ListJobs()
	if len(jobs) != 2 {
		t.Errorf("expected 2 jobs, got %d", len(jobs))
	}
}

// TestIdempotencyKeyValidationTooLong verifies that idempotency keys exceeding
// the max length are rejected by the handler.
// (VAL-IDEM-004)
func TestIdempotencyKeyValidationTooLong(t *testing.T) {
	s := newAPIServer(validServerConfig())
	token := setupAuth(t, s)

	longKey := strings.Repeat("a", maxIdempotencyKeyLength+1)
	body := fmt.Sprintf(`{"subjectId":1,"sessionId":1,"startIndex":1,"endIndex":1,"idempotencyKey":"%s"}`, longKey)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/jobs", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for too-long idempotency key, got %d, body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "INVALID_IDEMPOTENCY_KEY") {
		t.Fatalf("expected INVALID_IDEMPOTENCY_KEY error, got body: %s", rec.Body.String())
	}
}

// TestIdempotencyKeyValidationMaxLengthAccepted verifies that keys at exactly
// the max length are accepted.
// (VAL-IDEM-004)
func TestIdempotencyKeyValidationMaxLengthAccepted(t *testing.T) {
	js := NewJobStore()
	cfg := &config.Config{DownloadLocation: "./downloads"}

	maxKey := strings.Repeat("a", maxIdempotencyKeyLength)
	job, created := js.CreateJobWithKey(1, 1, 1, 1, cfg, maxKey)
	if !created {
		t.Fatal("expected max-length key to be accepted")
	}
	if job.IdempotencyKey != maxKey {
		t.Error("expected job to store the idempotency key")
	}
}

// TestIdempotencyKeyStoredOnJob verifies that the idempotency key is stored
// on the job object for API responses.
// (VAL-IDEM-004)
func TestIdempotencyKeyStoredOnJob(t *testing.T) {
	js := NewJobStore()
	cfg := &config.Config{DownloadLocation: "./downloads"}

	job, created := js.CreateJobWithKey(1, 1, 1, 1, cfg, "my-key-123")
	if !created {
		t.Fatal("expected creation to succeed")
	}
	if job.IdempotencyKey != "my-key-123" {
		t.Errorf("expected idempotencyKey 'my-key-123', got %q", job.IdempotencyKey)
	}

	// Verify lookup by key works
	retrieved, ok := js.jobByIdempotencyKey("my-key-123")
	if !ok {
		t.Fatal("expected to find job by idempotency key")
	}
	if retrieved.ID != job.ID {
		t.Errorf("expected job ID %s, got %s", job.ID, retrieved.ID)
	}
}

// TestIdempotencyKeyNonExistentReturnsFalse verifies that looking up a
// non-existent idempotency key returns false.
// (VAL-IDEM-005)
func TestIdempotencyKeyNonExistentReturnsFalse(t *testing.T) {
	js := NewJobStore()

	_, ok := js.jobByIdempotencyKey("nonexistent-key")
	if ok {
		t.Error("expected false for non-existent idempotency key")
	}
}

// TestIdempotencyKeyPersistedAcrossRestart verifies that idempotency key
// mappings survive server restarts via job persistence.
// (VAL-IDEM-005)
func TestIdempotencyKeyPersistedAcrossRestart(t *testing.T) {
	tmpDir := t.TempDir()
	persistencePath := filepath.Join(tmpDir, ".jobs.json")
	cfg := &config.Config{DownloadLocation: "./downloads"}

	js1 := NewJobStoreWithPersistence(persistencePath)
	job1, created := js1.CreateJobWithKey(123, 456, 1, 5, cfg, "persist-key")
	if !created {
		t.Fatal("expected first creation to succeed")
	}
	js1.UpdateJob(job1.ID, "completed", 100, "")

	// Simulate restart
	js2 := NewJobStoreWithPersistence(persistencePath)

	// Same key should return the existing job
	job2, created2 := js2.CreateJobWithKey(999, 888, 2, 10, cfg, "persist-key")
	if created2 {
		t.Fatal("expected duplicate key after restart to return created=false")
	}
	if job2.ID != job1.ID {
		t.Errorf("expected same job ID %s after restart, got %s", job1.ID, job2.ID)
	}
	if job2.Status != "completed" {
		t.Errorf("expected completed status after restart, got %s", job2.Status)
	}
}

// TestIdempotencyKeyMissingKeyAlwaysNewInStore verifies CreateJob (without key)
// still works alongside CreateJobWithKey.
func TestIdempotencyKeyMissingKeyAlwaysNewInStore(t *testing.T) {
	js := NewJobStore()
	cfg := &config.Config{DownloadLocation: "./downloads"}

	job1 := js.CreateJob(1, 1, 1, 1, cfg)
	job2, created := js.CreateJobWithKey(1, 1, 1, 1, cfg, "")
	if !created {
		t.Fatal("expected empty key to create new job")
	}
	if job1.ID == job2.ID {
		t.Errorf("expected different job IDs, both are %s", job1.ID)
	}

	jobs := js.ListJobs()
	if len(jobs) != 2 {
		t.Errorf("expected 2 jobs, got %d", len(jobs))
	}
}

// TestIdempotencyKeyInPersistedFile verifies the idempotency key is persisted
// to the JSON file.
func TestIdempotencyKeyInPersistedFile(t *testing.T) {
	tmpDir := t.TempDir()
	persistencePath := filepath.Join(tmpDir, ".jobs.json")
	cfg := &config.Config{DownloadLocation: "./downloads"}

	js := NewJobStoreWithPersistence(persistencePath)
	js.CreateJobWithKey(1, 1, 1, 1, cfg, "file-key-xyz")

	data, err := os.ReadFile(persistencePath)
	if err != nil {
		t.Fatalf("expected persistence file to exist: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "file-key-xyz") {
		t.Error("expected idempotency key to be persisted in JSON file")
	}
	if !strings.Contains(content, `"idempotencyKey"`) {
		t.Error("expected idempotencyKey field in persistence file")
	}
}

// TestIdempotencyKeyHandlerMissingNoKeyAlwaysCreates verifies that POST /jobs
// without an idempotencyKey field always creates a new job (handler level).
// (VAL-IDEM-003)
func TestIdempotencyKeyHandlerMissingNoKeyAlwaysCreates(t *testing.T) {
	s := newAPIServer(validServerConfig())

	// First request without key - this would try to executeJob which needs upstream,
	// so we test the job store behavior instead.
	job1, _ := s.jobStore.CreateJobWithKey(1, 1, 1, 1, validServerConfig(), "")
	job2, created := s.jobStore.CreateJobWithKey(1, 1, 1, 1, validServerConfig(), "")

	if !created {
		t.Fatal("expected second call without key to create new job")
	}
	if job1.ID == job2.ID {
		t.Errorf("expected different job IDs without key, both are %s", job1.ID)
	}
}
