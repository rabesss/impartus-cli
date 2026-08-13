---
version: "alpha"
name: Impartus Terminal Workspace
description: Responsive, information-dense workspace for browsing, playing, and downloading lectures.
colors:
  primary: "#7DD3FC"
  canvas-dark: "#0B1014"
  surface-dark: "#111820"
  surface-selected-dark: "#1D2A36"
  text-dark: "#E6EDF3"
  text-muted-dark: "#94A3B8"
  border-dark: "#94A3B8"
  accent-dark: "#7DD3FC"
  success-dark: "#86EFAC"
  warning-dark: "#FDE68A"
  danger-dark: "#FCA5A5"
  canvas-light: "#F8FAFC"
  surface-light: "#F1F5F9"
  surface-selected-light: "#DBEAFE"
  text-light: "#0F172A"
  text-muted-light: "#475569"
  border-light: "#64748B"
  accent-light: "#0369A1"
  success-light: "#166534"
  warning-light: "#854D0E"
  danger-light: "#B91C1C"
typography:
  title:
    fontFamily: monospace
    fontSize: 1rem
    fontWeight: 700
    lineHeight: 1
  body:
    fontFamily: monospace
    fontSize: 1rem
    fontWeight: 400
    lineHeight: 1
  metadata:
    fontFamily: monospace
    fontSize: 1rem
    fontWeight: 400
    lineHeight: 1
  key-hint:
    fontFamily: monospace
    fontSize: 1rem
    fontWeight: 600
    lineHeight: 1
rounded:
  none: 0px
spacing:
  none: 0px
  cell: 1px
  compact: 2px
  comfortable: 3px
components:
  pane:
    backgroundColor: "{colors.canvas-dark}"
    textColor: "{colors.text-dark}"
    rounded: "{rounded.none}"
    padding: 1px
  pane-focused:
    backgroundColor: "{colors.canvas-dark}"
    textColor: "{colors.accent-dark}"
    rounded: "{rounded.none}"
    padding: 1px
  row-selected:
    backgroundColor: "{colors.surface-selected-dark}"
    textColor: "{colors.text-dark}"
    rounded: "{rounded.none}"
    padding: 0px
  activity-dock:
    backgroundColor: "{colors.surface-dark}"
    textColor: "{colors.text-dark}"
    rounded: "{rounded.none}"
    padding: 1px
  error:
    backgroundColor: "{colors.canvas-dark}"
    textColor: "{colors.danger-dark}"
    rounded: "{rounded.none}"
    padding: 0px
  metadata-dark:
    backgroundColor: "{colors.canvas-dark}"
    textColor: "{colors.text-muted-dark}"
    rounded: "{rounded.none}"
    padding: 0px
  border-dark:
    backgroundColor: "{colors.canvas-dark}"
    textColor: "{colors.border-dark}"
    rounded: "{rounded.none}"
    padding: 0px
  success-dark:
    backgroundColor: "{colors.canvas-dark}"
    textColor: "{colors.success-dark}"
    rounded: "{rounded.none}"
    padding: 0px
  warning-dark:
    backgroundColor: "{colors.canvas-dark}"
    textColor: "{colors.warning-dark}"
    rounded: "{rounded.none}"
    padding: 0px
  pane-light:
    backgroundColor: "{colors.canvas-light}"
    textColor: "{colors.text-light}"
    rounded: "{rounded.none}"
    padding: 1px
  pane-surface-light:
    backgroundColor: "{colors.surface-light}"
    textColor: "{colors.text-light}"
    rounded: "{rounded.none}"
    padding: 1px
  row-selected-light:
    backgroundColor: "{colors.surface-selected-light}"
    textColor: "{colors.text-light}"
    rounded: "{rounded.none}"
    padding: 0px
  metadata-light:
    backgroundColor: "{colors.canvas-light}"
    textColor: "{colors.text-muted-light}"
    rounded: "{rounded.none}"
    padding: 0px
  border-light:
    backgroundColor: "{colors.canvas-light}"
    textColor: "{colors.border-light}"
    rounded: "{rounded.none}"
    padding: 0px
  focus-light:
    backgroundColor: "{colors.canvas-light}"
    textColor: "{colors.accent-light}"
    rounded: "{rounded.none}"
    padding: 0px
  success-light:
    backgroundColor: "{colors.canvas-light}"
    textColor: "{colors.success-light}"
    rounded: "{rounded.none}"
    padding: 0px
  warning-light:
    backgroundColor: "{colors.canvas-light}"
    textColor: "{colors.warning-light}"
    rounded: "{rounded.none}"
    padding: 0px
  error-light:
    backgroundColor: "{colors.canvas-light}"
    textColor: "{colors.danger-light}"
    rounded: "{rounded.none}"
    padding: 0px
---

## Overview

Impartus is a terminal workspace, not a sequence of decorative full-screen
cards. It should feel calm, dense, and inspectable: the current collection,
selected lecture context, playback state, and available actions remain visible
whenever the terminal has room.

This document uses the Google DESIGN.md schema. CSS-like dimensions in the
front matter are compatibility tokens only. Within this TUI, `1px` maps to one
terminal display cell horizontally and one terminal row vertically. The prose
below is normative whenever browser-oriented token semantics do not translate
to a terminal.

## Colors

Use `lipgloss.AdaptiveColor` to pair the `*-light` and `*-dark` values. The
terminal background remains user-owned; canvas tokens describe contrast intent
and must not force a full-screen background fill.

- Accent is reserved for focus, the selected primary action, and active
  playback—not general decoration.
- Success, warning, and danger always include a textual label or icon with an
  ASCII fallback. Color is never the sole status signal.
- Muted text is for secondary metadata and contextual hints, never required
  content.
- When `NO_COLOR` is present or the output profile cannot represent the
  palette, remove foreground/background colors while retaining borders,
  prefixes, bold focus titles, and explicit status words.

## Typography

The terminal supplies the font. The typography tokens express only weight and
role:

- Title: bold pane titles, breadcrumb, and modal title.
- Body: lists, values, and primary content.
- Metadata: secondary values; faint is permitted only when it remains legible.
- Key hint: the key itself may be bold; its explanation remains normal weight.

Do not assume italics, font families, font sizes, or ligatures. Underline is
reserved for the current focus when color is unavailable. Inverse video may be
used for a selected row only if a plain `>` prefix remains present.

## Layout

All measurements use visible display cells after ANSI styling. Compute the
interior rectangle of every pane before rendering its content. Never repair a
broken layout by truncating the fully composed screen as a final step.

### Breakpoints

- Compact: width below 76 cells, or height below 16 rows.
- Medium: width from 76 through 119 cells with height of at least 16 rows.
- Wide: width of at least 120 cells with height of at least 20 rows.

The layout function is pure and returns non-negative rectangles. If a minimum
cannot be satisfied, fall back to the next smaller layout mode.

### Wide workspace

- Header: one row for product, breadcrumb, and compact health/activity text.
- Navigation pane: target 22 cells, minimum 18, maximum 26.
- Content pane: consumes remaining space, minimum 40 cells.
- Inspector pane: target 36 cells, minimum 28, maximum 44.
- Pane borders are one cell. Adjacent panes share a visual separator rather
  than rendering doubled borders.
- Activity dock: zero rows when truly empty, otherwise three rows. Active
  playback, synchronous download/loading, errors, and notices appear here.
- Contextual footer: one row, with hints clipped by complete command rather
  than halfway through a key label.

### Medium workspace

Keep content and inspector visible. Navigation is an overlay opened by a
command or palette action. Inspector target width is 34 percent of the usable
width with a minimum of 28 cells; content retains at least 40 cells. When that
cannot be satisfied, use compact mode.

### Compact workspace

Preserve the existing routed one-pane flow. Breadcrumbs orient the user.
Details, help, navigation, and commands use full-width overlays or screens.
The footer is a single concise row. The 40x10 state must remain actionable.

### Cell safety

- Use ANSI-aware and grapheme/display-width-aware measurement and truncation.
- Sanitize backend text before measuring it.
- Truncate labels before joining columns; do not cut border glyphs or escape
  sequences.
- Border glyphs may use Unicode. Tests and the no-color/plain fallback must
  remain understandable with `|`, `-`, and `+` equivalents.

## Elevation & Depth

Terminals have no shadows. Depth is conveyed by focus, borders, and overlay
occlusion. An overlay is rendered last inside a bounded rectangle and replaces
the content underneath; it must not change terminal ownership or start another
alternate screen.

Only one modal overlay is interactive at a time. Opening an overlay records the
previous pane focus; closing it restores that focus if the pane is still
visible, otherwise focus moves to the content pane.

## Shapes

Use square corners and single-cell borders. Rounded box-drawing characters are
allowed only if their ASCII fallback preserves the same geometry. Avoid nested
boxes inside every pane; one clear shell and one active overlay are enough.

Selected rows retain a `>` marker. Focused panes use a stronger title and
border plus the literal word `ACTIVE` in no-color snapshots where ambiguity
would otherwise remain.

## Components

### Header

Shows `Impartus`, a sanitized breadcrumb, and a right-aligned compact state.
Right content disappears before the breadcrumb becomes unusable.

### Navigation

Contains Courses, Library, and Diagnostics. The active section and focused pane
are distinct states. Switching sections invokes existing application actions;
it does not create a second data-loading path.

### Collection

Shows the current courses, lectures, library artifacts, or diagnostics. Each
domain retains its own selection and filter state. Loading, empty, and error
states occupy the pane body without destroying its title or navigation.

### Inspector

Shows sanitized metadata and only actions valid for the selected item. A course
selection summarizes professor/session/lecture count. A lecture selection shows
topic, professor, room, start, duration, and play/download actions. Library and
diagnostic records show their stable local fields. Empty selection has an
explicit message rather than a blank pane.

### Activity dock

Displays active mpv telemetry and controls, synchronous loading/download state,
or the latest sanitized status/error. Playback remains owned by mpv and the
existing player/application layers. This first shell does not invent a
background download queue or progress protocol.

### Command palette

`Ctrl-P` opens a searchable overlay backed by the same typed command registry
used by keyboard dispatch and help. Commands declare stable ID, label, keys,
context predicate, and action. Disabled commands remain visible only when the
reason is useful and non-sensitive.

### Contextual help

Question mark opens help for the current focus and overlay. The footer renders
the most important subset from the same registry. Escape closes the top overlay
before navigating back.

## Do's and Don'ts

Do:

- Preserve the `internal/tui` to `internal/app` boundary.
- Preserve direct keys while introducing pane focus and commands.
- Route every remote string and error through the existing terminal
  sanitization and credential-redaction boundary before styling or measuring.
- Keep compact mode functionally equivalent to the released single-pane flow.
- Add deterministic compact, medium, and wide state tests and goldens.
- Keep playback, library commits, operation tracking, and teardown semantics
  unchanged.

Don't:

- Render video in terminal cells or let mpv own the terminal.
- Add Node, Zig, OpenTUI, `go-tui`, GSX generation, a new top-level TUI
  framework, or a second terminal event/rendering system. Reuse Bubbles where
  it fits and keep Impartus-specific components on Bubble Tea and Lip Gloss.
- Parse CLI JSON or NDJSON inside the TUI.
- Add background download queues before an app-owned typed job interface exists.
- Use color alone for selection, focus, status, or errors.
- Append the new shell to an existing near-limit file; split by state,
  rendering, commands, and layout ownership.
- Change downloader, player, library, server, or API behavior as part of this
  presentation/state-shell work.
