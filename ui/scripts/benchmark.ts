import { createTestRenderer } from "@opentui/core/testing"

import { FoundationView, type FoundationState } from "../src/view.ts"

const courseCount = 5_000
const iterations = 200
const state: FoundationState = {
  activeCourse: undefined,
  activeLecture: undefined,
  artifacts: [],
  courses: Array.from({ length: courseCount }, (_, index) => ({
    instituteId: 1,
    professorName: `Professor ${index}`,
    sessionId: 2,
    sessionName: "Monsoon 2026",
    subjectId: index + 1,
    subjectName: `Course ${String(index + 1).padStart(4, "0")}`,
    videoCount: 24,
  })),
  diagnostics: [],
  error: undefined,
  lectures: [],
  loading: false,
  operation: undefined,
  screen: "courses",
  selectedCourse: 0,
  selectedItem: 0,
  status: "Connected",
}

const setup = await createTestRenderer({ height: 40, kittyKeyboard: true, width: 140 })
const started = performance.now()
const view = new FoundationView(setup.renderer, state, {
  onBack() {},
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
  courseCount,
  iterations,
  p50InputToFrameMilliseconds: round(percentile(frameDurations, 0.5)),
  p95InputToFrameMilliseconds: round(percentile(frameDurations, 0.95)),
  p99InputToFrameMilliseconds: round(percentile(frameDurations, 0.99)),
  residentSetBytes: process.memoryUsage().rss,
}

view.destroy()
setup.renderer.destroy()
process.stdout.write(JSON.stringify(result) + "\n")

function percentile(values: readonly number[], fraction: number): number {
  return values[Math.min(values.length - 1, Math.floor(values.length * fraction))] ?? 0
}

function round(value: number): number {
  return Math.round(value * 100) / 100
}
