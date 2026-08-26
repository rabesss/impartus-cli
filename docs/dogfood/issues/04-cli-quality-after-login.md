---
title: [Bug]: invalid --quality is validated after login
labels: type: bug,priority: medium,area: cli
---

# [Bug]: invalid --quality is validated after login

Dogfood. https://cursor.com/agents/bc-01a03c93-2ac2-7097-89bb-3bb80797d748

## Bug Description

`applyAndValidateFlags` says fail before remote work. `executeDownloadWithDependenciesContext` still does parse → `ensureFFmpeg` → `initClient` (login) → `applyAndValidateFlags`. `runPlay` uses the same order. Bad `--quality` therefore hits the network first.

## Steps to Reproduce

1. `./impartus download --subject 1 --session 1 --quality 1080 --json`
2. Inspect stderr timestamps relative to token use.

## Expected Behavior

Reject `1080` before `NewLoggedIn`.

## Actual Behavior

```
{"success":false,"data":null,"error":{"message":"invalid quality value \"1080\": must be one of: 144, 450, 720"},"meta":{"command":"download","mode":"json"}}
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

- Run 1. https://cursor.com/agents/bc-01a03c93-2ac2-7097-89bb-3bb80797d748
- `/opt/cursor/artifacts/cli-dogfood/posix/download_bad_quality.stderr`
- `internal/cli/cli_download.go` `executeDownloadWithDependenciesContext`, `applyAndValidateFlags`
- `internal/cli/cli_helpers.go` `validateFlagOverrides`
- `internal/cli/cli_play.go` `runPlay`

## Grouping

Standalone. Stacked PR `PR-cli-quality`. Also fix play.
