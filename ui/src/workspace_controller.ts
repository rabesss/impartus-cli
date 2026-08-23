import type {
  ArtifactList,
  ArtifactSummary,
  AuthStatus,
  Course,
  CourseList,
  Diagnostic,
  DiagnosticList,
  Event,
  Health,
  Lecture,
  LectureList,
  OperationKind,
  OperationState,
} from "./protocol/types.gen.ts"
import { truncateGraphemes } from "./text_input.ts"

export type FoundationScreen = "courses" | "diagnostics" | "lectures" | "library" | "playback"
export type CollectionScreen = Exclude<FoundationScreen, "playback">

export interface FoundationOperation {
  durationSeconds: number
  id: string
  kind: OperationKind
  muted: boolean
  paused: boolean
  percent: number
  positionSeconds: number
  speed: number
  state: OperationState
  volume: number
}

export interface CollectionState {
  filter: string
  selected: number
}

export interface PendingRequest {
  courseKey: string | undefined
  generation: number
  target: CollectionScreen
}

export interface FoundationState {
  activeCourse: Course | undefined
  activeLecture: Lecture | undefined
  authStatus: AuthStatus
  artifacts: ArtifactSummary[]
  collections: Record<CollectionScreen, CollectionState>
  courses: Course[]
  diagnostics: Diagnostic[]
  error: string | undefined
  lectures: Lecture[]
  loading: boolean
  operation: FoundationOperation | undefined
  pending: PendingRequest | undefined
  screen: FoundationScreen
  status: string
}

export interface WorkspaceClient {
  artifacts(signal?: AbortSignal): Promise<ArtifactList>
  courses(signal?: AbortSignal): Promise<CourseList>
  diagnostics(signal?: AbortSignal): Promise<DiagnosticList>
  lectures(course: Course, signal?: AbortSignal): Promise<LectureList>
  retryAuthentication(signal?: AbortSignal): Promise<Health>
}

export type WorkspaceListener = (state: FoundationState) => void
export type FoundationStateOverrides = Partial<Omit<FoundationState, "collections">> & {
  collections?: Partial<Record<CollectionScreen, CollectionState>>
}

export class WorkspaceController {
  readonly #client: WorkspaceClient

  // The session can emit an operation event before the matching start
  // response reaches the UI. Keep the latest event per id only for that short
  // response window, then reconcile it when the operation is installed.
  readonly #earlyOperationEvents = new Map<string, Event>()
  readonly #listeners = new Set<WorkspaceListener>()
  #generation = 0
  #state: FoundationState

  public constructor(client: WorkspaceClient, initialState: FoundationState) {
    this.#client = client
    this.#state = cloneState(initialState)
  }

  public snapshot(): FoundationState {
    return cloneState(this.#state)
  }

  public subscribe(listener: WorkspaceListener): () => void {
    this.#listeners.add(listener)
    listener(this.snapshot())
    return () => this.#listeners.delete(listener)
  }

  public update(update: (state: FoundationState) => FoundationState): void {
    this.#set(update(this.snapshot()))
  }

  public setCollectionState(screen: CollectionScreen, state: CollectionState): void {
    this.#set({
      ...this.#state,
      collections: {
        ...this.#state.collections,
        [screen]: {
          filter: truncateGraphemes(state.filter, 120),
          selected: Math.max(0, Math.floor(state.selected)),
        },
      },
    })
  }

  public navigate(target: CollectionScreen, course?: Course, signal?: AbortSignal): boolean {
    if (this.#state.loading) return false
    if (this.#state.authStatus !== "ready" && (target === "courses" || target === "lectures")) return false
    if (target === "courses") void this.loadCourses(signal)
    else if (target === "lectures" && course !== undefined) void this.loadLectures(course, signal)
    else if (target === "library") void this.loadLibrary(signal)
    else if (target === "diagnostics") void this.loadDiagnostics(signal)
    else return false
    return true
  }

  public async retry(signal?: AbortSignal): Promise<void> {
    if (this.#state.loading) return
    if (this.#state.authStatus !== "ready") {
      await this.#retryAuthentication(signal)
      return
    }
    if (this.#state.screen === "lectures" && this.#state.activeCourse !== undefined) {
      await this.loadLectures(this.#state.activeCourse, signal)
    } else if (this.#state.screen === "library") {
      await this.loadLibrary(signal)
    } else if (this.#state.screen === "diagnostics") {
      await this.loadDiagnostics(signal)
    } else {
      await this.loadCourses(signal)
    }
  }

  public async loadCourses(signal?: AbortSignal): Promise<void> {
    if (this.#state.authStatus !== "ready") {
      this.#set({ ...this.#state, error: "Authentication is unavailable", status: "Authentication unavailable" })
      return
    }
    const request = this.#begin("courses")
    try {
      const result = await this.#client.courses(signal)
      this.#finish(request, (state) => ({
        ...state,
        courses: [...result.courses],
        error: undefined,
      }))
    } catch {
      this.#fail(request, "Course catalog is unavailable")
    }
  }

  public async loadLectures(course: Course, signal?: AbortSignal): Promise<void> {
    if (this.#state.authStatus !== "ready") {
      this.#set({ ...this.#state, error: "Authentication is unavailable", status: "Authentication unavailable" })
      return
    }
    const switchingCourse = this.#state.activeCourse === undefined || courseKey(this.#state.activeCourse) !== courseKey(course)
    const request = this.#begin("lectures", course)
    const collections = switchingCourse
      ? { ...this.#state.collections, lectures: { ...this.#state.collections.lectures, selected: 0 } }
      : this.#state.collections
    this.#set({ ...this.#state, activeCourse: course, collections, lectures: switchingCourse ? [] : this.#state.lectures, screen: "lectures" })
    try {
      const result = await this.#client.lectures(course, signal)
      this.#finish(request, (state) => ({
        ...state,
        lectures: [...result.lectures],
        error: undefined,
      }))
    } catch {
      this.#fail(request, "Lecture catalog is unavailable")
    }
  }

  public async loadLibrary(signal?: AbortSignal): Promise<void> {
    const request = this.#begin("library")
    try {
      const result = await this.#client.artifacts(signal)
      this.#finish(request, (state) => ({
        ...state,
        artifacts: [...result.artifacts],
        error: undefined,
      }))
    } catch {
      this.#fail(request, "Local lecture library is unavailable")
    }
  }

  public async loadDiagnostics(signal?: AbortSignal): Promise<void> {
    const request = this.#begin("diagnostics")
    try {
      const result = await this.#client.diagnostics(signal)
      this.#finish(request, (state) => ({
        ...state,
        diagnostics: [...result.diagnostics],
        error: undefined,
      }))
    } catch {
      this.#fail(request, "Diagnostics are unavailable")
    }
  }

  public abort(): void {
    this.#generation++
    this.#set({ ...this.#state, loading: false, pending: undefined })
  }

  public applyEvent(event: Event): void {
    if (event.type === "stream.overflow") {
      this.#set({ ...this.#state, status: "Refresh required" })
      return
    }
    const operation = this.#state.operation
    if (operation === undefined || event.operationId !== operation.id) {
      if (this.#state.loading && this.#state.pending === undefined && event.operationId !== undefined) {
        this.#earlyOperationEvents.set(event.operationId, event)
      }
      return
    }
    this.#set(applyOperationEvent(this.#state, event))
  }

  async #retryAuthentication(signal?: AbortSignal): Promise<void> {
    const target = this.#state.screen === "playback" ? "courses" : this.#state.screen
    const request = this.#begin(target)
    let health: Health
    try {
      health = await this.#client.retryAuthentication(signal)
    } catch {
      this.#fail(request, "Authentication is unavailable")
      return
    }
    if (health.authStatus !== "ready") {
      this.#fail(request, "Authentication is unavailable")
      return
    }
    if (!sameRequest(request, this.#state.pending)) return
    this.#set({ ...this.#state, authStatus: "ready", screen: "courses", status: "Connected" })
    try {
      const courses = await this.#client.courses(signal)
      this.#finish(request, (state) => ({
        ...state,
        courses: [...courses.courses],
        error: undefined,
      }))
    } catch {
      this.#fail(request, "Course catalog is unavailable")
    }
  }

  #begin(target: CollectionScreen, course?: Course): PendingRequest {
    const request: PendingRequest = {
      courseKey: course === undefined ? undefined : courseKey(course),
      generation: ++this.#generation,
      target,
    }
    this.#set({
      ...this.#state,
      error: undefined,
      loading: true,
      pending: request,
      screen: target,
    })
    return request
  }

  #finish(request: PendingRequest, apply: (state: FoundationState) => FoundationState): void {
    if (!sameRequest(request, this.#state.pending)) return
    this.#set({ ...apply(this.snapshot()), loading: false, pending: undefined })
  }

  #fail(request: PendingRequest, message: string): void {
    if (!sameRequest(request, this.#state.pending)) return
    this.#set({ ...this.#state, error: message, loading: false, pending: undefined })
  }

  #set(state: FoundationState): void {
    const operation = state.operation
    if (operation !== undefined) {
      const earlyEvent = this.#earlyOperationEvents.get(operation.id)
      if (earlyEvent !== undefined) {
        this.#earlyOperationEvents.delete(operation.id)
        state = applyOperationEvent(state, earlyEvent)
      }
    }
    if (!state.loading || state.pending !== undefined) this.#earlyOperationEvents.clear()
    this.#state = cloneState(state)
    for (const listener of this.#listeners) listener(this.snapshot())
  }
}

export function createFoundationState(overrides: FoundationStateOverrides = {}): FoundationState {
  const collections = defaultCollections()
  const { collections: collectionOverrides, ...stateOverrides } = overrides
  return cloneState({
    activeCourse: undefined,
    activeLecture: undefined,
    authStatus: "ready",
    artifacts: [],
    collections: { ...collections, ...collectionOverrides },
    courses: [],
    diagnostics: [],
    error: undefined,
    lectures: [],
    loading: false,
    operation: undefined,
    pending: undefined,
    screen: "courses",
    status: "Connected",
    ...stateOverrides,
  })
}

export function newOperation(id: string, kind: OperationKind, state: OperationState): FoundationOperation {
  return {
    durationSeconds: 0,
    id,
    kind,
    muted: false,
    paused: false,
    percent: 0,
    positionSeconds: 0,
    speed: 1,
    state,
    volume: 100,
  }
}

function defaultCollections(): Record<CollectionScreen, CollectionState> {
  return {
    courses: { filter: "", selected: 0 },
    diagnostics: { filter: "", selected: 0 },
    lectures: { filter: "", selected: 0 },
    library: { filter: "", selected: 0 },
  }
}

function cloneState(state: FoundationState): FoundationState {
  return {
    ...state,
    artifacts: [...state.artifacts],
    collections: {
      courses: { ...state.collections.courses },
      diagnostics: { ...state.collections.diagnostics },
      lectures: { ...state.collections.lectures },
      library: { ...state.collections.library },
    },
    courses: [...state.courses],
    diagnostics: [...state.diagnostics],
    lectures: [...state.lectures],
    operation: state.operation === undefined ? undefined : { ...state.operation },
    pending: state.pending === undefined ? undefined : { ...state.pending },
  }
}

function courseKey(course: Course): string {
  return `${course.instituteId}:${course.sessionId}:${course.subjectId}`
}

function sameRequest(left: PendingRequest, right: PendingRequest | undefined): boolean {
  return right !== undefined && left.generation === right.generation && left.target === right.target && left.courseKey === right.courseKey
}

function applyOperationEvent(state: FoundationState, event: Event): FoundationState {
  const operation = state.operation
  if (operation === undefined || event.operationId !== operation.id) return state
  return {
    ...state,
    operation: {
      ...operation,
      durationSeconds: event.durationSeconds ?? operation.durationSeconds,
      muted: event.muted ?? operation.muted,
      paused: event.paused ?? operation.paused,
      percent: event.percent ?? operation.percent,
      positionSeconds: event.positionSeconds ?? operation.positionSeconds,
      speed: event.speed ?? operation.speed,
      state: event.state ?? operation.state,
      volume: event.volume ?? operation.volume,
    },
    status: terminalStatus(operation.kind, event.state, event.message) ?? state.status,
  }
}

function terminalStatus(kind: OperationKind, state: OperationState | undefined, message: string | undefined): string | undefined {
  if (state === undefined || state === "running") return message
  const subject = kind === "download" ? "Download" : kind === "playback" ? "Playback" : "Session test"
  return message ?? `${subject} ${state}`
}
