---
title: [Bug]: wide-layout g leaves focus stuck on Navigation
labels: type: bug,priority: medium,area: cli
---

# [Bug]: wide-layout g leaves focus stuck on Navigation

Dogfood. https://cursor.com/agents/bc-01a03c93-2ac2-7097-89bb-3bb80797d748

## Bug Description

At 148x44 the layout is wide. Navigation is always visible. `g` sets `#focus = "navigation"` instead of opening the overlay. Compact `g` uses `#openOverlay` and Escape restores `previousFocus`.

Wide Escape runs `navigation.back` → `goBack`. On the course catalog that is a no-op. `#focus` stays on Navigation. Tab moves `[ACTIVE]` when the VTE has keyboard focus. If the GTK menu bar stole Tab, the session looks stuck.

## Steps to Reproduce

1. Run the TUI in a terminal at least 120 columns wide.
2. Click the TUI body, not File/Edit.
3. Press `g`. Navigation shows `> Courses`.
4. Press Escape. Focus stays on Navigation.
5. Press Tab. `[ACTIVE]` should move to Inspector if GTK did not steal Tab.

## Expected Behavior

Escape returns collection focus, matching compact overlay restore.

## Actual Behavior

Navigation stays focused. Footer drops `/` because `filterAvailability` requires collection focus.

## Version

0.1.28

## Environment

- OS: Ubuntu (Cursor Cloud Agent XFCE desktop, Linux 6.12)
- Binary: /workspace/impartus 0.1.28, build 2026-08-26T13:59:47Z
- ffmpeg: present
- mpv: absent in run 1, /usr/bin/mpv 0.37.0 in run 2
- bun: absent in run 1, 1.4.0 in run 2
- Config: environment-only IMPARTUS_USERNAME, IMPARTUS_PASSWORD, IMPARTUS_BASE_URL. No config.json.
- State: XDG_STATE_HOME=/tmp/impartus-dogfood/state, IMPARTUS_TOKEN_CACHE=/tmp/impartus-dogfood/token (0600)

## Media and source

- Run 2. https://cursor.com/agents/bc-01a03c93-2ac2-7097-89bb-3bb80797d748
- <img src="/opt/cursor/artifacts/tui_wide_navigation_focus.webp" alt="Wide layout with Navigation focused after g" />
- <img src="/opt/cursor/artifacts/tui_tab_inspector_active.webp" alt="Tab moved ACTIVE to Inspector" />
- `ui/src/view.ts` `#dispatch` `navigation.open` wide branch
- `ui/src/workspace_layout.ts` `WIDE_WIDTH` 120
- `ui/src/workspace_commands.ts` `backAvailability`

## Grouping

Standalone. Stacked PR `PR-tui-nav-focus`. Do not fold into `goBack` download cancel.
