import { afterEach, describe, expect, test } from "bun:test"
import { createTestRenderer, type TestRendererSetup } from "@opentui/core/testing"

import type { Course, Lecture, PlaybackCommand } from "../src/protocol/types.gen.ts"
import { courseRailLabels, FoundationView, lectureAudioLabel, type FoundationState } from "../src/view.ts"

const renderers: TestRendererSetup[] = []

afterEach(() => {
  for (const setup of renderers.splice(0)) {
    setup.renderer.destroy()
  }
})

describe("FoundationView", () => {
  test("uses distinguishing subject labels for courses with shared cohort prefixes", () => {
    const courses: Course[] = [
      course("BME V SEM 26 ODD_Biomaterials", 1530, 1),
      course("BME V SEM 26 ODD_Medical Devices", 1530, 2),
      course("BME V SEM 26 ODD_Basic Clinical Sciences –II (Neurology)", 1530, 3),
      course("BME V SEM 26 ODD_Basic Clinical Sciences –II (Radiology)", 1530, 4),
      course("PHY N FY 24 EVEN\u00a0 ENGINEERING CHEMISTRY", 1450, 5),
      course("PHY N FY 24 EVEN\u00a0 ENGINEERING GRAPHICS-II 312", 1450, 6),
      course("PHY N FY 24 EVEN\u00a0 ENVIRONMENTAL STUDIES", 1450, 7),
    ]

    const labels = courseRailLabels(courses)
    expect(labels.get(courses[0]!)).toBe("Biomaterials")
    expect(labels.get(courses[1]!)).toBe("Medical Devices")
    expect(labels.get(courses[2]!)).toBe("Basic Clinic…(Neurology)")
    expect(labels.get(courses[3]!)).toBe("Basic Clinic…(Radiology)")
    expect(labels.get(courses[4]!)).toBe("ENGINEERING CHEMISTRY")
    expect(labels.get(courses[5]!)).toBe("ENGINEERING …HICS-II 312")
  })

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

  test("keeps a long-list selection visible within panel row gaps", async () => {
    const setup = await createTestRenderer({ height: 24, kittyKeyboard: true, width: 80 })
    renderers.push(setup)
    const state = foundationState()
    const courses = Array.from({ length: 100 }, (_, index) => course(`Course ${String(index).padStart(3, "0")}`, 2000 + index, index + 1))
    const view = new FoundationView(setup.renderer, {
      ...state,
      collections: { ...state.collections, courses: { filter: "", selected: 30 } },
      courses,
    }, callbacks())

    await setup.renderOnce()
    expect(setup.captureCharFrame()).toContain("> Course 030")
    view.destroy()
  })

  test("moves and activates the focused wide navigation pane", async () => {
    const setup = await createTestRenderer({ height: 32, kittyKeyboard: true, width: 140 })
    renderers.push(setup)
    let diagnosticsCount = 0
    const view = new FoundationView(setup.renderer, foundationState(), {
      ...callbacks(),
      onDiagnostics() { diagnosticsCount++ },
    })

    setup.mockInput.pressKey("g")
    setup.mockInput.pressArrow("down")
    setup.mockInput.pressArrow("down")
    setup.mockInput.pressEnter()

    expect(diagnosticsCount).toBe(1)
    view.destroy()
  })

  test("renders the upstream microphone status in lecture rows and inspector", async () => {
    const setup = await createTestRenderer({ height: 18, width: 100 })
    renderers.push(setup)
    const state = foundationState()
    const lectures = [lecture(1, "Audio lecture", false), lecture(2, "Visual-only lecture", true)]
    const view = new FoundationView(setup.renderer, {
      ...state,
      collections: { ...state.collections, lectures: { filter: "", selected: 0 } },
      lectures,
      screen: "lectures",
    }, callbacks())

    await setup.renderOnce()
    const frame = setup.captureCharFrame()
    expect(frame).toContain("Audio lecture")
    expect(frame).toContain("Visual-only lecture")
    expect(frame).toContain(lectureAudioLabel(false))
    expect(frame).toContain(lectureAudioLabel(true))
    const audioRow = frame.split("\n").find((line) => line.includes("Audio lecture"))
    const visualOnlyRow = frame.split("\n").find((line) => line.includes("Visual-only lecture"))
    expect(audioRow).not.toContain(lectureAudioLabel(false))
    expect(visualOnlyRow).toContain(lectureAudioLabel(true))
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
      onCollectionState() {},
      onCourses() {},
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
    expect(setup.captureCharFrame()).toContain("Open command palette")

    setup.mockInput.pressBackspace()
    await setup.renderOnce()
    expect(setup.captureCharFrame()).not.toContain("Command guide")

    setup.mockInput.pressKey("g")
    await setup.renderOnce()
    expect(setup.captureCharFrame()).toContain("Navigation")
    setup.mockInput.pressBackspace()
    await setup.renderOnce()
    expect(setup.captureCharFrame()).not.toContain("Navigation")

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
    let state = foundationState()
    let view: FoundationView
    view = new FoundationView(setup.renderer, state, {
      ...callbacks(),
      onCollectionState(screen, collection) {
        state = { ...state, collections: { ...state.collections, [screen]: collection } }
        view.update(state)
      },
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
    await setup.renderOnce()
    expect(setup.captureCharFrame()).toContain("Filter: compiler")
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
    expect(frame).not.toContain("q Quit")

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

  test("renders terminal playback state without live controls", async () => {
    const setup = await createTestRenderer({ height: 24, kittyKeyboard: true, width: 80 })
    renderers.push(setup)
    const state = foundationState()
    const activeLecture = lecture(1, "Finished lecture", false)
    const view = new FoundationView(setup.renderer, {
      ...state,
      activeLecture,
      operation: {
        durationSeconds: 60,
        id: "playback-id",
        kind: "playback",
        muted: false,
        paused: false,
        percent: 100,
        positionSeconds: 60,
        speed: 1,
        state: "completed",
        volume: 100,
      },
      screen: "playback",
    }, callbacks())

    await setup.renderOnce()
    const frame = setup.captureCharFrame()
    expect(frame).toContain("Playback completed")
    expect(frame).not.toContain("Playing in mpv")
    expect(frame).not.toContain("space pause")
    expect(frame).toContain("Press esc to return")
    view.destroy()
  })

  test("renders playback startup instead of a false or stale terminal state", async () => {
    const setup = await createTestRenderer({ height: 24, kittyKeyboard: true, width: 80 })
    renderers.push(setup)
    const state = foundationState()
    const activeLecture = lecture(2, "Starting lecture", false)
    const view = new FoundationView(setup.renderer, {
      ...state,
      activeLecture,
      loading: true,
      operation: {
        durationSeconds: 60,
        id: "prior-playback-id",
        kind: "playback",
        muted: false,
        paused: false,
        percent: 100,
        positionSeconds: 60,
        speed: 1,
        state: "completed",
        volume: 100,
      },
      pending: undefined,
      screen: "playback",
    }, callbacks())

    await setup.renderOnce()
    const frame = setup.captureCharFrame()
    expect(frame).toContain("Starting playback")
    expect(frame).toContain("Starting lecture")
    expect(frame).not.toContain("Playback is unavailable")
    expect(frame).not.toContain("Playback completed")
    view.destroy()
  })

  test("keeps collections visible while an operation start response is pending", async () => {
    const setup = await createTestRenderer({ height: 24, kittyKeyboard: true, width: 80 })
    renderers.push(setup)
    const state = foundationState()
    const view = new FoundationView(setup.renderer, {
      ...state,
      lectures: [lecture(1, "Visible lecture", false)],
      loading: true,
      pending: undefined,
      screen: "lectures",
    }, callbacks())

    await setup.renderOnce()
    const frame = setup.captureCharFrame()
    expect(frame).toContain("Visible lecture")
    expect(frame).not.toContain("Loading current workspace")
    view.destroy()
  })

  test("distinguishes filtered-empty library and diagnostics from empty collections", async () => {
    const setup = await createTestRenderer({ height: 24, kittyKeyboard: true, width: 80 })
    renderers.push(setup)
    const state = foundationState()
    const view = new FoundationView(setup.renderer, {
      ...state,
      artifacts: [{ artifactId: "artifact-1", fileCount: 1, presentFileCount: 1, producedAt: "2026-08-22T00:00:00Z", sequence: 1, topic: "Downloaded lecture", totalBytes: 10 }],
      collections: { ...state.collections, library: { filter: "no-match", selected: 0 } },
      screen: "library",
    }, callbacks())

    await setup.renderOnce()
    let frame = setup.captureCharFrame()
    expect(frame).toContain("No matching downloaded lectures")
    expect(frame).not.toContain("No downloaded lectures yet")

    view.update({
      ...state,
      collections: { ...state.collections, diagnostics: { filter: "no-match", selected: 0 } },
      diagnostics: [{ detail: "available", name: "mpv", status: "ok" }],
      screen: "diagnostics",
    })
    await setup.renderOnce()
    frame = setup.captureCharFrame()
    expect(frame).toContain("No matching diagnostics")
    expect(frame).not.toContain("No diagnostics reported")
    view.destroy()
  })

  test("does not advertise disabled filter controls in an errored active-filter footer", async () => {
    const setup = await createTestRenderer({ height: 24, kittyKeyboard: true, width: 80 })
    renderers.push(setup)
    const state = foundationState()
    const view = new FoundationView(setup.renderer, {
      ...state,
      collections: { ...state.collections, courses: { filter: "compiler", selected: 0 } },
      error: "Course catalog is unavailable",
    }, callbacks())

    await setup.renderOnce()
    const frame = setup.captureCharFrame()
    expect(frame).toContain("Filter: compiler")
    expect(frame).not.toContain("/ edit")
    expect(frame).not.toContain("navigate")
    view.destroy()
  })

  test("edits the command palette with vim letters and dispatches its match", async () => {
    const setup = await createTestRenderer({ height: 24, kittyKeyboard: true, width: 80 })
    renderers.push(setup)
    let libraryCount = 0
    const view = new FoundationView(setup.renderer, foundationState(), {
      ...callbacks(),
      onLibrary() { libraryCount++ },
    })

    setup.mockInput.pressKey("p", { ctrl: true })
    await setup.mockInput.typeText("junk")
    await setup.renderOnce()
    expect(setup.captureCharFrame()).toContain("> junk█")
    setup.mockInput.pressBackspace()
    await setup.renderOnce()
    expect(setup.captureCharFrame()).toContain("> jun█")

    setup.mockInput.pressEscape()
    setup.mockInput.pressKey("p", { ctrl: true })
    await setup.mockInput.typeText("library")
    await setup.renderOnce()
    expect(setup.captureCharFrame()).toContain("Open library")
    setup.mockInput.pressEnter()
    expect(libraryCount).toBe(1)
    view.destroy()
  })

  test("scrolls the palette cursor instead of activating an off-screen command", async () => {
    const setup = await createTestRenderer({ height: 12, kittyKeyboard: true, width: 40 })
    renderers.push(setup)
    let diagnosticsCount = 0
    const view = new FoundationView(setup.renderer, foundationState(), {
      ...callbacks(),
      onDiagnostics() { diagnosticsCount++ },
    })

    setup.mockInput.pressKey("p", { ctrl: true })
    for (let index = 0; index < 6; index++) setup.mockInput.pressArrow("down")
    await setup.renderOnce()
    expect(setup.captureCharFrame()).toContain("> Open diagnostics")
    setup.mockInput.pressEnter()
    expect(diagnosticsCount).toBe(1)
    view.destroy()
  })

  test("restores pane focus after help and blocks pending palette navigation", async () => {
    const setup = await createTestRenderer({ height: 32, kittyKeyboard: true, width: 140 })
    renderers.push(setup)
    let libraryCount = 0
    const state = foundationState()
    const view = new FoundationView(setup.renderer, state, {
      ...callbacks(),
      onLibrary() { libraryCount++ },
    })

    setup.mockInput.pressTab()
    await setup.renderOnce()
    expect(setup.captureCharFrame()).toContain("[ACTIVE] Inspector")
    setup.mockInput.pressKey("?")
    setup.mockInput.pressEscape()
    await setup.renderOnce()
    expect(setup.captureCharFrame()).toContain("[ACTIVE] Inspector")

    view.update({ ...state, loading: true })
    setup.mockInput.pressKey("p", { ctrl: true })
    await setup.mockInput.typeText("library")
    await setup.renderOnce()
    expect(setup.captureCharFrame()).toContain("A request is pending")
    setup.mockInput.pressEnter()
    expect(libraryCount).toBe(0)
    view.destroy()
  })

  test("does not advertise retry on an unretryable playback-start error", async () => {
    const setup = await createTestRenderer({ height: 24, kittyKeyboard: true, width: 80 })
    renderers.push(setup)
    const state = foundationState()
    const view = new FoundationView(setup.renderer, {
      ...state,
      error: "Lecture playback could not start",
      screen: "playback",
    }, callbacks())

    await setup.renderOnce()
    const frame = setup.captureCharFrame()
    expect(frame).toContain("Press esc to return.")
    expect(frame).not.toContain("Press r to retry")
    view.destroy()
  })

  test("collapses fixed chrome when no workspace row fits", async () => {
    const setup = await createTestRenderer({ height: 5, kittyKeyboard: true, width: 40 })
    renderers.push(setup)
    const view = new FoundationView(setup.renderer, foundationState(), callbacks())

    await setup.renderOnce()
    const frame = setup.captureCharFrame()
    expect(frame).not.toContain("selection]")
    expect(frame).toContain("q quit")
    expect(frame.trimEnd().split("\n")).toHaveLength(5)
    view.destroy()
  })
})

function course(subjectName: string, sessionId: number, subjectId: number): Course {
  return {
    instituteId: 1207,
    professorName: "Professor",
    sessionId,
    sessionName: "Session",
    subjectId,
    subjectName,
    videoCount: 1,
  }
}

function lecture(sequence: number, topic: string, noAudio: boolean): Lecture {
  return {
    classroomName: "Room 7",
    durationSeconds: 3600,
    instituteId: 1207,
    noAudio,
    professorName: "Professor",
    sequence,
    sessionId: 1530,
    sessionName: "July - Dec 2026",
    startTime: "2026-08-21T10:00:00Z",
    subjectId: 3186296,
    subjectName: "Medical Devices",
    topic,
    ttid: 10913000 + sequence,
    views: 1,
  }
}

function foundationState(): FoundationState {
  return {
    activeCourse: undefined,
    activeLecture: undefined,
    artifacts: [],
    collections: {
      courses: { filter: "", selected: 0 },
      diagnostics: { filter: "", selected: 0 },
      lectures: { filter: "", selected: 0 },
      library: { filter: "", selected: 0 },
    },
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
    pending: undefined,
    screen: "courses",
    status: "Connected",
  }
}

function callbacks() {
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
