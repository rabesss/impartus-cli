import { describe, expect, test } from "bun:test"

import type {
  ArtifactList,
  Course,
  CourseList,
  DiagnosticList,
  Health,
  LectureList,
} from "../src/protocol/types.gen.ts"
import {
  WorkspaceController,
  createFoundationState,
  newOperation,
  type WorkspaceClient,
} from "../src/workspace_controller.ts"

describe("WorkspaceController", () => {
  test("retries unavailable authentication and reaches the catalog in the same controller", async () => {
    const client = new DeferredClient()
    const controller = new WorkspaceController(client, createFoundationState({
      authStatus: "unavailable",
      status: "Authentication unavailable",
    }))

    const retry = controller.retry()
    expect(client.authenticationRequests).toHaveLength(1)
    expect(client.courseRequests).toHaveLength(0)
    client.authenticationRequests[0]!.resolve(testHealth("ready"))
    await Promise.resolve()
    expect(client.courseRequests).toHaveLength(1)
    client.courseRequests[0]!.resolve({ courses: [testCourse(7)] })
    await retry

    expect(controller.snapshot().authStatus).toBe("ready")
    expect(controller.snapshot().courses.map((course) => course.subjectId)).toEqual([7])
    expect(controller.snapshot().status).toBe("Connected")
    expect(controller.snapshot().loading).toBe(false)
  })

  test("keeps authentication unavailable after a safe retry failure", async () => {
    const client = new DeferredClient()
    const controller = new WorkspaceController(client, createFoundationState({
      authStatus: "unavailable",
      screen: "library",
    }))

    const retry = controller.retry()
    expect(controller.snapshot().screen).toBe("library")
    client.authenticationRequests[0]!.reject(new Error("raw upstream body secret"))
    await retry

    expect(controller.snapshot().authStatus).toBe("unavailable")
    expect(controller.snapshot().error).toBe("Authentication is unavailable")
    expect(controller.snapshot().screen).toBe("library")
    expect(controller.snapshot().loading).toBe(false)
    expect(client.courseRequests).toHaveLength(0)
  })

  test("keeps successful authentication when the catalog refresh fails", async () => {
    const client = new DeferredClient()
    const controller = new WorkspaceController(client, createFoundationState({ authStatus: "unavailable" }))

    const retry = controller.retry()
    client.authenticationRequests[0]!.resolve(testHealth("ready"))
    await Promise.resolve()
    client.courseRequests[0]!.reject(new Error("catalog unavailable"))
    await retry

    expect(controller.snapshot().authStatus).toBe("ready")
    expect(controller.snapshot().error).toBe("Course catalog is unavailable")
    expect(controller.snapshot().loading).toBe(false)
  })

  test("blocks remote navigation but keeps local collections reachable while unavailable", () => {
    const client = new DeferredClient()
    const controller = new WorkspaceController(client, createFoundationState({ authStatus: "unavailable" }))

    expect(controller.navigate("courses")).toBe(false)
    expect(controller.navigate("lectures", testCourse(3))).toBe(false)
    expect(controller.navigate("library")).toBe(true)
    expect(client.courseRequests).toHaveLength(0)
    expect(client.lectureRequests).toHaveLength(0)
    expect(client.artifactRequests).toHaveLength(1)
    client.artifactRequests[0]!.resolve({ artifacts: [] })
  })

  test("applies a matching lecture response and clears loading", async () => {
    const client = new DeferredClient()
    const controller = new WorkspaceController(client, createFoundationState())
    const course = testCourse(3)

    const loading = controller.loadLectures(course)
    expect(controller.snapshot().loading).toBe(true)
    expect(controller.snapshot().screen).toBe("lectures")
    client.lectureRequests[0]!.resolve({ lectures: [testLecture(5)] })
    await loading

    expect(controller.snapshot().lectures.map((lecture) => lecture.ttid)).toEqual([5])
    expect(controller.snapshot().loading).toBe(false)
    expect(controller.snapshot().error).toBeUndefined()
  })

  test("retries lectures on the same course and always releases loading", async () => {
    const client = new DeferredClient()
    const course = testCourse(3)
    const initial = createFoundationState({ activeCourse: course, screen: "lectures" })
    const controller = new WorkspaceController(client, initial)

    const failed = controller.retry()
    client.lectureRequests[0]!.reject(new Error("unavailable"))
    await failed
    expect(controller.snapshot().error).toBe("Lecture catalog is unavailable")
    expect(controller.snapshot().loading).toBe(false)

    const succeeded = controller.retry()
    client.lectureRequests[1]!.resolve({ lectures: [testLecture(8)] })
    await succeeded
    expect(controller.snapshot().lectures.map((lecture) => lecture.ttid)).toEqual([8])
    expect(controller.snapshot().error).toBeUndefined()
    expect(controller.snapshot().loading).toBe(false)
  })

  test("ignores stale responses without clearing a newer request", async () => {
    const client = new DeferredClient()
    const controller = new WorkspaceController(client, createFoundationState())
    const first = controller.loadLectures(testCourse(1))
    const second = controller.loadLectures(testCourse(2))

    client.lectureRequests[0]!.resolve({ lectures: [testLecture(101)] })
    await first
    expect(controller.snapshot().loading).toBe(true)
    expect(controller.snapshot().lectures).toEqual([])

    client.lectureRequests[1]!.resolve({ lectures: [testLecture(202)] })
    await second
    expect(controller.snapshot().loading).toBe(false)
    expect(controller.snapshot().lectures.map((lecture) => lecture.ttid)).toEqual([202])
    expect(controller.snapshot().activeCourse?.subjectId).toBe(2)
  })

  test("clears another course's lectures before a failed switch", async () => {
    const client = new DeferredClient()
    const firstCourse = testCourse(1)
    const secondCourse = testCourse(2)
    const controller = new WorkspaceController(client, createFoundationState({
      activeCourse: firstCourse,
      lectures: [testLecture(101)],
      screen: "lectures",
    }))
    controller.setCollectionState("lectures", { filter: "persist", selected: 20 })

    const pending = controller.loadLectures(secondCourse)
    expect(controller.snapshot().lectures).toEqual([])
    expect(controller.snapshot().collections.lectures).toEqual({ filter: "persist", selected: 0 })
    client.lectureRequests[0]!.reject(new Error("unavailable"))
    await pending

    expect(controller.snapshot().activeCourse?.subjectId).toBe(2)
    expect(controller.snapshot().lectures).toEqual([])
    expect(controller.snapshot().collections.lectures).toEqual({ filter: "persist", selected: 0 })
    expect(controller.snapshot().error).toBe("Lecture catalog is unavailable")
  })

  test("blocks every screen navigation while a response is pending", async () => {
    const client = new DeferredClient()
    const controller = new WorkspaceController(client, createFoundationState())
    const pending = controller.loadCourses()

    expect(controller.navigate("courses")).toBe(false)
    expect(controller.navigate("lectures", testCourse(3))).toBe(false)
    expect(controller.navigate("library")).toBe(false)
    expect(controller.navigate("diagnostics")).toBe(false)
    expect(client.courseRequests).toHaveLength(1)
    expect(client.lectureRequests).toHaveLength(0)
    expect(client.artifactRequests).toHaveLength(0)
    expect(client.diagnosticRequests).toHaveLength(0)

    client.courseRequests[0]!.resolve({ courses: [] })
    await pending
    expect(controller.snapshot().loading).toBe(false)
  })

  test("does not start a retry while another response is pending", async () => {
    const client = new DeferredClient()
    const controller = new WorkspaceController(client, createFoundationState())
    const pending = controller.loadCourses()
    const retry = controller.retry()
    const requestCount = client.courseRequests.length

    for (const request of client.courseRequests) request.resolve({ courses: [] })
    await Promise.all([pending, retry])

    expect(requestCount).toBe(1)
    expect(controller.snapshot().loading).toBe(false)
  })

  test("aborting makes a later response a no-op", async () => {
    const client = new DeferredClient()
    const controller = new WorkspaceController(client, createFoundationState())
    const pending = controller.loadLibrary()
    controller.abort()
    client.artifactRequests[0]!.resolve({ artifacts: [{
      artifactId: "late",
      fileCount: 1,
      presentFileCount: 1,
      producedAt: "2026-08-22T00:00:00Z",
      sequence: 1,
      topic: "Late",
      totalBytes: 1,
    }] })
    await pending

    expect(controller.snapshot().artifacts).toEqual([])
    expect(controller.snapshot().loading).toBe(false)
  })

  test("operation events preserve controller-owned collection state", () => {
    const initial = createFoundationState({
      operation: {
        durationSeconds: 0,
        id: "operation-id",
        kind: "download",
        muted: false,
        paused: false,
        percent: 0,
        positionSeconds: 0,
        speed: 1,
        state: "running",
        volume: 100,
      },
    })
    const controller = new WorkspaceController(new DeferredClient(), initial)
    controller.setCollectionState("lectures", { filter: "raft", selected: 4 })
    controller.setCollectionState("library", { filter: "local", selected: 2 })

    controller.applyEvent({
      operationId: "operation-id",
      percent: 50,
      sequence: 2,
      state: "running",
      type: "operation.progress",
    })

    expect(controller.snapshot().collections.lectures).toEqual({ filter: "raft", selected: 4 })
    expect(controller.snapshot().collections.library).toEqual({ filter: "local", selected: 2 })
    expect(controller.snapshot().operation?.percent).toBe(50)
  })

  test("clamps externally supplied filters without splitting graphemes", () => {
    const controller = new WorkspaceController(new DeferredClient(), createFoundationState())
    const filter = `${"a\u0301\u0327".repeat(30)}${"x".repeat(29)}😀`

    controller.setCollectionState("courses", { filter, selected: 0 })

    expect(controller.snapshot().collections.courses.filter).toBe(filter)
  })

  test("reconciles a terminal operation event delivered before the start response", () => {
    const controller = new WorkspaceController(new DeferredClient(), createFoundationState({ loading: true }))

    controller.applyEvent({
      operationId: "operation-id",
      sequence: 2,
      state: "completed",
      type: "operation.completed",
    })
    controller.update((state) => ({
      ...state,
      loading: false,
      operation: newOperation("operation-id", "selftest", "running"),
    }))

    expect(controller.snapshot().operation?.state).toBe("completed")
    expect(controller.snapshot().status).toBe("Session test completed")
  })

  test("does not reconcile an early event for another operation id", () => {
    const controller = new WorkspaceController(new DeferredClient(), createFoundationState({ loading: true }))

    controller.applyEvent({
      operationId: "different-operation",
      sequence: 2,
      state: "completed",
      type: "operation.completed",
    })
    controller.update((state) => ({
      ...state,
      loading: false,
      operation: newOperation("installed-operation", "selftest", "running"),
    }))

    expect(controller.snapshot().operation?.id).toBe("installed-operation")
    expect(controller.snapshot().operation?.state).toBe("running")
  })

  test("discards early operation events when a start request fails", () => {
    const controller = new WorkspaceController(new DeferredClient(), createFoundationState({ loading: true }))

    controller.applyEvent({
      operationId: "reused-operation",
      sequence: 2,
      state: "completed",
      type: "operation.completed",
    })
    controller.update((state) => ({ ...state, loading: false, status: "Start failed" }))
    controller.update((state) => ({ ...state, loading: true }))
    controller.update((state) => ({
      ...state,
      loading: false,
      operation: newOperation("reused-operation", "selftest", "running"),
    }))

    expect(controller.snapshot().operation?.state).toBe("running")
    expect(controller.snapshot().status).toBe("Start failed")
  })

  test("merges partial collection overrides with domain defaults", () => {
    const state = createFoundationState({
      collections: { lectures: { filter: "raft", selected: 2 } },
    })

    expect(state.collections.lectures).toEqual({ filter: "raft", selected: 2 })
    expect(state.collections.courses).toEqual({ filter: "", selected: 0 })
    expect(state.collections.library).toEqual({ filter: "", selected: 0 })
    expect(state.collections.diagnostics).toEqual({ filter: "", selected: 0 })
  })
})

class DeferredClient implements WorkspaceClient {
  readonly authenticationRequests: Array<Deferred<Health>> = []
  readonly artifactRequests: Array<Deferred<ArtifactList>> = []
  readonly courseRequests: Array<Deferred<CourseList>> = []
  readonly diagnosticRequests: Array<Deferred<DiagnosticList>> = []
  readonly lectureRequests: Array<Deferred<LectureList>> = []

  public artifacts(): Promise<ArtifactList> {
    const request = deferred<ArtifactList>()
    this.artifactRequests.push(request)
    return request.promise
  }

  public retryAuthentication(): Promise<Health> {
    const request = deferred<Health>()
    this.authenticationRequests.push(request)
    return request.promise
  }

  public courses(): Promise<CourseList> {
    const request = deferred<CourseList>()
    this.courseRequests.push(request)
    return request.promise
  }

  public diagnostics(): Promise<DiagnosticList> {
    const request = deferred<DiagnosticList>()
    this.diagnosticRequests.push(request)
    return request.promise
  }

  public lectures(_course: Course): Promise<LectureList> {
    const request = deferred<LectureList>()
    this.lectureRequests.push(request)
    return request.promise
  }
}

function testHealth(authStatus: Health["authStatus"]): Health {
  return {
    authStatus,
    protocol: "tui/v2",
    sessionId: "session-id",
    status: "ok",
    version: "test",
  }
}

interface Deferred<T> {
  promise: Promise<T>
  reject(reason: unknown): void
  resolve(value: T): void
}

function deferred<T>(): Deferred<T> {
  const result = Promise.withResolvers<T>()
  return { promise: result.promise, reject: result.reject, resolve: result.resolve }
}

function testCourse(subjectId: number): Course {
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

function testLecture(ttid: number): LectureList["lectures"][number] {
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
    topic: "Lecture",
    ttid,
    views: 0,
  }
}
