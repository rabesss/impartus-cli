# Module Guide: Server

## Scope
- Directory: `internal/server`
- Owned paths: `internal/server/**`

## Purpose
- Runs the local HTTP/WebSocket API for auth, course/lecture lookups, and background download job orchestration.

## Interfaces / Contracts
- Server construction/start: `NewAPIServer`, `New`, `StartAPIServer`, `(*APIServer).Start`.
- Routes under `/api/v1`: public `/health`, `/auth/login`; protected `/ws`, `/courses`, `/lectures`, `/jobs`, `/jobs/{id}`.
- Job payload/contracts: `createJobRequest`, `JobConfigOptions`, `Job`, `JobRuntimeConfig`.
- WebSocket event types: `job.started`, `job.progress`, `job.completed`, `job.failed`, `job.cancelled`.

## Data / Types
- Keep shared/public types centralized (avoid duplicate local shape drift).
- Prefer explicit types over `any` and implicit contracts.

## Local Quality Bar
- Keep files small and readable (target <= 300 lines per file).
- Avoid very large flat folders (target <= 20 direct files per folder).
- Add or update tests when behavior changes.
- Keep auth flow consistent: `/auth/login` validates against local config credentials; protected routes require `Authorization: Bearer <token>`.
- Keep job index semantics explicit and stable (`startIndex/endIndex` are 1-based, matching CLI `--start/--end`, end-inclusive). Internally converted to 0-based for storage/execution.
- Validate per-job overrides through `mergeConfigWithJobOptions` before creating a job.

## Agent Rules (Enforced)
- Read this file before editing files in this module.
- If behavior/interfaces/contracts change, update this file in the same change.
- Keep changes scoped to this module unless cross-module contract changes are required.
