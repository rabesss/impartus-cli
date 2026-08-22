import type { FoundationState } from "./workspace_controller.ts"
import type { PaneFocus, WorkspaceLayout } from "./workspace_layout.ts"
import { visibleFocuses } from "./workspace_layout.ts"

export type OverlayKind = "help" | "navigation" | "palette"

export type CommandID =
  | "app.quit"
  | "collection.filter"
  | "collection.retry"
  | "focus.next"
  | "focus.previous"
  | "lecture.download"
  | "navigation.back"
  | "navigation.courses"
  | "navigation.diagnostics"
  | "navigation.library"
  | "navigation.open"
  | "overlay.help"
  | "overlay.palette"
  | "playback.control"
  | "selection.move"
  | "selection.open"
  | "session.selftest"

export interface Command {
  description: string
  footer: boolean
  id: CommandID
  keys: readonly string[]
  label: string
  palette: boolean
}

export interface CommandAvailability {
  enabled: boolean
  reason: string
  visible: boolean
}

export interface CommandContext {
  focus: PaneFocus
  layout: WorkspaceLayout
  overlay: OverlayKind | undefined
  state: FoundationState
}

export interface AvailableCommand {
  availability: CommandAvailability
  command: Command
}

type Availability = (context: CommandContext) => CommandAvailability

interface CommandDefinition extends Command {
  availability: Availability
}

const COMMANDS: readonly CommandDefinition[] = [
  command("selection.move", "Move selection", "Move through the focused list", ["up", "down", "j", "k"], false, true, moveAvailability),
  command("selection.open", "Open selection", "Open or play the selected item", ["enter"], false, true, openAvailability),
  command("collection.filter", "Filter collection", "Edit the current collection filter", ["/"], true, true, filterAvailability),
  command("overlay.palette", "Open command palette", "Search contextual workspace commands", ["ctrl+p"], false, true, baseAvailability),
  command("overlay.help", "Open help", "Show contextual command help", ["?"], false, true, baseAvailability),
  command("lecture.download", "Download lecture", "Start a Go-owned lecture download", ["d"], true, true, lectureAvailability),
  command("session.selftest", "Run connection test", "Start the private session self-test", ["s"], true, true, operationAvailability),
  command("playback.control", "Control playback", "Use the direct mpv playback keys", ["space", "left", "right", "m", "+", "=", "-", "[", "]", "v"], false, true, playbackAvailability),
  command("collection.retry", "Retry current view", "Reload the current collection", ["r"], true, true, retryAvailability),
  command("focus.next", "Next pane", "Move focus to the next visible pane", ["tab"], true, true, focusAvailability),
  command("focus.previous", "Previous pane", "Move focus to the previous visible pane", ["shift+tab"], true, false, focusAvailability),
  command("navigation.open", "Open navigation", "Focus or open workspace navigation", ["g"], true, true, navigationAvailability),
  command("navigation.courses", "Open courses", "Show the live course catalog", ["c"], true, false, sectionAvailability),
  command("navigation.library", "Open library", "Show the local lecture library", ["l"], true, true, sectionAvailability),
  command("navigation.diagnostics", "Open diagnostics", "Show local diagnostics", ["!"], true, true, sectionAvailability),
  command("navigation.back", "Close or go back", "Close the top overlay or return", ["escape", "backspace"], false, true, backAvailability),
  command("app.quit", "Quit", "Close the private UI session", ["q", "ctrl+c"], true, true, baseAvailability),
]

export function commandForKey(context: CommandContext, key: string): AvailableCommand | undefined {
  if (context.overlay === "palette" && (key === "j" || key === "k")) return undefined
  for (const candidate of COMMANDS) {
    if (!candidate.keys.includes(key)) continue
    const availability = candidate.availability(context)
    if (availability.visible) return availableCommand(candidate, availability)
  }
  return undefined
}

export function commandsForPalette(context: CommandContext, query: string): readonly AvailableCommand[] {
  const needle = query.trim().toLocaleLowerCase()
  const workspace = { ...context, overlay: undefined }
  return COMMANDS.flatMap((candidate) => {
    const availability = candidate.availability(workspace)
    if (!candidate.palette || !availability.visible) return []
    const haystack = `${candidate.id} ${candidate.label} ${candidate.keys.join(" ")}`.toLocaleLowerCase()
    return needle === "" || haystack.includes(needle) ? [availableCommand(candidate, availability)] : []
  })
}

export function commandsForHelp(context: CommandContext): readonly AvailableCommand[] {
  const workspace = { ...context, overlay: undefined }
  return COMMANDS.flatMap((candidate) => {
    const availability = candidate.availability(workspace)
    return availability.visible ? [availableCommand(candidate, availability)] : []
  })
}

export function footerCommands(context: CommandContext): readonly AvailableCommand[] {
  return COMMANDS.flatMap((candidate) => {
    const availability = candidate.availability(context)
    return candidate.footer && availability.visible && availability.enabled
      ? [availableCommand(candidate, availability)]
      : []
  })
}

function command(
  id: CommandID,
  label: string,
  description: string,
  keys: readonly string[],
  palette: boolean,
  footer: boolean,
  availability: Availability,
): CommandDefinition {
  return { availability, description, footer, id, keys, label, palette }
}

function availableCommand(definition: CommandDefinition, availability: CommandAvailability): AvailableCommand {
  const { availability: _availability, ...command } = definition
  return { availability, command }
}

function available(visible: boolean, enabled = visible, reason = ""): CommandAvailability {
  return { enabled, reason, visible }
}

function baseAvailability(): CommandAvailability {
  return available(true)
}

function moveAvailability(context: CommandContext): CommandAvailability {
  if (context.overlay === "palette" || context.overlay === "navigation") return available(true)
  if (context.overlay === "help") return available(false)
  if (context.state.screen === "playback") return available(false)
  if (context.focus === "navigation") return available(true)
  if (context.state.loading || context.focus !== "collection") return available(false)
  if (context.state.error !== undefined) return available(true, false, "Retry the current view first")
  return available(true, collectionCount(context.state) > 1, "Collection has one or fewer items")
}

function openAvailability(context: CommandContext): CommandAvailability {
  if (context.overlay === "palette" || context.overlay === "navigation") return available(true)
  if (context.overlay === "help") return available(false)
  if (context.focus === "navigation") {
    const enabled = !context.state.loading && context.state.screen !== "playback"
    return available(true, enabled, context.state.screen === "playback" ? "Return from playback first" : "A request is pending")
  }
  if (context.focus !== "collection" && context.focus !== "inspector") return available(false)
  const visible = context.state.screen === "courses" || context.state.screen === "lectures"
  const runningOperation = context.state.screen === "lectures" && context.state.operation?.state === "running"
  const enabled = visible && !context.state.loading && context.state.error === undefined && !runningOperation && collectionCount(context.state) > 0
  const reason = context.state.error !== undefined
    ? "Retry the current view first"
    : context.state.loading
      ? "A request is pending"
      : runningOperation
        ? "An operation is already running"
        : enabled
          ? ""
          : "No selection"
  return available(visible, enabled, reason)
}

function filterAvailability(context: CommandContext): CommandAvailability {
  const visible = context.overlay === undefined && context.focus === "collection" && context.state.screen !== "playback"
  return available(visible, visible && !context.state.loading && context.state.error === undefined, context.state.error === undefined ? "A request is pending" : "Retry the current view first")
}

function lectureAvailability(context: CommandContext): CommandAvailability {
  const visible = context.overlay === undefined && context.state.screen === "lectures"
  const running = context.state.operation?.state === "running"
  return available(visible, visible && !context.state.loading && context.state.error === undefined && !running && collectionCount(context.state) > 0, "Lecture action is unavailable")
}

function operationAvailability(context: CommandContext): CommandAvailability {
  const running = context.state.operation?.state === "running"
  const visible = context.overlay === undefined && context.state.screen !== "playback"
  const reason = running ? "An operation is already running" : context.state.loading ? "A request is pending" : ""
  return available(visible, visible && !context.state.loading && !running, reason)
}

function playbackAvailability(context: CommandContext): CommandAvailability {
  const visible = context.overlay === undefined && context.state.screen === "playback"
  return available(visible, visible && context.state.operation?.kind === "playback" && context.state.operation.state === "running", "Playback is unavailable")
}

function retryAvailability(context: CommandContext): CommandAvailability {
  const visible = context.overlay === undefined && context.state.screen !== "playback"
  return available(visible, visible && !context.state.loading, "A request is pending")
}

function focusAvailability(context: CommandContext): CommandAvailability {
  const visible = context.overlay === undefined && context.state.screen !== "playback" && visibleFocuses(context.layout).length > 1
  return available(visible)
}

function navigationAvailability(context: CommandContext): CommandAvailability {
  const visible = context.overlay === undefined && context.state.screen !== "playback"
  return available(visible, visible && !context.state.loading, "A request is pending")
}

function sectionAvailability(context: CommandContext): CommandAvailability {
  const visible = context.state.screen !== "playback"
  return available(visible, visible && !context.state.loading, "A request is pending")
}

function backAvailability(context: CommandContext): CommandAvailability {
  if (context.overlay !== undefined) return available(true)
  const visible = context.state.screen !== "courses"
  return available(visible, visible && !context.state.loading, "A request is pending")
}

function collectionCount(state: FoundationState): number {
  if (state.screen === "courses") {
    const query = normalizedQuery(state.collections.courses.filter)
    return query === "" ? state.courses.length : state.courses.filter((course) => normalizedQuery(`${course.subjectName} ${course.professorName} ${course.sessionName}`).includes(query)).length
  }
  if (state.screen === "lectures") {
    const query = normalizedQuery(state.collections.lectures.filter)
    return query === "" ? state.lectures.length : state.lectures.filter((lecture) => normalizedQuery(`${lecture.topic} ${lecture.professorName} ${lecture.classroomName} ${lecture.startTime}`).includes(query)).length
  }
  if (state.screen === "library") {
    const query = normalizedQuery(state.collections.library.filter)
    return query === "" ? state.artifacts.length : state.artifacts.filter((artifact) => normalizedQuery(artifact.topic).includes(query)).length
  }
  if (state.screen === "diagnostics") {
    const query = normalizedQuery(state.collections.diagnostics.filter)
    return query === "" ? state.diagnostics.length : state.diagnostics.filter((diagnostic) => normalizedQuery(`${diagnostic.name} ${diagnostic.status} ${diagnostic.detail}`).includes(query)).length
  }
  return 0
}

function normalizedQuery(value: string): string {
  return value.trim().toLocaleLowerCase()
}
