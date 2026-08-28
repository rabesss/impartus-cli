import { describe, expect, test } from "bun:test"

import type { Lecture } from "../src/protocol/types.gen.ts"
import {
  beginPlaybackStart,
  cancelOperationBeforeBack,
  completeBackNavigation,
  failOperationStart,
} from "../src/workspace_operations.ts"
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

  test("cancels a running download from the local library", async () => {
    const canceled: string[] = []
    await cancelOperationBeforeBack(createFoundationState({
      operation: newOperation("download-id", "download", "running"),
      screen: "library",
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

  test("does not cancel operations outside the back-navigation boundary", async () => {
    const canceled: string[] = []
    const cancel = async (identifier: string): Promise<void> => {
      canceled.push(identifier)
    }

    await cancelOperationBeforeBack(createFoundationState({
      operation: newOperation("selftest-id", "selftest", "running"),
      screen: "lectures",
    }), cancel)
    await cancelOperationBeforeBack(createFoundationState({
      operation: newOperation("playback-id", "playback", "running"),
      screen: "lectures",
    }), cancel)
    await cancelOperationBeforeBack(createFoundationState({
      operation: newOperation("completed-download-id", "download", "completed"),
      screen: "library",
    }), cancel)

    expect(canceled).toEqual([])
  })

  test("completes back navigation when only the operation changed during cancellation", () => {
    const before = createFoundationState({
      activeCourse: testCourse(3),
      operation: newOperation("download-id", "download", "running"),
      screen: "lectures",
    })
    const current = {
      ...before,
      operation: newOperation("download-id", "download", "canceled"),
      status: "Download canceled",
    }

    const state = completeBackNavigation(before, current)

    expect(state.screen).toBe("courses")
    expect(state.error).toBeUndefined()
    expect(state.status).toBe("Download canceled")
  })

  test("does not let a delayed back overwrite a newer screen or pending request", () => {
    const before = createFoundationState({ activeCourse: testCourse(3), screen: "lectures" })
    const library = createFoundationState({ activeCourse: testCourse(3), screen: "library" })
    const pending = createFoundationState({
      activeCourse: testCourse(3),
      loading: true,
      pending: { courseKey: "1:2:3", generation: 4, target: "lectures" },
      screen: "lectures",
    })

    expect(completeBackNavigation(before, library)).toEqual(library)
    expect(completeBackNavigation(before, pending)).toEqual(pending)
  })

  test("does not let a delayed back close a newer course on the same screen", () => {
    const before = createFoundationState({ activeCourse: testCourse(3), screen: "lectures" })
    const current = createFoundationState({ activeCourse: testCourse(9), screen: "lectures" })

    expect(completeBackNavigation(before, current)).toEqual(current)
  })
})

function testCourse(subjectId: number) {
  return {
    instituteId: 1,
    professorName: "Professor",
    sessionId: 2,
    sessionName: "Session",
    subjectId,
    subjectName: `Course ${subjectId}`,
    videoCount: 1,
  }
}

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
