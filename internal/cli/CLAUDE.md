# Module Guide: Cli

## Scope
- Directory: `internal/cli`
- Owned paths: `internal/cli/**`

## Purpose
- Implements command dispatch, interactive flow, and deterministic `--json` output envelopes for automation.

## Interfaces / Contracts
- `Execute(version, date)` dispatches `help|version|courses|lectures|download|serve`.
- JSON mode returns `{success,data,error,meta{command,mode}}` envelopes across commands.
- `lectures` and `download` require `--subject/-s` and `--session/-S`; `download --start/--end` is 1-based.
- `download` supports: `--quality`, `--views`, `--audio-only`, `--format`, `--output/-o` overrides.
- `serve --json` reports startup metadata only; non-JSON `serve` starts the blocking API server.

## Data / Types
- Keep shared/public types centralized (avoid duplicate local shape drift).
- Prefer explicit types over `any` and implicit contracts.

## Local Quality Bar
- Keep files small and readable (target <= 300 lines per file).
- Avoid very large flat folders (target <= 20 direct files per folder).
- Add or update tests when behavior changes.
- Preserve exact error/envelope behavior because `cli_test.go` asserts deterministic strings and shape.
- Keep view normalization (`first->left`, `second->right`) aligned with downloader/server contracts.

## Agent Rules (Enforced)
- Read this file before editing files in this module.
- If behavior/interfaces/contracts change, update this file in the same change.
- Keep changes scoped to this module unless cross-module contract changes are required.
