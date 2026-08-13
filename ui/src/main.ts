import { CliRenderer, createCliRenderer } from "@opentui/core"

import { consumeBootstrap } from "./bootstrap.ts"
import { SessionClient } from "./client.ts"
import type { Event, OperationState } from "./protocol/types.gen.ts"
import { FoundationView, type FoundationState } from "./view.ts"

let startupStage = "bootstrap"

async function main(): Promise<void> {
  const bootstrap = await consumeBootstrap(process.argv)
  startupStage = "health check"
  const client = new SessionClient(bootstrap)
  const health = await client.health()
  startupStage = "course catalog"
  const courses = await client.courses()
  startupStage = "diagnostics"
  const diagnostics = await client.diagnostics()
  const state: FoundationState = {
    activeCourse: undefined,
    artifacts: [],
    courses: courses.courses,
    diagnostics: diagnostics.diagnostics,
    error: undefined,
    lectures: [],
    loading: false,
    operation: undefined,
    screen: "courses",
    selectedCourse: 0,
    selectedItem: 0,
    status: health.status === "ok" ? "Connected" : "Unavailable",
  }
  if (process.argv.includes("--noninteractive-self-test")) {
    startupStage = "self-test"
    await runNonInteractiveSelfTest(client, state)
    return
  }
  startupStage = "interactive renderer"
  await runInteractive(client, state)
}

async function runInteractive(client: SessionClient, initialState: FoundationState): Promise<void> {
  const quit = Promise.withResolvers<void>()
  const renderer = await createCliRenderer({
    backgroundColor: "#0b1015",
    consoleMode: "disabled",
    exitOnCtrlC: false,
    maxFps: 30,
    onDestroy: () => quit.resolve(),
    openConsoleOnError: false,
    screenMode: "alternate-screen",
    useMouse: false,
  })
  const eventsAbort = new AbortController()
  let state = initialState
  let destroyed = false
  const update = (next: FoundationState): void => {
    state = next
    view.update(next)
  }
  const view = new FoundationView(renderer, state, {
    onBack: () => update({ ...state, error: undefined, loading: false, screen: "courses", selectedItem: 0 }),
    onDiagnostics: () => {
      void loadDiagnostics(client, () => state, update, eventsAbort.signal)
    },
    onLibrary: () => {
      void loadLibrary(client, () => state, update, eventsAbort.signal)
    },
    onOpenCourse: (course) => {
      void loadLectures(client, course, () => state, update, eventsAbort.signal)
    },
    onQuit: () => quit.resolve(),
    onRetry: () => {
      void retryScreen(client, () => state, update, eventsAbort.signal)
    },
    onSelfTest: () => {
      void startSelfTest(client, state, update, eventsAbort.signal)
    },
  })
  const eventTask = consumeEvents(client, () => state, update, eventsAbort.signal)
  try {
    await Promise.race([
      quit.promise,
      eventTask.then(() => {
        if (!eventsAbort.signal.aborted) {
          throw new Error("UI session event stream closed")
        }
      }),
    ])
  } finally {
    eventsAbort.abort()
    view.destroy()
    if (!destroyed && !renderer.isDestroyed) {
      destroyed = true
      renderer.destroy()
    }
    await eventTask.catch(() => undefined)
  }
}

async function runNonInteractiveSelfTest(client: SessionClient, state: FoundationState): Promise<void> {
  const eventsAbort = new AbortController()
  const events = client.events(eventsAbort.signal)[Symbol.asyncIterator]()
  let terminal: OperationState | undefined
  try {
    const ready = await events.next()
    if (ready.done || ready.value.type !== "session.ready") {
      throw new Error("UI session event stream is not ready")
    }
    const operation = await client.startSelfTest()
    while (true) {
      const next = await events.next()
      if (next.done) break
      const event = next.value
      if (event.operationId !== operation.id) continue
      if (event.state === "completed" || event.state === "canceled" || event.state === "failed") {
        terminal = event.state
        break
      }
    }
    if (terminal !== "completed") {
      throw new Error("UI session self-test failed")
    }
    await renderNonInteractiveResult(state, operation.id, terminal)
  } finally {
    eventsAbort.abort()
    await events.return?.(undefined).catch(() => undefined)
  }
  process.stdout.write(JSON.stringify({ courses: state.courses.length, status: "ok" }) + "\n")
}

async function renderNonInteractiveResult(state: FoundationState, operationID: string, terminal: OperationState): Promise<void> {
  const renderer = new CliRenderer(process.stdin, process.stdout, 80, 24, {
    bufferedOutput: "memory",
    consoleMode: "disabled",
    exitSignals: [],
    screenMode: "main-screen",
    useKittyKeyboard: null,
    useMouse: false,
  })
  const view = new FoundationView(renderer, {
    ...state,
    operation: { id: operationID, percent: 100, state: terminal },
  }, {
    onBack() {},
    onDiagnostics() {},
    onLibrary() {},
    onOpenCourse() {},
    onQuit() {},
    onRetry() {},
    onSelfTest() {},
  })
  try {
    renderer.requestRender()
    await renderer.idle()
  } finally {
    view.destroy()
    renderer.destroy()
  }
}

async function loadLectures(
  client: SessionClient,
  course: FoundationState["activeCourse"] & {},
  current: () => FoundationState,
  update: (state: FoundationState) => void,
  signal: AbortSignal,
): Promise<void> {
  update({ ...current(), activeCourse: course, error: undefined, loading: true, screen: "lectures", selectedItem: 0 })
  try {
    const result = await client.lectures(course, signal)
    if (signal.aborted) return
    update({ ...current(), lectures: result.lectures, loading: false, selectedItem: 0 })
  } catch {
    if (!signal.aborted) update({ ...current(), error: "Lecture catalog is unavailable", loading: false })
  }
}

async function loadLibrary(
  client: SessionClient,
  current: () => FoundationState,
  update: (state: FoundationState) => void,
  signal: AbortSignal,
): Promise<void> {
  update({ ...current(), error: undefined, loading: true, screen: "library", selectedItem: 0 })
  try {
    const result = await client.artifacts(signal)
    if (signal.aborted) return
    update({ ...current(), artifacts: result.artifacts, loading: false, selectedItem: 0 })
  } catch {
    if (!signal.aborted) update({ ...current(), error: "Local lecture library is unavailable", loading: false })
  }
}

async function loadDiagnostics(
  client: SessionClient,
  current: () => FoundationState,
  update: (state: FoundationState) => void,
  signal: AbortSignal,
): Promise<void> {
  update({ ...current(), error: undefined, loading: true, screen: "diagnostics", selectedItem: 0 })
  try {
    const result = await client.diagnostics(signal)
    if (signal.aborted) return
    update({ ...current(), diagnostics: result.diagnostics, loading: false, selectedItem: 0 })
  } catch {
    if (!signal.aborted) update({ ...current(), error: "Diagnostics are unavailable", loading: false })
  }
}

async function loadCourses(
  client: SessionClient,
  current: () => FoundationState,
  update: (state: FoundationState) => void,
  signal: AbortSignal,
): Promise<void> {
  update({ ...current(), error: undefined, loading: true, screen: "courses" })
  try {
    const result = await client.courses(signal)
    if (signal.aborted) return
    update({ ...current(), courses: result.courses, loading: false, selectedCourse: 0 })
  } catch {
    if (!signal.aborted) update({ ...current(), error: "Course catalog is unavailable", loading: false })
  }
}

async function retryScreen(
  client: SessionClient,
  current: () => FoundationState,
  update: (state: FoundationState) => void,
  signal: AbortSignal,
): Promise<void> {
  const state = current()
  if (state.screen === "lectures" && state.activeCourse !== undefined) {
    await loadLectures(client, state.activeCourse, current, update, signal)
  } else if (state.screen === "library") {
    await loadLibrary(client, current, update, signal)
  } else if (state.screen === "diagnostics") {
    await loadDiagnostics(client, current, update, signal)
  } else {
    await loadCourses(client, current, update, signal)
  }
}

async function startSelfTest(
  client: SessionClient,
  current: FoundationState,
  update: (state: FoundationState) => void,
  signal: AbortSignal,
): Promise<void> {
  if (current.operation?.state === "running") return
  try {
    const operation = await client.startSelfTest(signal)
    update({
      ...current,
      operation: { id: operation.id, percent: 0, state: operation.state },
    })
  } catch {
    update({ ...current, status: "Connection failed" })
  }
}

async function consumeEvents(
  client: SessionClient,
  current: () => FoundationState,
  update: (state: FoundationState) => void,
  signal: AbortSignal,
): Promise<void> {
  for await (const event of client.events(signal)) {
    const state = applyEvent(current(), event)
    if (state !== undefined) {
      update(state)
    }
  }
}

function applyEvent(state: FoundationState, event: Event): FoundationState | undefined {
  if (event.type === "stream.overflow") {
    return { ...state, status: "Refresh required" }
  }
  const operation = state.operation
  if (operation === undefined || event.operationId !== operation.id) {
    return undefined
  }
  return {
    ...state,
    operation: {
      ...operation,
      percent: event.percent ?? operation.percent,
      state: event.state ?? operation.state,
    },
  }
}

await main().catch(() => {
  process.stderr.write(`impartus-ui: terminal frontend failed during ${startupStage}\n`)
  process.exitCode = 1
})
