import {
  BoxRenderable,
  CliRenderEvents,
  TextAttributes,
  TextRenderable,
  type CliRenderer,
  type KeyEvent,
} from "@opentui/core"

import type { ArtifactSummary, Course, Diagnostic, Lecture, PlaybackCommand } from "./protocol/types.gen.ts"
import { graphemes, truncateGraphemes } from "./text_input.ts"
import {
  commandForKey,
  commandsForHelp,
  commandsForPalette,
  footerCommands,
  type CommandContext,
  type CommandID,
  type OverlayKind,
} from "./workspace_commands.ts"
import type { CollectionScreen, CollectionState, FoundationState } from "./workspace_controller.ts"
import {
  calculateLayout,
  effectiveFocus,
  moveFocus,
  type PaneFocus,
  type WorkspaceLayout,
} from "./workspace_layout.ts"

export type { FoundationOperation, FoundationScreen, FoundationState } from "./workspace_controller.ts"

const COLORS = {
  accent: "#7dd3fc",
  background: "#0b1015",
  border: "#334155",
  danger: "#fb7185",
  dim: "#64748b",
  foreground: "#e2e8f0",
  panel: "#111820",
  selected: "#172554",
  success: "#4ade80",
  warning: "#fbbf24",
}

const COURSE_LABEL_WIDTH = 24
const NAVIGATION: ReadonlyArray<{ label: string; screen: CollectionScreen }> = [
  { label: "Courses", screen: "courses" },
  { label: "Local library", screen: "library" },
  { label: "Diagnostics", screen: "diagnostics" },
]

export interface FoundationCallbacks {
  onBack(): void
  onBlockedCommand(reason: string): void
  onCollectionState(screen: CollectionScreen, state: CollectionState): void
  onCourses(): void
  onDiagnostics(): void
  onDownload(lecture: Lecture): void
  onLibrary(): void
  onOpenCourse(course: Course): void
  onPlay(lecture: Lecture): void
  onPlaybackCommand(command: PlaybackCommand): void
  onQuit(): void
  onRetry(): void
  onSelfTest(): void
}

interface OverlayState {
  kind: OverlayKind
  previousFocus: PaneFocus
}

interface BlockedCommandFeedback {
  commandId: CommandID
  key: string
  reason: string
}

export class FoundationView {
  readonly #renderer: CliRenderer
  readonly #callbacks: FoundationCallbacks
  #courseLabels: ReadonlyMap<Course, string>
  #state: FoundationState
  #tree: BoxRenderable | undefined
  #blockedCommandFeedback: BlockedCommandFeedback | undefined
  #focus: PaneFocus = "collection"
  #filtering = false
  #helpOffset = 0
  #navigationCursor = 0
  #wideNavigationPreviousFocus: PaneFocus | undefined
  #overlays: OverlayState[] = []
  #paletteCursor = 0
  #paletteQuery = ""

  public constructor(renderer: CliRenderer, state: FoundationState, callbacks: FoundationCallbacks) {
    this.#renderer = renderer
    this.#state = normalizeState(state)
    this.#courseLabels = courseLabels(this.#state.courses)
    this.#callbacks = callbacks
    this.#renderer.keyInput.on("keypress", this.#onKeyPress)
    this.#renderer.on(CliRenderEvents.RESIZE, this.#onResize)
    this.#rebuild()
  }

  public update(state: FoundationState): void {
    const next = normalizeState(state)
    if (!sameCourseCatalog(this.#state.courses, next.courses)) this.#courseLabels = courseLabels(next.courses)
    this.#state = next
    this.#refreshBlockedCommandFeedback()
    this.#transitionFocus(effectiveFocus(this.#focus, this.#layout()))
    this.#paletteCursor = clamp(this.#paletteCursor, 0, Math.max(0, this.#paletteMatches().length - 1))
    this.#rebuild()
  }

  public destroy(): void {
    this.#renderer.keyInput.off("keypress", this.#onKeyPress)
    this.#renderer.off(CliRenderEvents.RESIZE, this.#onResize)
    if (this.#tree !== undefined) {
      this.#renderer.root.remove(this.#tree)
      this.#tree.destroyRecursively()
      this.#tree = undefined
    }
  }

  readonly #onResize = (): void => {
    this.#transitionFocus(effectiveFocus(this.#focus, this.#layout()))
    this.#refreshBlockedCommandFeedback()
    this.#rebuild()
  }

  readonly #onKeyPress = (key: KeyEvent): void => {
    const normalized = normalizedKey(key)
    if (this.#blockedCommandFeedback !== undefined) {
      this.#blockedCommandFeedback = undefined
      this.#rebuild()
    }
    if (normalized === "ctrl+c") {
      this.#callbacks.onQuit()
      return
    }
    const overlay = this.#topOverlay()
    if (overlay?.kind === "palette") {
      this.#handlePaletteKey(key, normalized)
      return
    }
    if (overlay?.kind === "navigation") {
      this.#handleNavigationKey(normalized)
      return
    }
    if (overlay?.kind === "help") {
      if (normalized === "escape" || normalized === "backspace" || normalized === "?") {
        this.#closeOverlay()
      } else if (["up", "down", "j", "k"].includes(normalized)) {
        const commands = commandsForHelp(this.#commandContext())
        const rows = helpPageSize(commands.length, this.#renderer.terminalHeight)
        const maximumOffset = Math.max(0, commands.length - rows)
        if (maximumOffset > 0) {
          const delta = normalized === "up" || normalized === "k" ? -1 : 1
          const offset = clamp(this.#helpOffset, 0, maximumOffset)
          this.#helpOffset = (offset + delta + maximumOffset + 1) % (maximumOffset + 1)
          this.#rebuild()
        }
      }
      return
    }
    if (this.#focus === "navigation" && (normalized === "escape" || normalized === "backspace")) {
      const previousFocus = this.#wideNavigationPreviousFocus ?? "collection"
      this.#wideNavigationPreviousFocus = undefined
      this.#focus = effectiveFocus(previousFocus, this.#layout())
      this.#rebuild()
      return
    }
    if (this.#filtering) {
      this.#handleFilterKey(key, normalized)
      return
    }
    const result = commandForKey(this.#commandContext(), normalized)
    if (result?.availability.enabled === true) this.#dispatch(result.command.id, normalized)
    else if (result?.availability.visible === true && result.availability.reason !== "") {
      this.#blockedCommandFeedback = {
        commandId: result.command.id,
        key: normalized,
        reason: result.availability.reason,
      }
      this.#callbacks.onBlockedCommand(result.availability.reason)
      this.#rebuild()
    }
  }

  #refreshBlockedCommandFeedback(): void {
    const blocked = this.#blockedCommandFeedback
    if (blocked === undefined) return
    const current = commandForKey(this.#commandContext(), blocked.key)
    if (
      current?.command.id !== blocked.commandId
      || current.availability.enabled
      || current.availability.reason !== blocked.reason
    ) {
      this.#blockedCommandFeedback = undefined
    }
  }

  #handlePaletteKey(key: KeyEvent, normalized: string): void {
    if (normalized === "escape") {
      this.#closeOverlay()
    } else if (normalized === "up" || normalized === "down") {
      const matches = this.#paletteMatches()
      if (matches.length > 0) {
        const delta = normalized === "up" ? -1 : 1
        this.#paletteCursor = (this.#paletteCursor + delta + matches.length) % matches.length
        this.#rebuild()
      }
    } else if (normalized === "enter") {
      const selected = this.#paletteMatches()[this.#paletteCursor]
      if (selected?.availability.enabled === true) {
        this.#closeOverlay(false)
        this.#dispatch(selected.command.id, "")
      }
    } else if (normalized === "backspace") {
      this.#paletteQuery = graphemes(this.#paletteQuery).slice(0, -1).join("")
      this.#paletteCursor = 0
      this.#rebuild()
    } else if (printableInput(key)) {
      this.#paletteQuery = truncateGraphemes(this.#paletteQuery + key.sequence, 120)
      this.#paletteCursor = 0
      this.#rebuild()
    }
  }

  #handleNavigationKey(normalized: string): void {
    if (normalized === "escape" || normalized === "backspace") {
      this.#closeOverlay()
    } else if (["up", "down", "j", "k"].includes(normalized)) {
      const delta = normalized === "up" || normalized === "k" ? -1 : 1
      this.#navigationCursor = (this.#navigationCursor + delta + NAVIGATION.length) % NAVIGATION.length
      this.#rebuild()
    } else if (normalized === "enter") {
      const open = commandForKey(this.#commandContext(), "enter")
      if (open?.availability.enabled === true) {
        this.#closeOverlay(false)
        this.#dispatchNavigationSelection()
      } else {
        this.#rebuild()
      }
    }
  }

  #handleFilterKey(key: KeyEvent, normalized: string): void {
    if (normalized === "escape" || normalized === "enter") {
      this.#filtering = false
      this.#rebuild()
      return
    }
    const screen = collectionScreen(this.#state.screen)
    if (screen === undefined) return
    const collection = this.#state.collections[screen]
    if (normalized === "backspace") {
      this.#setCollection(screen, { filter: graphemes(collection.filter).slice(0, -1).join(""), selected: 0 })
    } else if (normalized !== "/" && printableInput(key)) {
      this.#setCollection(screen, { filter: truncateGraphemes(collection.filter + key.sequence, 120), selected: 0 })
    }
  }

  #dispatch(identifier: CommandID, key: string): void {
    switch (identifier) {
      case "app.quit": this.#callbacks.onQuit(); return
      case "collection.filter": this.#filtering = true; this.#rebuild(); return
      case "collection.retry": this.#callbacks.onRetry(); return
      case "focus.next": this.#transitionFocus(moveFocus(this.#focus, 1, this.#layout())); this.#rebuild(); return
      case "focus.previous": this.#transitionFocus(moveFocus(this.#focus, -1, this.#layout())); this.#rebuild(); return
      case "lecture.download": {
        const lecture = this.#selectedLecture()
        if (lecture !== undefined) this.#callbacks.onDownload(lecture)
        return
      }
      case "navigation.back": this.#callbacks.onBack(); return
      case "navigation.courses": this.#dispatchNavigationDestination("courses"); return
      case "navigation.diagnostics": this.#dispatchNavigationDestination("diagnostics"); return
      case "navigation.library": this.#dispatchNavigationDestination("library"); return
      case "navigation.open":
        if (this.#layout().mode === "wide") {
          if (this.#focus !== "navigation") this.#wideNavigationPreviousFocus = this.#focus
          this.#focus = "navigation"
          this.#rebuild()
        } else this.#openOverlay("navigation")
        return
      case "overlay.help": this.#helpOffset = 0; this.#openOverlay("help"); return
      case "overlay.palette":
        this.#paletteQuery = ""
        this.#paletteCursor = 0
        this.#openOverlay("palette")
        return
      case "playback.control": this.#dispatchPlayback(key); return
      case "selection.move":
        if (this.#focus === "navigation") {
          this.#navigationCursor = (this.#navigationCursor + (key === "up" || key === "k" ? -1 : 1) + NAVIGATION.length) % NAVIGATION.length
          this.#rebuild()
        } else this.#moveCollection(key === "up" || key === "k" ? -1 : 1)
        return
      case "selection.open":
        if (this.#focus === "navigation") this.#dispatchNavigationSelection()
        else if (this.#state.screen === "courses") {
          const course = this.#selectedCourse()
          if (course !== undefined) this.#callbacks.onOpenCourse(course)
        } else if (this.#state.screen === "lectures") {
          const lecture = this.#selectedLecture()
          if (lecture !== undefined) this.#callbacks.onPlay(lecture)
        }
        return
      case "session.selftest": this.#callbacks.onSelfTest()
    }
  }

  #dispatchPlayback(key: string): void {
    const operation = this.#state.operation
    if (operation?.kind !== "playback" || operation.state !== "running") return
    if (key === "space") this.#callbacks.onPlaybackCommand({ action: "pause", flag: !operation.paused })
    else if (key === "left") this.#callbacks.onPlaybackCommand({ action: "seek", value: -10 })
    else if (key === "right") this.#callbacks.onPlaybackCommand({ action: "seek", value: 10 })
    else if (key === "m") this.#callbacks.onPlaybackCommand({ action: "mute", flag: !operation.muted })
    else if (key === "+" || key === "=") this.#callbacks.onPlaybackCommand({ action: "volume", value: Math.min(130, operation.volume + 5) })
    else if (key === "-") this.#callbacks.onPlaybackCommand({ action: "volume", value: Math.max(0, operation.volume - 5) })
    else if (key === "]") this.#callbacks.onPlaybackCommand({ action: "speed", value: Math.min(4, operation.speed + 0.25) })
    else if (key === "[") this.#callbacks.onPlaybackCommand({ action: "speed", value: Math.max(0.25, operation.speed - 0.25) })
    else if (key === "v") this.#callbacks.onPlaybackCommand({ action: "cycleVideo" })
  }

  #dispatchNavigationSelection(): void {
    this.#dispatchNavigationDestination(NAVIGATION[this.#navigationCursor]?.screen)
  }

  #dispatchNavigationDestination(screen: CollectionScreen | undefined): void {
    if (screen === undefined) return
    if (this.#focus === "navigation") {
      this.#transitionFocus(effectiveFocus("collection", this.#layout()))
      this.#rebuild()
    }
    if (screen === "courses") this.#callbacks.onCourses()
    else if (screen === "library") this.#callbacks.onLibrary()
    else this.#callbacks.onDiagnostics()
  }

  #transitionFocus(next: PaneFocus): void {
    if (this.#focus === "navigation" && next !== "navigation") this.#wideNavigationPreviousFocus = undefined
    this.#focus = next
  }

  #moveCollection(delta: number): void {
    const screen = collectionScreen(this.#state.screen)
    if (screen === undefined) return
    const count = this.#currentItems().length
    if (count === 0) return
    const current = this.#state.collections[screen]
    const selected = this.#selectedIndex(screen, count)
    this.#setCollection(screen, { ...current, selected: clamp(selected + delta, 0, count - 1) })
  }

  #setCollection(screen: CollectionScreen, state: CollectionState): void {
    this.#callbacks.onCollectionState(screen, state)
  }

  #openOverlay(kind: OverlayKind): void {
    if (this.#overlays.length > 0) return
    this.#filtering = false
    this.#overlays.push({ kind, previousFocus: effectiveFocus(this.#focus, this.#layout()) })
    this.#rebuild()
  }

  #closeOverlay(rebuild = true): void {
    const closed = this.#overlays.pop()
    if (closed !== undefined) this.#focus = effectiveFocus(closed.previousFocus, this.#layout())
    if (rebuild) this.#rebuild()
  }

  #topOverlay(): OverlayState | undefined { return this.#overlays.at(-1) }

  #commandContext(): CommandContext {
    return {
      focus: effectiveFocus(this.#focus, this.#layout()),
      layout: this.#layout(),
      navigationTarget: NAVIGATION[this.#navigationCursor]?.screen,
      overlay: this.#topOverlay()?.kind,
      state: this.#state,
    }
  }

  #layout(): WorkspaceLayout {
    return calculateLayout(this.#renderer.terminalWidth, this.#renderer.terminalHeight, hasActivity(this.#state) || this.#blockedCommandFeedback !== undefined)
  }

  #rebuild(): void {
    if (this.#tree !== undefined) {
      this.#renderer.root.remove(this.#tree)
      this.#tree.destroyRecursively()
    }
    this.#tree = this.#buildTree()
    this.#renderer.root.add(this.#tree)
  }

  #buildTree(): BoxRenderable {
    const width = this.#renderer.terminalWidth
    const height = this.#renderer.terminalHeight
    const layout = this.#layout()
    const shell = new BoxRenderable(this.#renderer, { backgroundColor: COLORS.background, flexDirection: "column", height: "100%", id: "app-shell", width: "100%" })
    if (layout.header.height > 0) shell.add(this.#header(layout.header.height))
    if (layout.collection.height > 0) shell.add(this.#body(layout))
    if (layout.activity.height > 0) shell.add(this.#activityDock(layout.activity.height))
    if (layout.footer.height > 0) shell.add(this.#footer(layout.footer.height))
    const overlay = this.#topOverlay()
    if (overlay?.kind === "help") shell.add(this.#helpOverlay(width, height))
    else if (overlay?.kind === "palette") shell.add(this.#paletteOverlay(width, height))
    else if (overlay?.kind === "navigation") shell.add(this.#navigationOverlay(width, height))
    return shell
  }

  #header(height: number): BoxRenderable {
    const header = new BoxRenderable(this.#renderer, { alignItems: "center", border: ["bottom"], borderColor: COLORS.border, flexDirection: "row", height, justifyContent: "space-between", paddingX: 2, width: "100%" })
    const narrowRecovery = this.#state.authStatus !== "ready" && this.#renderer.terminalWidth < 60
    const title = narrowRecovery ? "IMPARTUS  /  Workspace" : `IMPARTUS  /  ${screenTitle(this.#state.screen)}`
    const status = this.#blockedCommandFeedback?.reason ?? (this.#state.authStatus !== "ready" && this.#renderer.terminalWidth < 100 ? "Auth unavailable" : this.#state.status)
    header.add(text(this.#renderer, title, COLORS.foreground, TextAttributes.BOLD))
    header.add(text(this.#renderer, `● ${status}`, status === "Connected" ? COLORS.success : COLORS.warning))
    return header
  }

  #body(layout: WorkspaceLayout): BoxRenderable {
    const body = bodyBox(this.#renderer)
    if (layout.mode === "wide") {
      body.add(this.#navigationPanel(layout.navigation.width))
      body.add(this.#workspacePanel("auto", layout.collection.height))
      body.add(this.#inspectorPanel(layout.inspector.width))
    } else if (layout.mode === "medium") {
      body.add(this.#workspacePanel("auto", layout.collection.height))
      body.add(this.#inspectorPanel(layout.inspector.width))
    } else body.add(this.#workspacePanel("100%", layout.collection.height))
    return body
  }

  #navigationPanel(width: number): BoxRenderable {
    const panel = panelBox(this.#renderer, paneTitle("Navigation", this.#focus === "navigation"), width, this.#focus === "navigation")
    NAVIGATION.forEach((entry, index) => {
      const selected = index === this.#navigationCursor && this.#focus === "navigation"
      const active = entry.screen === this.#state.screen || (entry.screen === "courses" && this.#state.screen === "lectures")
      const unavailable = entry.screen === "courses" && this.#state.authStatus !== "ready"
      const label = unavailable ? `${entry.label} [auth]` : entry.label
      panel.add(row(this.#renderer, `${selected ? ">" : " "} ${active ? "●" : "○"} ${label}`, selected))
    })
    return panel
  }

  #workspacePanel(width: number | "auto" | `${number}%`, bodyHeight: number): BoxRenderable {
    const panel = panelBox(this.#renderer, paneTitle(screenTitle(this.#state.screen), this.#focus === "collection"), width, this.#focus === "collection")
    panel.flexGrow = 1
    if (this.#state.pending !== undefined) {
      panel.add(text(this.#renderer, "Loading current workspace…", COLORS.accent, TextAttributes.BOLD))
      return panel
    }
    if (this.#state.error !== undefined) {
      panel.add(text(this.#renderer, this.#state.error, COLORS.danger, TextAttributes.BOLD))
      const recovery = this.#state.screen === "playback"
        ? "Press esc to return."
        : this.#state.screen === "courses"
          ? "Press r to retry."
          : "Press r to retry or esc to return."
      panel.add(text(this.#renderer, recovery, COLORS.dim))
      return panel
    }
    if (this.#state.screen === "courses" && this.#state.authStatus !== "ready") {
      if (bodyHeight <= 4) {
        panel.add(text(this.#renderer, "Auth unavailable; press r to retry", COLORS.warning, TextAttributes.BOLD))
        return panel
      }
      panel.add(text(this.#renderer, "Authentication is unavailable", COLORS.warning, TextAttributes.BOLD))
      panel.add(text(this.#renderer, "Press r to retry. Local library and diagnostics remain available.", COLORS.dim))
      return panel
    }
    const rows = Math.max(1, Math.floor((Math.max(1, bodyHeight - 4) + 1) / 2))
    if (this.#state.screen === "courses") this.#renderCourses(panel, rows)
    else if (this.#state.screen === "lectures") this.#renderLectures(panel, rows)
    else if (this.#state.screen === "library") this.#renderArtifacts(panel, rows)
    else if (this.#state.screen === "diagnostics") this.#renderDiagnostics(panel, rows)
    else this.#renderPlayback(panel)
    return panel
  }

  #renderCourses(panel: BoxRenderable, rows: number): void {
    const courses = this.#filteredCourses()
    const selected = this.#selectedIndex("courses", courses.length)
    const visible = visibleRange(courses, selected, rows)
    if (visible.items.length === 0) {
      panel.add(text(this.#renderer, this.#state.collections.courses.filter === "" ? "No courses available" : "No matching courses", COLORS.dim))
      return
    }
    visible.items.forEach((course, index) => {
      const active = visible.offset + index === selected
      const label = this.#courseLabels.get(course) ?? middleEllipsis(normalizedCourseName(course.subjectName), COURSE_LABEL_WIDTH)
      panel.add(row(this.#renderer, `${active ? ">" : " "} ${label}  ·  ${course.videoCount} lectures`, active))
    })
  }

  #renderLectures(panel: BoxRenderable, rows: number): void {
    const lectures = this.#filteredLectures()
    const selected = this.#selectedIndex("lectures", lectures.length)
    const visible = visibleRange(lectures, selected, rows)
    if (visible.items.length === 0) {
      panel.add(text(this.#renderer, this.#state.collections.lectures.filter === "" ? "No lectures available" : "No matching lectures", COLORS.dim))
      return
    }
    visible.items.forEach((lecture, index) => {
      const active = visible.offset + index === selected
      const audio = lecture.noAudio ? `  ${lectureAudioLabel(true)}` : ""
      panel.add(row(this.#renderer, `${active ? ">" : " "} ${padSequence(lecture.sequence)}  ${lecture.topic}${audio}`, active))
    })
  }

  #renderArtifacts(panel: BoxRenderable, rows: number): void {
    const artifacts = this.#filteredArtifacts()
    const selected = this.#selectedIndex("library", artifacts.length)
    const visible = visibleRange(artifacts, selected, rows)
    if (visible.items.length === 0) {
      panel.add(text(this.#renderer, this.#state.collections.library.filter === "" ? "No downloaded lectures yet" : "No matching downloaded lectures", COLORS.dim))
      return
    }
    visible.items.forEach((artifact, index) => {
      const active = visible.offset + index === selected
      panel.add(row(this.#renderer, `${active ? ">" : " "} ${padSequence(artifact.sequence)}  ${artifact.topic}`, active))
    })
  }

  #renderDiagnostics(panel: BoxRenderable, rows: number): void {
    const diagnostics = this.#filteredDiagnostics()
    const selected = this.#selectedIndex("diagnostics", diagnostics.length)
    const visible = visibleRange(diagnostics, selected, rows)
    if (visible.items.length === 0) {
      panel.add(text(this.#renderer, this.#state.collections.diagnostics.filter === "" ? "No diagnostics reported" : "No matching diagnostics", COLORS.dim))
      return
    }
    visible.items.forEach((diagnostic, index) => {
      const active = visible.offset + index === selected
      panel.add(row(this.#renderer, `${active ? ">" : " "} [${diagnostic.status.toUpperCase()}] ${diagnostic.name}`, active))
    })
  }

  #renderPlayback(panel: BoxRenderable): void {
    const lecture = this.#state.activeLecture
    const operation = this.#state.operation
    if (lecture !== undefined && this.#state.loading) {
      panel.add(text(this.#renderer, "Starting playback…", COLORS.accent, TextAttributes.BOLD))
      panel.add(text(this.#renderer, lecture.topic, COLORS.foreground, TextAttributes.BOLD))
      return
    }
    if (lecture === undefined || operation?.kind !== "playback") {
      panel.add(text(this.#renderer, "Playback is unavailable", COLORS.danger))
      return
    }
    if (operation.state !== "running") {
      panel.add(text(this.#renderer, `Playback ${operation.state}`, operation.state === "failed" ? COLORS.danger : COLORS.accent, TextAttributes.BOLD))
      panel.add(text(this.#renderer, lecture.topic, COLORS.foreground, TextAttributes.BOLD))
      panel.add(text(this.#renderer, "Press esc to return", COLORS.dim))
      return
    }
    panel.add(text(this.#renderer, "Playing in mpv", COLORS.success, TextAttributes.BOLD))
    panel.add(text(this.#renderer, lecture.topic, COLORS.foreground, TextAttributes.BOLD))
    panel.add(text(this.#renderer, `${formatDuration(operation.positionSeconds)} / ${formatDuration(operation.durationSeconds)}`, COLORS.accent))
    panel.add(text(this.#renderer, `${operation.paused ? "paused" : "playing"}  ·  ${operation.muted ? "muted" : `volume ${Math.round(operation.volume)}%`}  ·  ${operation.speed.toFixed(2)}x`, COLORS.dim))
    panel.add(text(this.#renderer, "space pause  ←/→ seek  m mute  +/- volume  [/] speed  v camera", COLORS.dim))
  }

  #inspectorPanel(width: number): BoxRenderable {
    const panel = panelBox(this.#renderer, paneTitle("Inspector", this.#focus === "inspector"), width, this.#focus === "inspector")
    if (this.#state.screen === "lectures") {
      const lecture = this.#selectedLecture()
      if (lecture !== undefined) {
        panel.add(text(this.#renderer, lecture.topic, COLORS.foreground, TextAttributes.BOLD))
        panel.add(text(this.#renderer, lecture.professorName, COLORS.dim))
        panel.add(text(this.#renderer, formatDuration(lecture.durationSeconds), COLORS.accent))
        panel.add(text(this.#renderer, lectureAudioLabel(lecture.noAudio), lecture.noAudio ? COLORS.danger : COLORS.accent))
        panel.add(text(this.#renderer, `${lecture.views} view${lecture.views === 1 ? "" : "s"}  ·  ${lecture.classroomName}`, COLORS.dim))
        return panel
      }
    } else if (this.#state.screen === "library") {
      const artifact = this.#selectedArtifact()
      if (artifact !== undefined) {
        panel.add(text(this.#renderer, artifact.topic, COLORS.foreground, TextAttributes.BOLD))
        panel.add(text(this.#renderer, `${artifact.presentFileCount}/${artifact.fileCount} files present`, COLORS.accent))
        panel.add(text(this.#renderer, formatBytes(artifact.totalBytes), COLORS.dim))
        return panel
      }
    } else if (this.#state.screen === "diagnostics") {
      const diagnostic = this.#selectedDiagnostic()
      if (diagnostic !== undefined) {
        const color = diagnostic.status === "pass" ? COLORS.success : diagnostic.status === "fail" ? COLORS.danger : COLORS.warning
        panel.add(text(this.#renderer, diagnostic.name, COLORS.foreground, TextAttributes.BOLD))
        panel.add(text(this.#renderer, diagnostic.status.toUpperCase(), color))
        panel.add(text(this.#renderer, diagnostic.detail, COLORS.dim))
        return panel
      }
    }
    if (this.#state.screen === "courses") {
      const course = this.#selectedCourse()
      if (course !== undefined) {
        panel.add(text(this.#renderer, course.subjectName, COLORS.foreground, TextAttributes.BOLD))
        panel.add(text(this.#renderer, course.professorName, COLORS.dim))
        panel.add(text(this.#renderer, `${course.videoCount} lectures`, COLORS.accent))
        const context = this.#commandContext()
        const open = commandForKey(context, "enter")
        if (!this.#filtering && context.overlay === undefined && context.focus !== "navigation" && open?.command.id === "selection.open" && open.availability.enabled) {
          panel.add(text(this.#renderer, "enter  open lectures", COLORS.dim))
        }
        return panel
      }
    }
    panel.add(text(this.#renderer, "No selection", COLORS.dim))
    return panel
  }

  #activityDock(height: number): BoxRenderable {
    const dock = new BoxRenderable(this.#renderer, { border: ["top"], borderColor: this.#focus === "activity" ? COLORS.accent : COLORS.border, flexDirection: "row", height, justifyContent: "space-between", paddingX: 2, width: "100%" })
    const operation = this.#state.operation
    const status = this.#blockedCommandFeedback?.reason ?? this.#state.error ?? this.#state.status
    dock.add(text(this.#renderer, `${this.#focus === "activity" ? "[ACTIVE] " : ""}${status}`, this.#state.error === undefined ? COLORS.dim : COLORS.danger))
    if (operation !== undefined) dock.add(text(this.#renderer, `${operation.kind} ${operation.state} ${Math.round(operation.percent)}%`, COLORS.accent))
    return dock
  }

  #footer(height: number): BoxRenderable {
    const footer = new BoxRenderable(this.#renderer, { alignItems: "center", border: ["top"], borderColor: COLORS.border, flexDirection: "row", height, justifyContent: "space-between", paddingX: 1, width: "100%" })
    const screen = collectionScreen(this.#state.screen)
    const filter = screen === undefined ? "" : this.#state.collections[screen].filter
    const context = this.#commandContext()
    if (context.overlay !== undefined) return footer
    const hints = footerCommands(context)
      .filter(({ command }) => command.id !== "app.quit")
      .slice(0, 6)
      .map(({ command }) => `${command.keys[0]} ${command.label}`)
      .join("   ")
    const activeFilterHints = [
      commandForKey(context, "/")?.availability.enabled === true ? "/ edit" : undefined,
      commandForKey(context, "down")?.availability.enabled === true ? "↑↓ navigate" : undefined,
    ].filter((hint): hint is string => hint !== undefined).join("   ")
    const innerWidth = Math.max(0, this.#renderer.terminalWidth - 2)
    const quitWidth = this.#filtering ? 0 : Math.min("q quit".length, innerWidth)
    const contextualWidth = innerWidth - quitWidth
    const content = this.#filtering
      ? editingFilterFooter(filter, contextualWidth)
      : filter !== ""
        ? `Filter: ${filter}${activeFilterHints === "" ? "" : `   ${activeFilterHints}`}`
        : hints
    if (contextualWidth > 0) footer.add(new TextRenderable(this.#renderer, { content, fg: this.#filtering ? COLORS.accent : COLORS.dim, height: 1, truncate: true, width: contextualWidth }))
    if (quitWidth > 0) footer.add(new TextRenderable(this.#renderer, { content: "q quit", fg: COLORS.dim, height: 1, truncate: true, width: quitWidth }))
    return footer
  }

  #helpOverlay(width: number, height: number): BoxRenderable {
    const commands = commandsForHelp(this.#commandContext())
    const overlay = overlayBox(this.#renderer, "Command guide", width, height, commands.length + 5)
    const rows = Math.max(1, overlay.height - 5)
    const offset = clamp(this.#helpOffset, 0, Math.max(0, commands.length - rows))
    const visible = commands.slice(offset, offset + rows)
    for (const entry of visible) {
      const reason = entry.availability.enabled || entry.availability.reason === "" ? "" : ` — ${entry.availability.reason}`
      overlay.add(singleLineText(this.#renderer, `${entry.command.keys.join("/").padEnd(12)} ${entry.command.label}${reason}`, entry.availability.enabled ? COLORS.foreground : COLORS.dim))
    }
    const first = commands.length === 0 ? 0 : offset + 1
    const last = offset + visible.length
    overlay.add(text(this.#renderer, `↑↓ scroll ${first}-${last}/${commands.length}   Esc close`, COLORS.dim))
    return overlay
  }

  #paletteOverlay(width: number, height: number): BoxRenderable {
    const overlay = overlayBox(this.#renderer, "Command palette", width, height, 16)
    overlay.add(text(this.#renderer, `> ${this.#paletteQuery}█`, COLORS.accent, TextAttributes.BOLD))
    const matches = this.#paletteMatches()
    const visible = visibleRange(matches, this.#paletteCursor, Math.max(1, overlay.height - 6))
    visible.items.forEach((entry, index) => {
      const selected = visible.offset + index === this.#paletteCursor
      const reason = entry.availability.enabled || entry.availability.reason === "" ? "" : ` — ${entry.availability.reason}`
      overlay.add(row(this.#renderer, `${selected ? ">" : " "} ${entry.command.label}${reason}`, selected))
    })
    if (matches.length === 0) overlay.add(text(this.#renderer, "No matching commands", COLORS.dim))
    overlay.add(text(this.#renderer, "↑↓ select   Enter run   Esc close", COLORS.dim))
    return overlay
  }

  #navigationOverlay(width: number, height: number): BoxRenderable {
    const overlay = overlayBox(this.#renderer, "Navigation", width, height, 10)
    NAVIGATION.forEach((entry, index) => {
      const unavailable = entry.screen === "courses" && this.#state.authStatus !== "ready"
      const reason = unavailable ? " — Authentication is unavailable" : ""
      overlay.add(row(this.#renderer, `${index === this.#navigationCursor ? ">" : " "} ${entry.label}${reason}`, index === this.#navigationCursor))
    })
    const open = commandForKey(this.#commandContext(), "enter")
    const hint = open?.availability.enabled === false && open.availability.reason !== ""
      ? `↑↓ select   ${open.availability.reason}   Esc close`
      : "↑↓ select   Enter open   Esc close"
    overlay.add(text(this.#renderer, hint, COLORS.dim))
    return overlay
  }

  #paletteMatches() { return commandsForPalette(this.#commandContext(), this.#paletteQuery) }

  #currentItems(): readonly unknown[] {
    if (this.#state.screen === "courses") return this.#filteredCourses()
    if (this.#state.screen === "lectures") return this.#filteredLectures()
    if (this.#state.screen === "library") return this.#filteredArtifacts()
    if (this.#state.screen === "diagnostics") return this.#filteredDiagnostics()
    return []
  }

  #filteredCourses(): readonly Course[] {
    const query = normalizedQuery(this.#state.collections.courses.filter)
    return query === "" ? this.#state.courses : this.#state.courses.filter((course) => normalizedQuery(`${course.subjectName} ${course.professorName} ${course.sessionName}`).includes(query))
  }

  #filteredLectures(): readonly Lecture[] {
    const query = normalizedQuery(this.#state.collections.lectures.filter)
    return query === "" ? this.#state.lectures : this.#state.lectures.filter((lecture) => normalizedQuery(`${lecture.topic} ${lecture.professorName} ${lecture.classroomName} ${lecture.startTime}`).includes(query))
  }

  #filteredArtifacts(): readonly ArtifactSummary[] {
    const query = normalizedQuery(this.#state.collections.library.filter)
    return query === "" ? this.#state.artifacts : this.#state.artifacts.filter((artifact) => normalizedQuery(artifact.topic).includes(query))
  }

  #filteredDiagnostics(): readonly Diagnostic[] {
    const query = normalizedQuery(this.#state.collections.diagnostics.filter)
    return query === "" ? this.#state.diagnostics : this.#state.diagnostics.filter((diagnostic) => normalizedQuery(`${diagnostic.name} ${diagnostic.status} ${diagnostic.detail}`).includes(query))
  }

  #selectedCourse(): Course | undefined { const items = this.#filteredCourses(); return items[this.#selectedIndex("courses", items.length)] }
  #selectedLecture(): Lecture | undefined { const items = this.#filteredLectures(); return items[this.#selectedIndex("lectures", items.length)] }
  #selectedArtifact(): ArtifactSummary | undefined { const items = this.#filteredArtifacts(); return items[this.#selectedIndex("library", items.length)] }
  #selectedDiagnostic(): Diagnostic | undefined { const items = this.#filteredDiagnostics(); return items[this.#selectedIndex("diagnostics", items.length)] }
  #selectedIndex(screen: CollectionScreen, count: number): number { return count === 0 ? 0 : clamp(this.#state.collections[screen].selected, 0, count - 1) }
}

function bodyBox(renderer: CliRenderer): BoxRenderable {
  return new BoxRenderable(renderer, { flexDirection: "row", flexGrow: 1, gap: 1, paddingX: 1, width: "100%" })
}

function panelBox(renderer: CliRenderer, title: string, width: number | "auto" | `${number}%`, focused: boolean): BoxRenderable {
  return new BoxRenderable(renderer, { backgroundColor: COLORS.panel, border: true, borderColor: focused ? COLORS.accent : COLORS.border, borderStyle: "rounded", flexDirection: "column", gap: 1, height: "100%", padding: 1, title, width })
}

function overlayBox(renderer: CliRenderer, title: string, width: number, height: number, targetHeight: number): BoxRenderable {
  const overlayWidth = Math.max(1, Math.min(68, width - 2))
  const overlayHeight = Math.max(1, Math.min(targetHeight, height - 2))
  return new BoxRenderable(renderer, { backgroundColor: COLORS.panel, border: true, borderColor: COLORS.accent, borderStyle: "rounded", height: overlayHeight, left: Math.max(0, Math.floor((width - overlayWidth) / 2)), padding: 1, position: "absolute", title, top: Math.max(0, Math.floor((height - overlayHeight) / 2)), width: overlayWidth, zIndex: 20 })
}

function row(renderer: CliRenderer, content: string, selected: boolean): TextRenderable {
  return new TextRenderable(renderer, { attributes: selected ? TextAttributes.BOLD : TextAttributes.NONE, bg: selected ? COLORS.selected : COLORS.panel, content, fg: selected ? COLORS.accent : COLORS.foreground, height: 1, truncate: true, width: "100%" })
}

function text(renderer: CliRenderer, content: string, color: string, attributes = TextAttributes.NONE): TextRenderable {
  return new TextRenderable(renderer, { attributes, content, fg: color, height: content === "" ? 1 : "auto", wrapMode: "word", width: "100%" })
}

function singleLineText(renderer: CliRenderer, content: string, color: string): TextRenderable {
  return new TextRenderable(renderer, { content, fg: color, height: 1, truncate: true, width: "100%" })
}

export function lectureAudioLabel(noAudio: boolean): string { return noAudio ? "🎙️× probably no audio" : "🎙️ audio reported" }
export function courseRailLabels(courses: readonly Course[]): ReadonlyMap<Course, string> { return courseLabels(courses) }

function courseLabels(courses: readonly Course[]): ReadonlyMap<Course, string> {
  const cohorts = new Map<string, Array<{ course: Course; name: string }>>()
  for (const course of courses) {
    const key = `${course.instituteId}:${course.sessionId}`
    const cohort = cohorts.get(key) ?? []
    cohort.push({ course, name: normalizedCourseName(course.subjectName) })
    cohorts.set(key, cohort)
  }
  const labels = new Map<Course, string>()
  for (const cohort of cohorts.values()) {
    const names = cohort.map(({ name }) => name)
    const prefixes = names.map((name) => name.includes("_") ? name.slice(0, name.indexOf("_")).trim() : "")
    const shared = prefixes[0] ?? ""
    const sharedUnderscore = cohort.length > 1 && shared !== "" && prefixes.every((prefix) => prefix.toLocaleLowerCase() === shared.toLocaleLowerCase())
    const common = cohort.length > 1 && !sharedUnderscore ? commonLeadingTokens(names) : 0
    for (const { course, name } of cohort) {
      let label = name
      if (sharedUnderscore) label = name.slice(name.indexOf("_") + 1).trim()
      else if (common >= 3) label = name.split(" ").slice(common).join(" ").trim()
      labels.set(course, middleEllipsis(label === "" ? name : label, COURSE_LABEL_WIDTH))
    }
  }
  return labels
}

function normalizeState(state: FoundationState): FoundationState {
  return { ...state, artifacts: [...state.artifacts], collections: { courses: { ...state.collections.courses }, diagnostics: { ...state.collections.diagnostics }, lectures: { ...state.collections.lectures }, library: { ...state.collections.library } }, courses: [...state.courses], diagnostics: [...state.diagnostics], lectures: [...state.lectures], operation: state.operation === undefined ? undefined : { ...state.operation }, pending: state.pending === undefined ? undefined : { ...state.pending } }
}

function hasActivity(state: FoundationState): boolean { return state.loading || state.error !== undefined || state.operation !== undefined || state.status !== "Connected" }
function collectionScreen(screen: FoundationState["screen"]): CollectionScreen | undefined { return screen === "playback" ? undefined : screen }
function paneTitle(title: string, focused: boolean): string { return focused ? `[ACTIVE] ${title}` : title }

function normalizedKey(key: KeyEvent): string {
  const name = key.name.toLocaleLowerCase()
  if ((key.ctrl && name === "c") || key.sequence === "\u0003") return "ctrl+c"
  if (key.ctrl && (name === "p" || key.sequence.toLocaleLowerCase() === "p")) return "ctrl+p"
  if (name === "tab" && key.shift) return "shift+tab"
  if (name === "return") return "enter"
  if (name === "space" || key.sequence === " ") return "space"
  if (name === "escape" || key.sequence === "\u001b") return "escape"
  if (name === "backspace" || key.sequence === "\u007f" || key.sequence === "\b") return "backspace"
  if (["up", "down", "left", "right", "tab", "enter"].includes(name)) return name
  return key.sequence.length > 0 ? key.sequence.toLocaleLowerCase() : name
}

function printableInput(key: KeyEvent): boolean { return !key.ctrl && !key.meta && !key.option && graphemes(key.sequence).length === 1 && key.sequence >= " " && key.sequence !== "\u007f" }
function visibleRange<T>(items: readonly T[], selected: number, rows: number): { items: readonly T[]; offset: number } { const count = Math.max(1, rows); const offset = selected >= count ? selected - count + 1 : 0; return { items: items.slice(offset, offset + count), offset } }
function helpPageSize(commandCount: number, terminalHeight: number): number { return Math.max(1, Math.min(commandCount + 5, terminalHeight - 2) - 5) }
function normalizedCourseName(value: string): string { return value.replace(/\s+/gu, " ").trim() }

function commonLeadingTokens(values: readonly string[]): number {
  const lists = values.map((value) => value.split(" "))
  const shortest = Math.min(...lists.map((tokens) => tokens.length))
  let count = 0
  while (count < shortest) {
    const candidate = lists[0]?.[count]?.toLocaleLowerCase()
    if (candidate === undefined || !lists.every((tokens) => tokens[count]?.toLocaleLowerCase() === candidate)) break
    count++
  }
  return count
}

function sameCourseCatalog(left: readonly Course[], right: readonly Course[]): boolean { return left.length === right.length && left.every((course, index) => course === right[index]) }
function middleEllipsis(value: string, width: number): string { const characters = graphemes(value); if (characters.length <= width) return value; const available = width - 1; const leading = Math.ceil(available / 2); const trailing = Math.floor(available / 2); return `${characters.slice(0, leading).join("")}…${characters.slice(-trailing).join("")}` }
function screenTitle(screen: FoundationState["screen"]): string { if (screen === "lectures") return "Lecture workspace"; if (screen === "library") return "Local library"; if (screen === "diagnostics") return "Diagnostics"; if (screen === "playback") return "Now playing"; return "Learning workspace" }
function padSequence(sequence: number): string { return String(Math.max(0, sequence)).padStart(3, "0") }

function formatDuration(seconds: number): string {
  const safe = Math.max(0, Math.floor(seconds)); const hours = Math.floor(safe / 3600); const minutes = Math.floor((safe % 3600) / 60); const remaining = safe % 60
  return hours > 0 ? `${String(hours).padStart(2, "0")}:${String(minutes).padStart(2, "0")}:${String(remaining).padStart(2, "0")}` : `${String(minutes).padStart(2, "0")}:${String(remaining).padStart(2, "0")}`
}

function formatBytes(bytes: number): string { if (bytes < 1024) return `${bytes} B`; if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`; if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`; return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GiB` }
function normalizedQuery(value: string): string { return value.trim().toLocaleLowerCase() }
function clamp(value: number, minimum: number, maximum: number): number { return Math.max(minimum, Math.min(maximum, value)) }

function editingFilterFooter(filter: string, width: number): string {
  const prefix = "Filter: "
  const suffix = "█  enter apply esc close"
  const available = Math.max(0, width - graphemes(prefix).length - graphemes(suffix).length)
  const characters = graphemes(filter)
  const query = characters.length <= available
    ? filter
    : available <= 0
      ? ""
      : available === 1
        ? "…"
        : `…${characters.slice(-(available - 1)).join("")}`
  return `${prefix}${query}${suffix}`
}
