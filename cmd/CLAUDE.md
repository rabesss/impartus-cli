# Module Guide: Cmd

## Scope
- Directory: `cmd`
- Owned paths: `cmd/**`

## Purpose
- This is the application entrypoint module. The `cmd/impartus/main.go` file bootstraps the CLI by calling `cli.Execute(buildinfo.Version, buildinfo.Date)` from `internal/cli`. It handles process-level concerns (exit codes, stderr output) and delegates all command routing and flag parsing to the `cli` package.

## Interfaces / Contracts
- `main()` — reads no arguments; calls `cli.Execute(version, date)` and exits with code 1 on error.

## Data / Types
- Keep shared/public types centralized (avoid duplicate local shape drift).
- Prefer explicit types over `any` and implicit contracts.

## Local Quality Bar
- Keep files small and readable (target <= 300 lines per file).
- Avoid very large flat folders (target <= 20 direct files per folder).
- Add or update tests when behavior changes.

## Agent Rules (Enforced)
- Read this file before editing files in this module.
- If behavior/interfaces/contracts change, update this file in the same change.
- Keep changes scoped to this module unless cross-module contract changes are required.
