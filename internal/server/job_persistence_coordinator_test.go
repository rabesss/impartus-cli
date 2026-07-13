package server

import (
	"context"
	"errors"
	"fmt"
	"os"
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
	if got, want := len(store.jobs), maxRetainedTerminalJobs+2; got != want {
		store.mu.Unlock()
		t.Fatalf("live retained job count = %d, want %d", got, want)
	}
	revision, snapshot := store.snapshotLocked()
	store.mu.Unlock()
	if err := store.publishAndFlush(revision, snapshot); err != nil {
		t.Fatalf("persist retained snapshot: %v", err)
	}

	restarted := newTestPersistentStore(t, path)
	jobs := restarted.ListJobCopies()
	if got, want := len(jobs), maxRetainedTerminalJobs; got != want {
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
		if _, ok := restarted.CopyJob(id); ok {
			t.Fatalf("interrupted job %s should be converted before retention", id)
		}
		if _, ok := restarted.jobByIdempotencyKey("key-" + id); ok {
			t.Fatalf("pruned interrupted job idempotency index %s remains", id)
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
	if got, want := len(store.ListJobCopies()), maxRetainedTerminalJobs; got != want {
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
	if _, ok := store.CopyJob("active"); ok {
		t.Fatal("interrupted job was not included in restored terminal retention")
	}
	if _, ok := store.jobByIdempotencyKey("key-active"); ok {
		t.Fatal("pruned interrupted job idempotency entry remains")
	}
	flushTestStore(t, store)
	if got := len(readPersistedJobs(t, path)); got != maxRetainedTerminalJobs {
		t.Fatalf("pruned persistence file contains %d jobs, want %d", got, maxRetainedTerminalJobs)
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

func TestJobStoreCloseWaitsForAdmittedMutationPublish(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	store := newTestPersistentStore(t, path)
	job := store.CreateJob(1, 1, 1, 5, &config.Config{DownloadLocation: "downloads"})

	snapshotted := make(chan struct{})
	releasePublish := make(chan struct{})
	producerDone := make(chan error, 1)
	go func() {
		if err := store.beginMutation(); err != nil {
			producerDone <- err
			return
		}
		store.mu.Lock()
		store.jobs[job.ID].CompletedLectures = 4
		store.jobs[job.ID].TotalLectures = 5
		revision, snapshot := store.snapshotLocked()
		store.mu.Unlock()
		close(snapshotted)
		<-releasePublish
		err := store.coordinator.publish(revision, snapshot)
		store.endMutation()
		producerDone <- err
	}()
	<-snapshotted

	closeDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		closeDone <- store.Close(ctx)
	}()
	select {
	case err := <-closeDone:
		t.Fatalf("close returned before admitted mutation published: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(releasePublish)
	if err := <-producerDone; err != nil {
		t.Fatalf("publish admitted mutation: %v", err)
	}
	if err := <-closeDone; err != nil {
		t.Fatalf("close store: %v", err)
	}

	restarted := newTestPersistentStore(t, path)
	restored, ok := restarted.CopyJob(job.ID)
	if !ok {
		t.Fatal("job missing after close")
	}
	if restored.CompletedLectures != 4 || restored.TotalLectures != 5 {
		t.Fatalf("restored progress = %d/%d, want 4/5", restored.CompletedLectures, restored.TotalLectures)
	}
}

func TestDurableMutationFailureRollsBackMemoryDiskAndIdempotency(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	store := newTestPersistentStore(t, path)
	cfg := &config.Config{DownloadLocation: "downloads"}
	baseline, created := store.CreateJobWithKey(1, 1, 1, 5, cfg, "baseline-key")
	if !created {
		t.Fatal("baseline job was not created")
	}

	realSync := store.persistence.syncFile
	failWrites := func() {
		store.persistence.mu.Lock()
		store.persistence.syncFile = func(*os.File) error { return errors.New("injected file sync failure") }
		store.persistence.mu.Unlock()
	}
	restoreWrites := func() {
		store.persistence.mu.Lock()
		store.persistence.syncFile = realSync
		store.persistence.mu.Unlock()
		flushTestStore(t, store)
	}

	failWrites()
	phantom, created, err := store.createJobWithKeyDurable(2, 2, 1, 1, cfg, "phantom-key")
	if err == nil || !created {
		t.Fatalf("failed create = (%v, %v), want created with error", created, err)
	}
	if _, ok := store.CopyJob(phantom.ID); ok {
		t.Fatal("failed create left a phantom in memory")
	}
	if _, ok := store.jobByIdempotencyKey("phantom-key"); ok {
		t.Fatal("failed create left an idempotency entry")
	}
	if _, ok := readPersistedJobs(t, path)[phantom.ID]; ok {
		t.Fatal("failed create reached disk")
	}
	restoreWrites()

	for _, test := range []struct {
		name   string
		mutate func() error
	}{
		{name: "failed", mutate: func() error { return store.updateJobDurable(baseline.ID, StatusFailed, 0, "failure") }},
		{name: "completed", mutate: func() error { return store.CompleteJob(baseline.ID, []string{"phantom.mp4"}) }},
		{name: "canceled", mutate: func() error { _, err := store.CancelJob(baseline.ID); return err }},
	} {
		t.Run(test.name, func(t *testing.T) {
			failWrites()
			if err := test.mutate(); err == nil {
				t.Fatal("durable mutation unexpectedly succeeded")
			}
			job, ok := store.CopyJob(baseline.ID)
			if !ok {
				t.Fatal("baseline job disappeared")
			}
			if job.Status != StatusPending || len(job.Outputs) != 0 || job.Error != "" {
				t.Fatalf("memory was not rolled back: status=%s outputs=%v error=%q", job.Status, job.Outputs, job.Error)
			}
			store.mu.RLock()
			ctxErr := store.jobs[baseline.ID].ctx.Err()
			store.mu.RUnlock()
			if ctxErr != nil {
				t.Fatalf("failed durable mutation canceled runtime context: %v", ctxErr)
			}
			persisted := readPersistedJobs(t, path)[baseline.ID]
			if persisted.Status != StatusPending || len(persisted.Outputs) != 0 || persisted.Error != "" {
				t.Fatalf("disk was not rolled back: status=%s outputs=%v error=%q", persisted.Status, persisted.Outputs, persisted.Error)
			}
			restoreWrites()
		})
	}
}

func TestCompatibilityCreateWrappersDoNotReturnRolledBackJobs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	store := newTestPersistentStore(t, path)
	cfg := &config.Config{DownloadLocation: "downloads"}

	realSync := store.persistence.syncFile
	var attempts atomic.Int64
	store.persistence.mu.Lock()
	store.persistence.syncFile = func(file *os.File) error {
		if attempts.Add(1)%2 == 1 {
			return errors.New("injected transient sync failure")
		}
		return realSync(file)
	}
	store.persistence.mu.Unlock()

	job, created := store.CreateJobWithKey(1, 1, 1, 1, cfg, "wrapper-key")
	if job != nil || created {
		t.Fatalf("CreateJobWithKey failure = (%+v, %v), want (nil, false)", job, created)
	}
	if _, ok := store.jobByIdempotencyKey("wrapper-key"); ok {
		t.Fatal("CreateJobWithKey failure left an idempotency entry")
	}
	if job := store.CreateJob(2, 2, 1, 1, cfg); job != nil {
		t.Fatalf("CreateJob failure returned rolled-back job %+v", job)
	}
	if jobs := store.ListJobs(); len(jobs) != 0 {
		t.Fatalf("compatibility wrapper failures left jobs in memory: %+v", jobs)
	}
	if jobs := readPersistedJobs(t, path); len(jobs) != 0 {
		t.Fatalf("compatibility wrapper failures left jobs on disk: %+v", jobs)
	}
	if got := attempts.Load(); got != 4 {
		t.Fatalf("sync attempts = %d, want failed writes plus synchronous rollbacks", got)
	}
}

func TestDurableMutationTransientFailureFlushesRollbackBeforeReturn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	store := newTestPersistentStore(t, path)
	job := store.CreateJob(1, 1, 1, 1, &config.Config{DownloadLocation: "downloads"})
	realSync := store.persistence.syncFile
	var attempts atomic.Int64
	store.persistence.mu.Lock()
	store.persistence.syncFile = func(file *os.File) error {
		if attempts.Add(1) == 1 {
			return errors.New("injected transient sync failure")
		}
		return realSync(file)
	}
	store.persistence.mu.Unlock()

	if err := store.updateJobDurable(job.ID, StatusFailed, 0, "rejected"); err == nil {
		t.Fatal("transient persistence failure was not returned")
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("sync attempts = %d, want rejected write plus synchronous rollback", got)
	}
	visible, ok := store.CopyJob(job.ID)
	if !ok || visible.Status != StatusPending || visible.Error != "" {
		t.Fatalf("memory after rollback = %+v, want pending", visible)
	}
	flushTestStore(t, store)
	persisted := readPersistedJobs(t, path)[job.ID]
	if persisted.Status != StatusPending || persisted.Error != "" {
		t.Fatalf("disk after rollback = status %s error %q", persisted.Status, persisted.Error)
	}
}

func TestDurableMutationIsNotVisibleBeforeCommit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	store := newTestPersistentStore(t, path)
	job := store.CreateJob(1, 1, 1, 1, &config.Config{DownloadLocation: "downloads"})
	realSync := store.persistence.syncFile
	syncStarted := make(chan struct{})
	releaseSync := make(chan struct{})
	var startedOnce sync.Once
	store.persistence.mu.Lock()
	store.persistence.syncFile = func(*os.File) error {
		startedOnce.Do(func() { close(syncStarted) })
		<-releaseSync
		return errors.New("injected blocked sync failure")
	}
	store.persistence.mu.Unlock()

	mutationDone := make(chan error, 1)
	go func() {
		mutationDone <- store.updateJobDurable(job.ID, StatusFailed, 0, "uncommitted")
	}()
	<-syncStarted
	readDone := make(chan *Job, 1)
	go func() {
		copy, _ := store.CopyJob(job.ID)
		readDone <- copy
	}()
	select {
	case visible := <-readDone:
		t.Fatalf("read observed transaction before persistence completed: %+v", visible)
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseSync)
	if err := <-mutationDone; err == nil {
		t.Fatal("blocked persistence failure was not returned")
	}
	visible := <-readDone
	if visible.Status != StatusPending || visible.Error != "" {
		t.Fatalf("read observed rolled-back transaction as status=%s error=%q", visible.Status, visible.Error)
	}

	store.persistence.mu.Lock()
	store.persistence.syncFile = realSync
	store.persistence.mu.Unlock()
	flushTestStore(t, store)
}

func TestJobStoreCloseHonorsContextWhileProducerIsStuck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	store := newTestPersistentStore(t, path)
	job := store.CreateJob(1, 1, 1, 1, &config.Config{DownloadLocation: "downloads"})
	realSync := store.persistence.syncFile
	syncStarted := make(chan struct{})
	releaseSync := make(chan struct{})
	store.persistence.mu.Lock()
	store.persistence.syncFile = func(file *os.File) error {
		close(syncStarted)
		<-releaseSync
		return realSync(file)
	}
	store.persistence.mu.Unlock()

	mutationDone := make(chan error, 1)
	go func() {
		mutationDone <- store.updateJobDurable(job.ID, StatusFailed, 0, "durable")
	}()
	<-syncStarted

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	started := time.Now()
	err := store.Close(ctx)
	elapsed := time.Since(started)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close error = %v, want deadline exceeded", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("close ignored context deadline for %v", elapsed)
	}
	close(releaseSync)
	if err := <-mutationDone; err != nil {
		t.Fatalf("stuck mutation did not finish: %v", err)
	}
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer closeCancel()
	if err := store.Close(closeCtx); err != nil {
		t.Fatalf("close after producer drain: %v", err)
	}
}

func TestPersistenceSaveSyncsFileAndDirectoryAndRollsBackSyncFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	persistence := newJobPersistence(path)
	stamp := time.Now().UTC().Format(persistedTimeFormat)
	oldSnapshot := map[string]persistedJob{"job": {ID: "job", Status: StatusPending, CreatedAt: stamp, UpdatedAt: stamp}}
	newSnapshot := map[string]persistedJob{"job": {ID: "job", Status: StatusCompleted, CreatedAt: stamp, UpdatedAt: stamp}}
	if err := persistence.save(oldSnapshot); err != nil {
		t.Fatalf("seed persistence: %v", err)
	}

	realFileSync := persistence.syncFile
	persistence.syncFile = func(*os.File) error { return errors.New("injected file sync failure") }
	if err := persistence.save(newSnapshot); err == nil {
		t.Fatal("file sync failure was not propagated")
	}
	if got := readPersistedJobs(t, path)["job"].Status; got != StatusPending {
		t.Fatalf("file sync failure changed live snapshot to %q", got)
	}
	if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temp file remains after file sync failure: %v", err)
	}
	persistence.syncFile = realFileSync

	realDirSync := persistence.syncDir
	persistence.syncDir = func(string) error { return errors.New("injected directory sync failure") }
	if err := persistence.save(newSnapshot); err == nil {
		t.Fatal("directory sync failure was not propagated")
	}
	if got := readPersistedJobs(t, path)["job"].Status; got != StatusPending {
		t.Fatalf("directory sync failure did not visibly roll back, status=%q", got)
	}
	if _, err := os.Stat(path + ".bak"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rollback link remains after directory sync failure: %v", err)
	}
	persistence.syncDir = realDirSync

	var directorySyncs atomic.Int64
	persistence.syncDir = func(path string) error {
		directorySyncs.Add(1)
		return realDirSync(path)
	}
	if err := persistence.save(newSnapshot); err != nil {
		t.Fatalf("save synced snapshot: %v", err)
	}
	if directorySyncs.Load() != 1 {
		t.Fatalf("parent directory sync count = %d, want 1", directorySyncs.Load())
	}
	if got := readPersistedJobs(t, path)["job"].Status; got != StatusCompleted {
		t.Fatalf("synced snapshot status = %q, want completed", got)
	}
}

func TestIdempotencyDuplicateDoesNotWriteOrIncrementRevision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	store := newTestPersistentStore(t, path)
	cfg := &config.Config{DownloadLocation: "downloads"}
	first, created := store.CreateJobWithKey(1, 1, 1, 1, cfg, "same-key")
	if !created {
		t.Fatal("first job was not created")
	}
	store.mu.RLock()
	revision := store.revision
	store.mu.RUnlock()

	var fileSyncs atomic.Int64
	realSync := store.persistence.syncFile
	store.persistence.mu.Lock()
	store.persistence.syncFile = func(file *os.File) error {
		fileSyncs.Add(1)
		return realSync(file)
	}
	store.persistence.mu.Unlock()

	duplicate, created := store.CreateJobWithKey(9, 9, 1, 9, cfg, "same-key")
	if created || duplicate.ID != first.ID {
		t.Fatalf("duplicate result = (%s, %v), want existing %s", duplicate.ID, created, first.ID)
	}
	store.mu.RLock()
	gotRevision := store.revision
	store.mu.RUnlock()
	if gotRevision != revision {
		t.Fatalf("duplicate revision = %d, want unchanged %d", gotRevision, revision)
	}
	if got := fileSyncs.Load(); got != 0 {
		t.Fatalf("duplicate triggered %d persistence writes", got)
	}
}

func TestJobStoreReadAPIsReturnDetachedCopiesAcrossRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	store := newTestPersistentStore(t, path)
	cfg := &config.Config{DownloadLocation: "downloads"}
	created, ok := store.CreateJobWithKey(1, 1, 1, 1, cfg, "detached-key")
	if !ok {
		t.Fatal("job was not created")
	}
	created.Progress = 88
	created.Outputs = []string{"created.mp4"}

	getCopy, _ := store.GetJob(created.ID)
	listCopy := store.ListJobs()[0]
	idempotencyCopy, _ := store.jobByIdempotencyKey("detached-key")
	getCopy.Status = StatusCompleted
	getCopy.Outputs = []string{"get.mp4"}
	listCopy.SubjectID = 999
	idempotencyCopy.Error = "mutated"
	actual, _ := store.CopyJob(created.ID)
	if actual.Status != StatusPending || actual.Progress != 0 || len(actual.Outputs) != 0 || actual.SubjectID != 1 || actual.Error != "" {
		t.Fatalf("read result aliased live state: %+v", actual)
	}

	realSync := store.persistence.syncFile
	var attempts atomic.Int64
	store.persistence.mu.Lock()
	store.persistence.syncFile = func(file *os.File) error {
		if attempts.Add(1) == 1 {
			return errors.New("injected rollback failure")
		}
		return realSync(file)
	}
	store.persistence.mu.Unlock()
	stale, _ := store.GetJob(created.ID)
	if err := store.updateJobDurable(created.ID, StatusFailed, 0, "rejected"); err == nil {
		t.Fatal("durable mutation unexpectedly succeeded")
	}
	stale.Status = StatusCanceled
	stale.Outputs = append(stale.Outputs, "stale.mp4")
	actual, _ = store.CopyJob(created.ID)
	if actual.Status != StatusPending || len(actual.Outputs) != 0 || actual.Error != "" {
		t.Fatalf("stale pointer affected rolled-back state: %+v", actual)
	}
}
