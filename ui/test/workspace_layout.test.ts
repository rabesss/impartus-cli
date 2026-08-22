import { describe, expect, test } from "bun:test"

import {
  calculateLayout,
  effectiveFocus,
  moveFocus,
  visibleFocuses,
  type PaneFocus,
  type WorkspaceLayout,
} from "../src/workspace_layout.ts"

describe("workspace layout", () => {
  test("uses the qualified compact medium and wide modes", () => {
    expect(calculateLayout(40, 10, false).mode).toBe("compact")
    expect(calculateLayout(80, 24, false).mode).toBe("medium")
    expect(calculateLayout(140, 32, false).mode).toBe("wide")
    expect(calculateLayout(140, 18, false).mode).toBe("medium")
    expect(calculateLayout(80, 15, false).mode).toBe("compact")
  })

  test("preserves minimum pane widths", () => {
    const medium = calculateLayout(80, 24, false)
    expect(medium.collection.width).toBeGreaterThanOrEqual(40)
    expect(medium.inspector.width).toBeGreaterThanOrEqual(28)

    const wide = calculateLayout(140, 32, false)
    expect(wide.navigation.width).toBe(22)
    expect(wide.collection.width).toBeGreaterThanOrEqual(40)
    expect(wide.inspector.width).toBe(36)
  })

  test("never returns a negative or out-of-bounds rectangle", () => {
    for (let width = 1; width <= 140; width++) {
      for (let height = 1; height <= 32; height++) {
        assertValidLayout(calculateLayout(width, height, true), width, height)
      }
    }
  })

  test("reserves activity only when the terminal can keep an actionable body", () => {
    expect(calculateLayout(80, 24, true).activity.height).toBe(3)
    expect(calculateLayout(40, 10, true).activity.height).toBe(0)
  })

  test("falls focus back to a visible pane after resize", () => {
    const wide = calculateLayout(140, 32, true)
    expect(visibleFocuses(wide)).toEqual(["navigation", "collection", "inspector", "activity"])
    expect(effectiveFocus("inspector", wide)).toBe("inspector")
    expect(moveFocus("collection", 1, wide)).toBe("inspector")
    expect(moveFocus("collection", -1, wide)).toBe("navigation")

    const compact = calculateLayout(40, 10, false)
    expect(visibleFocuses(compact)).toEqual(["collection"])
    for (const focus of ["navigation", "inspector", "activity"] satisfies PaneFocus[]) {
      expect(effectiveFocus(focus, compact)).toBe("collection")
    }
  })
})

function assertValidLayout(layout: WorkspaceLayout, rawWidth: number, rawHeight: number): void {
  const width = Math.max(1, rawWidth)
  const height = Math.max(1, rawHeight)
  for (const rectangle of [
    layout.header,
    layout.navigation,
    layout.collection,
    layout.inspector,
    layout.activity,
    layout.footer,
  ]) {
    expect(rectangle.x).toBeGreaterThanOrEqual(0)
    expect(rectangle.y).toBeGreaterThanOrEqual(0)
    expect(rectangle.width).toBeGreaterThanOrEqual(0)
    expect(rectangle.height).toBeGreaterThanOrEqual(0)
    expect(rectangle.x + rectangle.width).toBeLessThanOrEqual(width)
    expect(rectangle.y + rectangle.height).toBeLessThanOrEqual(height)
  }
}
