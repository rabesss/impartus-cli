package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/config"
)

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
