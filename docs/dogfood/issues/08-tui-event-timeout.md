---
title: [Bug]: TUI dies with TimeoutError after a few idle minutes
labels: type: bug,priority: critical,area: cli
---

# [Bug]: TUI dies with TimeoutError after a few idle minutes

Dogfood. https://cursor.com/agents/bc-01a03c93-2ac2-7097-89bb-3bb80797d748

## Bug Description

The OpenTUI session dies with exit 1 after about 5 to 6 minutes. `SessionClient.events` is one long `fetch` of `/events`. The hub writes `data:` events only. There are no SSE comments. Download publishes no progress, so a long download goes quiet. Bun then throws `DOMException TimeoutError`. `runInteractive` races `consumeEvents` and destroys the renderer. `main().catch` prints only the startup stage, so the first crash looked like a `/` key bug.

Live log after a temporary stderr dump (reverted, not shipped):

```
2026-08-26T14:49:06.577Z impartus-ui: terminal frontend failed during interactive renderer
[DOMException [TimeoutError]: The operation timed out.]
2026-08-26T14:57:36.537Z impartus-ui: terminal frontend failed during interactive renderer
[DOMException [TimeoutError]: The operation timed out.]
```

Two sessions. 14:44:05 start → 14:49:06 exit. 14:51:35 start → 14:57:36 exit. In-flight download jobs in `library.db` finished as `canceled`. Temp dir wiped. `downloads/` empty.

`#request` maps fetch failure to "UI session is unavailable", so even a kept stack would lie.

## Steps to Reproduce

1. `make build` so `impartus` and `impartus-ui` sit next to each other.
2. Run `./impartus tui` in a real TTY (stdout must be a TTY).
3. Sit on the catalog, or start a download with `d`.
4. Wait about five minutes with no playback progress events.

## Expected Behavior

The event stream stays alive (SSE comments or download progress). A timeout is not a fatal renderer crash. stderr prints the real error.

## Actual Behavior

```
impartus-ui: terminal frontend failed during interactive renderer
OpenTUI frontend exited: exit status 1
```

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
- <img src="/opt/cursor/artifacts/tui_filter_crash_exit.webp" alt="Terminal after TUI exited 1" />
- <img src="/opt/cursor/artifacts/tui_download_zero_percent_while_fetching.webp" alt="Download still at 0 percent when the session later died" />
- `/opt/cursor/artifacts/tui_timeout_crash.log`
- `/opt/cursor/artifacts/tui_download_chunk_progress.log`
- `ui/src/client.ts` `events`, `#request`
- `ui/src/main.ts` `consumeEvents`, `runInteractive`, `main().catch`
- `internal/tuisession/events.go` `writeSSEEvent`
- `internal/tuisession/session.go` `readTimeout`, `idleTimeout` (not the idle killer)

## Grouping

T1 (timeout) and T5 (swallowed error) together. Same crash path. Stacked PR `PR-tui-events`.
