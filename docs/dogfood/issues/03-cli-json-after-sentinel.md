---
title: [Bug]: --json after -- still enables JSON mode
labels: type: bug,priority: medium,area: cli
---

# [Bug]: --json after -- still enables JSON mode

Dogfood. https://cursor.com/agents/bc-01a03c93-2ac2-7097-89bb-3bb80797d748

## Bug Description

`stripGlobalJSONFlag` walks the whole argv and drops every exact `--json` before sentinel handling. Help and `--events` already stop at `--`. `--json` does not.

`./impartus download --subject 1 --session 2 -- --json` therefore runs in JSON mode. `--help` after `--` stays positional.

## Steps to Reproduce

1. `./impartus download --subject 1 --session 2 -- --json`
2. Compare `./impartus download --help` versus `./impartus download -- --help`

## Expected Behavior

`--json` after `--` is positional, like `--help`.

## Actual Behavior

JSON envelope. Fake ids returned "no lectures found" in JSON mode.

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
- `internal/cli/cli_json.go` `stripGlobalJSONFlag`
- Contrast `hasHelpBeforeSentinel` and `requestedEvents`

## Grouping

Standalone. Stacked PR `PR-cli-json`. File boundary `internal/cli/cli_json.go`. Land before `PR-cli-help` if both touch JSON help payloads.
