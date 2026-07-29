// Package watch implements the automated lecture poll / download / upload loop.
package watch

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const stateVersion = 1

// State is the durable record of lectures the watcher has already processed.
type State struct {
	Version   int                    `json:"version"`
	UpdatedAt string                 `json:"updatedAt"`
	Courses   map[string]CourseState `json:"courses"`
}

// CourseState tracks processed lectures for one subject/session pair.
type CourseState struct {
	SeenTTIDs map[string]SeenLecture `json:"seenTtids"`
}

// SeenLecture records one processed lecture.
type SeenLecture struct {
	SeqNo       int    `json:"seqNo"`
	Topic       string `json:"topic"`
	StartTime   string `json:"startTime"`
	OutputPath  string `json:"outputPath,omitempty"`
	Uploaded    bool   `json:"uploaded"`
	NotebookID  string `json:"notebookId,omitempty"`
	SourceID    string `json:"sourceId,omitempty"`
	ProcessedAt string `json:"processedAt"`
	Error       string `json:"error,omitempty"`
}

// Store is a file-backed State with atomic writes.
type Store struct {
	mu   sync.Mutex
	path string
	data State
}

// CourseKey builds the map key for a subject/session pair.
func CourseKey(subjectID, sessionID int) string {
	return strconv.Itoa(subjectID) + ":" + strconv.Itoa(sessionID)
}

// LoadStore reads state from path, or returns an empty store if the file is missing.
func LoadStore(path string) (*Store, error) {
	store := &Store{
		path: path,
		data: State{
			Version: stateVersion,
			Courses: map[string]CourseState{},
		},
	}
	if path == "" {
		return nil, errors.New("watch state path is required")
	}
	contents, err := os.ReadFile(path) // #nosec G304 -- operator-configured state path
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return store, nil
		}
		return nil, fmt.Errorf("read watch state: %w", err)
	}
	var loaded State
	if err := json.Unmarshal(contents, &loaded); err != nil {
		return nil, fmt.Errorf("parse watch state: %w", err)
	}
	if loaded.Courses == nil {
		loaded.Courses = map[string]CourseState{}
	}
	if loaded.Version == 0 {
		loaded.Version = stateVersion
	}
	store.data = loaded
	_ = os.Chmod(path, 0o600) //nolint:errcheck // best-effort privacy hardening
	return store, nil
}

// Has reports whether a lecture TTID was already processed for the course.
func (s *Store) Has(subjectID, sessionID, ttid int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	course, ok := s.data.Courses[CourseKey(subjectID, sessionID)]
	if !ok {
		return false
	}
	_, ok = course.SeenTTIDs[strconv.Itoa(ttid)]
	return ok
}

// Get returns a previously recorded lecture, if present.
func (s *Store) Get(subjectID, sessionID, ttid int) (SeenLecture, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	course, ok := s.data.Courses[CourseKey(subjectID, sessionID)]
	if !ok {
		return SeenLecture{}, false
	}
	seen, ok := course.SeenTTIDs[strconv.Itoa(ttid)]
	return seen, ok
}

// Mark records a lecture as processed and persists the store.
func (s *Store) Mark(subjectID, sessionID int, lecture SeenLecture, ttid int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := CourseKey(subjectID, sessionID)
	course, ok := s.data.Courses[key]
	if !ok {
		course = CourseState{SeenTTIDs: map[string]SeenLecture{}}
	}
	if course.SeenTTIDs == nil {
		course.SeenTTIDs = map[string]SeenLecture{}
	}
	if lecture.ProcessedAt == "" {
		lecture.ProcessedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	course.SeenTTIDs[strconv.Itoa(ttid)] = lecture
	s.data.Courses[key] = course
	s.data.Version = stateVersion
	s.data.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return s.saveLocked()
}

// Snapshot returns a deep-enough copy for inspection / JSON output.
func (s *Store) Snapshot() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := json.Marshal(s.data)
	if err != nil {
		return State{Version: stateVersion, Courses: map[string]CourseState{}}
	}
	var copy State
	_ = json.Unmarshal(raw, &copy) //nolint:errcheck // round-trip of our own data
	if copy.Courses == nil {
		copy.Courses = map[string]CourseState{}
	}
	return copy
}

func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(s.path, data, 0o600)
}

// atomicWriteFile writes data to path via a synced temp file + rename, with a
// best-effort rollback link of the previous file. Mirrors the job persistence
// pattern in internal/server/job_persistence.go.
func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create watch state directory: %w", err)
	}

	tmpPath := path + ".tmp"
	backupPath := path + ".bak"
	_ = os.Remove(tmpPath) //nolint:errcheck // stale temp cleanup is best-effort

	tmp, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode) // #nosec G304 -- operator-configured state path
	if err != nil {
		return fmt.Errorf("create watch state temp file: %w", err)
	}
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = os.Remove(tmpPath) //nolint:errcheck // deferred temp cleanup is best-effort
		}
	}()
	if chmodErr := tmp.Chmod(mode); chmodErr != nil {
		_ = tmp.Close() //nolint:errcheck // preserving the primary chmod error
		return fmt.Errorf("restrict watch state temp file: %w", chmodErr)
	}
	written, err := tmp.Write(data)
	if err == nil && written != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		_ = tmp.Close() //nolint:errcheck // preserving the primary write error
		return fmt.Errorf("write watch state temp file: %w", err)
	}
	if syncErr := tmp.Sync(); syncErr != nil {
		_ = tmp.Close() //nolint:errcheck // preserving the primary sync error
		return fmt.Errorf("sync watch state temp file: %w", syncErr)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close watch state temp file: %w", err)
	}

	_ = os.Remove(backupPath) //nolint:errcheck // stale rollback cleanup is best-effort
	hadPrevious := false
	if _, statErr := os.Stat(path); statErr == nil {
		hadPrevious = true
		if linkErr := os.Link(path, backupPath); linkErr != nil {
			if copyErr := copyFile(path, backupPath, mode); copyErr != nil {
				return errors.Join(
					fmt.Errorf("create watch state rollback link: %w", linkErr),
					fmt.Errorf("copy watch state rollback file: %w", copyErr),
				)
			}
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("inspect existing watch state: %w", statErr)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(backupPath) //nolint:errcheck // unused rollback
		return fmt.Errorf("replace watch state file: %w", err)
	}
	cleanupTemp = false
	_ = os.Remove(backupPath) //nolint:errcheck // primary is durable enough for watch state
	if hadPrevious {
		_ = os.Chmod(path, mode) //nolint:errcheck // best-effort
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src) // #nosec G304 -- operator-configured state path
	if err != nil {
		return err
	}
	defer in.Close() //nolint:errcheck // read-only

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode) // #nosec G304
	if err != nil {
		return err
	}
	defer out.Close() //nolint:errcheck // closed explicitly below

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	return out.Close()
}
