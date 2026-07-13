package server

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/config"
)

func TestPersistenceSnapshotIsImmutable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	store := newTestPersistentStore(t, path)
	job := store.CreateJob(1, 2, 1, 3, &config.Config{DownloadLocation: "downloads"})
	store.SetOutputs(job.ID, []string{"snapshot.mp4"})

	store.mu.Lock()
	revision, snapshot := store.snapshotLocked()
	store.jobs[job.ID].Outputs[0] = "mutated.mp4"
	store.jobs[job.ID].Status = StatusFailed
	store.mu.Unlock()

	if err := store.publishAndFlush(revision, snapshot); err != nil {
		t.Fatalf("persist immutable snapshot: %v", err)
	}
	persisted := readPersistedJobs(t, path)
	if got := persisted[job.ID].Outputs; len(got) != 1 || got[0] != "snapshot.mp4" {
		t.Fatalf("snapshot outputs changed through live alias: %v", got)
	}
	if persisted[job.ID].Status != StatusPending {
		t.Fatalf("snapshot status = %q, want pending", persisted[job.ID].Status)
	}
}

func TestPersistenceCoordinatorCoalescesProgress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	store := newTestPersistentStore(t, path)
	job := store.CreateJob(1, 2, 1, 500, &config.Config{DownloadLocation: "downloads"})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := store.coordinator.close(ctx); err != nil {
		t.Fatalf("close initial coordinator: %v", err)
	}
	var saves atomic.Int64
	store.coordinator = newPersistenceCoordinator(func(snapshot map[string]persistedJob) error {
		saves.Add(1)
		return store.persistence.save(snapshot)
	})

	for completed := 1; completed <= 500; completed++ {
		store.SetLectureProgress(job.ID, completed, 500)
	}
	flushTestStore(t, store)

	if got := saves.Load(); got >= 20 {
		t.Fatalf("expected heavily coalesced writes, got %d saves for 500 updates", got)
	}
	persisted := readPersistedJobs(t, path)
	if got := persisted[job.ID].CompletedLectures; got != 500 {
		t.Fatalf("persisted completed lectures = %d, want 500", got)
	}
}

func TestPersistenceCoordinatorNeverOverwritesNewerRevision(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var mu sync.Mutex
	written := make([]string, 0, 2)
	coordinator := newPersistenceCoordinator(func(snapshot map[string]persistedJob) error {
		value := snapshot["job"].Error
		if value == "revision-1" {
			close(firstStarted)
			<-releaseFirst
		}
		mu.Lock()
		written = append(written, value)
		mu.Unlock()
		return nil
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := coordinator.close(ctx); err != nil {
			t.Errorf("close coordinator: %v", err)
		}
	})

	if err := coordinator.publish(1, map[string]persistedJob{"job": {ID: "job", Error: "revision-1"}}); err != nil {
		t.Fatalf("publish revision 1: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	flushDone := make(chan error, 1)
	go func() { flushDone <- coordinator.flushTo(ctx, 1) }()
	select {
	case <-firstStarted:
	case <-ctx.Done():
		t.Fatal("first persistence write did not start")
	}
	if err := coordinator.publish(2, map[string]persistedJob{"job": {ID: "job", Error: "revision-2"}}); err != nil {
		t.Fatalf("publish revision 2: %v", err)
	}
	close(releaseFirst)
	if err := <-flushDone; err != nil {
		t.Fatalf("flush revision 1: %v", err)
	}
	if err := coordinator.flushTo(ctx, 2); err != nil {
		t.Fatalf("flush revision 2: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(written) != 2 || written[0] != "revision-1" || written[1] != "revision-2" {
		t.Fatalf("write order = %v, want [revision-1 revision-2]", written)
	}
}

func TestPersistenceDurabilityPointsSurviveImmediateRestart(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*JobStore, string) error
		status JobStatus
		output string
	}{
		{name: "created", mutate: func(_ *JobStore, _ string) error { return nil }, status: StatusFailed},
		{name: "canceled", mutate: func(store *JobStore, id string) error { _, err := store.CancelJob(id); return err }, status: StatusCanceled},
		{name: "completed", mutate: func(store *JobStore, id string) error { return store.CompleteJob(id, []string{"final.mp4"}) }, status: StatusCompleted, output: "final.mp4"},
		{name: "failed", mutate: func(store *JobStore, id string) error {
			return store.updateJobDurable(id, StatusFailed, 0, "expected failure")
		}, status: StatusFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "jobs.json")
			store := newTestPersistentStore(t, path)
			job := store.CreateJob(1, 2, 1, 3, &config.Config{DownloadLocation: "downloads"})
			if err := tt.mutate(store, job.ID); err != nil {
				t.Fatalf("durable mutation: %v", err)
			}

			restarted := newTestPersistentStore(t, path)
			got, ok := restarted.CopyJob(job.ID)
			if !ok {
				t.Fatal("job missing after immediate restart")
			}
			if got.Status != tt.status {
				t.Fatalf("status after restart = %q, want %q", got.Status, tt.status)
			}
			if tt.output != "" && (len(got.Outputs) != 1 || got.Outputs[0] != tt.output) {
				t.Fatalf("outputs after restart = %v, want %q", got.Outputs, tt.output)
			}
		})
	}
}

func TestJobStoreRetentionPreservesNewestTerminalAndActiveJobs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	store := newTestPersistentStore(t, path)
	base := time.Date(2026, time.July, 13, 0, 0, 0, 0, time.UTC)

	store.mu.Lock()
	for i := 0; i < maxRetainedTerminalJobs+5; i++ {
		id := fmt.Sprintf("terminal-%04d", i)
		ctx, cancel := context.WithCancel(context.Background())
		store.jobs[id] = &Job{
			ID: id, Status: StatusCompleted, Progress: 100,
			IdempotencyKey: "key-" + id, CreatedAt: base.Add(time.Duration(i) * time.Second),
			UpdatedAt: base.Add(time.Duration(i) * time.Second), ctx: ctx, cancel: cancel,
		}
		store.idempotencyKeys["key-"+id] = id
	}
	for i := 0; i < 2; i++ {
		id := fmt.Sprintf("active-%d", i)
		ctx, cancel := context.WithCancel(context.Background())
		store.jobs[id] = &Job{
			ID: id, Status: StatusRunning, IdempotencyKey: "key-" + id,
			CreatedAt: base, UpdatedAt: base, ctx: ctx, cancel: cancel,
		}
		store.idempotencyKeys["key-"+id] = id
	}
	store.pruneTerminalJobsLocked()
	revision, snapshot := store.snapshotLocked()
	store.mu.Unlock()
	if err := store.publishAndFlush(revision, snapshot); err != nil {
		t.Fatalf("persist retained snapshot: %v", err)
	}

	restarted := newTestPersistentStore(t, path)
	jobs := restarted.ListJobCopies()
	if got, want := len(jobs), maxRetainedTerminalJobs+2; got != want {
		t.Fatalf("restored job count = %d, want %d", got, want)
	}
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("terminal-%04d", i)
		if _, ok := restarted.CopyJob(id); ok {
			t.Errorf("old terminal job %s was not pruned", id)
		}
		if _, ok := restarted.jobByIdempotencyKey("key-" + id); ok {
			t.Errorf("idempotency index for pruned job %s remains", id)
		}
	}
	for i := 5; i < maxRetainedTerminalJobs+5; i++ {
		id := fmt.Sprintf("terminal-%04d", i)
		if _, ok := restarted.CopyJob(id); !ok {
			t.Fatalf("retained terminal job %s missing", id)
		}
	}
	for i := 0; i < 2; i++ {
		id := fmt.Sprintf("active-%d", i)
		job, ok := restarted.CopyJob(id)
		if !ok {
			t.Fatalf("active job %s missing", id)
		}
		if job.Status != StatusFailed {
			t.Fatalf("restored active job status = %q, want failed", job.Status)
		}
		if _, ok := restarted.jobByIdempotencyKey("key-" + id); !ok {
			t.Fatalf("active job idempotency index %s missing", id)
		}
	}
}

func TestPersistenceLoadPrunesTerminalJobsUsingIDTieBreaker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	stamp := time.Date(2026, time.July, 13, 0, 0, 0, 0, time.UTC).Format(persistedTimeFormat)
	snapshot := make(map[string]persistedJob, maxRetainedTerminalJobs+3)
	for i := 0; i < maxRetainedTerminalJobs+2; i++ {
		id := fmt.Sprintf("terminal-%04d", i)
		snapshot[id] = persistedJob{
			ID: id, Status: StatusCompleted, Progress: 100,
			IdempotencyKey: "key-" + id, CreatedAt: stamp, UpdatedAt: stamp,
		}
	}
	snapshot["active"] = persistedJob{
		ID: "active", Status: StatusRunning, IdempotencyKey: "key-active",
		CreatedAt: stamp, UpdatedAt: stamp,
	}
	if err := newJobPersistence(path).save(snapshot); err != nil {
		t.Fatalf("seed oversized persistence file: %v", err)
	}

	store := newTestPersistentStore(t, path)
	if got, want := len(store.ListJobCopies()), maxRetainedTerminalJobs+1; got != want {
		t.Fatalf("loaded job count = %d, want %d", got, want)
	}
	for _, id := range []string{"terminal-0000", "terminal-0001"} {
		if _, ok := store.CopyJob(id); ok {
			t.Errorf("tie-breaker did not prune %s", id)
		}
		if _, ok := store.jobByIdempotencyKey("key-" + id); ok {
			t.Errorf("pruned idempotency entry remains for %s", id)
		}
	}
	active, ok := store.CopyJob("active")
	if !ok {
		t.Fatal("active job was pruned while loading")
	}
	if active.Status != StatusFailed {
		t.Fatalf("active job restart status = %q, want failed", active.Status)
	}
	if _, ok := store.jobByIdempotencyKey("key-active"); !ok {
		t.Fatal("active job idempotency entry missing")
	}
	flushTestStore(t, store)
	if got := len(readPersistedJobs(t, path)); got != maxRetainedTerminalJobs+1 {
		t.Fatalf("pruned persistence file contains %d jobs, want %d", got, maxRetainedTerminalJobs+1)
	}
}

func TestJobStoreConcurrentMutationFlushAndClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	store := NewJobStoreWithPersistence(path)
	cfg := &config.Config{DownloadLocation: "downloads"}
	jobs := make([]*Job, 20)
	for i := range jobs {
		jobs[i] = store.CreateJob(i+1, i+1, 1, 100, cfg)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, job := range jobs {
		wg.Add(1)
		go func(index int, id string) {
			defer wg.Done()
			<-start
			for progress := 1; progress <= 100; progress++ {
				store.SetLectureProgress(id, progress, 100)
				_ = store.ListJobCopies()
			}
			if index%2 == 0 {
				if err := store.CompleteJob(id, []string{fmt.Sprintf("%d.mp4", index)}); err != nil {
					t.Errorf("complete job: %v", err)
				}
			} else if err := store.updateJobDurable(id, StatusFailed, 0, "expected"); err != nil {
				t.Errorf("fail job: %v", err)
			}
		}(i, job.ID)
	}
	close(start)
	wg.Wait()
	flushTestStore(t, store)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := store.Close(ctx); err != nil {
		t.Fatalf("close store: %v", err)
	}
	if err := store.Close(ctx); err != nil {
		t.Fatalf("idempotent close: %v", err)
	}

	restarted := newTestPersistentStore(t, path)
	if got := len(restarted.ListJobCopies()); got != len(jobs) {
		t.Fatalf("restored jobs = %d, want %d", got, len(jobs))
	}
}
