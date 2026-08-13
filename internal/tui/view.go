package tui

import (
	"fmt"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rabesss/impartus-cli/internal/secrets"
)

// View renders the current immutable model state. It performs no application,
// filesystem, network, database, or subprocess work.
func (model Model) View() tea.View {
	view := tea.NewView(model.renderShell())
	view.AltScreen = true
	view.WindowTitle = "Impartus"
	return view
}

func (model Model) heading() string {
	switch model.screen {
	case screenCourses:
		return "Impartus › Courses"
	case screenLectures:
		return fmt.Sprintf("Impartus › %s", terminalText(model.course.SubjectName))
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

// terminalText strips escape sequences and flattens control characters before
// backend-provided text reaches the terminal. Styling is applied only after
// this boundary, so application-owned ANSI sequences remain intact.
func terminalText(value string) string {
	value = secrets.Scrub(value)
	value = ansi.Strip(value)
	value = strings.Map(func(r rune) rune {
		if r == '\u200d' {
			return r
		}
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			return ' '
		}
		return r
	}, value)
	return strings.Join(strings.Fields(value), " ")
}

func formatClock(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	total := int(seconds)
	if total >= 3600 {
		return fmt.Sprintf("%02d:%02d:%02d", total/3600, (total%3600)/60, total%60)
	}
	return fmt.Sprintf("%02d:%02d", total/60, total%60)
}

func formatDuration(seconds int) string {
	if seconds < 0 {
		seconds = 0
	}
	return fmt.Sprintf("%02d:%02d:%02d", seconds/3600, (seconds%3600)/60, seconds%60)
}
