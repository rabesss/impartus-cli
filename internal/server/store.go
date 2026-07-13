package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/rabesss/impartus-cli/internal/config"
)

// JobStore provides thread-safe in-memory storage for download jobs with optional disk persistence.
type JobStore struct {
	jobs            map[string]*Job
	idempotencyKeys map[string]string // idempotencyKey -> jobID
	mu              sync.RWMutex
	mutationMu      sync.Mutex
	visibilityMu    sync.RWMutex
	lifecycleMu     sync.Mutex
	closed          bool
	producers       int
	producersDone   chan struct{}
	persistence     *jobPersistence
	coordinator     *persistenceCoordinator
	revision        uint64
}

const maxRetainedTerminalJobs = 1000

var errJobStoreClosed = errors.New("job store is closed")

// NewJobStore creates an in-memory job store with no persistence.
func NewJobStore() *JobStore {
	js := &JobStore{
		jobs:            make(map[string]*Job),
		idempotencyKeys: make(map[string]string),
	}
	js.producersDone = closedSignal()
	return js
}

// NewJobStoreWithPersistence creates a job store that persists to the given file path.
// If path is empty, defaults to ".jobs.json". Jobs are loaded from the file on creation.
func NewJobStoreWithPersistence(path string) *JobStore {
	js := &JobStore{
		jobs:            make(map[string]*Job),
		idempotencyKeys: make(map[string]string),
		persistence:     newJobPersistence(path),
	}
	js.producersDone = closedSignal()
	changed := js.loadFromDisk()
	js.coordinator = newPersistenceCoordinator(js.persistence.save)
	if changed {
		js.mu.Lock()
		revision, snapshot := js.snapshotLocked()
		js.mu.Unlock()
		if err := js.publishAndFlush(revision, snapshot); err != nil {
			log.Printf("warning: failed to persist restored jobs to %s: %v", js.persistence.path, err)
		}
	}
	return js
}

// CreateJob creates a new download job and stores it. Returns the created job,
// or nil if a persistent store cannot durably create it.
// It is a convenience wrapper around CreateJobWithKey with no idempotency key.
func (js *JobStore) CreateJob(subjectID, sessionID, startIndex, endIndex int, cfg *config.Config) *Job {
	job, _ := js.CreateJobWithKey(subjectID, sessionID, startIndex, endIndex, cfg, "")
	return job
}

// CreateJobWithKey creates a new job with an optional idempotency key.
// If the idempotency key is non-empty and already exists, it returns the
// existing job instead of creating a new one. This prevents duplicate job
// creation on network retries. Returns the job and a boolean indicating
// whether the job was newly created (true) or returned from the idempotency
// cache (false). A persistence failure is logged and returned as (nil, false).
func (js *JobStore) CreateJobWithKey(subjectID, sessionID, startIndex, endIndex int, cfg *config.Config, idempotencyKey string) (*Job, bool) {
	job, created, err := js.createJobWithKeyDurable(subjectID, sessionID, startIndex, endIndex, cfg, idempotencyKey)
	if err != nil {
		log.Printf("warning: failed to persist created job: %v", err)
		return nil, false
	}
	return job, created
}

func (js *JobStore) createJobWithKeyDurable(subjectID, sessionID, startIndex, endIndex int, cfg *config.Config, idempotencyKey string) (*Job, bool, error) {
	if err := js.beginMutation(); err != nil {
		return nil, false, err
	}
	defer js.endMutation()
	js.visibilityMu.Lock()
	defer js.visibilityMu.Unlock()

	js.mu.Lock()

	// Check idempotency key for existing job
	if idempotencyKey != "" {
		if existingID, ok := js.idempotencyKeys[idempotencyKey]; ok {
			if job, ok := js.jobs[existingID]; ok {
				copy := job.copy()
				js.mu.Unlock()
				return copy, false, nil
			}
		}
	}
	before := js.captureStateLocked()

	jobID := fmt.Sprintf("job-%s", uuid.NewString())
	ctx, cancel := context.WithCancel(context.Background())
	job := &Job{
		ID:             jobID,
		SubjectID:      subjectID,
		SessionID:      sessionID,
		StartIndex:     startIndex,
		EndIndex:       endIndex,
		Status:         StatusPending,
		Progress:       0,
		Config:         runtimeConfigFrom(cfg),
		IdempotencyKey: idempotencyKey,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		ctx:            ctx,
		cancel:         cancel,
		cfg:            cloneConfig(cfg),
	}

	js.jobs[jobID] = job

	// Register idempotency key mapping
	if idempotencyKey != "" {
		js.idempotencyKeys[idempotencyKey] = jobID
	}

	revision, snapshot := js.snapshotLocked()
	js.mu.Unlock()
	result := cloneJobState(job)
	if err := js.publishAndFlush(revision, snapshot); err != nil {
		return result, true, js.rollbackMutation(before, err)
	}
	return result, true, nil
}

func (js *JobStore) jobByIdempotencyKey(key string) (*Job, bool) {
	js.visibilityMu.RLock()
	defer js.visibilityMu.RUnlock()
	js.mu.RLock()
	defer js.mu.RUnlock()
	jobID, ok := js.idempotencyKeys[key]
	if !ok {
		return nil, false
	}
	job, ok := js.jobs[jobID]
	if !ok {
		return nil, false
	}
	return job.copy(), true
}

// GetJob retrieves a job by ID. Returns the job and whether it was found.
func (js *JobStore) GetJob(id string) (*Job, bool) {
	js.visibilityMu.RLock()
	defer js.visibilityMu.RUnlock()
	js.mu.RLock()
	defer js.mu.RUnlock()
	job, ok := js.jobs[id]
	if !ok {
		return nil, false
	}
	return job.copy(), true
}

// ListJobs returns a snapshot of all jobs in the store.
func (js *JobStore) ListJobs() []*Job {
	js.visibilityMu.RLock()
	defer js.visibilityMu.RUnlock()
	js.mu.RLock()
	defer js.mu.RUnlock()

	jobs := make([]*Job, 0, len(js.jobs))
	for _, job := range js.jobs {
		jobs = append(jobs, job.copy())
	}
	return jobs
}

// runtimeJobSnapshot returns detached job state while retaining the immutable
// runtime context and config references needed by the executor.
func (js *JobStore) runtimeJobSnapshot(id string) (*Job, bool) {
	js.visibilityMu.RLock()
	defer js.visibilityMu.RUnlock()
	js.mu.RLock()
	defer js.mu.RUnlock()
	job, ok := js.jobs[id]
	if !ok {
		return nil, false
	}
	return cloneJobState(job), true
}

// CopyJob retrieves a deep copy of a job by ID under the read lock.
// Returns the copy and whether the job was found.
func (js *JobStore) CopyJob(id string) (*Job, bool) {
	js.visibilityMu.RLock()
	defer js.visibilityMu.RUnlock()
	js.mu.RLock()
	defer js.mu.RUnlock()
	job, ok := js.jobs[id]
	if !ok {
		return nil, false
	}
	return job.copy(), true
}

// ListJobCopies returns a slice of deep copies of all jobs, copied under the read lock.
func (js *JobStore) ListJobCopies() []*Job {
	js.visibilityMu.RLock()
	defer js.visibilityMu.RUnlock()
	js.mu.RLock()
	defer js.mu.RUnlock()
	out := make([]*Job, 0, len(js.jobs))
	for _, j := range js.jobs {
		out = append(out, j.copy())
	}
	return out
}

// GetJobStatus returns the status of a job by ID under the read lock.
func (js *JobStore) GetJobStatus(id string) (JobStatus, bool) {
	js.visibilityMu.RLock()
	defer js.visibilityMu.RUnlock()
	js.mu.RLock()
	defer js.mu.RUnlock()
	job, ok := js.jobs[id]
	if !ok {
		return "", false
	}
	return job.Status, true
}

// SetFilteredLectures sets the filtered lecture count for a job under the write lock.
func (js *JobStore) SetFilteredLectures(id string, count int) {
	if err := js.beginMutation(); err != nil {
		return
	}
	defer js.endMutation()
	js.mu.Lock()
	job, ok := js.jobs[id]
	if !ok {
		js.mu.Unlock()
		return
	}
	job.FilteredLectures = count
	job.UpdatedAt = time.Now()
	revision, snapshot := js.snapshotLocked()
	js.mu.Unlock()
	js.publish(revision, snapshot)
}

// UpdateJob updates a job's status, progress, and error message.
func (js *JobStore) UpdateJob(id string, status JobStatus, progress float64, errMsg string) {
	if err := js.updateJobDurable(id, status, progress, errMsg); err != nil {
		log.Printf("warning: failed to persist updated job: %v", err)
	}
}

func (js *JobStore) updateJobDurable(id string, status JobStatus, progress float64, errMsg string) error {
	if err := js.beginMutation(); err != nil {
		return err
	}
	defer js.endMutation()
	if isTerminalStatus(status) {
		js.visibilityMu.Lock()
		defer js.visibilityMu.Unlock()
	}
	js.mu.Lock()

	job, ok := js.jobs[id]
	if !ok {
		js.mu.Unlock()
		return nil
	}
	var before jobStoreState
	if isTerminalStatus(status) {
		before = js.captureStateLocked()
	}

	job.Status = status
	job.Progress = progress
	job.Error = errMsg
	job.UpdatedAt = time.Now()
	var pruned []*Job
	if isTerminalStatus(status) {
		pruned = js.pruneTerminalJobsLocked()
	}
	revision, snapshot := js.snapshotLocked()
	js.mu.Unlock()
	if isTerminalStatus(status) {
		if err := js.publishAndFlush(revision, snapshot); err != nil {
			return js.rollbackMutation(before, err)
		}
		cancelJobs(pruned)
		return nil
	}
	js.publish(revision, snapshot)
	return nil
}

// SetLectureProgress updates the completed and total lecture counts for a job.
func (js *JobStore) SetLectureProgress(id string, completed, total int) {
	if err := js.beginMutation(); err != nil {
		return
	}
	defer js.endMutation()
	js.mu.Lock()

	job, ok := js.jobs[id]
	if !ok {
		js.mu.Unlock()
		return
	}
	job.CompletedLectures = completed
	job.TotalLectures = total
	job.UpdatedAt = time.Now()
	revision, snapshot := js.snapshotLocked()
	js.mu.Unlock()
	js.publish(revision, snapshot)
}

// SetOutputs sets the output file paths for a completed job, copying the slice to avoid aliasing.
func (js *JobStore) SetOutputs(id string, outputs []string) {
	if err := js.beginMutation(); err != nil {
		return
	}
	defer js.endMutation()
	js.mu.Lock()

	job, ok := js.jobs[id]
	if !ok {
		js.mu.Unlock()
		return
	}
	job.Outputs = append([]string{}, outputs...)
	job.UpdatedAt = time.Now()
	revision, snapshot := js.snapshotLocked()
	js.mu.Unlock()
	js.publish(revision, snapshot)
}

// CompleteJob atomically assigns outputs and transitions a job to completed.
func (js *JobStore) CompleteJob(id string, outputs []string) error {
	if err := js.beginMutation(); err != nil {
		return err
	}
	defer js.endMutation()
	js.visibilityMu.Lock()
	defer js.visibilityMu.Unlock()
	js.mu.Lock()
	job, ok := js.jobs[id]
	if !ok {
		js.mu.Unlock()
		return ErrJobNotFound
	}
	before := js.captureStateLocked()
	job.Outputs = append([]string{}, outputs...)
	job.Status = StatusCompleted
	job.Progress = 100
	job.Error = ""
	job.UpdatedAt = time.Now()
	pruned := js.pruneTerminalJobsLocked()
	revision, snapshot := js.snapshotLocked()
	js.mu.Unlock()
	if err := js.publishAndFlush(revision, snapshot); err != nil {
		return js.rollbackMutation(before, err)
	}
	cancelJobs(pruned)
	return nil
}

// CancelJob transitions a non-terminal job to canceled status and cancels its context.
// Returns an error if the job is not found or already in a terminal state.
func (js *JobStore) CancelJob(id string) (*Job, error) {
	if err := js.beginMutation(); err != nil {
		return nil, err
	}
	defer js.endMutation()
	js.visibilityMu.Lock()
	defer js.visibilityMu.Unlock()
	js.mu.Lock()

	job, ok := js.jobs[id]
	if !ok {
		js.mu.Unlock()
		return nil, ErrJobNotFound
	}

	if job.Status == StatusCompleted || job.Status == StatusFailed || job.Status == StatusCanceled {
		js.mu.Unlock()
		return nil, &TerminalStatusError{Status: job.Status}
	}
	before := js.captureStateLocked()

	job.Status = StatusCanceled
	job.UpdatedAt = time.Now()
	pruned := js.pruneTerminalJobsLocked()
	revision, snapshot := js.snapshotLocked()
	copy := job.copy()
	js.mu.Unlock()
	if err := js.publishAndFlush(revision, snapshot); err != nil {
		return copy, js.rollbackMutation(before, err)
	}
	job.cancel()
	cancelJobs(pruned)
	return copy, nil
}

// loadFromDisk loads previously persisted jobs from the persistence file.
// Jobs that were in a terminal state (completed, failed, canceled) are restored
// with their preserved state. Running/pending jobs are restored as "failed" since
// they cannot be resumed after a restart.
func (js *JobStore) loadFromDisk() bool {
	if js.persistence == nil {
		return false
	}

	persisted := js.persistence.load()
	if persisted == nil {
		return false
	}
	changed := false
	// Interrupted jobs become terminal before retention is enforced so the
	// restored store never exceeds the configured terminal-history bound.
	for id, pj := range persisted {
		if pj.Status == StatusPending || pj.Status == StatusRunning {
			pj.Status = StatusFailed
			if pj.Error == "" {
				pj.Error = "job interrupted by server restart"
			}
			persisted[id] = pj
			changed = true
		}
	}
	var pruned bool
	persisted, pruned = prunePersistedTerminalJobs(persisted)
	changed = changed || pruned

	for _, pj := range persisted {
		createdAt, err := time.Parse(persistedTimeFormat, pj.CreatedAt)
		if err != nil {
			createdAt = time.Time{}
		}
		updatedAt, err := time.Parse(persistedTimeFormat, pj.UpdatedAt)
		if err != nil {
			updatedAt = time.Time{}
		}

		ctx, cancel := context.WithCancel(context.Background())
		js.jobs[pj.ID] = &Job{
			ID:                pj.ID,
			SubjectID:         pj.SubjectID,
			SessionID:         pj.SessionID,
			StartIndex:        pj.StartIndex,
			EndIndex:          pj.EndIndex,
			Status:            pj.Status,
			Progress:          pj.Progress,
			Error:             pj.Error,
			TotalLectures:     pj.TotalLectures,
			CompletedLectures: pj.CompletedLectures,
			FilteredLectures:  pj.FilteredLectures,
			Outputs:           append([]string{}, pj.Outputs...),
			Config:            pj.Config,
			IdempotencyKey:    pj.IdempotencyKey,
			CreatedAt:         createdAt,
			UpdatedAt:         updatedAt,
			ctx:               ctx,
			cancel:            cancel,
		}

		// Rebuild idempotency key index
		if pj.IdempotencyKey != "" {
			js.idempotencyKeys[pj.IdempotencyKey] = pj.ID
		}
	}
	return changed
}

// Flush waits until all mutations visible at call time have been attempted on disk.
func (js *JobStore) Flush(ctx context.Context) error {
	if js.coordinator == nil {
		return nil
	}
	js.mu.RLock()
	revision := js.revision
	js.mu.RUnlock()
	return js.coordinator.flushTo(ctx, revision)
}

// Close flushes pending persistence and stops the persistence worker. It is idempotent.
func (js *JobStore) Close(ctx context.Context) error {
	if js.coordinator == nil {
		return nil
	}
	js.lifecycleMu.Lock()
	js.closed = true
	drained := js.producersDone
	js.lifecycleMu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-drained:
		return js.coordinator.close(ctx)
	}
}

func (js *JobStore) beginMutation() error {
	js.lifecycleMu.Lock()
	if js.closed {
		js.lifecycleMu.Unlock()
		return errJobStoreClosed
	}
	if js.producers == 0 {
		js.producersDone = make(chan struct{})
	}
	js.producers++
	js.lifecycleMu.Unlock()
	js.mutationMu.Lock()
	return nil
}

func (js *JobStore) endMutation() {
	js.mutationMu.Unlock()
	js.lifecycleMu.Lock()
	js.producers--
	if js.producers == 0 {
		close(js.producersDone)
	}
	js.lifecycleMu.Unlock()
}

func closedSignal() chan struct{} {
	closed := make(chan struct{})
	close(closed)
	return closed
}

type jobStoreState struct {
	jobs            map[string]*Job
	idempotencyKeys map[string]string
}

func (js *JobStore) captureStateLocked() jobStoreState {
	state := jobStoreState{
		jobs:            make(map[string]*Job, len(js.jobs)),
		idempotencyKeys: make(map[string]string, len(js.idempotencyKeys)),
	}
	for id, job := range js.jobs {
		state.jobs[id] = cloneJobState(job)
	}
	for key, id := range js.idempotencyKeys {
		state.idempotencyKeys[key] = id
	}
	return state
}

func cloneJobState(job *Job) *Job {
	clone := job.copy()
	clone.ctx = job.ctx
	clone.cancel = job.cancel
	clone.cfg = job.cfg
	return clone
}

func (js *JobStore) rollbackMutation(state jobStoreState, mutationErr error) error {
	js.mu.Lock()
	for id, job := range js.jobs {
		if _, existed := state.jobs[id]; !existed && job.cancel != nil {
			job.cancel()
		}
	}
	js.jobs = state.jobs
	js.idempotencyKeys = state.idempotencyKeys
	revision, snapshot := js.snapshotLocked()
	js.mu.Unlock()
	if rollbackErr := js.publishAndFlush(revision, snapshot); rollbackErr != nil {
		return errors.Join(mutationErr, fmt.Errorf("persist transaction rollback: %w", rollbackErr))
	}
	return mutationErr
}

func (js *JobStore) snapshotLocked() (uint64, map[string]persistedJob) {
	js.revision++
	snapshot := make(map[string]persistedJob, len(js.jobs))
	for id, job := range js.jobs {
		snapshot[id] = jobToPersisted(job)
	}
	return js.revision, snapshot
}

func (js *JobStore) publish(revision uint64, snapshot map[string]persistedJob) {
	if js.coordinator == nil {
		return
	}
	if err := js.coordinator.publish(revision, snapshot); err != nil {
		log.Printf("warning: failed to schedule job persistence: %v", err)
	}
}

func (js *JobStore) publishAndFlush(revision uint64, snapshot map[string]persistedJob) error {
	if js.coordinator == nil {
		return nil
	}
	if err := js.coordinator.publish(revision, snapshot); err != nil {
		return err
	}
	return js.coordinator.flushTo(context.Background(), revision)
}

// saveToDisk remains as an internal compatibility helper for callers that
// explicitly request a durable snapshot.
func (js *JobStore) saveToDisk() {
	if js.coordinator == nil {
		return
	}
	if err := js.beginMutation(); err != nil {
		return
	}
	defer js.endMutation()
	js.mu.Lock()
	revision, snapshot := js.snapshotLocked()
	js.mu.Unlock()
	if err := js.publishAndFlush(revision, snapshot); err != nil {
		log.Printf("warning: failed to persist jobs to %s: %v", js.persistence.path, err)
	}
}

func isTerminalStatus(status JobStatus) bool {
	return status == StatusCompleted || status == StatusFailed || status == StatusCanceled
}

func (js *JobStore) pruneTerminalJobsLocked() []*Job {
	type terminalJob struct {
		id        string
		updatedAt time.Time
	}
	terminal := make([]terminalJob, 0)
	for id, job := range js.jobs {
		if isTerminalStatus(job.Status) {
			terminal = append(terminal, terminalJob{id: id, updatedAt: job.UpdatedAt})
		}
	}
	if len(terminal) <= maxRetainedTerminalJobs {
		return nil
	}
	sort.Slice(terminal, func(i, j int) bool {
		if terminal[i].updatedAt.Equal(terminal[j].updatedAt) {
			return terminal[i].id < terminal[j].id
		}
		return terminal[i].updatedAt.Before(terminal[j].updatedAt)
	})
	pruned := make([]*Job, 0, len(terminal)-maxRetainedTerminalJobs)
	for _, old := range terminal[:len(terminal)-maxRetainedTerminalJobs] {
		job := js.jobs[old.id]
		pruned = append(pruned, job)
		if job.IdempotencyKey != "" {
			delete(js.idempotencyKeys, job.IdempotencyKey)
		}
		delete(js.jobs, old.id)
	}
	return pruned
}

func cancelJobs(jobs []*Job) {
	for _, job := range jobs {
		if job.cancel != nil {
			job.cancel()
		}
	}
}

func prunePersistedTerminalJobs(jobs map[string]persistedJob) (map[string]persistedJob, bool) {
	type terminalJob struct {
		id        string
		updatedAt time.Time
	}
	terminal := make([]terminalJob, 0)
	for id, job := range jobs {
		if !isTerminalStatus(job.Status) {
			continue
		}
		updatedAt, err := time.Parse(persistedTimeFormat, job.UpdatedAt)
		if err != nil {
			updatedAt = time.Time{}
		}
		terminal = append(terminal, terminalJob{id: id, updatedAt: updatedAt})
	}
	if len(terminal) <= maxRetainedTerminalJobs {
		return jobs, false
	}
	sort.Slice(terminal, func(i, j int) bool {
		if terminal[i].updatedAt.Equal(terminal[j].updatedAt) {
			return terminal[i].id < terminal[j].id
		}
		return terminal[i].updatedAt.Before(terminal[j].updatedAt)
	})
	for _, old := range terminal[:len(terminal)-maxRetainedTerminalJobs] {
		delete(jobs, old.id)
	}
	return jobs, true
}
