package client

import (
	"errors"
	"fmt"
)

type LoginResponse struct {
	Message  string `json:"message"`
	Token    string `json:"token"`
	UserType int    `json:"userType"`
	Success  bool   `json:"success"`
}

type Courses []Course

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

type Lectures []Lecture

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

type StreamInfo struct {
	Quality string
	URL     string
}

type ParsedPlaylist struct {
	KeyURL           string
	Title            string
	FirstViewURLs    []string
	SecondViewURLs   []string
	ID               int
	SeqNo            int
	HasMultipleViews bool
}

func (l Lectures) Reverse() Lectures {
	reversed := make(Lectures, len(l))
	for i := range l {
		reversed[i] = l[len(l)-1-i]
	}
	return reversed
}

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
