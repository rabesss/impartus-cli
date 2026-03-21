# Module Guide: Downloader

## Scope
- Directory: `internal/downloader`
- Owned paths: `internal/downloader/**`

## Purpose
- Handles playlist parsing, chunk download/decrypt, ffmpeg joining, and optional pipeline/progress/rate-limit orchestration.

## Interfaces / Contracts
- Main downloader API: `New`, `FetchLecturePlaylists`, `DownloadLecturePlaylists`, `DownloadPlaylist`, `DownloadAndJoinPlaylist`, `CreateTempM3U8File`, `JoinLectureOutput`.
- Playlist/type contracts: `Lecture`, `ParsedPlaylist`, `DownloadedPlaylist`, `JoinResult`.
- Supporting exports: `PlaylistParser`, `NewRateLimiterFromConfig`, `NewLecturePipeline`, `NewProgressTracker`, `ChunkCompleted`, `LectureCompleted`.

## Data / Types
- Keep shared/public types centralized (avoid duplicate local shape drift).
- Prefer explicit types over `any` and implicit contracts.

## Local Quality Bar
- Keep files small and readable (target <= 300 lines per file).
- Avoid very large flat folders (target <= 20 direct files per folder).
- Add or update tests when behavior changes.
- Downloader view contract is `left|right|both`; callers must normalize external aliases before invoking.
- Preserve retry/backoff and chunk decryption guards (`.temp` input and valid AES key lengths) when refactoring.
- Keep both execution paths working: pipelined (`EnablePipeline=true`) and sequential fallback.

## Agent Rules (Enforced)
- Read this file before editing files in this module.
- If behavior/interfaces/contracts change, update this file in the same change.
- Keep changes scoped to this module unless cross-module contract changes are required.
