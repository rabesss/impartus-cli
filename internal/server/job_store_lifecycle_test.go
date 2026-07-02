package server

import (
	"errors"
	"sync"
	"testing"

	"github.com/rabesss/impartus-cli/internal/config"
)

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
	if !errors.Is(err, ErrJobNotFound) {
		t.Errorf("expected ErrJobNotFound, got %v", err)
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
	var terminalErr *TerminalStatusError
	if !errors.As(err, &terminalErr) {
		t.Fatalf("expected TerminalStatusError, got %v", err)
	}
	if terminalErr.Status != StatusCanceled {
		t.Fatalf("expected canceled terminal status, got %s", terminalErr.Status)
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
