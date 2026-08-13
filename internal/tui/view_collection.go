package tui

import (
	"fmt"
	"strings"

	"github.com/rabesss/impartus-cli/internal/secrets"
)

func (model Model) collectionTitle(value screen) string {
	switch value {
	case screenCourses:
		return "Courses"
	case screenLectures:
		subject := terminalText(model.course.SubjectName)
		if subject == "" {
			return "Lectures"
		}
		return "Lectures · " + subject
	case screenLibrary:
		return "Library"
	case screenDiagnostics:
		return "Diagnostics"
	case screenResume, screenPlayback, screenDetails:
		return "Collection"
	}
	return "Collection"
}

func (model Model) renderCollectionBody(value screen, rows int) []string {
	if rows <= 0 {
		return nil
	}
	if model.err != nil && value == model.screen {
		return []string{model.styles.danger.Render("ERROR: " + terminalText(secrets.ScrubError(model.err))), "Press r to retry"}
	}

	filterVisible := (value == screenCourses || value == screenLectures) && (model.filtering || model.filter.Value() != "")
	result := make([]string, 0, rows)
	if filterVisible {
		result = append(result, model.filter.View())
		rows--
	}
	count, label := model.collectionLabels(value)
	if count == 0 {
		if model.loading {
			return append(result, model.styles.warning.Render("LOADING: Loading current collection…"))
		}
		return append(result, model.styles.muted.Render("EMPTY: "+emptyCollectionMessage(value)))
	}
	if rows <= 0 {
		return result
	}
	start, end := visibleBounds(count, model.cursor, rows)
	for index := start; index < end; index++ {
		prefix := "  "
		if index == model.cursor {
			prefix = "> "
		}
		line := prefix + label(index)
		if index == model.cursor {
			line = model.styles.selected.Render(line)
		}
		result = append(result, line)
	}
	return result
}

func (model Model) collectionLabels(value screen) (int, func(int) string) {
	switch value {
	case screenCourses:
		items := model.visibleCourses()
		return len(items), func(index int) string {
			course := items[index]
			return fmt.Sprintf("%s · %s · %d lectures", terminalText(course.SubjectName), terminalText(course.ProfessorName), course.VideoCount)
		}
	case screenLectures:
		items := model.visibleLectures()
		return len(items), func(index int) string {
			lecture := items[index]
			return fmt.Sprintf("%03d  %s", lecture.SeqNo, terminalText(lecture.Topic))
		}
	case screenLibrary:
		return len(model.artifacts), func(index int) string {
			manifest := model.artifacts[index].Manifest
			return fmt.Sprintf("%03d  %s · %d file(s)", manifest.Lecture.SeqNo, terminalText(manifest.Lecture.Topic), len(model.artifacts[index].Files))
		}
	case screenDiagnostics:
		return len(model.diagnostics), func(index int) string {
			diagnostic := model.diagnostics[index]
			return fmt.Sprintf("[%s] %s — %s", strings.ToUpper(terminalText(diagnostic.Status)), terminalText(diagnostic.Name), terminalText(diagnostic.Detail))
		}
	case screenResume, screenPlayback, screenDetails:
	}
	return 0, func(int) string { return "" }
}

func visibleBounds(count, cursor, rows int) (int, int) {
	rows = max(1, rows)
	start := 0
	if cursor >= rows {
		start = cursor - rows + 1
	}
	return start, min(count, start+rows)
}

func emptyCollectionMessage(value screen) string {
	switch value {
	case screenCourses:
		return "No courses available"
	case screenLectures:
		return "No lectures available"
	case screenLibrary:
		return "No downloaded artifacts"
	case screenDiagnostics:
		return "No diagnostics reported"
	case screenResume, screenPlayback, screenDetails:
		return "No items available"
	}
	return "No items available"
}

func (model Model) renderCompactBody(rows int) (string, []string) {
	switch model.screen {
	case screenResume:
		return "Resume", []string{
			fmt.Sprintf("Resume %s from %s?", terminalText(model.lecture.Topic), formatClock(model.resume.PositionSeconds)),
			"y/enter resume · n restart · esc back",
		}
	case screenPlayback:
		if model.playbackFinishing {
			return "Playing", []string{"Stopping playback for " + terminalText(model.lecture.Topic) + "…", "Saving the latest playback checkpoint"}
		}
		return "Playing", []string{
			"Playing " + terminalText(model.lecture.Topic) + " in mpv",
			fmt.Sprintf("%s / %s · volume %.0f%% · speed %.2fx", formatClock(model.position), formatClock(model.duration), model.volume, model.speed),
			"space pause · ←/→ seek · m mute · +/- volume · [/] speed · v camera · esc stop",
		}
	case screenDetails:
		return "Lecture details", model.lectureMetadata()
	case screenCourses, screenLectures, screenLibrary, screenDiagnostics:
		value := model.workspaceCollectionScreen()
		return model.collectionTitle(value), model.renderCollectionBody(value, rows)
	}
	return "Collection", nil
}
