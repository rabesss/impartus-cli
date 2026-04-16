package client

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
	Coverpic             string `json:"coverpic"`
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
	SubjectDescription  any    `json:"subjectDescription"`
	SessionName         string `json:"sessionName"`
	ClassroomName       string `json:"classroomName"`
	FilePath2           string `json:"filePath2"`
	FilePath            string `json:"filePath"`
	EndTime             string `json:"endTime"`
	Topic               string `json:"topic"`
	StartTime           string `json:"startTime"`
	Coverpic            string `json:"coverpic"`
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
	Ttid                int    `json:"ttid"`
	Selfenroll          int    `json:"selfenroll"`
	SubjectID           int    `json:"subjectId"`
	ActualDuration      int    `json:"actualDuration"`
	ClassroomID         int    `json:"classroomId"`
	Type                int    `json:"type"`
	Status              int    `json:"status"`
	SlideCount          int    `json:"slideCount"`
	Noaudio             int    `json:"noaudio"`
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
		if lecture.Noaudio == 1 {
			continue
		}
		filtered = append(filtered, lecture)
	}
	return filtered
}
