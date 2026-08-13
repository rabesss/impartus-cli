package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

func (model Model) dispatchCommand(key string) (Model, tea.Cmd, bool) {
	candidate, state, found := commandForKey(model, key)
	if !found {
		return model, nil, false
	}
	if !state.enabled {
		return model, nil, true
	}
	updated, command := candidate.action(model, key)
	return updated, command, true
}

func (model Model) updateOverlayKey(message tea.KeyPressMsg) (Model, tea.Cmd) {
	key := message.String()
	overlay, open := model.topOverlay()
	if !open {
		return model, nil
	}
	if overlay.kind != overlayPalette || overlayControlKey(key) {
		updated, command, handled := model.dispatchCommand(key)
		if handled {
			return updated, command
		}
	}
	if overlay.kind != overlayPalette {
		return model, nil
	}
	previous := model.palette.Value()
	palette, command := model.palette.Update(message)
	model.palette = palette
	if previous != model.palette.Value() {
		model.paletteCursor = 0
	}
	return model, command
}

func overlayControlKey(key string) bool {
	switch key {
	case "up", "down", "enter", "esc", "?", "ctrl+c":
		return true
	default:
		return false
	}
}

func moveAction(model Model, key string) (Model, tea.Cmd) {
	delta := 1
	if key == "up" || key == "k" {
		delta = -1
	}
	if overlay, ok := model.topOverlay(); ok {
		switch overlay.kind {
		case overlayPalette:
			count := len(model.paletteCommands())
			if count > 0 {
				model.paletteCursor = (model.paletteCursor + delta + count) % count
			}
		case overlayNavigation:
			model.navigationCursor = (model.navigationCursor + delta + 3) % 3
		case overlayHelp:
		}
		return model, nil
	}
	if model.effectiveFocus() == paneNavigation {
		model.navigationCursor = (model.navigationCursor + delta + 3) % 3
		return model, nil
	}
	return model.moveCursor(delta), nil
}

func selectAction(model Model, _ string) (Model, tea.Cmd) {
	if overlay, ok := model.topOverlay(); ok {
		switch overlay.kind {
		case overlayPalette:
			matches := model.paletteCommands()
			if len(matches) == 0 {
				return model, nil
			}
			model.paletteCursor = min(model.paletteCursor, len(matches)-1)
			selected := matches[model.paletteCursor]
			state := selected.context(model)
			if !state.enabled {
				return model, nil
			}
			model, _ = model.closeOverlay()
			return selected.action(model, "")
		case overlayNavigation:
			model, _ = model.closeOverlay()
			return model.openNavigationSelection()
		case overlayHelp:
			return model, nil
		}
	}
	if model.effectiveFocus() == paneNavigation {
		return model.openNavigationSelection()
	}
	switch model.screen {
	case screenCourses:
		return modelFromUpdate(model.updateCoursesKey("enter"))
	case screenLectures:
		return modelFromUpdate(model.updateLecturesKey("enter"))
	case screenResume:
		return modelFromUpdate(model.updateResumeKey("enter"))
	case screenLibrary, screenPlayback, screenDiagnostics, screenDetails:
		return model, nil
	}
	return model, nil
}

func detailsAction(model Model, _ string) (Model, tea.Cmd) {
	if model.layout().mode != layoutCompact {
		model.focus = paneInspector
		return model, nil
	}
	return modelFromUpdate(model.updateLecturesKey("i"))
}

func downloadAction(model Model, _ string) (Model, tea.Cmd) {
	return modelFromUpdate(model.updateLecturesKey("d"))
}

func playbackAction(model Model, key string) (Model, tea.Cmd) {
	if key == "" {
		key = "space"
	}
	return modelFromUpdate(model.updatePlaybackKey(key))
}

func resumeAction(model Model, _ string) (Model, tea.Cmd) {
	return modelFromUpdate(model.updateResumeKey("y"))
}

func restartAction(model Model, _ string) (Model, tea.Cmd) {
	return modelFromUpdate(model.updateResumeKey("n"))
}

func filterAction(model Model, _ string) (Model, tea.Cmd) {
	return model.startFiltering()
}

func retryAction(model Model, _ string) (Model, tea.Cmd) {
	return model.retry()
}

func focusNextAction(model Model, _ string) (Model, tea.Cmd) {
	return model.moveFocus(1), nil
}

func focusPreviousAction(model Model, _ string) (Model, tea.Cmd) {
	return model.moveFocus(-1), nil
}

func navigationAction(model Model, _ string) (Model, tea.Cmd) {
	if model.layout().mode == layoutWide {
		model.focus = paneNavigation
		return model, nil
	}
	return model.openOverlay(overlayNavigation)
}

func coursesAction(model Model, _ string) (Model, tea.Cmd) {
	return model.openCourses(), nil
}

func libraryAction(model Model, _ string) (Model, tea.Cmd) {
	return model.openLibrary()
}

func diagnosticsAction(model Model, _ string) (Model, tea.Cmd) {
	return model.openDiagnostics(), nil
}

func paletteAction(model Model, _ string) (Model, tea.Cmd) {
	return model.openOverlay(overlayPalette)
}

func helpAction(model Model, _ string) (Model, tea.Cmd) {
	if overlay, ok := model.topOverlay(); ok && overlay.kind == overlayHelp {
		return model.closeOverlay()
	}
	return model.openOverlay(overlayHelp)
}

func backAction(model Model, _ string) (Model, tea.Cmd) {
	if _, ok := model.topOverlay(); ok {
		return model.closeOverlay()
	}
	return model.goBack()
}

func quitAction(model Model, _ string) (Model, tea.Cmd) {
	return model.quit()
}

func (model Model) openOverlay(kind overlayKind) (Model, tea.Cmd) {
	if kind != overlayHelp && len(model.overlays) > 0 {
		return model, nil
	}
	if model.filtering {
		model.filtering = false
		model.filter.Blur()
	}
	model.overlays = append(model.overlays, overlayState{kind: kind, previousFocus: model.effectiveFocus()})
	switch kind {
	case overlayPalette:
		model.palette.SetValue("")
		model.paletteCursor = 0
		return model, model.palette.Focus()
	case overlayHelp, overlayNavigation:
		return model, nil
	}
	return model, nil
}

func (model Model) closeOverlay() (Model, tea.Cmd) {
	if len(model.overlays) == 0 {
		return model, nil
	}
	closed := model.overlays[len(model.overlays)-1]
	model.overlays = model.overlays[:len(model.overlays)-1]
	if overlay, open := model.topOverlay(); open && overlay.kind == overlayPalette {
		return model, model.palette.Focus()
	}
	model.palette.Blur()
	if len(model.overlays) == 0 {
		model.focus = closed.previousFocus
		model.focus = model.effectiveFocus()
	}
	return model, nil
}

func (model Model) openNavigationSelection() (Model, tea.Cmd) {
	switch model.navigationCursor {
	case 0:
		return model.openCourses(), nil
	case 1:
		return model.openLibrary()
	case 2:
		return model.openDiagnostics(), nil
	default:
		return model, nil
	}
}

func (model Model) openCourses() Model {
	if model.screen == screenCourses {
		model.focus = paneCollection
		return model
	}
	model.err = nil
	model = model.transitionScreen(screenCourses, false)
	model.focus = paneCollection
	model.navigationCursor = 0
	return model
}

func modelFromUpdate(updated tea.Model, command tea.Cmd) (Model, tea.Cmd) {
	model, ok := updated.(Model)
	if !ok {
		return Model{}, command
	}
	return model, command
}

func (model Model) contextualCommands(includeDisabled bool) []command {
	contextModel := model
	if overlay, ok := contextModel.topOverlay(); ok && overlay.kind == overlayHelp {
		contextModel.overlays = contextModel.overlays[:len(contextModel.overlays)-1]
	}
	result := make([]command, 0, len(commands))
	for _, candidate := range commands {
		state := candidate.context(contextModel)
		if state.visible && (includeDisabled || state.enabled) {
			result = append(result, candidate)
		}
	}
	return result
}

func commandHint(candidate command) string {
	hint := candidate.hint
	if hint == "" {
		hint = strings.Join(candidate.keys, "/")
	}
	return hint + " " + candidate.label
}
