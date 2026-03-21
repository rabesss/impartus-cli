<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/ktechhub/doctoc)*

<!---toc start-->

* [Documentation Validation Report](#documentation-validation-report)
  * [API Reference (`docs/api-reference.md`)](#api-reference-docsapi-referencemd)
  * [Error Codes (`docs/error-codes.md`)](#error-codes-docserror-codesmd)
  * [WebSocket Events (`docs/websocket-events.md`)](#websocket-events-docswebsocket-eventsmd)
  * [Architecture (`docs/architecture.md`)](#architecture-docsarchitecturemd)
  * [README.md](#readmemd)
  * [Summary](#summary)

<!---toc end-->

<!-- END doctoc generated TOC please keep comment here to allow auto update -->
# Documentation Validation Report

**Date:** 2026-02-26
**Validated against:** `internal/server/server.go`, `internal/server/auth.go`, `internal/cli/cli.go`
**Method:** Line-by-line cross-reference of docs against source code

See also: [`api-reference.md`](api-reference.md), [`error-codes.md`](error-codes.md), [`websocket-events.md`](websocket-events.md), [`architecture.md`](architecture.md)

---

## API Reference (`docs/api-reference.md`)

| Item | Status | Source |
|------|--------|--------|
| Base path `/api/v1` | ✅ Match | `server.go:344` |
| `GET /health` public | ✅ Match | `server.go:345` |
| `POST /auth/login` public | ✅ Match | `server.go:346` |
| `GET /ws` protected (Bearer) | ✅ Match | `server.go:350` — behind `authMiddleware` |
| `GET /courses` protected | ✅ Match | `server.go:351` |
| `GET /lectures` protected, query params | ✅ Match | `server.go:352`, handler reads `subject_id`/`session_id` + camelCase |
| `POST /jobs` protected, 201 | ✅ Match | `server.go:353`, handler returns `http.StatusCreated` |
| `GET /jobs` protected | ✅ Match | `server.go:354` |
| `GET /jobs/{id}` protected | ✅ Match | `server.go:355` |
| `DELETE /jobs/{id}` protected | ✅ Match | `server.go:356` |
| Login response `{success, data: {token, expires}}` | ✅ Match | `auth.go:149-152` |
| Error format `{success, error: {code, message, details}}` | ✅ Match | `auth.go:87-103` |
| Success format `{success, data}` | ✅ Match | `auth.go:106-111` |
| Job object fields (id, config, outputs, totalLectures, etc.) | ✅ Match | `server.go:100-119` |
| Job status values (pending/running/completed/failed/cancelled) | ✅ Match | `server.go:142,223-227` |
| `jobConfig` nested form + backward-compatible top-level fields | ✅ Match | `server.go:38-85` |
| Cancel returns `JOB_CANNOT_CANCEL` for terminal jobs | ✅ Match | `server.go:515` |
| Auth: `crypto/rand` 32-byte tokens, 24h expiry | ✅ Match | `auth.go:79-84,142` |
| Token cleanup runs hourly | ✅ Match | `auth.go:70-77` |

**Result: ✅ Fully accurate**

---

## Error Codes (`docs/error-codes.md`)

| Item | Status | Source |
|------|--------|--------|
| Error response format documented | ✅ Match | `auth.go:87-103` |
| `MISSING_TOKEN` (401) | ✅ Match | `auth.go:164` |
| `INVALID_TOKEN_FORMAT` (401) | ✅ Match | `auth.go:169` |
| `INVALID_TOKEN` (401) | ✅ Match | `auth.go:175` |
| `AUTH_FAILED` (401) | ✅ Match | `auth.go:132` |
| `TOKEN_GENERATION_FAILED` (500) | ✅ Match | `auth.go:138` |
| `INVALID_REQUEST` (400) — multiple messages | ✅ Match | `auth.go:127`, `server.go:415,420,444,457,461` |
| `MISSING_PARAMETER` (400) — multiple messages | ✅ Match | `server.go:409,449,453,487,504` |
| `JOB_NOT_FOUND` (404) | ✅ Match | `server.go:493,511` |
| `JOB_CANNOT_CANCEL` (400) with details | ✅ Match | `server.go:515` |
| `INVALID_JOB_CONFIG` (400) | ✅ Match | `server.go:467` |
| `CANCEL_FAILED` (500) | ✅ Match | `server.go:518` |
| `LOGIN_FAILED` (502) | ✅ Match | `server.go:384,427` |
| `COURSES_FETCH_FAILED` (502) | ✅ Match | `server.go:390` |
| `LECTURES_FETCH_FAILED` (502) | ✅ Match | `server.go:433` |

**Result: ✅ Fully accurate** (after rewrite on 2026-02-26)

---

## WebSocket Events (`docs/websocket-events.md`)

| Item | Status | Source |
|------|--------|--------|
| Path `/api/v1/ws` | ✅ Match | `server.go:350` |
| Auth required (Bearer) | ✅ Match | `server.go:348-350` — protected subrouter |
| Event fields: `type`, `jobId`, `timestamp` (Unix int) | ✅ Match | broadcast calls in `server.go` |
| `job.started` with `status` | ✅ Match | `server.go` executeJob broadcasts |
| `job.progress` with `progress`, `status`, `phase`, `details` | ✅ Match | `server.go` progress broadcast |
| `job.completed` with `status`, `progress`, `outputs` | ✅ Match | `server.go` completion broadcast |
| `job.failed` with `error` | ✅ Match | `server.go` failure broadcast |
| `job.cancelled` with `progress` | ✅ Match | `server.go` cancellation broadcast |
| Phase values: initializing → logging_in → fetching_lectures → downloading_slides → fetching_playlists → downloading | ✅ Match | `server.go` executeJob phases |
| "Differences from Design Spec" table | ✅ Accurate | Correctly documents spec vs implementation divergences |
| Usage example includes auth header | ⚠️ Fixed | Line 170 updated on 2026-02-26 |

**Result: ✅ Accurate** (minor fix applied)

---

## Architecture (`docs/architecture.md`)

| Item | Status | Source |
|------|--------|--------|
| Interactive mode flow diagram | ✅ Match | `cli.go:384-448` |
| JSON mode flow diagram | ✅ Match | `cli.go:108-152` |
| API job lifecycle diagram | ✅ Match | `server.go:282-339,341-356` |
| Package boundary diagram | ✅ Match | Import graph across all `internal/` packages |
| Two entrypoints (main.go, cmd/impartus/main.go) | ✅ Match | Both files exist and call `cli.Execute` |

**Result: ✅ Fully accurate**

---

## README.md

| Item | Status | Source |
|------|--------|--------|
| CLI commands (courses, lectures, download, serve, version, help) | ✅ Match | `cli.go:87-105` |
| `--json` mode documented | ✅ Match | `cli.go:74-85,108-152` |
| `serve --json` returns metadata, non-blocking | ✅ Match | `cli.go:136-148` |
| API routes, auth flow, job payload | ✅ Match | `server.go`, `auth.go` |
| Download flags (--quality, --views, --audio-only, etc.) | ✅ Match | `cli.go:321-336` |
| JSON envelope format | ✅ Match | `cli.go:31-45` |
| 1-based CLI vs 0-based API indexing noted | ✅ Match | `cli.go:329` vs `server.go:43` |

**Result: ✅ Fully accurate**

---

## Summary

| Document | Status |
|----------|--------|
| `api-reference.md` | ✅ Accurate |
| `error-codes.md` | ✅ Accurate (rewritten 2026-02-26) |
| `websocket-events.md` | ✅ Accurate (minor fix 2026-02-26) |
| `architecture.md` | ✅ Accurate |
| `README.md` | ✅ Accurate |
| `security-validation.md` | ✅ Accurate |
| `openclaw-manifest.json` | ✅ Accurate (updated 2026-02-26) |
