package tui

import (
	"fmt"
)

func (model Model) renderOverlay(width, height int) string {
	overlay, open := model.topOverlay()
	if !open {
		return ""
	}
	bodyRows := max(0, height-2)
	switch overlay.kind {
	case overlayPalette:
		return model.renderPane("Commands", width, height, true, model.renderPaletteBody(bodyRows))
	case overlayHelp:
		return model.renderPane("Help · "+model.effectiveFocus().String(), width, height, true, model.renderHelpBody(bodyRows))
	case overlayNavigation:
		return model.renderPane("Navigation", width, height, true, model.renderNavigationBody())
	}
	return ""
}

func (model Model) renderPaletteBody(rows int) []string {
	if rows <= 0 {
		return nil
	}
	result := []string{model.palette.View()}
	availableRows := rows - 1
	if availableRows <= 0 {
		return result
	}
	matches := model.paletteCommands()
	if len(matches) == 0 {
		return append(result, model.styles.muted.Render("EMPTY: No matching commands"))
	}
	cursor := min(model.paletteCursor, len(matches)-1)
	start, end := visibleBounds(len(matches), cursor, availableRows)
	for index := start; index < end; index++ {
		candidate := matches[index]
		state := candidate.context(model)
		prefix := "  "
		if index == cursor {
			prefix = "> "
		}
		line := fmt.Sprintf("%s%-12s %s", prefix, candidate.hint, candidate.label)
		if !state.enabled && state.reason != "" {
			line += " — " + terminalText(state.reason)
		}
		if index == cursor {
			line = model.styles.selected.Render(line)
		} else if !state.enabled {
			line = model.styles.muted.Render(line)
		}
		result = append(result, line)
	}
	return result
}

func (model Model) renderHelpBody(rows int) []string {
	if rows <= 0 {
		return nil
	}
	result := make([]string, 0, rows)
	for _, candidate := range model.contextualCommands(true) {
		state := candidate.context(model.helpContextModel())
		line := fmt.Sprintf("%-20s %s", candidate.hint, candidate.label)
		if !state.enabled && state.reason != "" {
			line += " — " + terminalText(state.reason)
			line = model.styles.muted.Render(line)
		}
		result = append(result, line)
		if len(result) == rows {
			break
		}
	}
	if len(result) == 0 {
		return []string{model.styles.muted.Render("No commands in this context")}
	}
	return result
}

func (model Model) helpContextModel() Model {
	if overlay, ok := model.topOverlay(); ok && overlay.kind == overlayHelp {
		model.overlays = model.overlays[:len(model.overlays)-1]
	}
	return model
}
