import { afterEach, describe, expect, test } from "bun:test"
import { createTestRenderer, type TestRendererSetup } from "@opentui/core/testing"

import type { PlaybackCommand } from "../src/protocol/types.gen.ts"
import { FoundationView, type FoundationState } from "../src/view.ts"

const renderers: TestRendererSetup[] = []

afterEach(() => {
  for (const setup of renderers.splice(0)) {
    setup.renderer.destroy()
  }
})

describe("FoundationView", () => {
  test("renders a modern three-region shell on wide terminals", async () => {
    const setup = await createTestRenderer({ height: 40, width: 140 })
    renderers.push(setup)
    const view = new FoundationView(setup.renderer, foundationState(), callbacks())

    await setup.renderOnce()
    const frame = setup.captureCharFrame()

    expect(frame).toContain("IMPARTUS")
    expect(frame).toContain("Courses")
    expect(frame).toContain("Learning workspace")
    expect(frame).toContain("Inspector")
    expect(frame).toContain("Distributed Systems")
    view.destroy()
  })

  test("uses one routed pane on narrow terminals and keeps commands discoverable", async () => {
    const setup = await createTestRenderer({ height: 12, kittyKeyboard: true, width: 40 })
    renderers.push(setup)
    let quitCount = 0
    let selfTestCount = 0
    let openCount = 0
    const view = new FoundationView(setup.renderer, foundationState(), {
      onBack() {},
      onDiagnostics() {},
      onDownload() {},
      onLibrary() {},
      onOpenCourse() {
        openCount++
      },
      onPlay() {},
      onPlaybackCommand() {},
      onQuit() {
        quitCount++
      },
      onRetry() {},
      onSelfTest() {
        selfTestCount++
      },
    })

    await setup.renderOnce()
    expect(setup.captureCharFrame()).toContain("Distributed Systems")
    expect(setup.captureCharFrame()).not.toContain("Inspector")

    setup.mockInput.pressKey("?")
    await setup.renderOnce()
    expect(setup.captureCharFrame()).toContain("Command guide")
    expect(setup.captureCharFrame()).toContain("download selected")

    setup.mockInput.pressEscape()
    await setup.renderOnce()
    setup.mockInput.pressEnter()
    setup.mockInput.pressKey("s")
    setup.mockInput.pressKey("q")
    setup.mockInput.pressCtrlC()
    expect(selfTestCount).toBe(1)
    expect(quitCount).toBe(2)
    expect(openCount).toBe(1)
    view.destroy()
  })

  test("filters the live catalog without rendering hidden rows", async () => {
    const setup = await createTestRenderer({ height: 16, kittyKeyboard: true, width: 60 })
    renderers.push(setup)
    let opened = ""
    const view = new FoundationView(setup.renderer, foundationState(), {
      ...callbacks(),
      onOpenCourse(course) {
        opened = course.subjectName
      },
    })

    setup.mockInput.pressKey("/")
    await setup.mockInput.typeText("compiler")
    await setup.renderOnce()
    const frame = setup.captureCharFrame()
    expect(frame).toContain("Filter: compiler")
    expect(frame).toContain("Compilers")
    expect(frame).not.toContain("Distributed Systems")

    setup.mockInput.pressEnter()
    setup.mockInput.pressEnter()
    expect(opened).toBe("Compilers")
    view.destroy()
  })

  test("renders playback telemetry and routes mpv controls", async () => {
    const setup = await createTestRenderer({ height: 24, kittyKeyboard: true, width: 80 })
    renderers.push(setup)
    const commands: PlaybackCommand[] = []
    const state = foundationState()
    const lecture = {
      classroomName: "Room 7",
      durationSeconds: 3600,
      instituteId: 1,
      noAudio: false,
      professorName: "Dr. Rao",
      sequence: 4,
      sessionId: 2,
      sessionName: "Monsoon 2026",
      startTime: "2026-08-13T10:00:00Z",
      subjectId: 3,
      subjectName: "Distributed Systems",
      topic: "Consensus",
      ttid: 5,
      views: 2,
    }
    const view = new FoundationView(setup.renderer, {
      ...state,
      activeLecture: lecture,
      lectures: [lecture],
      operation: {
        durationSeconds: 3600,
        id: "playback-id",
        kind: "playback",
        muted: false,
        paused: false,
        percent: 25,
        positionSeconds: 900,
        speed: 1,
        state: "running",
        volume: 100,
      },
      screen: "playback",
    }, {
      ...callbacks(),
      onPlaybackCommand(command) {
        commands.push(command)
      },
    })

    await setup.renderOnce()
    const frame = setup.captureCharFrame()
    expect(frame).toContain("Now playing")
    expect(frame).toContain("15:00 / 01:00:00")
    expect(frame).toContain("volume 100%")

    await setup.mockInput.typeText(" ")
    setup.mockInput.pressArrow("right")
    setup.mockInput.pressKey("m")
    setup.mockInput.pressKey("+")
    setup.mockInput.pressKey("]")
    setup.mockInput.pressKey("v")
    expect(commands).toEqual([
      { action: "pause", flag: true },
      { action: "seek", value: 10 },
      { action: "mute", flag: true },
      { action: "volume", value: 105 },
      { action: "speed", value: 1.25 },
      { action: "cycleVideo" },
    ])
    view.destroy()
  })
})

function foundationState(): FoundationState {
  return {
    activeCourse: undefined,
    activeLecture: undefined,
    artifacts: [],
    courses: [
      {
        instituteId: 1,
        professorName: "Dr. Rao",
        sessionId: 2,
        sessionName: "Monsoon 2026",
        subjectId: 3,
        subjectName: "Distributed Systems",
        videoCount: 12,
      },
      {
        instituteId: 1,
        professorName: "Dr. Sen",
        sessionId: 2,
        sessionName: "Monsoon 2026",
        subjectId: 4,
        subjectName: "Compilers",
        videoCount: 8,
      },
    ],
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
}

function callbacks() {
  return {
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
  }
}
