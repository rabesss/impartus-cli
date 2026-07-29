// Package watch implements the automated lecture poll / download / upload loop.
package watch

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const stateVersion = 1

// LectureStatus is the durable processing phase for one TTID.
type LectureStatus string

const (
	// StatusPending means the lecture was claimed but not yet downloaded.
	StatusPending LectureStatus = "pending"
	// StatusDownloaded means audio exists locally and upload may still be needed.
	StatusDownloaded LectureStatus = "downloaded"
	// StatusUploaded means the lecture was uploaded successfully.
	StatusUploaded LectureStatus = "uploaded"
	// StatusFailed means the last attempt failed and should be retried.
	StatusFailed LectureStatus = "failed"
)

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

// SeenLecture records one processed lecture and its pipeline phase.
type SeenLecture struct {
	Status      LectureStatus `json:"status"`
	SubjectID   int           `json:"subjectId,omitempty"`
	SessionID   int           `json:"sessionId,omitempty"`
	SeqNo       int           `json:"seqNo"`
	Topic       string        `json:"topic"`
	StartTime   string        `json:"startTime"`
	OutputPath  string        `json:"audioPath,omitempty"`
	Uploaded    bool          `json:"uploaded"`
	NotebookID  string        `json:"notebookId,omitempty"`
	SourceID    string        `json:"sourceId,omitempty"`
	Attempts    int           `json:"attempts,omitempty"`
	Error       string        `json:"lastError,omitempty"`
	FirstSeenAt string        `json:"firstSeenAt,omitempty"`
	ProcessedAt string        `json:"updatedAt"`
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

// LectureKey builds the full durable key for one lecture.
func LectureKey(subjectID, sessionID, ttid int) string {
	return CourseKey(subjectID, sessionID) + ":" + strconv.Itoa(ttid)
}

// LoadStore reads state from path, or returns an empty store if the file is missing
// or corrupt (corrupt files log a warning and start fresh, matching job persistence).
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
		log.Printf("watch: corrupt state file %s; starting fresh: %v", path, err)
		return store, nil
	}
	if loaded.Courses == nil {
		loaded.Courses = map[string]CourseState{}
	}
	if loaded.Version == 0 {
		loaded.Version = stateVersion
	}
	normalizeLoadedState(&loaded)
	store.data = loaded
	_ = os.Chmod(path, 0o600) //nolint:errcheck // best-effort privacy hardening
	return store, nil
}

func normalizeLoadedState(state *State) {
	for courseKey, course := range state.Courses {
		if course.SeenTTIDs == nil {
			course.SeenTTIDs = map[string]SeenLecture{}
		}
		for ttid, seen := range course.SeenTTIDs {
			if seen.Status == "" {
				seen.Status = inferLegacyStatus(seen)
				course.SeenTTIDs[ttid] = seen
			}
		}
		state.Courses[courseKey] = course
	}
}

func inferLegacyStatus(seen SeenLecture) LectureStatus {
	if seen.Uploaded || seen.SourceID != "" {
		return StatusUploaded
	}
	if seen.Error != "" {
		return StatusFailed
	}
	if seen.OutputPath != "" {
		return StatusDownloaded
	}
	return StatusPending
}

// NeedsWork reports whether a lecture should be processed (new, failed, or
// downloaded-but-not-uploaded when upload is enabled). Uploaded lectures are skipped.
func (s *Store) NeedsWork(subjectID, sessionID, ttid int, uploadEnabled bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen, ok := s.getLocked(subjectID, sessionID, ttid)
	if !ok {
		return true
	}
	switch seen.Status {
	case StatusUploaded:
		return false
	case StatusDownloaded:
		return uploadEnabled // resume upload without re-download
	case StatusFailed, StatusPending:
		return true
	default:
		return seen.Error != "" || seen.OutputPath == ""
	}
}

// Has reports whether a lecture TTID was already uploaded successfully.
func (s *Store) Has(subjectID, sessionID, ttid int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen, ok := s.getLocked(subjectID, sessionID, ttid)
	if !ok {
		return false
	}
	return seen.Status == StatusUploaded || (seen.Error == "" && seen.Uploaded && seen.OutputPath != "")
}

// Get returns a previously recorded lecture, if present.
func (s *Store) Get(subjectID, sessionID, ttid int) (SeenLecture, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getLocked(subjectID, sessionID, ttid)
}

func (s *Store) getLocked(subjectID, sessionID, ttid int) (SeenLecture, bool) {
	course, ok := s.data.Courses[CourseKey(subjectID, sessionID)]
	if !ok {
		return SeenLecture{}, false
	}
	seen, ok := course.SeenTTIDs[strconv.Itoa(ttid)]
	return seen, ok
}

// Mark records a lecture phase and persists the store.
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
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if existing, ok := course.SeenTTIDs[strconv.Itoa(ttid)]; ok {
		if lecture.FirstSeenAt == "" {
			lecture.FirstSeenAt = existing.FirstSeenAt
		}
		if lecture.Attempts == 0 {
			lecture.Attempts = existing.Attempts
		}
		if lecture.OutputPath == "" {
			lecture.OutputPath = existing.OutputPath
		}
	}
	if lecture.FirstSeenAt == "" {
		lecture.FirstSeenAt = now
	}
	if lecture.ProcessedAt == "" {
		lecture.ProcessedAt = now
	}
	if lecture.Status == "" {
		lecture.Status = inferLegacyStatus(lecture)
	}
	lecture.SubjectID = subjectID
	lecture.SessionID = sessionID
	lecture.Uploaded = lecture.Status == StatusUploaded
	course.SeenTTIDs[strconv.Itoa(ttid)] = lecture
	s.data.Courses[key] = course
	s.data.Version = stateVersion
	s.data.UpdatedAt = now
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

	if err := writeAndSyncTemp(tmpPath, data, mode); err != nil {
		_ = os.Remove(tmpPath) //nolint:errcheck // best-effort cleanup after failed write
		return err
	}

	_ = os.Remove(backupPath) //nolint:errcheck // stale rollback cleanup is best-effort
	hadPrevious, err := createRollbackBackup(path, backupPath, mode)
	if err != nil {
		_ = os.Remove(tmpPath) //nolint:errcheck // unused temp after rollback failure
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(backupPath) //nolint:errcheck // unused rollback
		return fmt.Errorf("replace watch state file: %w", err)
	}
	_ = os.Remove(backupPath) //nolint:errcheck // primary is durable enough for watch state
	if hadPrevious {
		_ = os.Chmod(path, mode) //nolint:errcheck // best-effort
	}
	return nil
}

func writeAndSyncTemp(tmpPath string, data []byte, mode os.FileMode) error {
	tmp, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode) // #nosec G304 -- operator-configured state path
	if err != nil {
		return fmt.Errorf("create watch state temp file: %w", err)
	}
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
	return nil
}

func createRollbackBackup(path, backupPath string, mode os.FileMode) (bool, error) {
	if _, statErr := os.Stat(path); errors.Is(statErr, os.ErrNotExist) {
		return false, nil
	} else if statErr != nil {
		return false, fmt.Errorf("inspect existing watch state: %w", statErr)
	}
	if linkErr := os.Link(path, backupPath); linkErr != nil {
		if copyErr := copyFile(path, backupPath, mode); copyErr != nil {
			return false, errors.Join(
				fmt.Errorf("create watch state rollback link: %w", linkErr),
				fmt.Errorf("copy watch state rollback file: %w", copyErr),
			)
		}
	}
	return true, nil
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
