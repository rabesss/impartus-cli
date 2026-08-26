---
title: [Bug]: TUI help and footer hide keys and blocked-command reasons
labels: type: bug,priority: medium,area: cli
---

# [Bug]: TUI help and footer hide keys and blocked-command reasons

Dogfood. https://cursor.com/agents/bc-01a03c93-2ac2-7097-89bb-3bb80797d748

## Bug Description

`#helpOverlay` slices `commandsForHelp` to `overlay.height - 5`. In the 148x44 window, `q`, `l`, `!`, and `d` fell off the visible list.

`#footer` prints `command.keys[0]` only, then `.slice(0, 6)`. Move selection shows `up` and hides `down` / `j` / `k`. Playback keys other than the first never appear.

While a download runs, `s` is a silent no-op. `operationAvailability` has reason "An operation is already running". `footerCommands` drops disabled rows. `#onKeyPress` only dispatches enabled commands. Help can show the reason if T4 did not slice it away.

## Steps to Reproduce

1. Open the TUI. Press `?`. Count missing commands versus `workspace_commands.ts` `COMMANDS`.
2. Read the footer. Confirm only `up` for move.
3. Start a download. Press `s`. Nothing in the status dock.

## Expected Behavior

Help scrolls or pages. Footer shows the keys people need, or `?` is complete. A blocked `s` / `d` writes the availability reason into the dock.

## Actual Behavior

Truncated help. First-key footer. Silent no-ops.

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
- <img src="/opt/cursor/artifacts/tui_help_overlay.webp" alt="Help overlay truncated" />
- <img src="/opt/cursor/artifacts/tui_command_palette.webp" alt="Command palette" />
- <img src="/opt/cursor/artifacts/tui_tab_inspector_active.webp" alt="Footer with tab Next pane when inspector is active" />
- `ui/src/view.ts` `#helpOverlay`, `#footer`
- `ui/src/workspace_commands.ts` `footerCommands`, `operationAvailability`, `lectureAvailability`
- `ui/src/main.ts` `startSelfTest`, `startDownload`

## Grouping

T4 and T8 together. Same command chrome. Stacked PR `PR-tui-chrome`.
