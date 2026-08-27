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
