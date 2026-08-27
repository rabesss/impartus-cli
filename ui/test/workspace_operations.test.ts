import { describe, expect, test } from "bun:test"

import type { Lecture } from "../src/protocol/types.gen.ts"
import { beginPlaybackStart, cancelOperationBeforeBack, failOperationStart } from "../src/workspace_operations.ts"
import { createFoundationState, newOperation } from "../src/workspace_controller.ts"

describe("workspace operation transitions", () => {
  test("starts playback without exposing a prior terminal operation", () => {
    const lecture = testLecture("New lecture")
    const state = beginPlaybackStart(createFoundationState({
      operation: newOperation("prior", "playback", "completed"),
      screen: "lectures",
    }), lecture)

    expect(state.activeLecture).toEqual(lecture)
    expect(state.loading).toBe(true)
    expect(state.operation).toBeUndefined()
    expect(state.screen).toBe("playback")
    expect(state.status).toBe("Starting New lecture")
  })

  test("reports download-start failure without replacing the lecture collection", () => {
    const lecture = testLecture("Visible lecture")
    const state = failOperationStart(createFoundationState({
      error: "stale error",
      lectures: [lecture],
      loading: true,
      screen: "lectures",
    }), "download")

    expect(state.error).toBeUndefined()
    expect(state.lectures).toEqual([lecture])
    expect(state.loading).toBe(false)
    expect(state.screen).toBe("lectures")
    expect(state.status).toBe("Lecture download could not start")
  })

  test("keeps playback-start failure on the playback back path", () => {
    const state = failOperationStart(createFoundationState({ loading: true, screen: "playback", status: "Starting Lecture" }), "playback")
    expect(state.error).toBe("Lecture playback could not start")
    expect(state.loading).toBe(false)
    expect(state.screen).toBe("playback")
    expect(state.status).toBe("Lecture playback could not start")
  })

  test("does not erase a collection error when self-test startup also fails", () => {
    const state = failOperationStart(createFoundationState({
      error: "Course catalog is unavailable",
      loading: true,
      status: "Connected",
    }), "selftest")

    expect(state.error).toBe("Course catalog is unavailable")
    expect(state.loading).toBe(false)
    expect(state.status).toBe("Connection failed")
  })

  test("requests cancellation before backing out of a running download", async () => {
    const canceled: string[] = []
    await cancelOperationBeforeBack(createFoundationState({
      operation: newOperation("download-id", "download", "running"),
      screen: "lectures",
    }), async (identifier) => {
      canceled.push(identifier)
    })

    expect(canceled).toEqual(["download-id"])
  })

  test("keeps playback cancellation on the playback back path", async () => {
    const canceled: string[] = []
    await cancelOperationBeforeBack(createFoundationState({
      operation: newOperation("playback-id", "playback", "running"),
      screen: "playback",
    }), async (identifier) => {
      canceled.push(identifier)
    })

    expect(canceled).toEqual(["playback-id"])
  })
})

function testLecture(topic: string): Lecture {
  return {
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
    topic,
    ttid: 4,
    views: 0,
  }
}
