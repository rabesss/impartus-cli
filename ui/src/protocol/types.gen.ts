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
 * Event is one ordered session event. Sequence numbers increase
 * monotonically per session.
 */
export interface Event {
  /**
   * Scrubbed human readable detail. Never carries upstream credentials.
   */
  message?: string

  /**
   * Operation this event belongs to, when the event is operation scoped.
   */
  operationId?: string

  /**
   * Coalesced progress percentage between 0 and 100.
   */
  percent?: number

  /**
   * Monotonic per-session sequence number.
   */
  sequence: number

  state?: OperationState

  type: EventType
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
export type OperationKind = "selftest"

/**
 * OperationRequest is the request body accepted when starting an operation.
 */
export interface OperationRequest {
  kind: OperationKind
}

/**
 * OperationState is one operation lifecycle state. Every state except
 * running is terminal.
 */
export type OperationState = "running" | "completed" | "canceled" | "failed"

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
