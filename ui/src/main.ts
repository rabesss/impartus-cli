import { CliRenderer, createCliRenderer } from "@opentui/core"

import { consumeBootstrap } from "./bootstrap.ts"
import { SessionClient } from "./client.ts"
import type { OperationState, PlaybackCommand } from "./protocol/types.gen.ts"
import { FoundationView } from "./view.ts"
import {
  WorkspaceController,
  createFoundationState,
  newOperation,
  type FoundationState,
} from "./workspace_controller.ts"

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
  const state = createFoundationState({
    courses: courses.courses,
    diagnostics: diagnostics.diagnostics,
    status: health.status === "ok" ? "Connected" : "Unavailable",
  })
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
  const controller = new WorkspaceController(client, initialState)
  let destroyed = false
  const current = (): FoundationState => controller.snapshot()
  const update = (state: FoundationState): void => controller.update(() => state)
  const view = new FoundationView(renderer, current(), {
    onBack: () => { void goBack(client, controller, eventsAbort.signal) },
    onCollectionState: (screen, state) => controller.setCollectionState(screen, state),
    onCourses: () => { controller.navigate("courses", undefined, eventsAbort.signal) },
    onDiagnostics: () => { controller.navigate("diagnostics", undefined, eventsAbort.signal) },
    onDownload: (lecture) => { void startDownload(client, lecture, controller, eventsAbort.signal) },
    onLibrary: () => { controller.navigate("library", undefined, eventsAbort.signal) },
    onOpenCourse: (course) => { controller.navigate("lectures", course, eventsAbort.signal) },
    onPlay: (lecture) => { void startPlayback(client, lecture, controller, eventsAbort.signal) },
    onPlaybackCommand: (command) => { void controlPlayback(client, command, current, update, eventsAbort.signal) },
    onQuit: () => quit.resolve(),
    onRetry: () => { void controller.retry(eventsAbort.signal) },
    onSelfTest: () => { void startSelfTest(client, controller, eventsAbort.signal) },
  })
  const unsubscribe = controller.subscribe((state) => view.update(state))
  const eventTask = consumeEvents(client, controller, eventsAbort.signal)
  try {
    await Promise.race([
      quit.promise,
      eventTask.then(() => {
        if (!eventsAbort.signal.aborted) throw new Error("UI session event stream closed")
      }),
    ])
  } finally {
    eventsAbort.abort()
    controller.abort()
    unsubscribe()
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
    if (ready.done || ready.value.type !== "session.ready") throw new Error("UI session event stream is not ready")
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
    if (terminal !== "completed") throw new Error("UI session self-test failed")
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
    operation: { ...newOperation(operationID, "selftest", terminal), percent: 100 },
  }, emptyCallbacks())
  try {
    renderer.requestRender()
    await renderer.idle()
  } finally {
    view.destroy()
    renderer.destroy()
  }
}

async function startPlayback(
  client: SessionClient,
  lecture: FoundationState["lectures"][number],
  controller: WorkspaceController,
  signal: AbortSignal,
): Promise<void> {
  const state = controller.snapshot()
  if (state.operation?.state === "running" || state.loading) return
  controller.update((current) => ({ ...current, activeLecture: lecture, error: undefined, loading: true, screen: "playback" }))
  try {
    const operation = await client.startPlayback(lecture, true, signal)
    if (signal.aborted) return
    controller.update((current) => ({
      ...current,
      loading: false,
      operation: newOperation(operation.id, operation.kind, operation.state),
      status: `Playing ${lecture.topic}`,
    }))
  } catch {
    if (!signal.aborted) controller.update((current) => ({ ...current, error: "Lecture playback could not start", loading: false }))
  }
}

async function startDownload(
  client: SessionClient,
  lecture: FoundationState["lectures"][number],
  controller: WorkspaceController,
  signal: AbortSignal,
): Promise<void> {
  const state = controller.snapshot()
  if (state.operation?.state === "running" || state.loading) return
  controller.update((current) => ({ ...current, error: undefined, loading: true }))
  try {
    const operation = await client.startDownload(lecture, signal)
    if (signal.aborted) return
    controller.update((current) => ({
      ...current,
      loading: false,
      operation: newOperation(operation.id, operation.kind, operation.state),
      status: `Downloading ${lecture.topic}`,
    }))
  } catch {
    if (!signal.aborted) controller.update((current) => ({ ...current, error: "Lecture download could not start", loading: false }))
  }
}

async function startSelfTest(client: SessionClient, controller: WorkspaceController, signal: AbortSignal): Promise<void> {
  const state = controller.snapshot()
  if (state.operation?.state === "running" || state.loading) return
  controller.update((current) => ({ ...current, loading: true }))
  try {
    const operation = await client.startSelfTest(signal)
    if (signal.aborted) return
    controller.update((current) => ({ ...current, loading: false, operation: newOperation(operation.id, operation.kind, operation.state) }))
  } catch {
    if (!signal.aborted) controller.update((current) => ({ ...current, loading: false, status: "Connection failed" }))
  }
}

async function controlPlayback(
  client: SessionClient,
  command: PlaybackCommand,
  current: () => FoundationState,
  update: (state: FoundationState) => void,
  signal: AbortSignal,
): Promise<void> {
  const operation = current().operation
  if (operation?.kind !== "playback" || operation.state !== "running") return
  try {
    await client.playbackCommand(operation.id, command, signal)
  } catch {
    if (!signal.aborted) update({ ...current(), status: "Playback control failed" })
  }
}

async function goBack(client: SessionClient, controller: WorkspaceController, signal: AbortSignal): Promise<void> {
  const state = controller.snapshot()
  if (state.screen === "playback") {
    if (state.operation?.kind === "playback" && state.operation.state === "running") {
      await client.cancelOperation(state.operation.id, signal).catch(() => undefined)
    }
    if (!signal.aborted) controller.update((current) => ({ ...current, error: undefined, loading: false, screen: "lectures" }))
  } else if (state.screen === "lectures") {
    controller.update((current) => ({ ...current, error: undefined, screen: "courses" }))
  } else {
    controller.update((current) => ({ ...current, error: undefined, screen: current.activeCourse === undefined ? "courses" : "lectures" }))
  }
}

async function consumeEvents(client: SessionClient, controller: WorkspaceController, signal: AbortSignal): Promise<void> {
  for await (const event of client.events(signal)) controller.applyEvent(event)
}

function emptyCallbacks() {
  return {
    onBack() {},
    onCollectionState() {},
    onCourses() {},
    onDiagnostics() {},
    onDownload() {},
    onLibrary() {},
    onOpenCourse() {},
    onPlay() {},
    onPlaybackCommand() {},
    onQuit() {},
    onRetry() {},
    onSelfTest() {},
  }
}

await main().catch(() => {
  process.stderr.write(`impartus-ui: terminal frontend failed during ${startupStage}\n`)
  process.exitCode = 1
})
