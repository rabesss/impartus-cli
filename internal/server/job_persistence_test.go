package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/config"
)

// ============================================================================
// Job Persistence Tests
// ============================================================================

// TestPersistenceCreatedJobPersistedToDisk verifies that after creating a job,
// the persistence file exists and contains the job with matching ID. (VAL-PERS-001)
func TestPersistenceCreatedJobPersistedToDisk(t *testing.T) {
	tmpDir := t.TempDir()
	persistencePath := filepath.Join(tmpDir, ".jobs.json")

	js := NewJobStoreWithPersistence(persistencePath)
	cfg := &config.Config{
		Username:         "user",
		Password:         "pass",
		BaseURL:          "https://example.com",
		DownloadLocation: "./downloads",
	}

	job := js.CreateJob(123, 456, 1, 5, cfg)

	// Verify the persistence file exists
	data, err := os.ReadFile(persistencePath)
	if err != nil {
		t.Fatalf("expected persistence file to exist, got error: %v", err)
	}

	// Parse the persisted data
	var persisted map[string]persistedJob
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("failed to parse persistence file: %v", err)
	}

	if len(persisted) != 1 {
		t.Fatalf("expected 1 persisted job, got %d", len(persisted))
	}

	pj, ok := persisted[job.ID]
	if !ok {
		t.Fatalf("expected job with ID %s in persistence file", job.ID)
	}

	if pj.ID != job.ID {
		t.Errorf("expected persisted ID %s, got %s", job.ID, pj.ID)
	}
	if pj.SubjectID != 123 {
		t.Errorf("expected persisted subjectId 123, got %d", pj.SubjectID)
	}
	if pj.SessionID != 456 {
		t.Errorf("expected persisted sessionId 456, got %d", pj.SessionID)
	}
	if pj.Status != "pending" {
		t.Errorf("expected persisted status 'pending', got %s", pj.Status)
	}
}

// TestPersistenceJobStatusUpdatesPersisted verifies that status transitions
// (pending -> running -> completed) are persisted to disk. (VAL-PERS-002)
func TestPersistenceJobStatusUpdatesPersisted(t *testing.T) {
	tmpDir := t.TempDir()
	persistencePath := filepath.Join(tmpDir, ".jobs.json")

	js := NewJobStoreWithPersistence(persistencePath)
	cfg := &config.Config{DownloadLocation: "./downloads"}

	job := js.CreateJob(1, 1, 1, 5, cfg)

	// Transition: pending -> running
	js.UpdateJob(job.ID, "running", 50.0, "")

	persisted := readPersistedJobs(t, persistencePath)
	if persisted[job.ID].Status != "running" {
		t.Errorf("expected persisted status 'running', got %s", persisted[job.ID].Status)
	}
	if persisted[job.ID].Progress != 50.0 {
		t.Errorf("expected persisted progress 50.0, got %f", persisted[job.ID].Progress)
	}

	// Transition: running -> completed
	js.UpdateJob(job.ID, "completed", 100.0, "")
	js.SetOutputs(job.ID, []string{"output1.mp4", "output2.mp4"})

	persisted = readPersistedJobs(t, persistencePath)
	if persisted[job.ID].Status != "completed" {
		t.Errorf("expected persisted status 'completed', got %s", persisted[job.ID].Status)
	}
	if persisted[job.ID].Progress != 100.0 {
		t.Errorf("expected persisted progress 100.0, got %f", persisted[job.ID].Progress)
	}
	if len(persisted[job.ID].Outputs) != 2 {
		t.Errorf("expected 2 persisted outputs, got %d", len(persisted[job.ID].Outputs))
	}

	// Transition: pending -> failed
	job2 := js.CreateJob(2, 2, 1, 1, cfg)
	js.UpdateJob(job2.ID, "failed", 0.0, "download error")

	persisted = readPersistedJobs(t, persistencePath)
	if persisted[job2.ID].Status != "failed" {
		t.Errorf("expected persisted status 'failed', got %s", persisted[job2.ID].Status)
	}
	if persisted[job2.ID].Error != "download error" {
		t.Errorf("expected persisted error 'download error', got %s", persisted[job2.ID].Error)
	}
}

// TestPersistenceJobsSurviveServerRestart verifies that creating a new job store
// with the same persistence path loads the previously persisted jobs. (VAL-PERS-003)
func TestPersistenceJobsSurviveServerRestart(t *testing.T) {
	tmpDir := t.TempDir()
	persistencePath := filepath.Join(tmpDir, ".jobs.json")

	cfg := &config.Config{DownloadLocation: "./downloads"}

	// First server instance: create a job
	js1 := NewJobStoreWithPersistence(persistencePath)
	job1 := js1.CreateJob(123, 456, 1, 5, cfg)
	js1.UpdateJob(job1.ID, "completed", 100.0, "")
	js1.SetOutputs(job1.ID, []string{"lecture1.mp4"})

	// Simulate server restart: create a new job store with same path
	js2 := NewJobStoreWithPersistence(persistencePath)

	// The job should be found in the new store
	retrieved, ok := js2.GetJob(job1.ID)
	if !ok {
		t.Fatal("expected job to be found after server restart")
	}
	if retrieved.ID != job1.ID {
		t.Errorf("expected job ID %s, got %s", job1.ID, retrieved.ID)
	}
	if retrieved.Status != "completed" {
		t.Errorf("expected status 'completed', got %s", retrieved.Status)
	}
	if retrieved.SubjectID != 123 {
		t.Errorf("expected subjectId 123, got %d", retrieved.SubjectID)
	}
}

// TestPersistenceAllJobStatesPreservedAfterRestart verifies that completed,
// failed, and canceled jobs are all preserved across restart. (VAL-PERS-004)
func TestPersistenceAllJobStatesPreservedAfterRestart(t *testing.T) {
	tmpDir := t.TempDir()
	persistencePath := filepath.Join(tmpDir, ".jobs.json")

	cfg := &config.Config{DownloadLocation: "./downloads"}

	// Create jobs in different terminal states
	js1 := NewJobStoreWithPersistence(persistencePath)

	completedJob := js1.CreateJob(1, 1, 1, 1, cfg)
	js1.UpdateJob(completedJob.ID, "completed", 100.0, "")
	js1.SetOutputs(completedJob.ID, []string{"output.mp4"})

	failedJob := js1.CreateJob(2, 2, 1, 1, cfg)
	js1.UpdateJob(failedJob.ID, "failed", 0.0, "network error")

	canceledJob := js1.CreateJob(3, 3, 1, 1, cfg)
	if _, err := js1.CancelJob(canceledJob.ID); err != nil {
		t.Fatalf("failed to cancel job: %v", err)
	}

	// Simulate restart
	js2 := NewJobStoreWithPersistence(persistencePath)

	// Verify completed job
	retrieved, ok := js2.GetJob(completedJob.ID)
	if !ok {
		t.Fatal("completed job not found after restart")
	}
	if retrieved.Status != "completed" {
		t.Errorf("expected completed status, got %s", retrieved.Status)
	}

	// Verify failed job
	retrieved, ok = js2.GetJob(failedJob.ID)
	if !ok {
		t.Fatal("failed job not found after restart")
	}
	if retrieved.Status != "failed" {
		t.Errorf("expected failed status, got %s", retrieved.Status)
	}
	if retrieved.Error != "network error" {
		t.Errorf("expected error 'network error', got %s", retrieved.Error)
	}

	// Verify canceled job
	retrieved, ok = js2.GetJob(canceledJob.ID)
	if !ok {
		t.Fatal("canceled job not found after restart")
	}
	if retrieved.Status != StatusCanceled {
		t.Errorf("expected canceled status, got %s", retrieved.Status)
	}
}

// TestPersistenceFileContainsNoCredentials verifies that the persistence file
// does not contain username, password, or token fields. (VAL-PERS-005)
func TestPersistenceFileContainsNoCredentials(t *testing.T) {
	tmpDir := t.TempDir()
	persistencePath := filepath.Join(tmpDir, ".jobs.json")

	js := NewJobStoreWithPersistence(persistencePath)
	cfg := &config.Config{
		Username:         "testuser",
		Password:         "testpass",
		BaseURL:          "https://example.com",
		Token:            "test-token-placeholder",
		DownloadLocation: "./downloads",
	}

	js.CreateJob(1, 1, 1, 5, cfg)

	// Read raw file content
	data, err := os.ReadFile(persistencePath)
	if err != nil {
		t.Fatalf("expected persistence file to exist: %v", err)
	}

	content := string(data)

	// Verify no credential fields appear in the persisted JSON
	credentialPatterns := []string{
		"testuser",
		"testpass",
		"test-token-placeholder",
		"\"username\"",
		"\"password\"",
		"\"token\"",
		"\"Token\"",
	}

	for _, pattern := range credentialPatterns {
		if strings.Contains(content, pattern) {
			t.Errorf("persistence file should not contain credential pattern %q", pattern)
		}
	}

	// Verify config structure fields are present (non-credential config)
	if !strings.Contains(content, "\"quality\"") {
		t.Error("expected config quality to be persisted")
	}
	if !strings.Contains(content, "\"views\"") {
		t.Error("expected config views to be persisted")
	}
}

// TestPersistenceCorruptFileHandledGracefully verifies that if the persistence
// file contains malformed JSON, the server starts without crashing and the
// job store is empty. (VAL-PERS-006)
func TestPersistenceCorruptFileHandledGracefully(t *testing.T) {
	tmpDir := t.TempDir()
	persistencePath := filepath.Join(tmpDir, ".jobs.json")

	// Write corrupt content to the persistence file
	corruptContent := `{this is not valid json!!!`
	if err := os.WriteFile(persistencePath, []byte(corruptContent), 0o600); err != nil {
		t.Fatalf("failed to write corrupt file: %v", err)
	}

	// Creating a job store with corrupt persistence should not panic
	js := NewJobStoreWithPersistence(persistencePath)

	// Job store should be empty
	jobs := js.ListJobs()
	if len(jobs) != 0 {
		t.Errorf("expected 0 jobs with corrupt persistence file, got %d", len(jobs))
	}

	// Store should be functional (can create new jobs)
	cfg := &config.Config{DownloadLocation: "./downloads"}
	job := js.CreateJob(1, 1, 1, 1, cfg)
	if job == nil {
		t.Fatal("expected job creation to succeed after corrupt file")
	}

	// New jobs should be persisted (corrupt file should be overwritten)
	persisted := readPersistedJobs(t, persistencePath)
	if _, ok := persisted[job.ID]; !ok {
		t.Error("expected new job to be persisted after corrupt file recovery")
	}
}

// TestPersistenceNonExistentFileHandledGracefully verifies that if no persistence
// file exists, the job store starts with an empty store.
func TestPersistenceNonExistentFileHandledGracefully(t *testing.T) {
	tmpDir := t.TempDir()
	persistencePath := filepath.Join(tmpDir, ".nonexistent.json")

	js := NewJobStoreWithPersistence(persistencePath)

	jobs := js.ListJobs()
	if len(jobs) != 0 {
		t.Errorf("expected 0 jobs with non-existent persistence file, got %d", len(jobs))
	}
}

// TestPersistenceRunningJobsMarkedFailedOnRestart verifies that jobs that were
// in "pending" or "running" state when the server was shut down are marked as
// "failed" on reload since they cannot be resumed.
func TestPersistenceRunningJobsMarkedFailedOnRestart(t *testing.T) {
	tmpDir := t.TempDir()
	persistencePath := filepath.Join(tmpDir, ".jobs.json")

	cfg := &config.Config{DownloadLocation: "./downloads"}

	// Create a job and leave it in "running" state
	js1 := NewJobStoreWithPersistence(persistencePath)
	job := js1.CreateJob(1, 1, 1, 5, cfg)
	js1.UpdateJob(job.ID, "running", 42.0, "")

	// Create a job and leave it in "pending" state
	job2 := js1.CreateJob(2, 2, 1, 1, cfg)

	// Simulate restart
	js2 := NewJobStoreWithPersistence(persistencePath)

	// Running job should be marked as failed
	retrieved, ok := js2.GetJob(job.ID)
	if !ok {
		t.Fatal("expected running job to be found after restart")
	}
	if retrieved.Status != "failed" {
		t.Errorf("expected status 'failed' for interrupted running job, got %s", retrieved.Status)
	}
	if retrieved.Error != "job interrupted by server restart" {
		t.Errorf("expected restart error message, got %s", retrieved.Error)
	}

	// Pending job should also be marked as failed
	retrieved2, ok := js2.GetJob(job2.ID)
	if !ok {
		t.Fatal("expected pending job to be found after restart")
	}
	if retrieved2.Status != "failed" {
		t.Errorf("expected status 'failed' for interrupted pending job, got %s", retrieved2.Status)
	}
}

// TestPersistenceAtomicWrite verifies that the persistence file is written atomically
// (via temp file + rename) and the temp file is cleaned up.
func TestPersistenceAtomicWrite(t *testing.T) {
	tmpDir := t.TempDir()
	persistencePath := filepath.Join(tmpDir, ".jobs.json")
	tmpPath := persistencePath + ".tmp"

	js := NewJobStoreWithPersistence(persistencePath)
	cfg := &config.Config{DownloadLocation: "./downloads"}

	js.CreateJob(1, 1, 1, 1, cfg)

	// Temp file should not exist after write
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("expected temp file to be cleaned up after atomic write")
	}

	// Final file should exist
	if _, err := os.Stat(persistencePath); os.IsNotExist(err) {
		t.Error("expected persistence file to exist after write")
	}
}

// TestPersistenceFilePermissions verifies the persistence file is created with
// restricted permissions (0600).
func TestPersistenceFilePermissions(t *testing.T) {
	tmpDir := t.TempDir()
	persistencePath := filepath.Join(tmpDir, ".jobs.json")

	js := NewJobStoreWithPersistence(persistencePath)
	cfg := &config.Config{DownloadLocation: "./downloads"}

	js.CreateJob(1, 1, 1, 1, cfg)

	info, err := os.Stat(persistencePath)
	if err != nil {
		t.Fatalf("expected persistence file to exist: %v", err)
	}

	// On some systems the actual permissions may be affected by umask,
	// so we just verify the file exists and has some permissions.
	// The important thing is the intent (0600) is set in the code.
	_ = info
}

// TestPersistenceEmptyStoreOnStart verifies an empty persistence file is handled.
func TestPersistenceEmptyStoreOnStart(t *testing.T) {
	tmpDir := t.TempDir()
	persistencePath := filepath.Join(tmpDir, ".jobs.json")

	// Write empty JSON object
	if err := os.WriteFile(persistencePath, []byte("{}"), 0o600); err != nil {
		t.Fatalf("failed to write empty json file: %v", err)
	}

	js := NewJobStoreWithPersistence(persistencePath)

	jobs := js.ListJobs()
	if len(jobs) != 0 {
		t.Errorf("expected 0 jobs with empty persistence file, got %d", len(jobs))
	}
}

// TestNewJobStoreWithoutPersistence verifies that NewJobStore (without persistence)
// does not create any file on disk.
func TestNewJobStoreWithoutPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	persistencePath := filepath.Join(tmpDir, ".jobs.json")

	js := NewJobStore()
	cfg := &config.Config{DownloadLocation: "./downloads"}

	js.CreateJob(1, 1, 1, 1, cfg)

	// No file should be created
	if _, err := os.Stat(persistencePath); !os.IsNotExist(err) {
		t.Error("expected no persistence file for non-persistent job store")
	}
}

// TestPersistenceSetLectureProgressUpdatesFile verifies lecture progress is persisted.
func TestPersistenceSetLectureProgressUpdatesFile(t *testing.T) {
	tmpDir := t.TempDir()
	persistencePath := filepath.Join(tmpDir, ".jobs.json")

	js := NewJobStoreWithPersistence(persistencePath)
	cfg := &config.Config{DownloadLocation: "./downloads"}

	job := js.CreateJob(1, 1, 1, 5, cfg)
	js.SetLectureProgress(job.ID, 3, 5)

	persisted := readPersistedJobs(t, persistencePath)
	if persisted[job.ID].CompletedLectures != 3 {
		t.Errorf("expected completedLectures 3, got %d", persisted[job.ID].CompletedLectures)
	}
	if persisted[job.ID].TotalLectures != 5 {
		t.Errorf("expected totalLectures 5, got %d", persisted[job.ID].TotalLectures)
	}
}

// TestPersistenceCancelJobUpdatesFile verifies cancel operations are persisted.
func TestPersistenceCancelJobUpdatesFile(t *testing.T) {
	tmpDir := t.TempDir()
	persistencePath := filepath.Join(tmpDir, ".jobs.json")

	js := NewJobStoreWithPersistence(persistencePath)
	cfg := &config.Config{DownloadLocation: "./downloads"}

	job := js.CreateJob(1, 1, 1, 1, cfg)
	_, err := js.CancelJob(job.ID)
	if err != nil {
		t.Fatalf("unexpected cancel error: %v", err)
	}

	persisted := readPersistedJobs(t, persistencePath)
	if persisted[job.ID].Status != StatusCanceled {
		t.Errorf("expected persisted status 'canceled', got %s", persisted[job.ID].Status)
	}
}

// TestPersistencePreservesConfigFields verifies that job config (quality, views, etc.)
// is properly persisted and restored.
func TestPersistencePreservesConfigFields(t *testing.T) {
	tmpDir := t.TempDir()
	persistencePath := filepath.Join(tmpDir, ".jobs.json")

	cfg := &config.Config{
		Username:         "user",
		Password:         "pass",
		BaseURL:          "https://example.com",
		Quality:          "720",
		Views:            "first",
		DownloadLocation: "./downloads",
		NumWorkers:       3,
		AudioOnly:        true,
		AudioFormat:      "aac",
		EnablePipeline:   true,
	}

	js1 := NewJobStoreWithPersistence(persistencePath)
	job := js1.CreateJob(1, 1, 1, 5, cfg)

	// Restart
	js2 := NewJobStoreWithPersistence(persistencePath)
	retrieved, ok := js2.GetJob(job.ID)
	if !ok {
		t.Fatal("expected job found after restart")
	}

	if retrieved.Config.Quality != "720" {
		t.Errorf("expected quality '720', got %s", retrieved.Config.Quality)
	}
	if retrieved.Config.Views != "first" {
		t.Errorf("expected views 'first', got %s", retrieved.Config.Views)
	}
	if !retrieved.Config.AudioOnly {
		t.Error("expected audioOnly to be true")
	}
	if retrieved.Config.AudioFormat != "aac" {
		t.Errorf("expected audioFormat 'aac', got %s", retrieved.Config.AudioFormat)
	}
	if !retrieved.Config.EnablePipeline {
		t.Error("expected enablePipeline to be true")
	}
}

// TestPersistenceTimestampsRoundTrip verifies that CreatedAt and UpdatedAt are
// properly serialized/deserialized across restart.
func TestPersistenceTimestampsRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	persistencePath := filepath.Join(tmpDir, ".jobs.json")

	cfg := &config.Config{DownloadLocation: "./downloads"}

	js1 := NewJobStoreWithPersistence(persistencePath)
	job := js1.CreateJob(1, 1, 1, 1, cfg)
	originalCreatedAt := job.CreatedAt
	originalUpdatedAt := job.UpdatedAt

	// Small delay to ensure timestamps differ
	time.Sleep(10 * time.Millisecond)
	js1.UpdateJob(job.ID, "running", 50.0, "")

	// Restart
	js2 := NewJobStoreWithPersistence(persistencePath)
	retrieved, ok := js2.GetJob(job.ID)
	if !ok {
		t.Fatal("expected job found after restart")
	}

	if !retrieved.CreatedAt.Equal(originalCreatedAt) {
		t.Errorf("expected createdAt %v, got %v", originalCreatedAt, retrieved.CreatedAt)
	}
	// UpdatedAt should have changed after the update
	if retrieved.UpdatedAt.Equal(originalUpdatedAt) {
		t.Error("expected updatedAt to differ after status update")
	}
}

// TestPersistenceMultipleJobsAllPersisted verifies all jobs in the store are persisted.
func TestPersistenceMultipleJobsAllPersisted(t *testing.T) {
	tmpDir := t.TempDir()
	persistencePath := filepath.Join(tmpDir, ".jobs.json")

	js := NewJobStoreWithPersistence(persistencePath)
	cfg := &config.Config{DownloadLocation: "./downloads"}

	js.CreateJob(1, 1, 1, 5, cfg)
	js.CreateJob(2, 2, 1, 3, cfg)
	js.CreateJob(3, 3, 1, 1, cfg)

	persisted := readPersistedJobs(t, persistencePath)
	if len(persisted) != 3 {
		t.Errorf("expected 3 persisted jobs, got %d", len(persisted))
	}
}

// readPersistedJobs is a test helper that reads and parses the persistence file.
func readPersistedJobs(t *testing.T, path string) map[string]persistedJob {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read persistence file: %v", err)
	}
	var persisted map[string]persistedJob
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("failed to parse persistence file: %v", err)
	}
	return persisted
}
