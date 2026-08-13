import { CliRenderer, createCliRenderer } from "@opentui/core"

import { consumeBootstrap } from "./bootstrap.ts"
import { SessionClient } from "./client.ts"
import type { Event, OperationState } from "./protocol/types.gen.ts"
import { FoundationView, type FoundationState } from "./view.ts"

async function main(): Promise<void> {
  const bootstrap = await consumeBootstrap(process.argv)
  const client = new SessionClient(bootstrap)
  const health = await client.health()
  const courses = await client.courses()
  const state: FoundationState = {
    courses: courses.courses,
    operation: undefined,
    selectedCourse: 0,
    status: health.status === "ok" ? "Connected" : "Unavailable",
  }
  if (process.argv.includes("--noninteractive-self-test")) {
    await runNonInteractiveSelfTest(client, state)
    return
  }
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
    onQuit: () => quit.resolve(),
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
  }, { onQuit() {}, onSelfTest() {} })
  try {
    renderer.requestRender()
    await renderer.idle()
  } finally {
    view.destroy()
    renderer.destroy()
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
  process.stderr.write("impartus-ui: terminal frontend failed\n")
  process.exitCode = 1
})
