export type LayoutMode = "compact" | "medium" | "wide"

export type PaneFocus = "navigation" | "collection" | "inspector" | "activity"

export interface Rectangle {
  height: number
  width: number
  x: number
  y: number
}

export interface WorkspaceLayout {
  activity: Rectangle
  collection: Rectangle
  footer: Rectangle
  header: Rectangle
  inspector: Rectangle
  mode: LayoutMode
  navigation: Rectangle
}

const WIDE_WIDTH = 120
const WIDE_HEIGHT = 20
const MEDIUM_WIDTH = 76
const MEDIUM_HEIGHT = 16
const MIN_COLLECTION_WIDTH = 40
const MIN_INSPECTOR_WIDTH = 28

export function calculateLayout(rawWidth: number, rawHeight: number, showActivity: boolean): WorkspaceLayout {
  const width = Math.max(1, Math.floor(rawWidth))
  const height = Math.max(1, Math.floor(rawHeight))
  const headerHeight = Math.min(3, height)
  const footerHeight = height > headerHeight ? Math.min(3, height - headerHeight) : 0
  const availableBodyHeight = height - headerHeight - footerHeight
  const activityHeight = showActivity && availableBodyHeight >= 6 ? Math.min(3, availableBodyHeight) : 0
  const workspaceHeight = Math.max(0, availableBodyHeight - activityHeight)
  const mode = layoutMode(width, height)
  const layout: WorkspaceLayout = {
    activity: rectangle(0, headerHeight + workspaceHeight, width, activityHeight),
    collection: rectangle(0, headerHeight, width, workspaceHeight),
    footer: rectangle(0, height - footerHeight, width, footerHeight),
    header: rectangle(0, 0, width, headerHeight),
    inspector: rectangle(0, headerHeight, 0, workspaceHeight),
    mode,
    navigation: rectangle(0, headerHeight, 0, workspaceHeight),
  }

  if (mode === "wide") {
    const navigationWidth = 22
    const inspectorWidth = 36
    const collectionX = navigationWidth + 1
    const inspectorX = width - inspectorWidth
    layout.navigation = rectangle(0, headerHeight, navigationWidth, workspaceHeight)
    layout.collection = rectangle(collectionX, headerHeight, Math.max(0, inspectorX - collectionX - 1), workspaceHeight)
    layout.inspector = rectangle(inspectorX, headerHeight, inspectorWidth, workspaceHeight)
  } else if (mode === "medium") {
    const inspectorWidth = Math.max(MIN_INSPECTOR_WIDTH, Math.floor(width * 0.34))
    const inspectorX = width - inspectorWidth
    layout.collection = rectangle(0, headerHeight, Math.max(0, inspectorX - 1), workspaceHeight)
    layout.inspector = rectangle(inspectorX, headerHeight, inspectorWidth, workspaceHeight)
  }

  return layout
}

export function visibleFocuses(layout: WorkspaceLayout): readonly PaneFocus[] {
  const focuses: PaneFocus[] = []
  if (visible(layout.navigation)) focuses.push("navigation")
  if (visible(layout.collection)) focuses.push("collection")
  if (visible(layout.inspector)) focuses.push("inspector")
  if (visible(layout.activity)) focuses.push("activity")
  return focuses.length === 0 ? ["collection"] : focuses
}

export function effectiveFocus(focus: PaneFocus, layout: WorkspaceLayout): PaneFocus {
  return visibleFocuses(layout).includes(focus) ? focus : "collection"
}

export function moveFocus(focus: PaneFocus, delta: number, layout: WorkspaceLayout): PaneFocus {
  const focuses = visibleFocuses(layout)
  const current = Math.max(0, focuses.indexOf(effectiveFocus(focus, layout)))
  const next = (current + delta % focuses.length + focuses.length) % focuses.length
  return focuses[next] ?? "collection"
}

function layoutMode(width: number, height: number): LayoutMode {
  if (width >= WIDE_WIDTH && height >= WIDE_HEIGHT) return "wide"
  if (width >= MEDIUM_WIDTH && height >= MEDIUM_HEIGHT) {
    const inspectorWidth = Math.max(MIN_INSPECTOR_WIDTH, Math.floor(width * 0.34))
    if (width - inspectorWidth - 1 >= MIN_COLLECTION_WIDTH) return "medium"
  }
  return "compact"
}

function rectangle(x: number, y: number, width: number, height: number): Rectangle {
  return {
    height: Math.max(0, height),
    width: Math.max(0, width),
    x: Math.max(0, x),
    y: Math.max(0, y),
  }
}

function visible(rectangle: Rectangle): boolean {
  return rectangle.width > 0 && rectangle.height > 0
}
