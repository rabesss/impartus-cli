package tui

import (
	"os"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
)

type styleSet struct {
	title    lipgloss.Style
	text     lipgloss.Style
	muted    lipgloss.Style
	border   lipgloss.Style
	focus    lipgloss.Style
	selected lipgloss.Style
	success  lipgloss.Style
	warning  lipgloss.Style
	danger   lipgloss.Style
}

func noColorEnabled() bool {
	_, present := os.LookupEnv("NO_COLOR")
	return present
}

func adaptive(light, dark string) compat.AdaptiveColor {
	return compat.AdaptiveColor{Light: lipgloss.Color(light), Dark: lipgloss.Color(dark)}
}

func newStyleSet(noColor bool) styleSet {
	styles := styleSet{
		title:    lipgloss.NewStyle().Bold(true),
		text:     lipgloss.NewStyle(),
		muted:    lipgloss.NewStyle(),
		border:   lipgloss.NewStyle(),
		focus:    lipgloss.NewStyle().Bold(true),
		selected: lipgloss.NewStyle().Bold(true),
		success:  lipgloss.NewStyle().Bold(true),
		warning:  lipgloss.NewStyle().Bold(true),
		danger:   lipgloss.NewStyle().Bold(true),
	}
	if noColor {
		return styles
	}
	styles.title = styles.title.Foreground(adaptive("#0F172A", "#E6EDF3"))
	styles.text = styles.text.Foreground(adaptive("#0F172A", "#E6EDF3"))
	styles.muted = styles.muted.Foreground(adaptive("#475569", "#94A3B8"))
	styles.border = styles.border.Foreground(adaptive("#64748B", "#94A3B8"))
	styles.focus = styles.focus.Foreground(adaptive("#0369A1", "#7DD3FC"))
	styles.selected = styles.selected.
		Foreground(adaptive("#0F172A", "#E6EDF3")).
		Background(adaptive("#DBEAFE", "#1D2A36"))
	styles.success = styles.success.Foreground(adaptive("#166534", "#86EFAC"))
	styles.warning = styles.warning.Foreground(adaptive("#854D0E", "#FDE68A"))
	styles.danger = styles.danger.Foreground(adaptive("#B91C1C", "#FCA5A5"))
	return styles
}
