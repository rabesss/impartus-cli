# Impartus TUI Workspace Brief

## Outcome

Evolve the existing single-pane Bubble Tea frontend into a responsive terminal
workspace inspired by OpenCode and other current terminal coding interfaces.
The result should feel materially more like a desktop application on a wide
terminal without sacrificing the current narrow-terminal workflow.

## Product boundaries

- Keep Go, Bubble Tea v2, Bubbles, Lip Gloss, and the existing
  `internal/tui` to `internal/app` boundary.
- Keep supervised mpv playback, library ownership, security redaction,
  operation tracking, and graceful terminal shutdown intact.
- Treat mpv as the video surface. Do not render video into terminal cells.
- Preserve existing direct-action keys during the first migration.
- Keep downloads synchronous in the first presentation/state-shell PR. A
  typed app-owned background job stream belongs in a later PR.
- Do not replace the framework, introduce Node or Zig runtimes, or mix in
  downloader, player, library, or dependency refactors.
- Do not add `go-tui`, its `.gsx` compiler, or another terminal event/rendering
  loop. `go-tui` is a competing framework rather than a Bubble Tea widget
  library; using both would duplicate terminal ownership, layout, focus, and
  rendering responsibilities.
- Reuse Bubbles widgets where they fit the product. Prefer its existing
  viewport, table, text input, spinner, and help primitives over rebuilding
  generic controls, while keeping product-specific rows and panes small and
  native to `internal/tui`.

## Responsive workspace

### Wide

Render a three-pane workspace:

1. Navigation: Courses, Library, Diagnostics.
2. Content: course, lecture, or library collection.
3. Inspector: selected item metadata and available actions.

Keep a persistent activity dock for playback, loading/download status, and
short-lived notices. Render contextual key hints below it.

### Medium

Render content and inspector together. Navigation becomes an overlay or
command-palette action rather than consuming a permanent pane.

### Compact

Preserve the routed single-pane interaction. Details, help, and commands use
full-width screens or overlays. Breadcrumbs and Escape provide orientation.

The exact breakpoints must be derived from minimum usable pane widths and
tested at 40x10, 80x24, and 140x32.

## Interaction model

- Explicit layout mode: compact, medium, wide.
- Explicit pane focus independent from the current domain screen.
- Modal overlay stack with focus capture and restoration.
- One command registry used by keyboard dispatch, command palette, help, and
  future mouse handling. Do not create parallel business-logic paths.
- Tab and Shift-Tab move between visible panes.
- Slash filters the focused collection.
- Ctrl-P opens the command palette.
- Question mark opens contextual help.
- Escape dismisses the top overlay before performing back navigation.
- Existing playback and direct-action keys remain available where applicable.

## Visual direction

- Dense, calm, and information-forward rather than decorative.
- Use borders and spacing to communicate pane ownership; never rely on color
  alone for focus or status.
- Prefer one restrained accent and terminal-adaptive neutrals.
- Use typography attributes available in terminals: bold, faint, underline,
  and inverse only when they retain readable no-color fallbacks.
- All widths are display-cell widths. Truncation must be Unicode-safe.
- Every color treatment needs a `NO_COLOR` and low-color fallback.
- Every new rendered remote error or metadata path must pass through the
  existing terminal sanitization and credential-redaction boundary.

## First-PR scope

The first coherent PR should deliver a visibly desktop-like responsive shell
without changing backend behavior:

- semantic layout calculations and style tokens;
- wide, medium, and compact rendering;
- navigation/content/inspector/activity/footer composition;
- pane focus and focus-aware key hints;
- command registry plus a minimal searchable palette;
- contextual help overlay;
- deterministic model and rendering tests.

It may split existing TUI files where that reduces ownership ambiguity, but it
must not turn presentation work into a repository-wide refactor.

## Acceptance gates

- Existing user flows remain available in compact mode.
- Wide mode shows navigation, current collection, and selected-item context at
  the same time.
- Medium and compact modes do not produce negative widths, wrapped borders, or
  clipped terminal-control sequences.
- Focus is visible without color and restored after closing an overlay.
- Late asynchronous results cannot update an unrelated pane or selection.
- Help and command availability reflect the focused pane and current state.
- Remote text and errors remain sanitized and secret-redacted.
- Alternate-screen teardown, operation waiting, and mpv lifecycle tests remain
  green.
- Golden coverage includes 40x10, 80x24, and 140x32 plus empty, loading,
  error, Unicode, and no-color states where those are newly affected.
- `go test ./...`, proportional race tests, `go vet ./...`, `make lint`, and
  supported cross-builds pass.
- `View()` remains pure: no network, SQLite, filesystem, or subprocess work.
- Collection rendering is proportional to visible terminal rows, not total
  catalogue size.
- Immutable base styles are constructed once rather than inside row loops.
- Informational benchmarks cover 80x24, 140x32, a large lecture catalogue, and
  playback updates. View generation stays comfortably below a 16.7ms frame
  budget on the qualification machine; timing is reported rather than used as
  a flaky wall-clock unit-test assertion.
- Before/after native binary size and benchmark allocations are recorded in the
  PR so any growth is explainable by used components.

## Deferred work

- Background download queues, progress streams, retry, and cancellation UI.
- Mouse input and persisted themes/preferences.
- Inline terminal graphics.
- A plugin system or framework migration.
- `go-tui`/GSX experimentation. Reconsider it only as a measured full frontend
  replacement if Bubble Tea proves unable to express a required interaction;
  never import both application loops into the same terminal process.
