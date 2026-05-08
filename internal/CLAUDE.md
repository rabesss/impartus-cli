# Module Guide: Internal

## Scope
- Directory: `internal`
- Owned paths: `internal/**`

## Purpose
- Core runtime package group for CLI/API orchestration: config loading, Impartus HTTP access, media download/join, and API job execution.

## Go Version
- **Go 1.25.0** is required (`go.mod`). CI uses `actions/setup-go@v6` with the version from `go.mod`.

## CI Workflows
Changes to `internal/` trigger the full CI suite (lint, test, build) as well as security scanning (`security.yml`) and PR title validation (`lint-pr.yml`).

## Interfaces / Contracts
- `internal/cli.Execute(version, date)` is the CLI entrypoint used by binaries.
- `internal/config` owns config parsing/defaulting/validation (`Parse`, `Load`, `Get`). Config includes:
  - Core: `Username`, `Password`, `BaseUrl`, `Quality`, `Views`, `DownloadLocation`, `TempDirLocation`
  - Concurrency: `NumWorkers`, `RateLimit`, `APIRateLimit`, `EnableJitter`
  - Pipeline: `EnablePipeline`, `DownloadWorkersPerLecture`, `DecryptWorkersPerLecture`
  - Progress: `ProgressTracking` with `Enabled`, `ShowSpeed`, `ShowETA`, `UpdateInterval`, `SpeedWindowSize`
  - HTTP: `HTTPTimeout`
- `internal/client` owns Impartus auth/data fetch contracts (`LoginAndSetToken`, `GetCourses`, `GetLectures`, `GetPlaylists`).
- `internal/downloader` owns playlist/chunk download+decrypt+join contracts. Supports pipelined execution (`EnablePipeline=true`) and sequential fallback.
- `internal/server` owns `/api/v1` HTTP+WebSocket contracts and async job lifecycle.

## Data / Types
- Keep shared/public types centralized (avoid duplicate local shape drift).
- Prefer explicit types over `any` and implicit contracts.

## Local Quality Bar
- Keep files small and readable (target <= 300 lines per file).
- Avoid very large flat folders (target <= 20 direct files per folder).
- Add or update tests when behavior changes.
- Keep cross-module index semantics explicit: CLI ranges are 1-based, API job ranges are 0-based inclusive.
- Normalize view aliases (`first/second` vs `left/right`) at module boundaries, not ad hoc in deep call paths.

## Agent Rules (Enforced)
- Read this file before editing files in this module.
- If behavior/interfaces/contracts change, update this file in the same change.
- Keep changes scoped to this module unless cross-module contract changes are required.
