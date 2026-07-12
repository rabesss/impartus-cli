# Code Review & Security Analysis Spec — impartus-cli v0.1.18

**Date:** 2026-07-12
**Scope:** Complete codebase on `main` at `bef5adf`
**Method:** Parallel subagent tracks, each applying STRIDE threat modeling, OWASP Top 10, and the repo's own `docs/review-checklist.md` rubric.

## Codebase Summary

- **Language:** Go 1.25.0
- **Packages:** 8 (buildinfo, cli, client, config, downloader, paths, secrets, server)
- **Production files:** 35
- **Test files:** ~45
- **Key deps:** gorilla/mux, gorilla/websocket, vbauerster/mpb, golang.org/x/time, google/uuid

## Review Tracks

### Track 1 — Auth, Secrets & Network Exposure
**Files:**
- `internal/client/client.go`, `internal/client/http.go`
- `internal/secrets/secrets.go`
- `internal/server/auth.go`, `internal/server/middleware.go`, `internal/server/ratelimiter.go`
- `.token` file handling (grep for `.token`, `0600`)

**Focus:**
- Token generation, storage, validation, and lifecycle
- Auth token leak into logs/errors (upstream URLs carry `?token=...`)
- Secret redaction completeness (`RedactURL`, `SanitizeError`, `Scrub`, `ScrubError`)
- Auth middleware bypass paths
- CORS policy and WebSocket origin checking
- Loopback bind enforcement (`IMPARTUS_ALLOW_REMOTE_ACCESS`)
- Rate limiter correctness and bypass
- `.token` file permissions (must be `0600`)

**STRIDE:** Spoofing, Information Disclosure, Elevation of Privilege

### Track 2 — Server API, WebSocket & Job Persistence
**Files:**
- `internal/server/server.go`, `internal/server/handlers.go`, `internal/server/types.go`
- `internal/server/responses.go`, `internal/server/store.go`
- `internal/server/hub.go` (WebSocket hub)
- `internal/server/job_runner.go`, `internal/server/job_executor.go`, `internal/server/job_persistence.go`

**Focus:**
- Input validation on all API endpoints (job creation, lecture query params)
- Job persistence file permissions and data integrity
- Idempotency key handling (collision, max length, persistence)
- WebSocket event payloads (auth required, no sensitive data in events)
- Concurrency safety (mutex usage, channel handling, goroutine leaks)
- Job lifecycle state machine (pending -> running -> completed/failed/canceled)
- Restart behavior (pending/running jobs marked failed on restart)
- Response envelope consistency and error code accuracy
- Resource cleanup on job cancellation

**STRIDE:** Tampering, Repudiation, Denial of Service

### Track 3 — Download Pipeline, Subprocess & Path Safety
**Files:**
- `internal/downloader/downloader.go`, `internal/downloader/download_chunks.go`
- `internal/downloader/decrypt.go`, `internal/downloader/ffmpeg.go`
- `internal/downloader/m3u8.go`, `internal/downloader/pipeline.go`
- `internal/downloader/play.go`, `internal/downloader/progress_tracker.go`
- `internal/downloader/rate_limiter.go`
- `internal/client/streamutils.go`, `internal/client/types.go`
- `internal/paths/paths.go`

**Focus:**
- Path traversal validation (`ValidateDownloadLocation` — CLI allows absolute, API rejects)
- FFmpeg/mpv subprocess execution (must use `exec.CommandContext` with argv arrays, never shell)
- M3U8 parsing safety (malformed input, URL injection, resource exhaustion)
- Decryption operations (key handling, memory safety)
- Download chunk concurrency and error handling
- Rate limiter correctness
- Temp file creation and cleanup
- Progress tracker thread safety
- Stream utility robustness (partial reads, connection drops)

**STRIDE:** Tampering, Elevation of Privilege, Denial of Service

### Track 4 — CLI, Config & Supply Chain
**Files:**
- `internal/cli/cli.go`, `internal/cli/cli_download.go`, `internal/cli/cli_play.go`
- `internal/cli/cli_serve.go`, `internal/cli/cli_helpers.go`
- `internal/cli/cli_interactive.go`, `internal/cli/cli_json.go`
- `internal/config/config.go`
- `main.go`
- `go.mod`, `go.sum`
- `Dockerfile`
- `.github/workflows/*.yml`

**Focus:**
- Config loading, validation, and env override security
- CLI flag validation and positional argument rejection
- JSON envelope consistency and error handling
- Interactive mode safety (no credential echo, no command injection)
- Config field checklist compliance (JSON tag, env override, defaulting, validation, README entry)
- Dependency audit (known vulnerabilities, pinned versions)
- Dockerfile security (non-root user, minimal image, no secrets in layers)
- CI/CD workflow security (secret handling, permissions, injection risks)
- Default values and zero-value handling (enableJitter, allowRemoteAccess, etc.)

**STRIDE:** Elevation of Privilege, Information Disclosure, Repudiation

## Severity Calibration

| Severity | Criteria |
|----------|----------|
| **Critical** | Data loss, privilege escalation, token/secret exposure, broken auth, RCE, destructive ops without safeguards |
| **Warning** | Missing validation, unsafe defaults, untested edge cases, concurrency/race risks, resource leaks, misleading errors |
| **Nit** | Maintainability issues that materially affect code quality |

## Output Format

Each track produces a structured report:

```
## Track N: [Track Name]

### Summary
[1-2 paragraph overview]

### Findings

#### [CRITICAL/WARNING/NIT] Finding Title
- **File:** `path/to/file.go:NNN`
- **Category:** [STRIDE category or OWASP category]
- **Description:** [What's wrong and why it matters]
- **Impact:** [What could go wrong]
- **Recommendation:** [Smallest credible fix]
- **Confidence:** [High/Medium/Low]

### Positive Observations
[What's done well — for context]

### Compliance with review-checklist.md
[Pass/fail for each applicable BLOCKER and SHOULD item]
```

## Validation

After all tracks complete:
1. Synthesize findings into a unified report
2. Deduplicate overlapping findings
3. Sort by severity
4. Cross-reference with `docs/review-checklist.md` BLOCKER items
5. Run `go build`, `go vet`, `go test` to confirm current state
