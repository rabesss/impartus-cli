---
title: [Bug]: pressing / while filtering inserts a slash into the query
labels: type: bug,priority: low,area: cli
---

# [Bug]: pressing / while filtering inserts a slash into the query

Dogfood. https://cursor.com/agents/bc-01a03c93-2ac2-7097-89bb-3bb80797d748

## Bug Description

The first `/` opens the filter editor. A second `/` is printable input (`#handleFilterKey` + `printableInput`), so the query becomes `/med` when you type `med`. `normalizedQuery` looks for a literal `/med`. "Medical Devices" does not match. Escape keeps the filter. Clear it with `/` then backspace.

The first live `/` crash (exit 1) was the TimeoutError in the sibling issue, not this editor. After rebuild, `/` opened `Filter: █ enter apply esc close` and filtered.

## Steps to Reproduce

1. On the catalog, press `/`.
2. Press `/` again, then `med`.
3. See "No matching courses". Footer `Filter: /med`.
4. Escape. Filter stays applied.

## Expected Behavior

`/` while filtering is a no-op or toggles the editor closed. Typing `med` matches Medical Devices.

## Actual Behavior

<img src="/opt/cursor/artifacts/tui_filter_editing_no_match.webp" alt="Filter /med and no matching courses" />

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
- <img src="/opt/cursor/artifacts/tui_filter_applied_no_match.webp" alt="Applied filter /med, catalog empty" />
- `ui/src/view.ts` `#onKeyPress`, `#handleFilterKey`, `printableInput`
- `ui/test/view.test.ts` filters the live catalog (one `/`, then type)

## Grouping

Standalone. Stacked PR `PR-tui-filter-slash`.
