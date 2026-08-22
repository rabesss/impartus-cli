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

  test("moves and opens the focused wide navigation pane", () => {
    const context = commandContext({ focus: "navigation" })
    expect(commandForKey(context, "j")?.availability.enabled).toBe(true)
    expect(commandForKey(context, "enter")?.availability.enabled).toBe(true)

    const pending = commandContext({ focus: "navigation", loading: true })
    expect(commandForKey(pending, "j")?.availability.enabled).toBe(true)
    expect(commandForKey(pending, "enter")?.availability.enabled).toBe(false)
  })

  test("offers filtering for every collection domain", () => {
    for (const screen of ["courses", "lectures", "library", "diagnostics"] as const) {
      const filter = commandForKey(commandContext({ screen }), "/")
      expect(filter?.command.id).toBe("collection.filter")
      expect(filter?.availability.enabled).toBe(true)
    }
  })

  test("evaluates help and palette commands against the underlying workspace", () => {
    const palette = commandsForPalette(commandContext({ overlay: "palette", screen: "library" }), "filter")
    expect(palette.map((entry) => entry.command.id)).toEqual(["collection.filter"])

    const help = commandsForHelp(commandContext({ overlay: "help", screen: "library" }))
    expect(help.map((entry) => entry.command.id)).toContain("collection.filter")
    expect(help.map((entry) => entry.command.id)).toContain("collection.retry")
  })

  test("disables hidden selection actions for errors and empty filter results", () => {
    const filtered = commandContext({
      collections: {
        courses: { filter: "no-match", selected: 0 },
        diagnostics: { filter: "", selected: 0 },
        lectures: { filter: "", selected: 0 },
        library: { filter: "", selected: 0 },
      },
      courses: [{
        instituteId: 1,
        professorName: "Professor",
        sessionId: 2,
        sessionName: "Session",
        subjectId: 3,
        subjectName: "Course",
        videoCount: 1,
      }],
    })
    expect(commandForKey(filtered, "enter")?.availability.enabled).toBe(false)

    const errored = commandContext({
      error: "Lecture catalog is unavailable",
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
        topic: "Old lecture",
        ttid: 4,
        views: 0,
      }],
      screen: "lectures",
    })
    expect(commandForKey(errored, "enter")?.availability.enabled).toBe(false)
    expect(commandForKey(errored, "d")?.availability.enabled).toBe(false)
  })

  test("does not expose the session self-test inside playback", () => {
    const context = commandContext({
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
    })

    expect(commandForKey(context, "s")).toBeUndefined()
    expect(commandsForHelp(context).map((entry) => entry.command.id)).not.toContain("session.selftest")
  })

  test("does not let wide-pane focus navigate away from playback", () => {
    const context = commandContext({
      operation: {
        durationSeconds: 60,
        id: "playback-id",
        kind: "playback",
        muted: false,
        paused: false,
        percent: 25,
        positionSeconds: 15,
        speed: 1,
        state: "running",
        volume: 100,
      },
      screen: "playback",
    })
    const navigation = { ...context, focus: "navigation" as const }

    expect(commandForKey(context, "tab")).toBeUndefined()
    expect(commandForKey(navigation, "enter")?.availability.enabled).toBe(false)
  })

  test("does not advertise lecture playback while another operation is running", () => {
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
      operation: {
        durationSeconds: 0,
        id: "download-id",
        kind: "download",
        muted: false,
        paused: false,
        percent: 25,
        positionSeconds: 0,
        speed: 1,
        state: "running",
        volume: 100,
      },
      screen: "lectures",
    })

    const open = commandForKey(context, "enter")
    expect(open?.availability.enabled).toBe(false)
    expect(open?.availability.reason).toContain("operation")
  })
})

function commandContext(overrides: Partial<CommandContext["state"]> & {
  focus?: CommandContext["focus"]
  overlay?: CommandContext["overlay"]
}): CommandContext {
  const { focus = "collection", overlay, ...stateOverrides } = overrides
  return {
    focus,
    layout: calculateLayout(140, 32, false),
    overlay,
    state: createFoundationState(stateOverrides),
  }
}
