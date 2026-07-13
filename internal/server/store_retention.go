package server

import (
	"sort"
	"time"
)

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
