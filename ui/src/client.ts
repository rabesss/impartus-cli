import {
  CAPABILITY_HEADER,
  PROTOCOL_HEADER,
  PROTOCOL_VERSION,
  type ArtifactList,
  type ArtifactSummary,
  type Bootstrap,
  type Course,
  type CourseList,
  type Diagnostic,
  type DiagnosticList,
  type Event,
  type EventType,
  type Health,
  type Lecture,
  type LectureList,
  type Operation,
  type OperationKind,
  type OperationState,
  type PlaybackCommand,
} from "./protocol/types.gen.ts"

const EVENT_TYPES = new Set<EventType>([
  "session.ready",
  "operation.started",
  "operation.progress",
  "operation.completed",
  "operation.canceled",
  "operation.failed",
  "stream.overflow",
])
const OPERATION_STATES = new Set<OperationState>(["running", "completed", "canceled", "failed"])
const OPERATION_KINDS = new Set<OperationKind>(["download", "playback", "selftest"])

export class SessionClient {
  readonly #bootstrap: Bootstrap

  public constructor(bootstrap: Bootstrap) {
    this.#bootstrap = bootstrap
  }

  public async health(signal?: AbortSignal): Promise<Health> {
    const value = await this.#json("/health", { method: "GET" }, signal)
    if (!isHealth(value) || value.protocol !== this.#bootstrap.protocol || value.sessionId !== this.#bootstrap.sessionId) {
      throw new Error("Invalid UI session response")
    }
    return value
  }

  public async courses(signal?: AbortSignal): Promise<CourseList> {
    const value = await this.#json("/courses", { method: "GET" }, signal)
    if (!isCourseList(value)) {
      throw new Error("Invalid UI session response")
    }
    return value
  }

  public async lectures(course: Course, signal?: AbortSignal): Promise<LectureList> {
    const query = new URLSearchParams({
      instituteId: String(course.instituteId),
      sessionId: String(course.sessionId),
      subjectId: String(course.subjectId),
    })
    const value = await this.#json(`/lectures?${query.toString()}`, { method: "GET" }, signal)
    if (!isLectureList(value)) {
      throw new Error("Invalid UI session response")
    }
    return value
  }

  public async artifacts(signal?: AbortSignal): Promise<ArtifactList> {
    const value = await this.#json("/library", { method: "GET" }, signal)
    if (!isArtifactList(value)) {
      throw new Error("Invalid UI session response")
    }
    return value
  }

  public async diagnostics(signal?: AbortSignal): Promise<DiagnosticList> {
    const value = await this.#json("/diagnostics", { method: "GET" }, signal)
    if (!isDiagnosticList(value)) {
      throw new Error("Invalid UI session response")
    }
    return value
  }

  public async startSelfTest(signal?: AbortSignal): Promise<Operation> {
    const value = await this.#json(
      "/operations",
      {
        body: JSON.stringify({ kind: "selftest" }),
        headers: { "Content-Type": "application/json" },
        method: "POST",
      },
      signal,
    )
    if (!isOperation(value)) {
      throw new Error("Invalid UI session response")
    }
    return value
  }

  public async startDownload(lecture: Lecture, signal?: AbortSignal): Promise<Operation> {
    const value = await this.#json(
      "/operations",
      {
        body: JSON.stringify({
          kind: "download",
          lecture: {
            instituteId: lecture.instituteId,
            sessionId: lecture.sessionId,
            subjectId: lecture.subjectId,
            ttid: lecture.ttid,
          },
        }),
        headers: { "Content-Type": "application/json" },
        method: "POST",
      },
      signal,
    )
    if (!isOperation(value) || value.kind !== "download") {
      throw new Error("Invalid UI session response")
    }
    return value
  }

  public async startPlayback(lecture: Lecture, resume: boolean, signal?: AbortSignal): Promise<Operation> {
    const value = await this.#json(
      "/operations",
      {
        body: JSON.stringify({
          kind: "playback",
          lecture: lectureIdentity(lecture),
          resume,
        }),
        headers: { "Content-Type": "application/json" },
        method: "POST",
      },
      signal,
    )
    if (!isOperation(value) || value.kind !== "playback") {
      throw new Error("Invalid UI session response")
    }
    return value
  }

  public async playbackCommand(identifier: string, command: PlaybackCommand, signal?: AbortSignal): Promise<Operation> {
    const value = await this.#json(
      `/operations/${encodeURIComponent(identifier)}/commands`,
      {
        body: JSON.stringify(command),
        headers: { "Content-Type": "application/json" },
        method: "POST",
      },
      signal,
    )
    if (!isOperation(value) || value.kind !== "playback") {
      throw new Error("Invalid UI session response")
    }
    return value
  }

  public async cancelOperation(identifier: string, signal?: AbortSignal): Promise<Operation> {
    const value = await this.#json(`/operations/${encodeURIComponent(identifier)}`, { method: "DELETE" }, signal)
    if (!isOperation(value)) {
      throw new Error("Invalid UI session response")
    }
    return value
  }

  public async *events(signal?: AbortSignal): AsyncGenerator<Event> {
    const response = await this.#request("/events", { method: "GET" }, signal)
    if (!response.headers.get("Content-Type")?.toLowerCase().startsWith("text/event-stream") || response.body === null) {
      throw new Error("Invalid UI event stream")
    }
    const reader = response.body.getReader()
    const decoder = new TextDecoder()
    let buffered = ""
    let previousSequence = 0
    try {
      while (true) {
        const { done, value } = await reader.read()
        buffered += decoder.decode(value, { stream: !done }).replaceAll("\r\n", "\n")
        let boundary = buffered.indexOf("\n\n")
        while (boundary >= 0) {
          const block = buffered.slice(0, boundary)
          buffered = buffered.slice(boundary + 2)
          const event = parseEventBlock(block)
          if (event !== undefined) {
            if (event.sequence <= previousSequence) {
              throw new Error("Invalid UI event ordering")
            }
            previousSequence = event.sequence
            yield event
            if (event.type === "stream.overflow") {
              return
            }
          }
          boundary = buffered.indexOf("\n\n")
        }
        if (done) {
          return
        }
      }
    } finally {
      await reader.cancel().catch(() => undefined)
      reader.releaseLock()
    }
  }

  async #json(path: string, init: RequestInit, signal?: AbortSignal): Promise<unknown> {
    const response = await this.#request(path, init, signal)
    try {
      return await response.json()
    } catch {
      throw new Error("Invalid UI session response")
    }
  }

  async #request(path: string, init: RequestInit, signal?: AbortSignal): Promise<Response> {
    const headers = new Headers(init.headers)
    headers.set(CAPABILITY_HEADER, this.#bootstrap.capability)
    headers.set(PROTOCOL_HEADER, PROTOCOL_VERSION)
    const request: RequestInit = {
      ...init,
      cache: "no-store",
      headers,
    }
    if (signal !== undefined) {
      request.signal = signal
    }
    let response: Response
    try {
      response = await fetch(this.#bootstrap.baseUrl + path, request)
    } catch {
      throw new Error("UI session is unavailable")
    }
    if (!response.ok) {
      await response.body?.cancel().catch(() => undefined)
      throw new Error(`UI session request failed (${response.status})`)
    }
    return response
  }
}

function parseEventBlock(block: string): Event | undefined {
  const data = block
    .split("\n")
    .filter((line) => line.startsWith("data:"))
    .map((line) => line.slice(5).trimStart())
    .join("\n")
  if (data === "") {
    return undefined
  }
  let value: unknown
  try {
    value = JSON.parse(data) as unknown
  } catch {
    throw new Error("Invalid UI event stream")
  }
  if (!isEvent(value)) {
    throw new Error("Invalid UI event stream")
  }
  return value
}

function isHealth(value: unknown): value is Health {
  return (
    isRecord(value) &&
    value.protocol === PROTOCOL_VERSION &&
    typeof value.sessionId === "string" &&
    value.sessionId.length > 0 &&
    value.status === "ok" &&
    typeof value.version === "string"
  )
}

function isCourseList(value: unknown): value is CourseList {
  return isRecord(value) && Array.isArray(value.courses) && value.courses.every(isCourse)
}

function isCourse(value: unknown): value is Course {
  return (
    isRecord(value) &&
    isInteger(value.instituteId) &&
    typeof value.professorName === "string" &&
    isInteger(value.sessionId) &&
    typeof value.sessionName === "string" &&
    isInteger(value.subjectId) &&
    typeof value.subjectName === "string" &&
    isInteger(value.videoCount)
  )
}

function isLectureList(value: unknown): value is LectureList {
  return isRecord(value) && Array.isArray(value.lectures) && value.lectures.every(isLecture)
}

function isLecture(value: unknown): value is Lecture {
  return (
    isRecord(value) &&
    typeof value.classroomName === "string" &&
    isNonNegativeInteger(value.durationSeconds) &&
    isInteger(value.instituteId) &&
    typeof value.noAudio === "boolean" &&
    typeof value.professorName === "string" &&
    isInteger(value.sequence) &&
    isInteger(value.sessionId) &&
    typeof value.sessionName === "string" &&
    typeof value.startTime === "string" &&
    isInteger(value.subjectId) &&
    typeof value.subjectName === "string" &&
    typeof value.topic === "string" &&
    isInteger(value.ttid) &&
    isNonNegativeInteger(value.views)
  )
}

function isArtifactList(value: unknown): value is ArtifactList {
  return isRecord(value) && Array.isArray(value.artifacts) && value.artifacts.every(isArtifactSummary)
}

function isArtifactSummary(value: unknown): value is ArtifactSummary {
  return (
    isRecord(value) &&
    typeof value.artifactId === "string" &&
    value.artifactId.length > 0 &&
    isNonNegativeInteger(value.fileCount) &&
    isNonNegativeInteger(value.presentFileCount) &&
    value.presentFileCount <= value.fileCount &&
    typeof value.producedAt === "string" &&
    !Number.isNaN(Date.parse(value.producedAt)) &&
    isInteger(value.sequence) &&
    typeof value.topic === "string" &&
    isNonNegativeInteger(value.totalBytes)
  )
}

function isDiagnosticList(value: unknown): value is DiagnosticList {
  return isRecord(value) && Array.isArray(value.diagnostics) && value.diagnostics.every(isDiagnostic)
}

function isDiagnostic(value: unknown): value is Diagnostic {
  return isRecord(value) && typeof value.detail === "string" && typeof value.name === "string" && typeof value.status === "string"
}

function isOperation(value: unknown): value is Operation {
  return (
    isRecord(value) &&
    typeof value.id === "string" &&
    value.id.length > 0 &&
    typeof value.kind === "string" &&
    OPERATION_KINDS.has(value.kind as OperationKind) &&
    typeof value.state === "string" &&
    OPERATION_STATES.has(value.state as OperationState)
  )
}

function isEvent(value: unknown): value is Event {
  if (
    !isRecord(value) ||
    !isInteger(value.sequence) ||
    value.sequence <= 0 ||
    typeof value.type !== "string" ||
    !EVENT_TYPES.has(value.type as EventType)
  ) {
    return false
  }
  if (value.message !== undefined && typeof value.message !== "string") return false
  if (value.durationSeconds !== undefined && !isFiniteNonNegative(value.durationSeconds)) return false
  if (value.muted !== undefined && typeof value.muted !== "boolean") return false
  if (value.operationId !== undefined && typeof value.operationId !== "string") return false
  if (value.paused !== undefined && typeof value.paused !== "boolean") return false
  if (value.percent !== undefined && (typeof value.percent !== "number" || value.percent < 0 || value.percent > 100)) return false
  if (value.positionSeconds !== undefined && !isFiniteNonNegative(value.positionSeconds)) return false
  if (value.speed !== undefined && (typeof value.speed !== "number" || !Number.isFinite(value.speed) || value.speed < 0.25 || value.speed > 4)) return false
  if (value.state !== undefined && (typeof value.state !== "string" || !OPERATION_STATES.has(value.state as OperationState))) {
    return false
  }
  if (value.volume !== undefined && (typeof value.volume !== "number" || !Number.isFinite(value.volume) || value.volume < 0 || value.volume > 130)) return false
  return true
}

function lectureIdentity(lecture: Lecture) {
  return {
    instituteId: lecture.instituteId,
    sessionId: lecture.sessionId,
    subjectId: lecture.subjectId,
    ttid: lecture.ttid,
  }
}

function isInteger(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value)
}

function isNonNegativeInteger(value: unknown): value is number {
  return isInteger(value) && value >= 0
}

function isFiniteNonNegative(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value) && value >= 0
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}
