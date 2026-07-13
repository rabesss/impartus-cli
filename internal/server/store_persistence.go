package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"
)

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
