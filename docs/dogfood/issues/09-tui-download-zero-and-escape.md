---
title: [Bug]: TUI download stays at 0% and Escape does not cancel it
labels: type: bug,priority: high,area: cli,area: downloader
---

# [Bug]: TUI download stays at 0% and Escape does not cancel it

Dogfood. https://cursor.com/agents/bc-01a03c93-2ac2-7097-89bb-3bb80797d748

## Bug Description

`d` on lecture 015 started a real download. After 55s the temp dir had 32 `.ts` chunks (~32M). The activity dock still said `download running 0%`. `runDownload` never publishes `EventTypeOperationProgress`. Self-test and playback do.

Escape on the lecture list does not cancel. `goBack` only `cancelOperation`s when `screen === "playback"` and `kind === "playback"`. Leaving the list left the yellow "Downloading" status on the catalog. `q` or the TimeoutError from the sibling issue cancels the job via session close, then deletes temp files. Library stayed empty.

Overlapping `impartus play` of the same lecture while this download ran opened mpv as a black window. Solo play showed video. That is bandwidth contention, not a second player bug.

## Steps to Reproduce

1. Open the TUI with mpv installed. Enter a course. Highlight a lecture with audio.
2. Press `d`. Watch the dock.
3. `du -sh $IMPARTUS_TEMP_DIR` in another shell.
4. Press Escape. Confirm the dock still says running.
5. Press `s`. No status line explains that self-test is blocked.

## Expected Behavior

Percent tracks bytes or playlist progress. Escape cancels the running download. The dock says so.

## Actual Behavior

0% forever. Escape navigates. Chunks keep landing until the session dies.

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
- <img src="/opt/cursor/artifacts/tui_download_started_lecture015.webp" alt="Lecture 015 download running 0 percent" />
- <img src="/opt/cursor/artifacts/tui_download_running_0pct.webp" alt="Download status on the lecture list" />
- <img src="/opt/cursor/artifacts/tui_library_while_download_0pct.webp" alt="Library empty while download runs" />
- <img src="/opt/cursor/artifacts/tui_and_cli_play_mpv_black.webp" alt="mpv black while TUI download of the same lecture ran" />
- `/opt/cursor/artifacts/tui_download_chunk_progress.log`
- `internal/tuisession/operations.go` `runDownload` versus `runSelfTest` and `playbackTelemetry.event`
- `internal/app/frontend.go` `DownloadLecture`
- `ui/src/main.ts` `goBack`, `startDownload`
- `internal/downloader/progress_tracker.go` (CLI path only)

## Grouping

T2 (progress) and T3 (cancel) in one issue. Two PRs. `PR-tui-download-progress` then `PR-tui-download-cancel`. Cancel must not wait on a progress callback.
