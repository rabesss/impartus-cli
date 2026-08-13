import {
  BoxRenderable,
  CliRenderEvents,
  TextAttributes,
  TextRenderable,
  type CliRenderer,
  type KeyEvent,
} from "@opentui/core"

import type {
  ArtifactSummary,
  Course,
  Diagnostic,
  Lecture,
  OperationState,
} from "./protocol/types.gen.ts"

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

const WIDE_WIDTH = 120
const MEDIUM_WIDTH = 72

export type FoundationScreen = "courses" | "diagnostics" | "lectures" | "library"

export interface FoundationOperation {
  id: string
  percent: number
  state: OperationState
}

export interface FoundationState {
  activeCourse: Course | undefined
  artifacts: ArtifactSummary[]
  courses: Course[]
  diagnostics: Diagnostic[]
  error: string | undefined
  lectures: Lecture[]
  loading: boolean
  operation: FoundationOperation | undefined
  screen: FoundationScreen
  selectedCourse: number
  selectedItem: number
  status: string
}

export interface FoundationCallbacks {
  onBack(): void
  onDiagnostics(): void
  onLibrary(): void
  onOpenCourse(course: Course): void
  onQuit(): void
  onRetry(): void
  onSelfTest(): void
}

interface Command {
  description: string
  key: string
}

const COMMANDS: readonly Command[] = [
  { key: "↑/k ↓/j", description: "move selection" },
  { key: "enter", description: "open selected course" },
  { key: "l", description: "open local library" },
  { key: "!", description: "open diagnostics" },
  { key: "r", description: "retry the current view" },
  { key: "s", description: "run connection test" },
  { key: "esc", description: "return to courses" },
  { key: "q", description: "quit" },
]

export class FoundationView {
  readonly #renderer: CliRenderer
  readonly #callbacks: FoundationCallbacks
  #state: FoundationState
  #tree: BoxRenderable | undefined
  #helpVisible = false

  public constructor(renderer: CliRenderer, state: FoundationState, callbacks: FoundationCallbacks) {
    this.#renderer = renderer
    this.#state = normalizeState(state)
    this.#callbacks = callbacks
    this.#renderer.keyInput.on("keypress", this.#onKeyPress)
    this.#renderer.on(CliRenderEvents.RESIZE, this.#onResize)
    this.#rebuild()
  }

  public update(state: FoundationState): void {
    this.#state = normalizeState(state)
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
    this.#rebuild()
  }

  readonly #onKeyPress = (key: KeyEvent): void => {
    const name = key.name.toLowerCase()
    const sequence = key.sequence
    if ((key.ctrl && name === "c") || sequence === "\u0003") {
      this.#callbacks.onQuit()
      return
    }
    if (this.#helpVisible) {
      if (name === "escape" || sequence === "\u001b" || sequence === "?") {
        this.#helpVisible = false
        this.#rebuild()
      }
      return
    }
    if (sequence === "?" || name === "?") {
      this.#helpVisible = true
      this.#rebuild()
      return
    }
    if (sequence === "q" || name === "q") {
      this.#callbacks.onQuit()
      return
    }
    if (this.#state.loading) return
    if (sequence === "l" || name === "l") {
      this.#callbacks.onLibrary()
      return
    }
    if (sequence === "!" || name === "!") {
      this.#callbacks.onDiagnostics()
      return
    }
    if (name === "escape" || sequence === "\u001b" || name === "backspace") {
      this.#callbacks.onBack()
      return
    }
    if (sequence === "r" || name === "r") {
      this.#callbacks.onRetry()
      return
    }
    if ((sequence === "s" || name === "s") && this.#state.operation?.state !== "running") {
      this.#callbacks.onSelfTest()
      return
    }
    if ((name === "return" || name === "enter" || sequence === "\r") && this.#state.screen === "courses") {
      const course = this.#state.courses[this.#state.selectedCourse]
      if (course !== undefined) this.#callbacks.onOpenCourse(course)
      return
    }
    const direction = name === "up" || sequence === "k" ? -1 : name === "down" || sequence === "j" ? 1 : 0
    if (direction !== 0) this.#moveSelection(direction)
  }

  #moveSelection(direction: number): void {
    if (this.#state.screen === "courses") {
      if (this.#state.courses.length === 0) return
      this.#state = {
        ...this.#state,
        selectedCourse: clamp(this.#state.selectedCourse + direction, 0, this.#state.courses.length - 1),
      }
    } else {
      const count = currentItemCount(this.#state)
      if (count === 0) return
      this.#state = {
        ...this.#state,
        selectedItem: clamp(this.#state.selectedItem + direction, 0, count - 1),
      }
    }
    this.#rebuild()
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
    const shell = new BoxRenderable(this.#renderer, {
      backgroundColor: COLORS.background,
      flexDirection: "column",
      height: "100%",
      id: "app-shell",
      width: "100%",
    })
    shell.add(this.#header())
    if (width >= WIDE_WIDTH) shell.add(this.#wideBody(height))
    else if (width >= MEDIUM_WIDTH) shell.add(this.#mediumBody(height))
    else shell.add(this.#narrowBody(height))
    shell.add(this.#footer())
    if (this.#helpVisible) shell.add(this.#helpOverlay(width, height))
    return shell
  }

  #header(): BoxRenderable {
    const header = new BoxRenderable(this.#renderer, {
      alignItems: "center",
      border: ["bottom"],
      borderColor: COLORS.border,
      flexDirection: "row",
      height: 3,
      justifyContent: "space-between",
      paddingX: 2,
      width: "100%",
    })
    header.add(new TextRenderable(this.#renderer, {
      attributes: TextAttributes.BOLD,
      content: `IMPARTUS  /  ${screenTitle(this.#state.screen)}`,
      fg: COLORS.foreground,
      height: 1,
    }))
    header.add(new TextRenderable(this.#renderer, {
      content: `● ${this.#state.status}`,
      fg: this.#state.status === "Connected" ? COLORS.success : COLORS.warning,
      height: 1,
    }))
    return header
  }

  #wideBody(height: number): BoxRenderable {
    const body = bodyBox(this.#renderer)
    const rail = this.#courseRail(30, height)
    rail.flexShrink = 0
    body.add(rail)
    body.add(this.#workspacePanel("auto", height))
    const inspector = this.#inspectorPanel(34)
    inspector.flexShrink = 0
    body.add(inspector)
    return body
  }

  #mediumBody(height: number): BoxRenderable {
    const body = bodyBox(this.#renderer)
    body.add(this.#workspacePanel("62%", height))
    body.add(this.#inspectorPanel("38%"))
    return body
  }

  #narrowBody(height: number): BoxRenderable {
    const body = bodyBox(this.#renderer)
    body.add(this.#workspacePanel("100%", height))
    return body
  }

  #courseRail(width: number | `${number}%`, terminalHeight: number): BoxRenderable {
    const panel = panelBox(this.#renderer, "Courses", width)
    const rows = Math.max(1, terminalHeight - 9)
    const visible = visibleRange(this.#state.courses, this.#state.selectedCourse, rows)
    if (visible.items.length === 0) {
      panel.add(text(this.#renderer, "No courses available", COLORS.dim))
      return panel
    }
    visible.items.forEach((course, index) => {
      const selected = visible.offset + index === this.#state.selectedCourse
      panel.add(row(this.#renderer, `${selected ? "›" : " "} ${course.subjectName}`, selected))
    })
    return panel
  }

  #workspacePanel(width: number | "auto" | `${number}%`, terminalHeight: number): BoxRenderable {
    const panel = panelBox(this.#renderer, screenTitle(this.#state.screen), width)
    panel.flexGrow = 1
    if (this.#state.loading) {
      panel.add(text(this.#renderer, "Loading current workspace…", COLORS.accent, TextAttributes.BOLD))
      return panel
    }
    if (this.#state.error !== undefined) {
      panel.add(text(this.#renderer, this.#state.error, COLORS.danger, TextAttributes.BOLD))
      panel.add(text(this.#renderer, "Press r to retry or esc to return.", COLORS.dim))
      return panel
    }
    if (this.#state.screen === "courses") {
      this.#renderCourseOverview(panel)
      return panel
    }
    const rows = Math.max(1, terminalHeight - 9)
    if (this.#state.screen === "lectures") this.#renderLectures(panel, rows)
    else if (this.#state.screen === "library") this.#renderArtifacts(panel, rows)
    else this.#renderDiagnostics(panel, rows)
    return panel
  }

  #renderCourseOverview(panel: BoxRenderable): void {
    const selected = this.#state.courses[this.#state.selectedCourse]
    if (selected === undefined) {
      panel.add(text(this.#renderer, "No course catalog is available.", COLORS.dim))
      return
    }
    panel.add(text(this.#renderer, selected.subjectName, COLORS.foreground, TextAttributes.BOLD))
    panel.add(text(this.#renderer, `${selected.professorName}  ·  ${selected.sessionName}`, COLORS.dim))
    panel.add(text(this.#renderer, `${selected.videoCount} lectures`, COLORS.accent))
    panel.add(text(this.#renderer, "", COLORS.dim))
    panel.add(text(this.#renderer, "Press enter to open the lecture workspace.", COLORS.dim))
  }

  #renderLectures(panel: BoxRenderable, rows: number): void {
    const visible = visibleRange(this.#state.lectures, this.#state.selectedItem, rows)
    if (visible.items.length === 0) {
      panel.add(text(this.#renderer, "No lectures available", COLORS.dim))
      return
    }
    visible.items.forEach((lecture, index) => {
      const selected = visible.offset + index === this.#state.selectedItem
      const audio = lecture.noAudio ? "  no audio" : ""
      panel.add(row(this.#renderer, `${selected ? "›" : " "} ${padSequence(lecture.sequence)}  ${lecture.topic}${audio}`, selected))
    })
  }

  #renderArtifacts(panel: BoxRenderable, rows: number): void {
    const visible = visibleRange(this.#state.artifacts, this.#state.selectedItem, rows)
    if (visible.items.length === 0) {
      panel.add(text(this.#renderer, "No downloaded lectures yet", COLORS.dim))
      return
    }
    visible.items.forEach((artifact, index) => {
      const selected = visible.offset + index === this.#state.selectedItem
      panel.add(row(this.#renderer, `${selected ? "›" : " "} ${padSequence(artifact.sequence)}  ${artifact.topic}`, selected))
    })
  }

  #renderDiagnostics(panel: BoxRenderable, rows: number): void {
    const visible = visibleRange(this.#state.diagnostics, this.#state.selectedItem, rows)
    if (visible.items.length === 0) {
      panel.add(text(this.#renderer, "No diagnostics reported", COLORS.dim))
      return
    }
    visible.items.forEach((diagnostic, index) => {
      const selected = visible.offset + index === this.#state.selectedItem
      panel.add(row(this.#renderer, `${selected ? "›" : " "} [${diagnostic.status.toUpperCase()}] ${diagnostic.name}`, selected))
    })
  }

  #inspectorPanel(width: number | `${number}%`): BoxRenderable {
    const panel = panelBox(this.#renderer, "Inspector", width)
    if (this.#state.operation !== undefined) {
      const operation = this.#state.operation
      panel.add(text(this.#renderer, `Session test  ${Math.round(operation.percent)}%`, COLORS.accent, TextAttributes.BOLD))
      panel.add(text(this.#renderer, operation.state, operation.state === "failed" ? COLORS.danger : COLORS.dim))
      panel.add(text(this.#renderer, "", COLORS.dim))
    }
    if (this.#state.screen === "lectures") {
      const lecture = this.#state.lectures[this.#state.selectedItem]
      if (lecture !== undefined) {
        panel.add(text(this.#renderer, lecture.topic, COLORS.foreground, TextAttributes.BOLD))
        panel.add(text(this.#renderer, lecture.professorName, COLORS.dim))
        panel.add(text(this.#renderer, formatDuration(lecture.durationSeconds), COLORS.accent))
        panel.add(text(this.#renderer, `${lecture.views} view${lecture.views === 1 ? "" : "s"}  ·  ${lecture.classroomName}`, COLORS.dim))
        return panel
      }
    }
    if (this.#state.screen === "library") {
      const artifact = this.#state.artifacts[this.#state.selectedItem]
      if (artifact !== undefined) {
        panel.add(text(this.#renderer, artifact.topic, COLORS.foreground, TextAttributes.BOLD))
        panel.add(text(this.#renderer, `${artifact.presentFileCount}/${artifact.fileCount} files present`, COLORS.accent))
        panel.add(text(this.#renderer, formatBytes(artifact.totalBytes), COLORS.dim))
        return panel
      }
    }
    if (this.#state.screen === "diagnostics") {
      const diagnostic = this.#state.diagnostics[this.#state.selectedItem]
      if (diagnostic !== undefined) {
        const color = diagnostic.status === "pass" ? COLORS.success : diagnostic.status === "fail" ? COLORS.danger : COLORS.warning
        panel.add(text(this.#renderer, diagnostic.name, COLORS.foreground, TextAttributes.BOLD))
        panel.add(text(this.#renderer, diagnostic.status.toUpperCase(), color))
        panel.add(text(this.#renderer, diagnostic.detail, COLORS.dim))
        return panel
      }
    }
    const course = this.#state.courses[this.#state.selectedCourse]
    if (course !== undefined) {
      panel.add(text(this.#renderer, course.subjectName, COLORS.foreground, TextAttributes.BOLD))
      panel.add(text(this.#renderer, course.professorName, COLORS.dim))
      panel.add(text(this.#renderer, "enter  open lectures", COLORS.accent))
    } else {
      panel.add(text(this.#renderer, "Connection ready", COLORS.success))
    }
    return panel
  }

  #footer(): BoxRenderable {
    const footer = new BoxRenderable(this.#renderer, {
      alignItems: "center",
      border: ["top"],
      borderColor: COLORS.border,
      flexDirection: "row",
      height: 2,
      justifyContent: "space-between",
      paddingX: 1,
      width: "100%",
    })
    footer.add(text(this.#renderer, "↑↓ navigate   enter open   l library   ! health   ? commands", COLORS.dim))
    footer.add(text(this.#renderer, "q quit", COLORS.dim))
    return footer
  }

  #helpOverlay(width: number, height: number): BoxRenderable {
    const overlayWidth = Math.min(68, width - 2)
    const overlayHeight = Math.min(13, Math.max(8, height - 2))
    const overlay = new BoxRenderable(this.#renderer, {
      backgroundColor: COLORS.panel,
      border: true,
      borderColor: COLORS.accent,
      borderStyle: "rounded",
      height: overlayHeight,
      left: Math.max(1, Math.floor((width - overlayWidth) / 2)),
      padding: 1,
      position: "absolute",
      title: "Command guide",
      top: 1,
      width: overlayWidth,
      zIndex: 20,
    })
    const visibleCommands = COMMANDS.slice(0, Math.max(1, overlayHeight - 5))
    for (const command of visibleCommands) overlay.add(text(this.#renderer, `${command.key.padEnd(12)} ${command.description}`, COLORS.foreground))
    overlay.add(text(this.#renderer, "Esc closes this overlay", COLORS.dim))
    return overlay
  }
}

function bodyBox(renderer: CliRenderer): BoxRenderable {
  return new BoxRenderable(renderer, {
    flexDirection: "row",
    flexGrow: 1,
    gap: 1,
    padding: 1,
    width: "100%",
  })
}

function panelBox(renderer: CliRenderer, title: string, width: number | "auto" | `${number}%`): BoxRenderable {
  return new BoxRenderable(renderer, {
    backgroundColor: COLORS.panel,
    border: true,
    borderColor: COLORS.border,
    borderStyle: "rounded",
    flexDirection: "column",
    gap: 1,
    height: "100%",
    padding: 1,
    title,
    width,
  })
}

function row(renderer: CliRenderer, content: string, selected: boolean): TextRenderable {
  return new TextRenderable(renderer, {
    attributes: selected ? TextAttributes.BOLD : TextAttributes.NONE,
    bg: selected ? COLORS.selected : COLORS.panel,
    content,
    fg: selected ? COLORS.accent : COLORS.foreground,
    height: 1,
    truncate: true,
    width: "100%",
  })
}

function text(renderer: CliRenderer, content: string, color: string, attributes = TextAttributes.NONE): TextRenderable {
  return new TextRenderable(renderer, {
    attributes,
    content,
    fg: color,
    height: content === "" ? 1 : "auto",
    wrapMode: "word",
    width: "100%",
  })
}

function normalizeState(state: FoundationState): FoundationState {
  const itemCount = currentItemCount(state)
  return {
    ...state,
    artifacts: [...state.artifacts],
    courses: [...state.courses],
    diagnostics: [...state.diagnostics],
    lectures: [...state.lectures],
    selectedCourse: state.courses.length === 0 ? 0 : clamp(state.selectedCourse, 0, state.courses.length - 1),
    selectedItem: itemCount === 0 ? 0 : clamp(state.selectedItem, 0, itemCount - 1),
  }
}

function currentItemCount(state: FoundationState): number {
  if (state.screen === "courses") return state.courses.length
  if (state.screen === "lectures") return state.lectures.length
  if (state.screen === "library") return state.artifacts.length
  return state.diagnostics.length
}

function visibleRange<T>(items: readonly T[], selected: number, rows: number): { items: readonly T[]; offset: number } {
  const count = Math.max(1, rows)
  let offset = 0
  if (selected >= count) offset = selected - count + 1
  return { items: items.slice(offset, offset + count), offset }
}

function screenTitle(screen: FoundationScreen): string {
  if (screen === "lectures") return "Lecture workspace"
  if (screen === "library") return "Local library"
  if (screen === "diagnostics") return "Diagnostics"
  return "Learning workspace"
}

function padSequence(sequence: number): string {
  return String(Math.max(0, sequence)).padStart(3, "0")
}

function formatDuration(seconds: number): string {
  const safe = Math.max(0, Math.floor(seconds))
  const hours = Math.floor(safe / 3600)
  const minutes = Math.floor((safe % 3600) / 60)
  const remaining = safe % 60
  return hours > 0
    ? `${String(hours).padStart(2, "0")}:${String(minutes).padStart(2, "0")}:${String(remaining).padStart(2, "0")}`
    : `${String(minutes).padStart(2, "0")}:${String(remaining).padStart(2, "0")}`
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GiB`
}

function clamp(value: number, minimum: number, maximum: number): number {
  return Math.max(minimum, Math.min(maximum, value))
}
