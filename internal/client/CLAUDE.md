# Module Guide: Client

## Scope
- Directory: `internal/client`
- Owned paths: `internal/client/**`

## Purpose
- Wraps Impartus HTTP authentication and data endpoints for courses, lectures, and playlist discovery.

## Interfaces / Contracts
- `New(httpClient, userAgentProvider)` creates the client with defaults when nil inputs are passed.
- Auth/token APIs: `LoginAndSetToken`, `Token`, `SetToken`, `GetAuthorizedWithToken`.
- Data APIs: `GetCourses`, `GetLectures`, `GetPlaylists`.
- Public response/data shapes are defined in `types.go` (`Course`, `Lecture`, `ParsedPlaylist`, etc.).

## Data / Types
- Keep shared/public types centralized (avoid duplicate local shape drift).
- Prefer explicit types over `any` and implicit contracts.

## Local Quality Bar
- Keep files small and readable (target <= 300 lines per file).
- Avoid very large flat folders (target <= 20 direct files per folder).
- Add or update tests when behavior changes.
- Keep `LoginAndSetToken` token-cache behavior (`.token` reuse/write) explicit and backward compatible.
- Preserve filename sanitization of course/lecture names before returning data.
- Return status/body-rich errors for non-200 responses to aid CLI/API diagnostics.

## Agent Rules (Enforced)
- Read this file before editing files in this module.
- If behavior/interfaces/contracts change, update this file in the same change.
- Keep changes scoped to this module unless cross-module contract changes are required.
