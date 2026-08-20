package client

import (
	"errors"
	"fmt"
	"strings"
)

// LoginResponse represents the response from the Impartus authentication endpoint.
type LoginResponse struct {
	Message  string `json:"message"`
	Token    string `json:"token"`
	UserType int    `json:"userType"`
	Success  bool   `json:"success"`
}

// Courses is a collection of Course entries returned by the Impartus API.
type Courses []Course

// Course preserves the upstream API shape because course payloads are returned
// to CLI JSON mode and API consumers without projection. Fields that are not
// used in local business logic may still be part of that public payload contract.
type Course struct {
	Institute            string `json:"institute"`
	SubjectName          string `json:"subjectName"`
	SessionName          string `json:"sessionName"`
	ProfessorName        string `json:"professorName"`
	Department           string `json:"department"`
	CoverPic             string `json:"coverpic"`
	SessionID            int    `json:"sessionId"`
	ProfessorID          int    `json:"professorId"`
	DepartmentID         int    `json:"departmentId"`
	InstituteID          int    `json:"instituteId"`
	SubjectID            int    `json:"subjectId"`
	VideoCount           int    `json:"videoCount"`
	FlippedLecturesCount int    `json:"flippedLecturesCount"`
}

// Lectures is a collection of Lecture entries returned by the Impartus API.
type Lectures []Lecture

// ErrNoLecturesAfterFiltering reports that a valid range became empty only
// because every selected lecture was marked no-audio.
var ErrNoLecturesAfterFiltering = errors.New("no lectures available after filtering")

const unsupportedQualityLabel = "unsupported"

// QualityUnavailableError reports an exact requested quality that Impartus did
// not expose. Its message contains only validated quality labels or a fixed
// sentinel, so API boundaries never return arbitrary upstream text.
type QualityUnavailableError struct {
	requested string
	available []string
}

func newQualityUnavailableError(requested string, available []string) *QualityUnavailableError {
	return &QualityUnavailableError{
		requested: requested,
		available: append([]string(nil), available...),
	}
}

func (err *QualityUnavailableError) Error() string {
	if err == nil {
		return "requested quality is unavailable"
	}
	requested := acceptedQualityLabel(err.requested)
	if requested == "" {
		requested = unsupportedQualityLabel
	}
	return fmt.Sprintf("requested quality %q is unavailable; available qualities: %s", requested, strings.Join(err.available, ", "))
}

// Lecture likewise mirrors the upstream payload. The downloader uses only a
// subset of fields, but the full struct is retained so downstream JSON output
// stays faithful to the upstream schema exposed by this application.
type Lecture struct {
	SubjectDescription  string `json:"subjectDescription"`
	SessionName         string `json:"sessionName"`
	ClassroomName       string `json:"classroomName"`
	FilePath2           string `json:"filePath2"`
	FilePath            string `json:"filePath"`
	EndTime             string `json:"endTime"`
	Topic               string `json:"topic"`
	StartTime           string `json:"startTime"`
	CoverPic            string `json:"coverpic"`
	SubjectCode         string `json:"subjectCode"`
	ProfessorImageURL   string `json:"professorImageUrl"`
	ProfessorName       string `json:"professorName"`
	Institute           string `json:"institute"`
	SubjectName         string `json:"subjectName"`
	Department          string `json:"department"`
	VideoID             int    `json:"videoId"`
	TapNToggle          int    `json:"tapNToggle"`
	Trending            int    `json:"trending"`
	SeqNo               int    `json:"seqNo"`
	DepartmentID        int    `json:"departmentId"`
	ProfessorID         int    `json:"professorId"`
	InstituteID         int    `json:"instituteId"`
	TTID                int    `json:"ttid"`
	SelfEnroll          int    `json:"selfenroll"`
	SubjectID           int    `json:"subjectId"`
	ActualDuration      int    `json:"actualDuration"`
	ClassroomID         int    `json:"classroomId"`
	Type                int    `json:"type"`
	Status              int    `json:"status"`
	SlideCount          int    `json:"slideCount"`
	NoAudio             int    `json:"noaudio"`
	Views               int    `json:"views"`
	DocumentCount       int    `json:"documentCount"`
	LessonPlanAvailable int    `json:"lessonPlanAvailable"`
	SessionID           int    `json:"sessionId"`
	LastPosition        int    `json:"lastPosition"`
}

// StreamInfo holds the quality label and URL for a single HLS stream variant.
type StreamInfo struct {
	Quality string
	URL     string
}

// ParsedPlaylist holds the parsed contents of an HLS playlist for a single lecture.
type ParsedPlaylist struct {
	KeyURL           string
	Title            string
	FirstViewURLs    []string
	SecondViewURLs   []string
	FirstDurations   []float64
	SecondDurations  []float64
	ID               int
	InstituteID      int
	SubjectID        int
	SessionID        int
	SeqNo            int
	HasMultipleViews bool
}

// Reverse returns a new Lectures slice with the order reversed.
func (l Lectures) Reverse() Lectures {
	reversed := make(Lectures, len(l))
	for i := range l {
		reversed[i] = l[len(l)-1-i]
	}
	return reversed
}

// FilterNoAudio returns a new Lectures slice excluding entries marked as having no audio.
func (l Lectures) FilterNoAudio() Lectures {
	filtered := make(Lectures, 0, len(l))
	for _, lecture := range l {
		if lecture.NoAudio == 1 {
			continue
		}
		filtered = append(filtered, lecture)
	}
	return filtered
}

// SelectRange returns a 1-indexed slice of the lectures.
// It reverses the lectures first (matching the platform's chronological order),
// then returns lectures[start..end] where start and end are 1-based inclusive.
// Pass start=0 or end=0 to use defaults (1 and len respectively).
func (l Lectures) SelectRange(start, end int) (Lectures, error) {
	reversed := l.Reverse()
	if len(reversed) == 0 {
		return nil, errors.New("no lectures found")
	}
	if start <= 0 {
		start = 1
	}
	if end <= 0 {
		end = len(reversed)
	}
	if start < 1 || end > len(reversed) || start > end {
		return nil, fmt.Errorf("invalid lecture range: start=%d end=%d (available 1-%d)", start, end, len(reversed))
	}
	return append(Lectures(nil), reversed[start-1:end]...), nil
}

// SelectForDownload applies the standard lecture-selection pipeline shared by
// the CLI and the server: range selection, optional no-audio filtering, and an
// empty-result guard. It returns the selected lectures and the count filtered
// out by the no-audio filter. This consolidates logic previously duplicated
// across cli_download, cli_play, and the server job executor.
func (l Lectures) SelectForDownload(start, end int, skipNoAudio bool) (Lectures, int, error) {
	selected, err := l.SelectRange(start, end)
	if err != nil {
		return nil, 0, err
	}
	filtered := 0
	if skipNoAudio {
		before := len(selected)
		selected = selected.FilterNoAudio()
		filtered = before - len(selected)
	}
	if len(selected) == 0 {
		return nil, filtered, fmt.Errorf("%w (all lectures have noaudio=1 in the selected range)", ErrNoLecturesAfterFiltering)
	}
	return selected, filtered, nil
}

// SelectForDownloadTTID selects exactly one lecture by its upstream TTID.
// Unlike range selection, TTID selection never depends on response ordering
// or sequence labels. Duplicate TTIDs are rejected, including duplicates that
// differ in their returned scope, so a caller cannot silently download the
// wrong lecture. The no-audio policy is applied after exact matching.
func (l Lectures) SelectForDownloadTTID(ttid int, skipNoAudio bool) (Lectures, int, error) {
	if ttid <= 0 {
		return nil, 0, errors.New("ttid must be positive")
	}

	matches := make(Lectures, 0, 1)
	for _, lecture := range l {
		if lecture.TTID == ttid {
			matches = append(matches, lecture)
		}
	}
	if len(matches) == 0 {
		return nil, 0, fmt.Errorf("no lecture found for ttid %d", ttid)
	}
	if len(matches) > 1 {
		return nil, 0, fmt.Errorf("multiple lectures found for ttid %d", ttid)
	}

	if skipNoAudio && matches[0].NoAudio == 1 {
		return nil, 1, fmt.Errorf("%w (selected lecture ttid %d has noaudio=1)", ErrNoLecturesAfterFiltering, ttid)
	}
	return append(Lectures(nil), matches...), 0, nil
}

// SelectForDownloadTTIDInScope selects exactly one TTID from the requested
// subject/session scope. The upstream lectures endpoint can return rows with
// omitted scope fields (the client overlays those fields from the request), so
// zero subject/session fields are treated as compatible here. Non-zero rows
// from another scope are ignored; duplicate matches inside the requested scope
// remain an ambiguity and fail closed.
func (l Lectures) SelectForDownloadTTIDInScope(ttid, subjectID, sessionID int, skipNoAudio bool) (Lectures, int, error) {
	if subjectID <= 0 || sessionID <= 0 {
		return nil, 0, errors.New("subject and session must be positive")
	}
	if ttid <= 0 {
		return nil, 0, errors.New("ttid must be positive")
	}

	matches := make(Lectures, 0, 1)
	for _, lecture := range l {
		if lecture.TTID != ttid {
			continue
		}
		if lecture.SubjectID != 0 && lecture.SubjectID != subjectID {
			continue
		}
		if lecture.SessionID != 0 && lecture.SessionID != sessionID {
			continue
		}
		matches = append(matches, lecture)
	}
	if len(matches) == 0 {
		return nil, 0, fmt.Errorf("no lecture found for ttid %d in subject %d session %d", ttid, subjectID, sessionID)
	}
	if len(matches) > 1 {
		return nil, 0, fmt.Errorf("multiple lectures found for ttid %d in subject %d session %d", ttid, subjectID, sessionID)
	}
	if skipNoAudio && matches[0].NoAudio == 1 {
		return nil, 1, fmt.Errorf("%w (selected lecture ttid %d has noaudio=1)", ErrNoLecturesAfterFiltering, ttid)
	}
	return append(Lectures(nil), matches...), 0, nil
}
