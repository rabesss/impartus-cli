package client

import (
	"errors"
	"fmt"
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
		return nil, filtered, errors.New("no lectures available after filtering (all lectures have noaudio=1 in the selected range)")
	}
	return selected, filtered, nil
}
