package tui

import (
	"slices"
)

type layoutMode uint8

const (
	layoutCompact layoutMode = iota
	layoutMedium
	layoutWide
)

func (mode layoutMode) String() string {
	switch mode {
	case layoutCompact:
		return "COMPACT"
	case layoutMedium:
		return "MEDIUM"
	case layoutWide:
		return "WIDE"
	}
	return "COMPACT"
}

type paneFocus uint8

const (
	paneNavigation paneFocus = iota
	paneCollection
	paneInspector
	paneActivity
)

func (focus paneFocus) String() string {
	switch focus {
	case paneNavigation:
		return "NAVIGATION"
	case paneCollection:
		return "COLLECTION"
	case paneInspector:
		return "INSPECTOR"
	case paneActivity:
		return "ACTIVITY"
	}
	return "COLLECTION"
}

type overlayKind uint8

const (
	overlayPalette overlayKind = iota
	overlayHelp
	overlayNavigation
)

type overlayState struct {
	kind          overlayKind
	previousFocus paneFocus
}

type collectionState struct {
	cursor int
	filter string
}

type rectangle struct {
	x      int
	y      int
	width  int
	height int
}

type workspaceLayout struct {
	mode       layoutMode
	header     rectangle
	navigation rectangle
	collection rectangle
	inspector  rectangle
	activity   rectangle
	footer     rectangle
}

func calculateLayout(width, height int, activity bool) workspaceLayout {
	width = max(1, width)
	height = max(1, height)
	mode := layoutCompact
	if width >= 120 && height >= 20 {
		mode = layoutWide
	} else if width >= 76 && height >= 16 {
		inspectorWidth := max(28, width*34/100)
		if width-inspectorWidth+1 >= 40 {
			mode = layoutMedium
		}
	}

	headerHeight := 1
	footerHeight := 0
	if height >= 2 {
		footerHeight = 1
	}
	activityHeight := 0
	if activity && height-headerHeight-footerHeight >= 6 {
		activityHeight = 3
	}
	workspaceHeight := max(0, height-headerHeight-footerHeight-activityHeight)
	layout := workspaceLayout{
		mode:       mode,
		header:     rectangle{width: width, height: headerHeight},
		collection: rectangle{y: headerHeight, width: width, height: workspaceHeight},
		activity:   rectangle{y: headerHeight + workspaceHeight, width: width, height: activityHeight},
		footer:     rectangle{y: height - footerHeight, width: width, height: footerHeight},
	}

	switch mode {
	case layoutWide:
		navigationWidth := 22
		inspectorWidth := 36
		layout.navigation = rectangle{y: headerHeight, width: navigationWidth, height: workspaceHeight}
		layout.collection = rectangle{
			x: navigationWidth - 1, y: headerHeight,
			width: width - navigationWidth - inspectorWidth + 2, height: workspaceHeight,
		}
		layout.inspector = rectangle{x: width - inspectorWidth, y: headerHeight, width: inspectorWidth, height: workspaceHeight}
	case layoutMedium:
		inspectorWidth := max(28, width*34/100)
		layout.collection.width = width - inspectorWidth + 1
		layout.inspector = rectangle{x: width - inspectorWidth, y: headerHeight, width: inspectorWidth, height: workspaceHeight}
	case layoutCompact:
	}
	return layout
}

func (model Model) hasActivity() bool {
	return model.screen == screenPlayback || model.playback != nil || model.loading || model.err != nil || model.status != ""
}

func (model Model) layout() workspaceLayout {
	return calculateLayout(model.width, model.height, model.hasActivity())
}

func (model Model) visibleFocuses() []paneFocus {
	layout := model.layout()
	focuses := make([]paneFocus, 0, 4)
	if layout.navigation.width > 0 && layout.navigation.height > 0 {
		focuses = append(focuses, paneNavigation)
	}
	if layout.collection.height > 0 {
		focuses = append(focuses, paneCollection)
	}
	if layout.inspector.width > 0 && layout.inspector.height > 0 {
		focuses = append(focuses, paneInspector)
	}
	if layout.activity.height > 0 {
		focuses = append(focuses, paneActivity)
	}
	if len(focuses) == 0 {
		return []paneFocus{paneCollection}
	}
	return focuses
}

func (model Model) effectiveFocus() paneFocus {
	focuses := model.visibleFocuses()
	if slices.Contains(focuses, model.focus) {
		return model.focus
	}
	return paneCollection
}

func (model Model) moveFocus(delta int) Model {
	focuses := model.visibleFocuses()
	current := slices.Index(focuses, model.effectiveFocus())
	if current < 0 {
		current = 0
	}
	next := (current + delta) % len(focuses)
	if next < 0 {
		next += len(focuses)
	}
	model.focus = focuses[next]
	return model
}

func (model Model) topOverlay() (overlayState, bool) {
	if len(model.overlays) == 0 {
		return overlayState{}, false
	}
	return model.overlays[len(model.overlays)-1], true
}

func isCollectionScreen(value screen) bool {
	switch value {
	case screenCourses, screenLectures, screenLibrary, screenDiagnostics:
		return true
	case screenResume, screenPlayback, screenDetails:
		return false
	}
	return false
}

func (model Model) transitionScreen(next screen, reset bool) Model {
	if model.collections == nil {
		model.collections = make(map[screen]collectionState, 4)
	}
	if isCollectionScreen(model.screen) {
		model.collections[model.screen] = collectionState{cursor: model.cursor, filter: model.filter.Value()}
	}
	model.screen = next
	if !isCollectionScreen(next) {
		return model
	}
	if reset {
		delete(model.collections, next)
	}
	state := model.collections[next]
	model.cursor = state.cursor
	model.filter.SetValue(state.filter)
	model.filtering = false
	model.filter.Blur()
	model.clampCursor()
	return model
}

func (model *Model) clampCursor() {
	if model == nil {
		return
	}
	count := model.itemCount()
	if count == 0 {
		model.cursor = 0
	} else {
		model.cursor = min(max(0, model.cursor), count-1)
	}
}

func (model Model) workspaceCollectionScreen() screen {
	switch model.screen {
	case screenResume, screenPlayback, screenDetails:
		return screenLectures
	case screenCourses, screenLectures, screenLibrary, screenDiagnostics:
		return model.screen
	}
	return screenCourses
}
