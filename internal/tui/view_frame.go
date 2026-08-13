package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

type borderGlyphs struct {
	topLeft     string
	topRight    string
	bottomLeft  string
	bottomRight string
	horizontal  string
	vertical    string
}

func (model Model) glyphs() borderGlyphs {
	if model.noColor {
		return borderGlyphs{topLeft: "+", topRight: "+", bottomLeft: "+", bottomRight: "+", horizontal: "-", vertical: "|"}
	}
	return borderGlyphs{topLeft: "┌", topRight: "┐", bottomLeft: "└", bottomRight: "┘", horizontal: "─", vertical: "│"}
}

func (model Model) renderPane(title string, width, height int, focused bool, body []string) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	glyphs := model.glyphs()
	borderStyle := model.styles.border
	titleStyle := model.styles.title
	if focused {
		borderStyle = model.styles.focus
		titleStyle = model.styles.focus
		title += " [ACTIVE]"
	}
	if width == 1 {
		return strings.Repeat(borderStyle.Render(glyphs.vertical)+"\n", height-1) + borderStyle.Render(glyphs.vertical)
	}

	innerWidth := width - 2
	label := ansi.Truncate(" "+terminalText(title)+" ", innerWidth, "")
	labelWidth := ansi.StringWidth(label)
	top := borderStyle.Render(glyphs.topLeft) + titleStyle.Render(label) +
		borderStyle.Render(strings.Repeat(glyphs.horizontal, max(0, innerWidth-labelWidth))+glyphs.topRight)
	if height == 1 {
		return fitCellLine(top, width)
	}

	lines := make([]string, height)
	lines[0] = fitCellLine(top, width)
	bodyRows := max(0, height-2)
	for row := range bodyRows {
		content := ""
		if row < len(body) {
			content = body[row]
		}
		lines[row+1] = borderStyle.Render(glyphs.vertical) + fitCellLine(content, innerWidth) + borderStyle.Render(glyphs.vertical)
	}
	bottom := borderStyle.Render(glyphs.bottomLeft + strings.Repeat(glyphs.horizontal, innerWidth) + glyphs.bottomRight)
	lines[height-1] = fitCellLine(bottom, width)
	return strings.Join(lines, "\n")
}

func fitCellLine(value string, width int) string {
	if width <= 0 {
		return ""
	}
	value = ansi.Truncate(value, width, "")
	return value + strings.Repeat(" ", max(0, width-ansi.StringWidth(value)))
}

func fitSides(left, right string, width int) string {
	if width <= 0 {
		return ""
	}
	left = ansi.Truncate(left, width, "")
	remaining := width - ansi.StringWidth(left)
	if remaining <= 1 || ansi.StringWidth(right) >= remaining {
		return fitCellLine(left, width)
	}
	return left + strings.Repeat(" ", remaining-ansi.StringWidth(right)) + right
}

func joinSharedPanes(blocks ...string) string {
	if len(blocks) == 0 {
		return ""
	}
	rows := make([][]string, len(blocks))
	height := 0
	for index, block := range blocks {
		rows[index] = strings.Split(block, "\n")
		height = max(height, len(rows[index]))
	}
	joined := make([]string, height)
	for row := range height {
		var line strings.Builder
		for index, blockRows := range rows {
			if row >= len(blockRows) {
				continue
			}
			value := blockRows[row]
			if index > 0 {
				value = ansi.Cut(value, 1, ansi.StringWidth(value))
			}
			line.WriteString(value)
		}
		joined[row] = line.String()
	}
	return strings.Join(joined, "\n")
}

func normalizeScreen(content string, width, height int) []string {
	lines := strings.Split(content, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	for index := range lines {
		lines[index] = fitCellLine(lines[index], width)
	}
	return lines
}

func replaceRectangle(screen []string, rect rectangle, block string, screenWidth int) {
	if rect.width <= 0 || rect.height <= 0 {
		return
	}
	rows := normalizeScreen(block, rect.width, rect.height)
	for offset, overlayLine := range rows {
		y := rect.y + offset
		if y < 0 || y >= len(screen) {
			continue
		}
		prefix := ansi.Cut(screen[y], 0, max(0, rect.x))
		suffix := ansi.Cut(screen[y], min(screenWidth, rect.x+rect.width), screenWidth)
		screen[y] = fitCellLine(prefix+overlayLine+suffix, screenWidth)
	}
}

func renderLines(lines []string) string {
	return strings.Join(lines, "\n")
}
