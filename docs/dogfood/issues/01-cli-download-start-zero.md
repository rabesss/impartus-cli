---
title: [Bug]: download --start 0 silently selects the whole course
labels: type: bug,priority: high,area: cli,area: downloader
---

# [Bug]: download --start 0 silently selects the whole course

Dogfood. https://cursor.com/agents/bc-01a03c93-2ac2-7097-89bb-3bb80797d748

## Bug Description

`impartus play` rejects a non-positive lecture index. `impartus download --start 0` does not. `parseDownloadFlags` records `startSet` but never requires `>= 1`. `SelectRange` then rewrites `start <= 0` to 1 and `end <= 0` to the last lecture, so `--start 0` means "the whole audio-bearing course".

This is a live-data footgun. One mistyped flag starts `fetchvideo` across every lecture.

## Steps to Reproduce

1. Build `./impartus` 0.1.28 with live `IMPARTUS_*` credentials.
2. Pick a real `--subject` / `--session`.
3. Run `./impartus download --subject <id> --session <id> --start 0 --json`.
4. Watch stderr. Kill the PID when `fetchvideo` starts. Do not let it finish.

## Expected Behavior

Reject a non-positive 1-based `--start` / `--end` before login, playlist fetch, or media work. Same rule as `--ttid` and the HTTP `startIndex < 1` check in `createJob`.

## Actual Behavior

JSON download started. `fetchvideo` ran. SIGTERM produced exit 130. Completion of every lecture was not observed on purpose.

Do not re-run `--start 0` against a live course.

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

## Configuration (sanitized)

Environment-only. No config.json.

## Relevant Logs

```
{"success":false,"data":null,"error":{"message":"request failed, host-redacted fetchvideo context canceled"},"meta":{"command":"download","mode":"json"}}
```

Harness case lives in `/opt/cursor/artifacts/cli-dogfood/dogfood_cli.py`. Decision row in `.audit/cli-dogfood.tsv` (killed PID 6021).

## Media and source

- Run 1 (CLI, no mpv). https://cursor.com/agents/bc-01a03c93-2ac2-7097-89bb-3bb80797d748
- `internal/cli/cli_download_flags.go` `parseDownloadFlags`
- `internal/client/types.go` `SelectRange`, `SelectForDownload`
- Contrast `internal/cli/cli_play.go` `validatePlayFlags` and `internal/server/handlers.go` `createJob`

## Grouping

Standalone. Stacked PR `PR-cli-start`.
