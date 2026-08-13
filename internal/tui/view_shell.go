package tui

import (
	"strings"
)

func (model Model) renderShell() string {
	layout := model.layout()
	sections := make([]string, 0, 4)
	sections = append(sections, model.renderHeader(layout.header.width))
	if layout.collection.height > 0 {
		sections = append(sections, model.renderWorkspace(layout))
	}
	if layout.activity.height > 0 {
		sections = append(sections, model.renderActivity(layout.activity.width, layout.activity.height))
	}
	if layout.footer.height > 0 {
		sections = append(sections, model.renderFooter(layout.footer.width))
	}
	screen := normalizeScreen(strings.Join(sections, "\n"), model.width, model.height)
	if _, open := model.topOverlay(); open {
		rect := model.overlayRectangle(layout.mode)
		replaceRectangle(screen, rect, model.renderOverlay(rect.width, rect.height), model.width)
	}
	return renderLines(screen)
}

func (model Model) renderWorkspace(layout workspaceLayout) string {
	focus := model.effectiveFocus()
	collectionScreen := model.workspaceCollectionScreen()
	switch layout.mode {
	case layoutWide:
		navigation := model.renderPane(
			"Navigation", layout.navigation.width, layout.navigation.height,
			focus == paneNavigation, model.renderNavigationBody(),
		)
		collection := model.renderPane(
			model.collectionTitle(collectionScreen), layout.collection.width, layout.collection.height,
			focus == paneCollection, model.renderCollectionBody(collectionScreen, max(0, layout.collection.height-2)),
		)
		inspector := model.renderPane(
			"Inspector", layout.inspector.width, layout.inspector.height,
			focus == paneInspector, model.renderInspectorBody(max(0, layout.inspector.height-2)),
		)
		return joinSharedPanes(navigation, collection, inspector)
	case layoutMedium:
		collection := model.renderPane(
			model.collectionTitle(collectionScreen), layout.collection.width, layout.collection.height,
			focus == paneCollection, model.renderCollectionBody(collectionScreen, max(0, layout.collection.height-2)),
		)
		inspector := model.renderPane(
			"Inspector", layout.inspector.width, layout.inspector.height,
			focus == paneInspector, model.renderInspectorBody(max(0, layout.inspector.height-2)),
		)
		return joinSharedPanes(collection, inspector)
	case layoutCompact:
		title, body := model.renderCompactBody(max(0, layout.collection.height-2))
		return model.renderPane(title, layout.collection.width, layout.collection.height, true, body)
	}
	return ""
}

func (model Model) overlayRectangle(mode layoutMode) rectangle {
	if mode == layoutCompact {
		return rectangle{x: 0, y: min(1, model.height-1), width: model.width, height: max(1, model.height-2)}
	}
	width := min(72, max(1, model.width-4))
	height := min(14, max(1, model.height-4))
	return rectangle{x: (model.width - width) / 2, y: (model.height - height) / 2, width: width, height: height}
}
