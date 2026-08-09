package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
)

// View renders a bounded responsive screen and always reserves Bubble Tea's
// alternate screen so mpv never competes for terminal ownership.
func (model Model) View() tea.View {
	view := tea.NewView(model.render())
	view.AltScreen = true
	view.WindowTitle = "Impartus"
	return view
}

func (model Model) render() string {
	var body strings.Builder
	body.WriteString(titleStyle.Render(model.heading()))
	body.WriteByte('\n')
	if model.loading {
		body.WriteString("Loading…")
	} else if model.err != nil {
		body.WriteString(errorStyle.Render(model.err.Error()))
	} else if model.screen == screenResume {
		appendf(&body, "Resume %s from %s?\n", model.lecture.Topic, formatClock(model.resume.PositionSeconds))
		body.WriteString("y/enter resume • n restart • esc back")
	} else if model.screen == screenPlayback {
		if model.playbackFinishing {
			appendf(&body, "Stopping playback for %s…\n", model.lecture.Topic)
			body.WriteString("Saving the latest playback checkpoint")
		} else {
			appendf(&body, "Playing %s in mpv\n", model.lecture.Topic)
			appendf(&body, "%s / %s  •  volume %.0f%%  •  speed %.2fx\n", formatClock(model.position), formatClock(model.duration), model.volume, model.speed)
			body.WriteString("space pause • ←/→ seek • m mute • v camera • esc stop")
		}
	} else if model.screen == screenDetails {
		appendf(&body, "Topic: %s\n", model.lecture.Topic)
		appendf(&body, "Professor: %s\n", model.lecture.ProfessorName)
		appendf(&body, "Classroom: %s\n", model.lecture.ClassroomName)
		appendf(&body, "Started: %s\n", model.lecture.StartTime)
		appendf(&body, "Duration: %s\n", formatDuration(model.lecture.ActualDuration))
		body.WriteString("esc back")
	} else {
		if model.filtering || model.filter.Value() != "" {
			body.WriteString(model.filter.View())
			body.WriteByte('\n')
		}
		model.renderItems(&body)
	}
	if model.status != "" && model.err == nil && model.screen != screenPlayback {
		body.WriteByte('\n')
		body.WriteString(dimStyle.Render(model.status))
	}
	body.WriteByte('\n')
	body.WriteString(dimStyle.Render(model.help.View(model.keys)))
	return clampLines(body.String(), model.width, model.height)
}

func (model Model) heading() string {
	switch model.screen {
	case screenCourses:
		return "Impartus › Courses"
	case screenLectures:
		return fmt.Sprintf("Impartus › %s", model.course.SubjectName)
	case screenLibrary:
		return "Impartus › Library"
	case screenResume:
		return "Impartus › Resume"
	case screenPlayback:
		return "Impartus › Playing"
	case screenDiagnostics:
		return "Impartus › Diagnostics"
	case screenDetails:
		return "Impartus › Lecture details"
	}
	return "Impartus"
}

func appendf(body *strings.Builder, format string, values ...any) {
	body.WriteString(string(fmt.Appendf(nil, format, values...)))
}

func (model Model) renderItems(body *strings.Builder) {
	count := model.itemCount()
	if count == 0 {
		body.WriteString(dimStyle.Render("No items available"))
		return
	}
	reservedRows := 3
	if model.filtering || model.filter.Value() != "" {
		reservedRows++
	}
	visibleRows := max(1, model.height-reservedRows)
	start := 0
	if model.cursor >= visibleRows {
		start = model.cursor - visibleRows + 1
	}
	end := min(count, start+visibleRows)
	for index := start; index < end; index++ {
		prefix := "  "
		label := model.itemLabel(index)
		if index == model.cursor {
			prefix = "› "
			label = selectedStyle.Render(label)
		}
		body.WriteString(prefix)
		body.WriteString(label)
		if index+1 < end {
			body.WriteByte('\n')
		}
	}
}

func (model Model) itemLabel(index int) string {
	if model.screen == screenCourses {
		course := model.visibleCourses()[index]
		return fmt.Sprintf("%s  ·  %s  ·  %d lectures", course.SubjectName, course.ProfessorName, course.VideoCount)
	}
	if model.screen == screenLibrary {
		manifest := model.artifacts[index].Manifest
		return fmt.Sprintf("%03d  %s  ·  %d file(s)", manifest.Lecture.SeqNo, manifest.Lecture.Topic, len(model.artifacts[index].Files))
	}
	if model.screen == screenDiagnostics {
		diagnostic := model.diagnostics[index]
		return fmt.Sprintf("[%s] %s — %s", strings.ToUpper(diagnostic.Status), diagnostic.Name, diagnostic.Detail)
	}
	lecture := model.visibleLectures()[index]
	return fmt.Sprintf("%03d  %s", lecture.SeqNo, lecture.Topic)
}

func clampLines(content string, width, height int) string {
	width = max(1, width)
	height = max(1, height)
	lines := strings.Split(content, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for index, line := range lines {
		lines[index] = lipgloss.NewStyle().MaxWidth(width).Render(line)
	}
	return strings.Join(lines, "\n")
}

func formatClock(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	total := int(seconds)
	return fmt.Sprintf("%02d:%02d", total/60, total%60)
}

func formatDuration(seconds int) string {
	if seconds < 0 {
		seconds = 0
	}
	return fmt.Sprintf("%02d:%02d:%02d", seconds/3600, (seconds%3600)/60, seconds%60)
}
