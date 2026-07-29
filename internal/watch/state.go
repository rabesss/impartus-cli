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
	UploadKey   string        `json:"uploadKey,omitempty"`
	Attempts    int           `json:"attempts,omitempty"`
	Error       string        `json:"lastError,omitempty"`
	FirstSeenAt string        `json:"firstSeenAt,omitempty"`
	ProcessedAt string        `json:"updatedAt"`
}

// UnmarshalJSON accepts the original outputPath key as well as audioPath so
// state written by earlier watch builds can still resume without re-downloading.
func (s *SeenLecture) UnmarshalJSON(data []byte) error {
	type seenLectureAlias SeenLecture
	var wire struct {
		seenLectureAlias
		LegacyOutputPath string `json:"outputPath"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*s = SeenLecture(wire.seenLectureAlias)
	if s.OutputPath == "" {
		s.OutputPath = wire.LegacyOutputPath
	}
	return nil
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

// LoadStore reads state from path, or returns an empty store if the file is
// missing. Corrupt state is fatal because silently resetting deduplication can
// upload every previously completed lecture again.
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
		return nil, fmt.Errorf("decode watch state %s: %w", path, err)
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
	previousCourse, hadCourse := s.data.Courses[key]
	course := CourseState{SeenTTIDs: make(map[string]SeenLecture, len(previousCourse.SeenTTIDs)+1)}
	for existingTTID, existingLecture := range previousCourse.SeenTTIDs {
		course.SeenTTIDs[existingTTID] = existingLecture
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if existing, ok := course.SeenTTIDs[strconv.Itoa(ttid)]; ok {
		lecture = mergeSeenLecture(existing, lecture)
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
	previousVersion, previousUpdatedAt := s.data.Version, s.data.UpdatedAt
	s.data.Courses[key] = course
	s.data.Version = stateVersion
	s.data.UpdatedAt = now
	if err := s.saveLocked(); err != nil {
		if hadCourse {
			s.data.Courses[key] = previousCourse
		} else {
			delete(s.data.Courses, key)
		}
		s.data.Version = previousVersion
		s.data.UpdatedAt = previousUpdatedAt
		return err
	}
	return nil
}

func mergeSeenLecture(existing, lecture SeenLecture) SeenLecture {
	if lecture.FirstSeenAt == "" {
		lecture.FirstSeenAt = existing.FirstSeenAt
	}
	if lecture.Attempts == 0 {
		lecture.Attempts = existing.Attempts
	}
	if lecture.OutputPath == "" {
		lecture.OutputPath = existing.OutputPath
	}
	if lecture.NotebookID == "" {
		lecture.NotebookID = existing.NotebookID
	}
	if lecture.UploadKey == "" {
		lecture.UploadKey = existing.UploadKey
	}
	if lecture.Status == "" {
		lecture.Status = existing.Status
		lecture.Uploaded = existing.Uploaded
		if lecture.SourceID == "" {
			lecture.SourceID = existing.SourceID
		}
		if lecture.Error == "" {
			lecture.Error = existing.Error
		}
	}
	return lecture
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

// atomicWriteFile writes data through a uniquely-created, synced temporary
// file, then atomically replaces the live snapshot.
func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return fmt.Errorf("create watch state directory: %w", err)
	}

	tmp, err := os.CreateTemp(parent, "."+filepath.Base(path)+".tmp-*") // #nosec G304 -- operator-configured state directory
	if err != nil {
		return fmt.Errorf("create watch state temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath) //nolint:errcheck // best-effort cleanup
		}
	}()
	if err := writeAndSyncTemp(tmp, data, mode); err != nil {
		return err
	}
	if err := replaceStateFile(tmpPath, path); err != nil {
		return fmt.Errorf("replace watch state file: %w", err)
	}
	cleanup = false
	if err := syncStateDirectory(parent); err != nil {
		return fmt.Errorf("sync watch state directory: %w", err)
	}
	return nil
}

func writeAndSyncTemp(tmp *os.File, data []byte, mode os.FileMode) error {
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
