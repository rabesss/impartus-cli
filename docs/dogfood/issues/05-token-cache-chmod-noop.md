---
title: [Bug]: failed token publish can leave a world-readable cache
labels: type: bug,type: security,priority: high,area: cli,area: config
---

# [Bug]: failed token publish can leave a world-readable cache

Dogfood. https://cursor.com/agents/bc-01a03c93-2ac2-7097-89bb-3bb80797d748

## Bug Description

`writeTokenCacheFile` writes a temp file, `Chmod(0600)`, renames onto the published path, then `validatePublishedTokenCache`. On a filesystem where `chmod` is a no-op, the published file is 0666 and validation fails after the token is already at the final path.

Dogfood hit this on `/opt/cursor/artifacts` (symlink to a store that ignores chmod). POSIX `/tmp/impartus-dogfood/token` stayed 0600.

## Steps to Reproduce

1. Point `IMPARTUS_TOKEN_CACHE` at a chmod-noop filesystem.
2. Run any command that logs in (`./impartus courses --json`).
3. `stat` the cache path. Mode is 0666. CLI reports a permission error.

## Expected Behavior

Refuse to publish. Leave no token at the destination path. Do not rename until the mode is 0600.

## Actual Behavior

A 200-byte world-readable cache remained under `/opt/cursor/artifacts/cli-dogfood/token-cache` until deleted. Validation error after publish.

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
- `/opt/cursor/artifacts/cli-dogfood/token-modes.txt`
- `.audit/cli-dogfood.tsv` row "moved live auth tests to /tmp"
- `internal/client/token_cache.go` `writeTokenCacheFile`
- `internal/client/token_cache_unix.go` `createTokenCacheTemp`, `replaceTokenCacheFile`, `validatePublishedTokenCache`

## Grouping

Standalone security fix. Stacked PR `PR-token-cache`. First in the stack. Do not recreate a token under `/opt/cursor/artifacts`.
