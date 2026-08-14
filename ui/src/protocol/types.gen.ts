// Code generated from internal/tuiproto/protocol.schema.json. DO NOT EDIT.
// Regenerate with: go run scripts/gen-tui-protocol.go

/** Protocol identity every session request must declare. */
export const PROTOCOL_VERSION = "tui/v1" as const

/** Versioned public path prefix of the session contract. */
export const PROTOCOL_BASE_PATH = "/tui/v1" as const

/** Session protocol header name. */
export const CAPABILITY_HEADER = "X-Impartus-Capability" as const

/** Session protocol header name. */
export const PROTOCOL_HEADER = "X-Impartus-Protocol" as const

/** Session protocol header name. */
export const SUPPORTED_PROTOCOL_HEADER = "X-Impartus-Supported-Protocol" as const

/**
 * ArtifactList contains the current durable local lecture library.
 */
export interface ArtifactList {
  /**
   * Artifacts newest first in the store order.
   */
  artifacts: ArtifactSummary[]
}

/**
 * ArtifactSummary is one presentation-safe local library record without
 * exposing filesystem paths.
 */
export interface ArtifactSummary {
  /**
   * Canonical logical artifact identity.
   */
  artifactId: string

  /**
   * Number of materialized files recorded for the artifact.
   */
  fileCount: number

  /**
   * Number of recorded files currently marked present.
   */
  presentFileCount: number

  /**
   * UTC RFC3339 production timestamp.
   */
  producedAt: string

  /**
   * Human-facing lecture sequence number.
   */
  sequence: number

  /**
   * Lecture topic stored in the manifest.
   */
  topic: string

  /**
   * Total recorded bytes across materialized files.
   */
  totalBytes: number
}

/**
 * Bootstrap is the one-use private handoff from the Go parent to its
 * OpenTUI child.
 */
export interface Bootstrap {
  /**
   * Loopback URL of this one foreground session. Never contains
   * credentials.
   */
  baseUrl: string

  /**
   * Fresh per-launch capability read once from an owner-private bootstrap
   * file.
   */
  capability: string

  /**
   * Exact session protocol identity the child must send.
   */
  protocol: string

  /**
   * Opaque session identity used to reject stale bootstrap state.
   */
  sessionId: string
}

/**
 * Course is one read-only catalog course projected from the Go application
 * service.
 */
export interface Course {
  /**
   * Upstream institute identifier.
   */
  instituteId: number

  /**
   * Course owner as reported upstream.
   */
  professorName: string

  /**
   * Upstream session identifier.
   */
  sessionId: number

  /**
   * Academic session label.
   */
  sessionName: string

  /**
   * Upstream subject identifier.
   */
  subjectId: number

  /**
   * Human readable course name.
   */
  subjectName: string

  /**
   * Lecture count advertised upstream.
   */
  videoCount: number
}

/**
 * CourseList is the read-only catalog projection proving the frontend
 * reaches live Go state.
 */
export interface CourseList {
  /**
   * Courses in upstream order.
   */
  courses: Course[]
}

/**
 * Diagnostic is one non-blocking dependency or local-state preflight result
 * already scrubbed by the Go parent.
 */
export interface Diagnostic {
  /**
   * Safe human-readable result detail.
   */
  detail: string

  /**
   * Stable dependency or subsystem name.
   */
  name: string

  /**
   * Presentation status such as pass, warn, or fail.
   */
  status: string
}

/**
 * DiagnosticList contains startup diagnostics owned by the Go parent.
 */
export interface DiagnosticList {
  /**
   * Diagnostics in their stable collection order.
   */
  diagnostics: Diagnostic[]
}

/**
 * Event is one ordered session event. Sequence numbers increase
 * monotonically per session.
 */
export interface Event {
  /**
   * Coalesced playback duration when known.
   */
  durationSeconds?: number

  /**
   * Scrubbed human readable detail. Never carries upstream credentials.
   */
  message?: string

  /**
   * Current playback mute state when known.
   */
  muted?: boolean

  /**
   * Operation this event belongs to, when the event is operation scoped.
   */
  operationId?: string

  /**
   * Current playback pause state when known.
   */
  paused?: boolean

  /**
   * Coalesced progress percentage between 0 and 100.
   */
  percent?: number

  /**
   * Coalesced playback position when known.
   */
  positionSeconds?: number

  /**
   * Monotonic per-session sequence number.
   */
  sequence: number

  /**
   * Current playback speed when known.
   */
  speed?: number

  state?: OperationState

  type: EventType

  /**
   * Current playback volume percentage when known.
   */
  volume?: number
}

/**
 * EventType names the ordered session event kinds delivered over the event
 * stream.
 */
export type EventType = "session.ready" | "operation.started" | "operation.progress" | "operation.completed" | "operation.canceled" | "operation.failed" | "stream.overflow"

/**
 * Health is the session readiness probe answered before the frontend
 * renders anything.
 */
export interface Health {
  /**
   * Protocol identity this session speaks.
   */
  protocol: string

  /**
   * Opaque per-launch session identity. Not a credential.
   */
  sessionId: string

  status: HealthStatus

  /**
   * Parent impartus build version.
   */
  version: string
}

/**
 * HealthStatus is the aggregate session readiness value. The session never
 * reports which credentials are configured.
 */
export type HealthStatus = "ok"

/**
 * Lecture is the presentation-safe subset of one live Impartus lecture.
 */
export interface Lecture {
  /**
   * Classroom label reported upstream.
   */
  classroomName: string

  /**
   * Advertised lecture duration in seconds.
   */
  durationSeconds: number

  /**
   * Upstream institute identifier.
   */
  instituteId: number

  /**
   * Whether upstream marks this lecture as lacking audio.
   */
  noAudio: boolean

  /**
   * Lecture owner reported upstream.
   */
  professorName: string

  /**
   * Human-facing lecture sequence number.
   */
  sequence: number

  /**
   * Upstream session identifier.
   */
  sessionId: number

  /**
   * Academic session label.
   */
  sessionName: string

  /**
   * Lecture start time as supplied upstream.
   */
  startTime: string

  /**
   * Upstream subject identifier.
   */
  subjectId: number

  /**
   * Course name reported on the lecture.
   */
  subjectName: string

  /**
   * Lecture topic.
   */
  topic: string

  /**
   * Stable upstream lecture timetable identifier.
   */
  ttid: number

  /**
   * Number of camera views advertised upstream.
   */
  views: number
}

/**
 * LectureIdentity is the minimal authoritative identity accepted for
 * lecture mutations. The Go parent re-resolves the full live lecture before
 * acting.
 */
export interface LectureIdentity {
  /**
   * Upstream institute identifier.
   */
  instituteId: number

  /**
   * Upstream session identifier.
   */
  sessionId: number

  /**
   * Upstream subject identifier.
   */
  subjectId: number

  /**
   * Stable upstream lecture timetable identifier.
   */
  ttid: number
}

/**
 * LectureList contains the live lectures for one requested course identity.
 */
export interface LectureList {
  /**
   * Lectures in the application service order.
   */
  lectures: Lecture[]
}

/**
 * Operation is the handle returned when an operation is accepted or
 * inspected.
 */
export interface Operation {
  /**
   * Session-unique operation identifier.
   */
  id: string

  kind: OperationKind

  state: OperationState
}

/**
 * OperationKind names the operations the session may start. The bounded
 * foundation exposes only a transport self test.
 */
export type OperationKind = "selftest" | "download" | "playback"

/**
 * OperationRequest is the request body accepted when starting an operation.
 */
export interface OperationRequest {
  kind: OperationKind

  /**
   * Lecture identity required by lecture-scoped operations.
   */
  lecture?: LectureIdentity

  /**
   * Whether playback should use an existing durable resume checkpoint when
   * available.
   */
  resume?: boolean
}

/**
 * OperationState is one operation lifecycle state. Every state except
 * running is terminal.
 */
export type OperationState = "running" | "completed" | "canceled" | "failed"

/**
 * PlaybackCommand is one typed playback-control request. The action
 * determines whether flag or value is required.
 */
export interface PlaybackCommand {
  action: PlaybackCommandAction

  /**
   * Boolean value used by pause and mute.
   */
  flag?: boolean

  /**
   * Numeric value used by seek, volume, and speed.
   */
  value?: number
}

/**
 * PlaybackCommandAction names one bounded mpv control owned by the Go
 * playback session.
 */
export type PlaybackCommandAction = "pause" | "seek" | "mute" | "volume" | "speed" | "cycleVideo"

/**
 * Problem is the uniform session error body. It never discloses local state
 * or credentials.
 */
export interface Problem {
  /**
   * Stable machine readable failure code.
   */
  code: string

  /**
   * Short actionable failure summary.
   */
  error: string

  /**
   * Protocol identity this session speaks, sent with protocol upgrade
   * failures.
   */
  supportedProtocol?: string
}
