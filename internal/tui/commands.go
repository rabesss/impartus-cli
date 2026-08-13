package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

type commandID string

const (
	commandQuit           commandID = "app.quit"
	commandMove           commandID = "selection.move"
	commandSelect         commandID = "selection.open"
	commandFocusNext      commandID = "focus.next"
	commandFocusPrevious  commandID = "focus.previous"
	commandPalette        commandID = "overlay.palette"
	commandHelp           commandID = "overlay.help"
	commandNavigation     commandID = "navigation.open"
	commandCourses        commandID = "navigation.courses"
	commandLibrary        commandID = "navigation.library"
	commandDiagnostics    commandID = "navigation.diagnostics"
	commandFilter         commandID = "collection.filter"
	commandDetails        commandID = "lecture.details"
	commandDownload       commandID = "lecture.download"
	commandRetry          commandID = "collection.retry"
	commandBack           commandID = "navigation.back"
	commandPlayback       commandID = "playback.control"
	commandResumeLecture  commandID = "playback.resume"
	commandRestartLecture commandID = "playback.restart"
)

type commandAvailability struct {
	visible bool
	enabled bool
	reason  string
}

type commandAction func(Model, string) (Model, tea.Cmd)

type command struct {
	id        commandID
	label     string
	hint      string
	keys      []string
	context   func(Model) commandAvailability
	action    commandAction
	palette   bool
	footer    bool
	inspector bool
}

var commands []command

func init() {
	commands = []command{
		{id: commandMove, label: "Move", hint: "↑/↓", keys: []string{"up", "down", "k", "j"}, context: moveContext, action: moveAction, footer: true},
		{id: commandSelect, label: "Open / play", hint: "enter", keys: []string{"enter"}, context: selectContext, action: selectAction, footer: true, inspector: true},
		{id: commandDetails, label: "Lecture details", hint: "i", keys: []string{"i"}, context: lectureContext, action: detailsAction, palette: true, footer: true, inspector: true},
		{id: commandDownload, label: "Download lecture", hint: "d", keys: []string{"d"}, context: lectureContext, action: downloadAction, palette: true, footer: true, inspector: true},
		{id: commandPlayback, label: "Control mpv", hint: "space pause · ←/→ seek · +/- volume · [/] speed · v camera", keys: []string{"space", "left", "right", "m", "v", "+", "=", "-", "[", "]"}, context: playbackContext, action: playbackAction, footer: true, inspector: true},
		{id: commandResumeLecture, label: "Resume lecture", hint: "y", keys: []string{"y"}, context: resumeContext, action: resumeAction, footer: true, inspector: true},
		{id: commandRestartLecture, label: "Restart lecture", hint: "n", keys: []string{"n"}, context: resumeContext, action: restartAction, footer: true, inspector: true},
		{id: commandFilter, label: "Filter", hint: "/", keys: []string{"/"}, context: filterContext, action: filterAction, palette: true, footer: true},
		{id: commandRetry, label: "Retry", hint: "r", keys: []string{"r"}, context: retryContext, action: retryAction, palette: true, footer: true},
		{id: commandFocusNext, label: "Next pane", hint: "tab", keys: []string{"tab"}, context: focusContext, action: focusNextAction, palette: true, footer: true},
		{id: commandFocusPrevious, label: "Previous pane", hint: "shift+tab", keys: []string{"shift+tab"}, context: focusContext, action: focusPreviousAction, palette: true},
		{id: commandNavigation, label: "Navigation", hint: "g", keys: []string{"g"}, context: navigationContext, action: navigationAction, palette: true, footer: true},
		{id: commandCourses, label: "Open courses", hint: "c", keys: []string{"c"}, context: sectionContext, action: coursesAction, palette: true},
		{id: commandLibrary, label: "Open library", hint: "l", keys: []string{"l"}, context: sectionContext, action: libraryAction, palette: true, footer: true},
		{id: commandDiagnostics, label: "Open diagnostics", hint: "!", keys: []string{"!"}, context: sectionContext, action: diagnosticsAction, palette: true, footer: true},
		{id: commandPalette, label: "Commands", hint: "ctrl+p", keys: []string{"ctrl+p"}, context: baseContext, action: paletteAction, footer: true},
		{id: commandHelp, label: "Help", hint: "?", keys: []string{"?"}, context: baseContext, action: helpAction, footer: true},
		{id: commandBack, label: "Close / back", hint: "esc", keys: []string{"esc", "backspace"}, context: backContext, action: backAction, footer: true},
		{id: commandQuit, label: "Quit", hint: "q", keys: []string{"q", "ctrl+c"}, context: baseContext, action: quitAction, palette: true, footer: true},
	}
}

func available(visible, enabled bool, reason string) commandAvailability {
	return commandAvailability{visible: visible, enabled: enabled, reason: reason}
}

func baseContext(Model) commandAvailability {
	return available(true, true, "")
}

func moveContext(model Model) commandAvailability {
	if overlay, ok := model.topOverlay(); ok {
		if overlay.kind == overlayPalette {
			return available(true, true, "")
		}
		return available(overlay.kind == overlayNavigation, true, "")
	}
	if model.loading {
		return available(false, false, "")
	}
	focus := model.effectiveFocus()
	if focus == paneNavigation {
		return available(true, true, "")
	}
	visible := focus == paneCollection && isCollectionScreen(model.screen)
	return available(visible, model.itemCount() > 1, "Collection has one or fewer items")
}

func selectContext(model Model) commandAvailability {
	if overlay, ok := model.topOverlay(); ok {
		switch overlay.kind {
		case overlayPalette:
			return available(true, true, "")
		case overlayNavigation:
			return available(true, true, "")
		case overlayHelp:
			return available(false, false, "")
		}
	}
	if model.loading {
		return available(false, false, "")
	}
	if model.effectiveFocus() == paneNavigation {
		return available(true, true, "")
	}
	switch model.screen {
	case screenCourses, screenLectures:
		return available(model.effectiveFocus() != paneActivity, model.err == nil && model.itemCount() > 0, "No selection")
	case screenResume:
		return available(true, model.err == nil, "Resume state is unavailable")
	case screenLibrary, screenPlayback, screenDiagnostics, screenDetails:
		return available(false, false, "")
	}
	return available(false, false, "")
}

func lectureContext(model Model) commandAvailability {
	visible := model.screen == screenLectures && model.effectiveFocus() != paneNavigation && model.effectiveFocus() != paneActivity
	return available(visible, !model.loading && model.err == nil && model.itemCount() > 0, "No lecture selected")
}

func playbackContext(model Model) commandAvailability {
	visible := model.screen == screenPlayback
	enabled := visible && model.playback != nil && !model.playbackFinishing
	return available(visible, enabled, "Playback is stopping")
}

func resumeContext(model Model) commandAvailability {
	visible := model.screen == screenResume
	return available(visible, visible && !model.loading && model.err == nil, "Resume state is unavailable")
}

func filterContext(model Model) commandAvailability {
	visible := (model.screen == screenCourses || model.screen == screenLectures) && model.effectiveFocus() == paneCollection
	return available(visible, visible && !model.loading, "Collection is loading")
}

func retryContext(model Model) commandAvailability {
	visible := isCollectionScreen(model.screen)
	return available(visible, visible && !model.loading, "An operation is already running")
}

func focusContext(model Model) commandAvailability {
	_, overlayOpen := model.topOverlay()
	visible := !overlayOpen && len(model.visibleFocuses()) > 1
	return available(visible, visible, "Only one pane is visible")
}

func navigationContext(model Model) commandAvailability {
	_, overlayOpen := model.topOverlay()
	visible := !overlayOpen && model.screen != screenPlayback && model.screen != screenResume
	return available(visible, visible && !model.loading, "Navigation is unavailable during an operation")
}

func sectionContext(model Model) commandAvailability {
	visible := model.screen != screenPlayback && model.screen != screenResume
	return available(visible, visible && !model.loading, "Unavailable during an operation")
}

func backContext(model Model) commandAvailability {
	if _, ok := model.topOverlay(); ok {
		return available(true, true, "")
	}
	return available(model.screen != screenCourses, !model.loading, "An operation is running")
}

func commandForKey(model Model, key string) (command, commandAvailability, bool) {
	for _, candidate := range commands {
		if !slicesContains(candidate.keys, key) {
			continue
		}
		state := candidate.context(model)
		if state.visible {
			return candidate, state, true
		}
	}
	return command{}, commandAvailability{}, false
}

func slicesContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (model Model) paletteCommands() []command {
	query := strings.ToLower(strings.TrimSpace(model.palette.Value()))
	matches := make([]command, 0, len(commands))
	for _, candidate := range commands {
		state := candidate.context(model)
		if !candidate.palette || !state.visible {
			continue
		}
		haystack := strings.ToLower(string(candidate.id) + " " + candidate.label + " " + strings.Join(candidate.keys, " "))
		if query == "" || strings.Contains(haystack, query) {
			matches = append(matches, candidate)
		}
	}
	return matches
}
