package server

import (
	"context"
	"fmt"
	"log"
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
	if isTerminalStatus(job.Status) {
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

// UpdateRunningIfActive advances a non-terminal job to running state without
// allowing progress to regress. It returns true only when the update was
// applied, so callers can keep emitted progress events aligned with store
// transition order.
func (js *JobStore) UpdateRunningIfActive(id string, progress float64) (bool, error) {
	if err := js.beginMutation(); err != nil {
		return false, err
	}
	defer js.endMutation()
	js.mu.Lock()

	job, ok := js.jobs[id]
	if !ok || isTerminalStatus(job.Status) || progress < job.Progress {
		js.mu.Unlock()
		return false, nil
	}
	job.Status = StatusRunning
	job.Progress = progress
	job.Error = ""
	job.UpdatedAt = time.Now()
	revision, snapshot := js.snapshotLocked()
	js.mu.Unlock()
	js.publish(revision, snapshot)
	return true, nil
}

// FailJobIfNonTerminal atomically and durably transitions an existing
// non-terminal job to failed. It returns true only after the transition has
// been persisted. Callers must use the result to avoid emitting a failed event
// for a job that already completed, failed, or was canceled concurrently.
func (js *JobStore) FailJobIfNonTerminal(id, errMsg string) (bool, error) {
	if err := js.beginMutation(); err != nil {
		return false, err
	}
	defer js.endMutation()
	js.visibilityMu.Lock()
	defer js.visibilityMu.Unlock()
	js.mu.Lock()

	job, ok := js.jobs[id]
	if !ok || isTerminalStatus(job.Status) {
		js.mu.Unlock()
		return false, nil
	}
	before := js.captureStateLocked()

	job.Status = StatusFailed
	job.Progress = 0
	job.Error = errMsg
	job.UpdatedAt = time.Now()
	pruned := js.pruneTerminalJobsLocked()
	revision, snapshot := js.snapshotLocked()
	js.mu.Unlock()
	if err := js.publishAndFlush(revision, snapshot); err != nil {
		return false, js.rollbackMutation(before, err)
	}
	cancelJobs(pruned)
	return true, nil
}

// SetLectureProgress updates the completed and total lecture counts for a job.
func (js *JobStore) SetLectureProgress(id string, completed, total int) {
	if err := js.beginMutation(); err != nil {
		return
	}
	defer js.endMutation()
	js.mu.Lock()

	job, ok := js.jobs[id]
	if !ok || isTerminalStatus(job.Status) || completed < job.CompletedLectures {
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
	if isTerminalStatus(job.Status) {
		status := job.Status
		js.mu.Unlock()
		return &TerminalStatusError{Status: status}
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

	if isTerminalStatus(job.Status) {
		status := job.Status
		js.mu.Unlock()
		return nil, &TerminalStatusError{Status: status}
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
