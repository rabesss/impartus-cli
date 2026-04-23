package server

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/rabesss/impartus-cli/internal/config"
)

type JobStore struct {
	jobs            map[string]*Job
	idempotencyKeys map[string]string // idempotencyKey -> jobID
	mu              sync.RWMutex
	persistence     *jobPersistence
}

// NewJobStore creates an in-memory job store with no persistence.
func NewJobStore() *JobStore {
	return &JobStore{
		jobs:            make(map[string]*Job),
		idempotencyKeys: make(map[string]string),
	}
}

// NewJobStoreWithPersistence creates a job store that persists to the given file path.
// If path is empty, defaults to ".jobs.json". Jobs are loaded from the file on creation.
func NewJobStoreWithPersistence(path string) *JobStore {
	js := &JobStore{
		jobs:            make(map[string]*Job),
		idempotencyKeys: make(map[string]string),
		persistence:     newJobPersistence(path),
	}
	js.loadFromDisk()
	return js
}

func (js *JobStore) CreateJob(subjectID, sessionID, startIndex, endIndex int, cfg *config.Config) *Job {
	js.mu.Lock()
	defer js.mu.Unlock()

	jobID := fmt.Sprintf("job-%d", time.Now().UnixNano())
	ctx, cancel := context.WithCancel(context.Background())
	job := &Job{
		ID:         jobID,
		SubjectID:  subjectID,
		SessionID:  sessionID,
		StartIndex: startIndex,
		EndIndex:   endIndex,
		Status:     StatusPending,
		Progress:   0,
		Config:     runtimeConfigFrom(cfg),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		ctx:        ctx,
		cancel:     cancel,
		cfg:        cloneConfig(cfg),
	}

	js.jobs[jobID] = job
	js.saveToDisk()
	return job
}

// CreateJobWithKey creates a new job with an optional idempotency key.
// If the idempotency key is non-empty and already exists, it returns the
// existing job instead of creating a new one. This prevents duplicate job
// creation on network retries. Returns the job and a boolean indicating
// whether the job was newly created (true) or returned from the idempotency
// cache (false).
func (js *JobStore) CreateJobWithKey(subjectID, sessionID, startIndex, endIndex int, cfg *config.Config, idempotencyKey string) (*Job, bool) {
	js.mu.Lock()
	defer js.mu.Unlock()

	// Check idempotency key for existing job
	if idempotencyKey != "" {
		if existingID, ok := js.idempotencyKeys[idempotencyKey]; ok {
			if job, ok := js.jobs[existingID]; ok {
				return job, false
			}
		}
	}

	jobID := fmt.Sprintf("job-%d", time.Now().UnixNano())
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

	js.saveToDisk()
	return job, true
}

func (js *JobStore) jobByIdempotencyKey(key string) (*Job, bool) {
	js.mu.RLock()
	defer js.mu.RUnlock()
	jobID, ok := js.idempotencyKeys[key]
	if !ok {
		return nil, false
	}
	job, ok := js.jobs[jobID]
	return job, ok
}

func (js *JobStore) GetJob(id string) (*Job, bool) {
	js.mu.RLock()
	defer js.mu.RUnlock()
	job, ok := js.jobs[id]
	return job, ok
}

func (js *JobStore) ListJobs() []*Job {
	js.mu.RLock()
	defer js.mu.RUnlock()

	jobs := make([]*Job, 0, len(js.jobs))
	for _, job := range js.jobs {
		jobs = append(jobs, job)
	}
	return jobs
}

func (js *JobStore) UpdateJob(id string, status JobStatus, progress float64, errMsg string) {
	js.mu.Lock()
	defer js.mu.Unlock()

	job, ok := js.jobs[id]
	if !ok {
		return
	}

	job.Status = status
	job.Progress = progress
	job.Error = errMsg
	job.UpdatedAt = time.Now()
	js.saveToDisk()
}

func (js *JobStore) SetLectureProgress(id string, completed, total int) {
	js.mu.Lock()
	defer js.mu.Unlock()

	job, ok := js.jobs[id]
	if !ok {
		return
	}
	job.CompletedLectures = completed
	job.TotalLectures = total
	job.UpdatedAt = time.Now()
	js.saveToDisk()
}

func (js *JobStore) SetOutputs(id string, outputs []string) {
	js.mu.Lock()
	defer js.mu.Unlock()

	job, ok := js.jobs[id]
	if !ok {
		return
	}
	job.Outputs = append([]string{}, outputs...)
	job.UpdatedAt = time.Now()
	js.saveToDisk()
}

func (js *JobStore) CancelJob(id string) (*Job, error) {
	js.mu.Lock()
	defer js.mu.Unlock()

	job, ok := js.jobs[id]
	if !ok {
		return nil, ErrJobNotFound
	}

	if job.Status == StatusCompleted || job.Status == StatusFailed || job.Status == StatusCanceled {
		return nil, &TerminalStatusError{Status: job.Status}
	}

	job.Status = StatusCanceled
	job.UpdatedAt = time.Now()
	job.cancel()
	js.saveToDisk()
	return job, nil
}

// loadFromDisk loads previously persisted jobs from the persistence file.
// Jobs that were in a terminal state (completed, failed, canceled) are restored
// with their preserved state. Running/pending jobs are restored as "failed" since
// they cannot be resumed after a restart.
func (js *JobStore) loadFromDisk() {
	if js.persistence == nil {
		return
	}

	persisted := js.persistence.load()
	if persisted == nil {
		return
	}

	for _, pj := range persisted {
		createdAt, err := time.Parse(persistedTimeFormat, pj.CreatedAt)
		if err != nil {
			createdAt = time.Time{}
		}
		updatedAt, err := time.Parse(persistedTimeFormat, pj.UpdatedAt)
		if err != nil {
			updatedAt = time.Time{}
		}

		// Jobs that were running/pending at shutdown cannot be resumed
		status := pj.Status
		if status == StatusPending || status == StatusRunning {
			status = StatusFailed
			if pj.Error == "" {
				pj.Error = "job interrupted by server restart"
			}
		}

		ctx, cancel := context.WithCancel(context.Background())
		js.jobs[pj.ID] = &Job{
			ID:                pj.ID,
			SubjectID:         pj.SubjectID,
			SessionID:         pj.SessionID,
			StartIndex:        pj.StartIndex,
			EndIndex:          pj.EndIndex,
			Status:            status,
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
}

// saveToDisk persists all jobs to the persistence file.
func (js *JobStore) saveToDisk() {
	if js.persistence == nil {
		return
	}
	if err := js.persistence.save(js.jobs); err != nil {
		log.Printf("warning: failed to persist jobs to %s: %v", js.persistence.path, err)
	}
}
