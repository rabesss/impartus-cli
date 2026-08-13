package tui

import (
	"fmt"
	"strings"

	"github.com/rabesss/impartus-cli/internal/client"
)

func (model Model) renderInspectorBody(rows int) []string {
	if rows <= 0 {
		return nil
	}
	body := model.inspectorMetadata()
	if len(body) == 0 {
		body = []string{model.styles.muted.Render("No selection")}
	}
	actions := model.inspectorActions()
	if len(actions) > 0 {
		body = append(body, "", model.styles.title.Render("Actions"))
		body = append(body, actions...)
	}
	return body[:min(rows, len(body))]
}

func (model Model) inspectorMetadata() []string {
	switch model.screen {
	case screenResume:
		return append(model.lectureMetadata(), "Resume from: "+formatClock(model.resume.PositionSeconds))
	case screenPlayback:
		playbackState := "Playing " + terminalText(model.lecture.Topic) + " in mpv"
		if model.playbackFinishing {
			playbackState = "Stopping playback for " + terminalText(model.lecture.Topic) + "…"
		}
		body := append([]string{playbackState}, model.lectureMetadata()...)
		body = append(body,
			fmt.Sprintf("Position: %s / %s", formatClock(model.position), formatClock(model.duration)),
			fmt.Sprintf("Volume: %.0f%%", model.volume),
			fmt.Sprintf("Speed: %.2fx", model.speed),
			"space pause · ←/→ seek · m mute",
			"+/- volume · [/] speed · v camera",
		)
		return body
	case screenDetails:
		return model.lectureMetadata()
	case screenCourses, screenLectures, screenLibrary, screenDiagnostics:
	}
	return model.collectionInspectorMetadata()
}

func (model Model) collectionInspectorMetadata() []string {
	switch model.workspaceCollectionScreen() {
	case screenCourses:
		return model.courseInspectorMetadata()
	case screenLectures:
		lecture, ok := model.selectedLecture()
		if ok {
			return model.lectureMetadataFor(lecture)
		}
	case screenLibrary:
		return model.libraryInspectorMetadata()
	case screenDiagnostics:
		return model.diagnosticInspectorMetadata()
	case screenResume, screenPlayback, screenDetails:
	}
	return nil
}

func (model Model) courseInspectorMetadata() []string {
	course, ok := model.selectedCourse()
	if !ok {
		return nil
	}
	return []string{
		model.styles.title.Render(terminalText(course.SubjectName)),
		metadataLine("Professor", course.ProfessorName),
		metadataLine("Session", course.SessionName),
		metadataLine("Department", course.Department),
		fmt.Sprintf("Lectures: %d", course.VideoCount),
	}
}

func (model Model) libraryInspectorMetadata() []string {
	if model.cursor < 0 || model.cursor >= len(model.artifacts) {
		return nil
	}
	record := model.artifacts[model.cursor]
	manifest := record.Manifest
	present := 0
	for _, file := range record.Files {
		if file.Present {
			present++
		}
	}
	return []string{
		model.styles.title.Render(terminalText(manifest.Lecture.Topic)),
		metadataLine("Artifact", manifest.ArtifactID),
		fmt.Sprintf("Sequence: %03d", manifest.Lecture.SeqNo),
		fmt.Sprintf("Files: %d (%d present)", len(record.Files), present),
		metadataLine("Views", manifest.Selection.Views),
		metadataLine("Quality", manifest.Selection.Quality),
		metadataLine("Produced", manifest.ProducedAt.Format("2006-01-02 15:04 UTC")),
	}
}

func (model Model) diagnosticInspectorMetadata() []string {
	if model.cursor < 0 || model.cursor >= len(model.diagnostics) {
		return nil
	}
	diagnostic := model.diagnostics[model.cursor]
	return []string{
		model.styles.title.Render(terminalText(diagnostic.Name)),
		metadataLine("Status", strings.ToUpper(diagnostic.Status)),
		metadataLine("Detail", diagnostic.Detail),
	}
}

func (model Model) selectedCourse() (client.Course, bool) {
	items := model.visibleCourses()
	if model.cursor < 0 || model.cursor >= len(items) {
		return client.Course{}, false
	}
	return items[model.cursor], true
}

func (model Model) selectedLecture() (client.Lecture, bool) {
	switch model.screen {
	case screenResume, screenPlayback, screenDetails:
		return model.lecture, model.lecture.Topic != "" || model.lecture.TTID != 0
	case screenCourses, screenLectures, screenLibrary, screenDiagnostics:
	}
	items := model.visibleLectures()
	if model.cursor < 0 || model.cursor >= len(items) {
		return client.Lecture{}, false
	}
	return items[model.cursor], true
}

func (model Model) lectureMetadata() []string {
	return model.lectureMetadataFor(model.lecture)
}

func (model Model) lectureMetadataFor(lecture client.Lecture) []string {
	return []string{
		model.styles.title.Render(terminalText(lecture.Topic)),
		metadataLine("Professor", lecture.ProfessorName),
		metadataLine("Classroom", lecture.ClassroomName),
		metadataLine("Started", lecture.StartTime),
		"Duration: " + formatDuration(lecture.ActualDuration),
		metadataLine("Session", lecture.SessionName),
	}
}

func metadataLine(label, value string) string {
	value = terminalText(value)
	if value == "" {
		value = "—"
	}
	return label + ": " + value
}

func (model Model) inspectorActions() []string {
	actions := make([]string, 0, 4)
	contextModel := model
	contextModel.focus = paneInspector
	for _, candidate := range commands {
		state := candidate.context(contextModel)
		if candidate.inspector && state.visible && state.enabled {
			actions = append(actions, candidate.hint+"  "+candidate.label)
		}
	}
	return actions
}
