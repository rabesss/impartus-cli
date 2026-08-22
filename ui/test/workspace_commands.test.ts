import { describe, expect, test } from "bun:test"

import { calculateLayout } from "../src/workspace_layout.ts"
import {
  commandForKey,
  commandsForHelp,
  commandsForPalette,
  footerCommands,
  type CommandContext,
} from "../src/workspace_commands.ts"
import { createFoundationState } from "../src/workspace_controller.ts"

describe("workspace command registry", () => {
  test("disables every screen-changing command while a response is pending", () => {
    const context = commandContext({ loading: true, screen: "lectures" })
    for (const key of ["c", "g", "l", "!"]) {
      const result = commandForKey(context, key)
      expect(result?.availability.visible).toBe(true)
      expect(result?.availability.enabled).toBe(false)
      expect(result?.availability.reason).toContain("request")
    }
  })

  test("filters palette labels and keys through one registry", () => {
    const context = commandContext({ screen: "courses" })
    const matches = commandsForPalette(context, "library")
    expect(matches.map((match) => match.command.id)).toEqual(["navigation.library"])
    expect(matches[0]?.command.keys).toContain("l")
    expect(matches[0]?.availability.enabled).toBe(true)
  })

  test("uses the same contextual entries for help and footer", () => {
    const context = commandContext({
      lectures: [{
        classroomName: "Room",
        durationSeconds: 60,
        instituteId: 1,
        noAudio: false,
        professorName: "Professor",
        sequence: 1,
        sessionId: 2,
        sessionName: "Session",
        startTime: "2026-08-22T00:00:00Z",
        subjectId: 3,
        subjectName: "Course",
        topic: "Lecture",
        ttid: 4,
        views: 0,
      }],
      screen: "lectures",
    })
    const help = commandsForHelp(context)
    const footer = footerCommands(context)
    expect(help.map((entry) => entry.command.id)).toContain("lecture.download")
    expect(footer.map((entry) => entry.command.id)).toContain("lecture.download")
    expect(commandForKey(context, "d")?.command.id).toBe("lecture.download")
  })

  test("keeps palette navigation distinct from ordinary j and k query text", () => {
    const context = commandContext({ overlay: "palette" })
    expect(commandForKey(context, "j")).toBeUndefined()
    expect(commandForKey(context, "k")).toBeUndefined()
    expect(commandForKey(context, "down")?.command.id).toBe("selection.move")
    expect(commandForKey(context, "up")?.command.id).toBe("selection.move")
  })
})

function commandContext(overrides: Partial<CommandContext["state"]> & { overlay?: CommandContext["overlay"] }): CommandContext {
  const { overlay, ...stateOverrides } = overrides
  return {
    focus: "collection",
    layout: calculateLayout(140, 32, false),
    overlay,
    state: createFoundationState(stateOverrides),
  }
}
