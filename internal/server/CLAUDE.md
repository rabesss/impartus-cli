# Module Guide: Server

## Scope
- Directory: `internal/server`
- Owned paths: `internal/server/**`

## Purpose
- Runs the local HTTP/WebSocket API for auth, course/lecture lookups, and background download job orchestration.

## Interfaces / Contracts
- Server construction/start: `NewAPIServer`, `NewAPIServerWithPersistence`, `New`, `StartAPIServer`, `(*APIServer).Start`.
- Upstream token cache: The server caches the authenticated upstream Impartus client using `upstreamCacheMu` (sync.RWMutex) for thread-safe double-checked locking. Cached entries (`upstreamCacheEntry`) hold the client, config, token, and `expiresAt` (23h TTL). Use `getOrRefreshUpstreamClient(ctx)` to get or lazily refresh the cached client.
- The CLI `serve` path should construct the persistence-backed server so jobs survive process restarts.
- Routes under `/api/v1`: public `/health`, `/auth/login`; protected `/ws`, `/courses`, `/lectures`, `/jobs`, `/jobs/{id}`.
- Job payload/contracts: `createJobRequest`, `JobConfigOptions`, `Job`, `JobRuntimeConfig`.
- Job persistence: `NewJobStoreWithPersistence(path)` persists jobs to a JSON file on disk. Survives server restarts. No credentials in persistence file. Corrupt files handled gracefully.
- Job idempotency: `POST /jobs` accepts optional `idempotencyKey` (string, max 256 chars). Same key returns existing job (409 Conflict) instead of creating a duplicate. Keys are persisted and survive restarts. `CreateJobWithKey` method on `JobStore` handles the logic. Omitting the key always creates a new job.
- WebSocket event types: `job.started`, `job.progress`, `job.completed`, `job.failed`, `job.cancelled`.
- Health endpoint (`GET /api/v1/health`) returns a structured JSON envelope via `respondWithEnvelope`. The response includes sub-checks: `config` (status: ok/misconfigured), `upstream` (status: not_configured/reachable/unreachable), and `ffmpeg` (status: available/not_found). The overall status is "ok" or "degraded".
- API envelope pattern: All API responses use `respondWithEnvelope(w, status, command, data)` which produces `{"success": true, "data": <data>, "error": null, "meta": {"command": <cmd>, "mode": "api"}}`. Errors use `respondWithError(w, status, code, message, command, hint)` producing `{"success": false, "error": {"code": <code>, "message": <msg>, "details": <hint>}, "meta": {...}}`.

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
