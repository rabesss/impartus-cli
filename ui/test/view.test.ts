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
    const view = new FoundationView(setup.renderer, foundationState(), { onQuit() {}, onSelfTest() {} })

    await setup.renderOnce()
    const frame = setup.captureCharFrame()

    expect(frame).toContain("IMPARTUS")
    expect(frame).toContain("Courses")
    expect(frame).toContain("Workspace")
    expect(frame).toContain("Session")
    expect(frame).toContain("Distributed Systems")
    view.destroy()
  })

  test("uses one routed pane on narrow terminals and keeps commands discoverable", async () => {
    const setup = await createTestRenderer({ height: 12, kittyKeyboard: true, width: 40 })
    renderers.push(setup)
    let quitCount = 0
    let selfTestCount = 0
    const view = new FoundationView(setup.renderer, foundationState(), {
      onQuit() {
        quitCount++
      },
      onSelfTest() {
        selfTestCount++
      },
    })

    await setup.renderOnce()
    expect(setup.captureCharFrame()).toContain("Distributed Systems")
    expect(setup.captureCharFrame()).not.toContain("Workspace")

    setup.mockInput.pressKey("?")
    await setup.renderOnce()
    expect(setup.captureCharFrame()).toContain("Keyboard")
    expect(setup.captureCharFrame()).toContain("run connection test")

    setup.mockInput.pressEscape()
    await setup.renderOnce()
    setup.mockInput.pressKey("s")
    setup.mockInput.pressKey("q")
    setup.mockInput.pressCtrlC()
    expect(selfTestCount).toBe(1)
    expect(quitCount).toBe(2)
    view.destroy()
  })
})

function foundationState(): FoundationState {
  return {
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
    operation: undefined,
    selectedCourse: 0,
    status: "Connected",
  }
}
