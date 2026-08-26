---
title: [Bug]: library opens the store before rejecting a bad subcommand
labels: type: bug,priority: low,area: cli
---

# [Bug]: library opens the store before rejecting a bad subcommand

Dogfood. https://cursor.com/agents/bc-01a03c93-2ac2-7097-89bb-3bb80797d748

## Bug Description

`executeLibrary` rejects empty argv, then `library.Open` (mkdir + SQLite migrate), then the subcommand switch. `library vrfy` can create `library.db` before "unknown command".

`--help` is safe. `Execute` never calls `executeLibrary` for help.

## Steps to Reproduce

1. Point `XDG_STATE_HOME` at a fresh temp dir.
2. `./impartus library vrfy`
3. `ls $XDG_STATE_HOME/impartus/library.db`

## Expected Behavior

Unknown subcommand, no store file.

## Actual Behavior

Error string is right. The database already exists.

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
- `internal/cli/cli_library.go` `executeLibrary`
- `internal/library/db.go` `Open`

## Grouping

Standalone. Stacked PR `PR-library-argv`.
