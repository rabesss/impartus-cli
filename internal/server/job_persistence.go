package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
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
	mu       sync.Mutex
	path     string
	syncFile func(*os.File) error
	syncDir  func(string) error
}

func newJobPersistence(path string) *jobPersistence {
	if path == "" {
		path = defaultPersistencePath
	}
	return &jobPersistence{
		path:     path,
		syncFile: func(file *os.File) error { return file.Sync() },
		syncDir:  syncPersistenceDirectory,
	}
}

// save writes an immutable snapshot of the current job store to disk.
func (p *jobPersistence) save(persisted map[string]persistedJob) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	data, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return err
	}

	// Write and sync a restrictive temp file before atomically replacing the
	// live snapshot. Keep a hard-link rollback point until the parent directory
	// confirms the rename is durable.
	tmpPath := p.path + ".tmp"
	backupPath := p.path + ".bak"
	_ = os.Remove(tmpPath)                                                      //nolint:errcheck // stale temp cleanup is best-effort
	tmp, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) // #nosec G304 -- operator-configured persistence path
	if err != nil {
		return err
	}
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = os.Remove(tmpPath) //nolint:errcheck // deferred temp cleanup is best-effort
		}
	}()
	if chmodErr := tmp.Chmod(0o600); chmodErr != nil {
		_ = tmp.Close() //nolint:errcheck // preserving the primary chmod error
		return fmt.Errorf("restrict persistence temp file: %w", chmodErr)
	}
	written, err := tmp.Write(data)
	if err == nil && written != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		_ = tmp.Close() //nolint:errcheck // preserving the primary write error
		return fmt.Errorf("write persistence temp file: %w", err)
	}
	if syncErr := p.syncFile(tmp); syncErr != nil {
		_ = tmp.Close() //nolint:errcheck // preserving the primary sync error
		return fmt.Errorf("sync persistence temp file: %w", syncErr)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close persistence temp file: %w", err)
	}

	_ = os.Remove(backupPath) //nolint:errcheck // stale rollback link cleanup is best-effort
	hadPrevious := false
	if _, err := os.Stat(p.path); err == nil {
		if linkErr := os.Link(p.path, backupPath); linkErr != nil {
			return fmt.Errorf("create persistence rollback link: %w", linkErr)
		}
		hadPrevious = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect existing persistence file: %w", err)
	}

	if err := os.Rename(tmpPath, p.path); err != nil {
		_ = os.Remove(backupPath) //nolint:errcheck // rollback link is unused when rename fails
		return fmt.Errorf("replace persistence file: %w", err)
	}
	cleanupTemp = false

	parent := filepath.Dir(p.path)
	if err := p.syncDir(parent); err != nil {
		rollbackErr := rollbackPersistenceRename(p.path, backupPath, hadPrevious)
		syncRollbackErr := p.syncDir(parent)
		return errors.Join(
			fmt.Errorf("sync persistence directory: %w", err),
			wrapPersistenceRollbackError(rollbackErr),
			wrapPersistenceRollbackSyncError(syncRollbackErr),
		)
	}
	_ = os.Remove(backupPath) //nolint:errcheck // a stale rollback link does not affect the durable primary

	return nil
}

func rollbackPersistenceRename(path, backupPath string, hadPrevious bool) error {
	if hadPrevious {
		return os.Rename(backupPath, path)
	}
	return os.Remove(path)
}

func wrapPersistenceRollbackError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("roll back persistence rename: %w", err)
}

func wrapPersistenceRollbackSyncError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("sync persistence rollback: %w", err)
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
