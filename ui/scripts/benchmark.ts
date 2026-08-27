import { createTestRenderer } from "@opentui/core/testing"

import { FoundationView, type FoundationState } from "../src/view.ts"
import { createFoundationState, newOperation } from "../src/workspace_controller.ts"

const courseCount = 5_000
const iterations = 200
const courses = Array.from({ length: courseCount }, (_, index) => ({
  instituteId: 1,
  professorName: `Professor ${index}`,
  sessionId: 2,
  sessionName: "Monsoon 2026",
  subjectId: index + 1,
  subjectName: `Course ${String(index + 1).padStart(4, "0")}`,
  videoCount: 24,
}))

const baseline = createFoundationState({ courses })
const activity = createFoundationState({
  courses,
  operation: { ...newOperation("benchmark-operation", "download", "running"), percent: 42 },
  status: "Downloading lecture",
})

const results = []
results.push(await benchmarkCase("medium-catalog", 80, 24, baseline))
results.push(await benchmarkCase("wide-catalog", 140, 32, baseline))
results.push(await benchmarkCase("wide-activity", 140, 32, activity))
process.stdout.write(JSON.stringify({ courseCount, iterations, results }) + "\n")

async function benchmarkCase(label: string, width: number, height: number, initial: FoundationState) {
  const setup = await createTestRenderer({ height, kittyKeyboard: true, width })
  let state = initial
  let view: FoundationView
  const started = performance.now()
  view = new FoundationView(setup.renderer, state, {
    onBack() {},
    onBlockedCommand() {},
    onCollectionState(screen, collection) {
      state = { ...state, collections: { ...state.collections, [screen]: collection } }
      view.update(state)
    },
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
  })
  await setup.renderOnce()
  const coldFrameMilliseconds = performance.now() - started
  const frameDurations: number[] = []
  for (let iteration = 0; iteration < iterations; iteration++) {
    const frameStarted = performance.now()
    setup.mockInput.pressKey("down")
    await setup.renderOnce()
    frameDurations.push(performance.now() - frameStarted)
  }
  frameDurations.sort((left, right) => left - right)
  const result = {
    coldFrameMilliseconds: round(coldFrameMilliseconds),
    height,
    label,
    p50InputToFrameMilliseconds: round(percentile(frameDurations, 0.5)),
    p95InputToFrameMilliseconds: round(percentile(frameDurations, 0.95)),
    p99InputToFrameMilliseconds: round(percentile(frameDurations, 0.99)),
    residentSetBytes: process.memoryUsage().rss,
    width,
  }
  view.destroy()
  setup.renderer.destroy()
  return result
}

function percentile(values: readonly number[], fraction: number): number {
  return values[Math.min(values.length - 1, Math.floor(values.length * fraction))] ?? 0
}

function round(value: number): number {
  return Math.round(value * 100) / 100
}
