package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rabesss/impartus-cli/internal/config"
)

func TestJobStore_CreateAndGet(t *testing.T) {
	store := NewJobStore()
	cfg := &config.Config{Quality: "360p"}

	job := store.CreateJob(1, 2, 1, 10, cfg)
	if job == nil {
		t.Fatal("expected non-nil job")
	}
	if job.SubjectID != 1 {
		t.Errorf("SubjectID = %d, want 1", job.SubjectID)
	}
	if job.SessionID != 2 {
		t.Errorf("SessionID = %d, want 2", job.SessionID)
	}
	if job.Status != StatusPending {
		t.Errorf("Status = %q, want %q", job.Status, StatusPending)
	}

	got, ok := store.GetJob(job.ID)
	if !ok {
		t.Fatal("expected to find job")
	}
	if got.ID != job.ID {
		t.Errorf("GetJob ID = %q, want %q", got.ID, job.ID)
	}
}

func TestJobStore_ListJobs(t *testing.T) {
	store := NewJobStore()
	cfg := &config.Config{}

	store.CreateJob(1, 2, 1, 10, cfg)
	store.CreateJob(3, 4, 1, 5, cfg)

	jobs := store.ListJobs()
	if len(jobs) != 2 {
		t.Fatalf("ListJobs returned %d jobs, want 2", len(jobs))
	}
}

func TestJobStore_UpdateJob(t *testing.T) {
	store := NewJobStore()
	cfg := &config.Config{}
	job := store.CreateJob(1, 2, 1, 10, cfg)

	store.UpdateJob(job.ID, StatusRunning, 50.0, "")

	got, _ := store.GetJob(job.ID)
	if got.Status != StatusRunning {
		t.Errorf("Status = %q, want %q", got.Status, StatusRunning)
	}
	if got.Progress != 50.0 {
		t.Errorf("Progress = %f, want 50.0", got.Progress)
	}
}

func TestJobStore_CancelJob(t *testing.T) {
	store := NewJobStore()
	cfg := &config.Config{}
	job := store.CreateJob(1, 2, 1, 10, cfg)

	canceled, err := store.CancelJob(job.ID)
	if err != nil {
		t.Fatalf("CancelJob error: %v", err)
	}
	if canceled.Status != StatusCanceled {
		t.Errorf("Status = %q, want %q", canceled.Status, StatusCanceled)
	}
}

func TestJobStore_CancelTerminalJob(t *testing.T) {
	store := NewJobStore()
	cfg := &config.Config{}
	job := store.CreateJob(1, 2, 1, 10, cfg)

	store.UpdateJob(job.ID, StatusCompleted, 100.0, "")
	_, err := store.CancelJob(job.ID)
	if err == nil {
		t.Error("expected error canceling terminal job")
	}
}

func TestJobStore_CancelNotFound(t *testing.T) {
	store := NewJobStore()
	_, err := store.CancelJob("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent job")
	}
}

func TestJobStore_SetLectureProgress(t *testing.T) {
	store := NewJobStore()
	cfg := &config.Config{}
	job := store.CreateJob(1, 2, 1, 10, cfg)

	store.SetLectureProgress(job.ID, 3, 5)
	got, _ := store.GetJob(job.ID)
	if got.CompletedLectures != 3 {
		t.Errorf("CompletedLectures = %d, want 3", got.CompletedLectures)
	}
	if got.TotalLectures != 5 {
		t.Errorf("TotalLectures = %d, want 5", got.TotalLectures)
	}
}

func TestJobStore_SetOutputs(t *testing.T) {
	store := NewJobStore()
	cfg := &config.Config{}
	job := store.CreateJob(1, 2, 1, 10, cfg)

	store.SetOutputs(job.ID, []string{"a.mp4", "b.mp4"})
	got, _ := store.GetJob(job.ID)
	if len(got.Outputs) != 2 {
		t.Fatalf("Outputs len = %d, want 2", len(got.Outputs))
	}
	if got.Outputs[0] != "a.mp4" {
		t.Errorf("Outputs[0] = %q, want %q", got.Outputs[0], "a.mp4")
	}
}

func TestJobStore_IdempotencyKey(t *testing.T) {
	store := NewJobStore()
	cfg := &config.Config{}

	job1, created1 := store.CreateJobWithKey(1, 2, 1, 10, cfg, "key-1")
	if !created1 {
		t.Error("expected first job to be newly created")
	}

	job2, created2 := store.CreateJobWithKey(1, 2, 1, 10, cfg, "key-1")
	if created2 {
		t.Error("expected duplicate to return existing job")
	}
	if job2.ID != job1.ID {
		t.Errorf("returned job ID = %q, want %q", job2.ID, job1.ID)
	}

	found, ok := store.GetJobByIdempotencyKey("key-1")
	if !ok {
		t.Fatal("expected to find job by idempotency key")
	}
	if found.ID != job1.ID {
		t.Errorf("found ID = %q, want %q", found.ID, job1.ID)
	}
}

func TestJobStore_EmptyIdempotencyKey(t *testing.T) {
	store := NewJobStore()
	cfg := &config.Config{}

	_, created1 := store.CreateJobWithKey(1, 2, 1, 10, cfg, "")
	_, created2 := store.CreateJobWithKey(1, 2, 1, 10, cfg, "")
	if !created1 || !created2 {
		t.Error("empty key should always create new jobs")
	}
}

func TestJobStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".jobs.json")
	cfg := &config.Config{Quality: "720p"}

	store1 := NewJobStoreWithPersistence(path)
	job := store1.CreateJob(1, 2, 1, 10, cfg)
	store1.UpdateJob(job.ID, StatusCompleted, 100.0, "")

	// Create new store loading from same file
	store2 := NewJobStoreWithPersistence(path)
	got, ok := store2.GetJob(job.ID)
	if !ok {
		t.Fatal("expected persisted job to be loaded")
	}
	if got.SubjectID != 1 {
		t.Errorf("SubjectID = %d, want 1", got.SubjectID)
	}
	if got.Status != StatusCompleted {
		t.Errorf("Status = %q, want %q", got.Status, StatusCompleted)
	}
}

func TestJobStore_PendingBecomesFailedOnLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".jobs.json")
	cfg := &config.Config{}

	store1 := NewJobStoreWithPersistence(path)
	job := store1.CreateJob(1, 2, 1, 10, cfg)
	// Job is still pending, should become failed on reload
	_ = job

	store2 := NewJobStoreWithPersistence(path)
	got, _ := store2.GetJob(job.ID)
	if got.Status != StatusFailed {
		t.Errorf("pending job on reload: Status = %q, want %q", got.Status, StatusFailed)
	}
	if got.Error == "" {
		t.Error("expected error message for interrupted job")
	}
}

func TestJobStore_NoPersistence(t *testing.T) {
	store := NewJobStore()
	cfg := &config.Config{}
	store.CreateJob(1, 2, 1, 10, cfg)

	// No file should exist
	if _, err := os.Stat(".jobs.json"); !os.IsNotExist(err) {
		t.Error("expected no .jobs.json file for in-memory store")
	}
}
