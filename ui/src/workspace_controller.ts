import type {
  ArtifactList,
  ArtifactSummary,
  Course,
  CourseList,
  Diagnostic,
  DiagnosticList,
  Event,
  Lecture,
  LectureList,
  OperationKind,
  OperationState,
} from "./protocol/types.gen.ts"

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
}

export type WorkspaceListener = (state: FoundationState) => void

export class WorkspaceController {
  readonly #client: WorkspaceClient
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
          filter: state.filter.slice(0, 120),
          selected: Math.max(0, Math.floor(state.selected)),
        },
      },
    })
  }

  public navigate(target: CollectionScreen, course?: Course, signal?: AbortSignal): boolean {
    if (this.#state.loading) return false
    if (target === "courses") void this.loadCourses(signal)
    else if (target === "lectures" && course !== undefined) void this.loadLectures(course, signal)
    else if (target === "library") void this.loadLibrary(signal)
    else if (target === "diagnostics") void this.loadDiagnostics(signal)
    else return false
    return true
  }

  public async retry(signal?: AbortSignal): Promise<void> {
    if (this.#state.loading) return
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
    const request = this.#begin("lectures", course)
    this.#set({ ...this.#state, activeCourse: course, screen: "lectures" })
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
    if (operation === undefined || event.operationId !== operation.id) return
    this.#set({
      ...this.#state,
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
      status: terminalStatus(operation.kind, event.state, event.message) ?? this.#state.status,
    })
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
    this.#state = cloneState(state)
    for (const listener of this.#listeners) listener(this.snapshot())
  }
}

export function createFoundationState(overrides: Partial<FoundationState> = {}): FoundationState {
  const collections = defaultCollections()
  return cloneState({
    activeCourse: undefined,
    activeLecture: undefined,
    artifacts: [],
    collections: { ...collections, ...overrides.collections },
    courses: [],
    diagnostics: [],
    error: undefined,
    lectures: [],
    loading: false,
    operation: undefined,
    pending: undefined,
    screen: "courses",
    status: "Connected",
    ...overrides,
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

function terminalStatus(kind: OperationKind, state: OperationState | undefined, message: string | undefined): string | undefined {
  if (state === undefined || state === "running") return message
  const subject = kind === "download" ? "Download" : kind === "playback" ? "Playback" : "Session test"
  return message ?? `${subject} ${state}`
}
