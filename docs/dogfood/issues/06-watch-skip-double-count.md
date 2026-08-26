---
title: [Bug]: watch dry-run JSON counts a budgeted lecture as both new and skipped
labels: type: bug,priority: medium,area: cli
---

# [Bug]: watch dry-run JSON counts a budgeted lecture as both new and skipped

Dogfood. https://cursor.com/agents/bc-01a03c93-2ac2-7097-89bb-3bb80797d748

## Bug Description

Default `maxLecturesPerCycle` is 3. For 6 audio lectures, dry-run JSON reported listed=6, new=6, skipped=3.

`inspectAndProcess` sets `New: 1`, then also `Skipped: 1` when `!withinBudget`. `RunCycle` sums both. "Skipped" also means already committed, so the JSON cannot tell a budget tail from a duplicate.

`TestWatcherDryRunAppliesGlobalBudgetBeforePlaylistFetch` currently asserts `New == 2 && Skipped == 1` for budget 1 and two new lectures. That test encodes the bug.

## Steps to Reproduce

1. `./impartus watch --dry-run --json` against a course with more than 3 new audio lectures.
2. Read `data.cycle.new` and `data.cycle.skipped`.

## Expected Behavior

A lecture over budget is skipped, not also new. Or the JSON grows a third field and stops overloading `skipped`.

## Actual Behavior

new=6 and skipped=3 on a 6-new-lecture course with budget 3. Watch listed=6 versus 15 total lectures is the noaudio filter, not this bug.

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

- Run 1. `/opt/cursor/artifacts/cli-dogfood` watch dry-run JSON (redacted copies under `/tmp/impartus-dogfood/wave2/`)
- `internal/watch/watch.go` `inspectAndProcess`, `RunCycle`, `CycleResult`
- `internal/watch/watch_budget_test.go` `TestWatcherDryRunAppliesGlobalBudgetBeforePlaylistFetch`

## Grouping

Standalone. Stacked PR `PR-watch-skip`. Update the budget test in the same PR.
