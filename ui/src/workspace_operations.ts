import type { Lecture } from "./protocol/types.gen.ts"
import type { FoundationState } from "./workspace_controller.ts"

export type OperationStartKind = "download" | "playback" | "selftest"

export function beginPlaybackStart(state: FoundationState, lecture: Lecture): FoundationState {
  return {
    ...state,
    activeLecture: lecture,
    error: undefined,
    loading: true,
    operation: undefined,
    screen: "playback",
    status: `Starting ${lecture.topic}`,
  }
}

export function failOperationStart(state: FoundationState, kind: OperationStartKind): FoundationState {
  if (kind === "playback") {
    return { ...state, error: "Lecture playback could not start", loading: false, status: "Lecture playback could not start" }
  }
  if (kind === "download") {
    return { ...state, error: undefined, loading: false, status: "Lecture download could not start" }
  }
  return { ...state, loading: false, status: "Connection failed" }
}

export async function cancelOperationBeforeBack(
  state: FoundationState,
  cancel: (identifier: string) => Promise<unknown>,
): Promise<void> {
  const operation = state.operation
  if (operation?.state !== "running") return
  const shouldCancel = operation.kind === "download" || (state.screen === "playback" && operation.kind === "playback")
  if (!shouldCancel) return
  await cancel(operation.id).catch(() => undefined)
}

export function completeBackNavigation(start: FoundationState, current: FoundationState): FoundationState {
  if (!sameBackOrigin(start, current) || current.loading || current.pending !== undefined) return current
  if (current.screen === "playback") {
    return { ...current, error: undefined, loading: false, screen: "lectures" }
  }
  if (current.screen === "lectures") {
    return { ...current, error: undefined, screen: "courses" }
  }
  return { ...current, error: undefined, screen: current.activeCourse === undefined ? "courses" : "lectures" }
}

function sameBackOrigin(start: FoundationState, current: FoundationState): boolean {
  if (start.screen !== current.screen) return false
  if (start.activeCourse === undefined || current.activeCourse === undefined) {
    return start.activeCourse === undefined && current.activeCourse === undefined
  }
  return start.activeCourse.instituteId === current.activeCourse.instituteId
    && start.activeCourse.sessionId === current.activeCourse.sessionId
    && start.activeCourse.subjectId === current.activeCourse.subjectId
}
