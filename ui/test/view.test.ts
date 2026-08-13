import { afterEach, describe, expect, test } from "bun:test"
import { createTestRenderer, type TestRendererSetup } from "@opentui/core/testing"

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
      onLibrary() {},
      onOpenCourse() {
        openCount++
      },
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
    expect(setup.captureCharFrame()).toContain("open diagnostics")

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
})

function foundationState(): FoundationState {
  return {
    activeCourse: undefined,
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
    onLibrary() {},
    onOpenCourse() {},
    onQuit() {},
    onRetry() {},
    onSelfTest() {},
  }
}
