import {
  BoxRenderable,
  CliRenderEvents,
  TextAttributes,
  TextRenderable,
  type CliRenderer,
  type KeyEvent,
} from "@opentui/core"

import type { Course, OperationState } from "./protocol/types.gen.ts"

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
}

const WIDE_WIDTH = 120
const MEDIUM_WIDTH = 72

export interface FoundationOperation {
  id: string
  percent: number
  state: OperationState
}

export interface FoundationState {
  courses: Course[]
  operation: FoundationOperation | undefined
  selectedCourse: number
  status: string
}

export interface FoundationCallbacks {
  onQuit(): void
  onSelfTest(): void
}

interface Command {
  description: string
  key: string
}

const COMMANDS: readonly Command[] = [
  { key: "↑/k ↓/j", description: "move selection" },
  { key: "s", description: "run connection test" },
  { key: "?", description: "toggle keyboard help" },
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
    if ((sequence === "s" || name === "s") && this.#state.operation?.state !== "running") {
      this.#callbacks.onSelfTest()
      return
    }
    const direction = name === "up" || sequence === "k" ? -1 : name === "down" || sequence === "j" ? 1 : 0
    if (direction !== 0 && this.#state.courses.length > 0) {
      this.#state = {
        ...this.#state,
        selectedCourse: clamp(this.#state.selectedCourse + direction, 0, this.#state.courses.length - 1),
      }
      this.#rebuild()
    }
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
    if (width >= WIDE_WIDTH) {
      shell.add(this.#wideBody(height))
    } else if (width >= MEDIUM_WIDTH) {
      shell.add(this.#mediumBody(height))
    } else {
      shell.add(this.#narrowBody(height))
    }
    shell.add(this.#footer())
    if (this.#helpVisible) {
      shell.add(this.#helpOverlay(width, height))
    }
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
    header.add(
      new TextRenderable(this.#renderer, {
        attributes: TextAttributes.BOLD,
        content: "IMPARTUS  /  learning workspace",
        fg: COLORS.foreground,
        height: 1,
      }),
    )
    header.add(
      new TextRenderable(this.#renderer, {
        content: `● ${this.#state.status}`,
        fg: this.#state.status === "Connected" ? COLORS.success : COLORS.danger,
        height: 1,
      }),
    )
    return header
  }

  #wideBody(height: number): BoxRenderable {
    const body = bodyBox(this.#renderer)
    const courses = this.#coursePanel(28, height)
    courses.flexShrink = 0
    body.add(courses)
    body.add(this.#workspacePanel(height))
    const activity = this.#activityPanel(32)
    activity.flexShrink = 0
    body.add(activity)
    return body
  }

  #mediumBody(height: number): BoxRenderable {
    const body = bodyBox(this.#renderer)
    body.add(this.#coursePanel("58%", height))
    body.add(this.#activityPanel("42%"))
    return body
  }

  #narrowBody(height: number): BoxRenderable {
    const body = bodyBox(this.#renderer)
    body.add(this.#coursePanel("100%", height))
    return body
  }

  #coursePanel(width: number | "auto" | `${number}%`, terminalHeight: number): BoxRenderable {
    const panel = panelBox(this.#renderer, "Courses", width)
    const availableRows = Math.max(1, terminalHeight - 9)
    const { courses, offset } = visibleCourses(this.#state.courses, this.#state.selectedCourse, availableRows)
    if (courses.length === 0) {
      panel.add(text(this.#renderer, "No courses available", COLORS.dim))
      return panel
    }
    courses.forEach((course, index) => {
      const absoluteIndex = offset + index
      const selected = absoluteIndex === this.#state.selectedCourse
      panel.add(
        new TextRenderable(this.#renderer, {
          attributes: selected ? TextAttributes.BOLD : TextAttributes.NONE,
          bg: selected ? COLORS.selected : COLORS.panel,
          content: `${selected ? "›" : " "} ${course.subjectName}`,
          fg: selected ? COLORS.accent : COLORS.foreground,
          height: 1,
          truncate: true,
          width: "100%",
        }),
      )
    })
    return panel
  }

  #workspacePanel(terminalHeight: number): BoxRenderable {
    const panel = panelBox(this.#renderer, "Workspace", "auto")
    panel.flexGrow = 1
    const selected = this.#state.courses[this.#state.selectedCourse]
    if (selected === undefined) {
      panel.add(text(this.#renderer, "Select a course to inspect its lecture workspace.", COLORS.dim))
      return panel
    }
    panel.add(text(this.#renderer, selected.subjectName, COLORS.foreground, TextAttributes.BOLD))
    panel.add(text(this.#renderer, `${selected.professorName}  ·  ${selected.sessionName}`, COLORS.dim))
    panel.add(text(this.#renderer, `${selected.videoCount} lectures`, COLORS.accent))
    panel.add(text(this.#renderer, "", COLORS.dim))
    panel.add(
      text(
        this.#renderer,
        terminalHeight < 18
          ? "Open a lecture to continue."
          : "Lecture browsing, playback, resume, downloads, and library state remain owned by the Go application service.",
        COLORS.dim,
      ),
    )
    return panel
  }

  #activityPanel(width: number | "auto" | `${number}%`): BoxRenderable {
    const panel = panelBox(this.#renderer, "Session", width)
    const operation = this.#state.operation
    if (operation === undefined) {
      panel.add(text(this.#renderer, "Connection ready", COLORS.success))
      panel.add(text(this.#renderer, "Press s to run the transport test", COLORS.dim))
      return panel
    }
    panel.add(text(this.#renderer, `Transport test  ${Math.round(operation.percent)}%`, COLORS.accent, TextAttributes.BOLD))
    panel.add(text(this.#renderer, operation.state, operation.state === "failed" ? COLORS.danger : COLORS.dim))
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
    footer.add(text(this.#renderer, "↑↓ navigate   s test   ? help", COLORS.dim))
    footer.add(text(this.#renderer, "q quit", COLORS.dim))
    return footer
  }

  #helpOverlay(width: number, height: number): BoxRenderable {
    const overlay = new BoxRenderable(this.#renderer, {
      backgroundColor: COLORS.panel,
      border: true,
      borderColor: COLORS.accent,
      borderStyle: "rounded",
      height: Math.min(9, Math.max(6, height - 2)),
      left: Math.max(1, Math.floor((width - Math.min(64, width - 2)) / 2)),
      padding: 1,
      position: "absolute",
      title: "Keyboard",
      top: 1,
      width: Math.min(64, width - 2),
      zIndex: 20,
    })
    for (const command of COMMANDS) {
      overlay.add(text(this.#renderer, `${command.key.padEnd(10)} ${command.description}`, COLORS.foreground))
    }
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
  return {
    ...state,
    courses: [...state.courses],
    selectedCourse: state.courses.length === 0 ? 0 : clamp(state.selectedCourse, 0, state.courses.length - 1),
  }
}

function visibleCourses(all: Course[], selected: number, count: number): { courses: Course[]; offset: number } {
  if (all.length <= count) {
    return { courses: all, offset: 0 }
  }
  const offset = clamp(selected - Math.floor(count / 2), 0, all.length - count)
  return { courses: all.slice(offset, offset + count), offset }
}

function clamp(value: number, minimum: number, maximum: number): number {
  return Math.min(maximum, Math.max(minimum, value))
}
