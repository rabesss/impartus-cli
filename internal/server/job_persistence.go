package server

import (
	"encoding/json"
	"log"
	"os"
	"sync"
)

const (
	defaultPersistencePath = ".jobs.json"
	persistedTimeFormat    = "2006-01-02T15:04:05.999999999Z07:00"
)

// persistedJob represents a job stored on disk, without runtime-only fields
// like context, cancel func, or config (which may contain credentials).
type persistedJob struct {
	ID                string           `json:"id"`
	SubjectID         int              `json:"subjectId"`
	SessionID         int              `json:"sessionId"`
	StartIndex        int              `json:"startIndex"`
	EndIndex          int              `json:"endIndex"`
	Status            JobStatus        `json:"status"`
	Progress          float64          `json:"progress"`
	Error             string           `json:"error,omitempty"`
	TotalLectures     int              `json:"totalLectures,omitempty"`
	CompletedLectures int              `json:"completedLectures,omitempty"`
	FilteredLectures  int              `json:"filteredLectures,omitempty"`
	Outputs           []string         `json:"outputs,omitempty"`
	Config            JobRuntimeConfig `json:"config"`
	IdempotencyKey    string           `json:"idempotencyKey,omitempty"`
	CreatedAt         string           `json:"createdAt"`
	UpdatedAt         string           `json:"updatedAt"`
}

// jobPersistence handles reading/writing jobs to a JSON file on disk.
// It ensures no credentials are persisted by only storing the
// JobRuntimeConfig (which excludes username, password, token).
type jobPersistence struct {
	mu   sync.Mutex
	path string
}

func newJobPersistence(path string) *jobPersistence {
	if path == "" {
		path = defaultPersistencePath
	}
	return &jobPersistence{path: path}
}

// save writes the current job store contents to disk.
func (p *jobPersistence) save(jobs map[string]*Job) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	persisted := make(map[string]persistedJob, len(jobs))
	for id, job := range jobs {
		persisted[id] = jobToPersisted(job)
	}

	data, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return err
	}

	// Write to temp file then rename for atomicity
	tmpPath := p.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, p.path); err != nil {
		// Clean up temp file on rename failure
		//nolint:errcheck
		_ = os.Remove(tmpPath)
		return err
	}

	return nil
}

// load reads jobs from the persistence file. Returns nil map if file
// does not exist or is corrupt (handles gracefully).
func (p *jobPersistence) load() map[string]persistedJob {
	p.mu.Lock()
	defer p.mu.Unlock()

	data, err := os.ReadFile(p.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		// File exists but can't be read - log warning and start fresh
		log.Printf("warning: could not read job persistence file %s: %v", p.path, err)
		return nil
	}

	// Enforce owner-only permissions on an existing file (e.g. one left
	// world-readable by an older build or manual creation).
	_ = os.Chmod(p.path, 0o600) //nolint:errcheck // best-effort permission enforcement

	var persisted map[string]persistedJob
	if err := json.Unmarshal(data, &persisted); err != nil {
		log.Printf("warning: corrupt job persistence file %s: %v", p.path, err)
		return nil
	}

	return persisted
}

func jobToPersisted(job *Job) persistedJob {
	return persistedJob{
		ID:                job.ID,
		SubjectID:         job.SubjectID,
		SessionID:         job.SessionID,
		StartIndex:        job.StartIndex,
		EndIndex:          job.EndIndex,
		Status:            job.Status,
		Progress:          job.Progress,
		Error:             job.Error,
		TotalLectures:     job.TotalLectures,
		CompletedLectures: job.CompletedLectures,
		FilteredLectures:  job.FilteredLectures,
		Outputs:           append([]string{}, job.Outputs...),
		Config:            job.Config,
		IdempotencyKey:    job.IdempotencyKey,
		CreatedAt:         job.CreatedAt.Format(persistedTimeFormat),
		UpdatedAt:         job.UpdatedAt.Format(persistedTimeFormat),
	}
}
