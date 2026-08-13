package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/rabesss/impartus-cli/internal/secrets"
)

func (model Model) renderHeader(width int) string {
	left := model.styles.title.Render(model.heading())
	right := model.styles.muted.Render(model.headerState())
	return fitSides(left, right, width)
}

func (model Model) headerState() string {
	switch {
	case model.screen == screenPlayback || model.playback != nil:
		return "PLAYING · " + formatClock(model.position)
	case model.loading:
		return "LOADING"
	case model.err != nil:
		return "ERROR"
	case model.status != "":
		return "STATUS"
	default:
		return model.layout().mode.String() + " · " + model.effectiveFocus().String()
	}
}

func (model Model) renderNavigationBody() []string {
	active := model.activeSection()
	labels := []string{"Courses", "Library", "Diagnostics"}
	rows := make([]string, len(labels))
	for index, label := range labels {
		cursor := "  "
		if index == model.navigationCursor {
			cursor = "> "
		}
		current := ""
		if index == active {
			current = " [CURRENT]"
		}
		rows[index] = cursor + label + current
		if index == model.navigationCursor {
			rows[index] = model.styles.selected.Render(rows[index])
		}
	}
	return rows
}

func (model Model) activeSection() int {
	switch model.workspaceCollectionScreen() {
	case screenCourses, screenLectures:
		return 0
	case screenLibrary:
		return 1
	case screenDiagnostics:
		return 2
	case screenResume, screenPlayback, screenDetails:
		return 0
	}
	return 0
}

func (model Model) renderActivity(width, height int) string {
	label := "STATUS"
	line := terminalText(model.status)
	lineStyle := model.styles.text
	switch {
	case model.err != nil:
		label = "ERROR"
		line = terminalText(secrets.ScrubError(model.err))
		lineStyle = model.styles.danger
	case model.screen == screenPlayback || model.playback != nil:
		label = "ACTIVE PLAYBACK"
		state := "PLAYING"
		if model.playbackFinishing {
			state = "STOPPING"
		} else if model.paused {
			state = "PAUSED"
		}
		mute := "AUDIO"
		if model.muted {
			mute = "MUTED"
		}
		line = fmt.Sprintf("%s · %s / %s · %.0f%% · %.2fx · %s · %s", state, formatClock(model.position), formatClock(model.duration), model.volume, model.speed, mute, terminalText(model.lecture.Topic))
		lineStyle = model.styles.success
	case model.loading:
		label = "LOADING"
		if line == "" {
			line = "Loading current operation…"
		} else {
			line = "Loading: " + line
		}
		lineStyle = model.styles.warning
	case model.status != "":
		label = "STATUS"
		lineStyle = model.styles.success
	}
	return model.renderPane("Activity · "+label, width, height, model.effectiveFocus() == paneActivity, []string{lineStyle.Render(line)})
}

func (model Model) renderFooter(width int) string {
	parts := make([]string, 0, 8)
	used := 0
	for _, candidate := range model.contextualCommands(false) {
		if !candidate.footer {
			continue
		}
		part := commandHint(candidate)
		separator := 0
		if len(parts) > 0 {
			separator = 3
		}
		if used+separator+ansi.StringWidth(part) > width {
			continue
		}
		parts = append(parts, part)
		used += separator + ansi.StringWidth(part)
	}
	return fitCellLine(model.styles.muted.Render(strings.Join(parts, " · ")), width)
}
