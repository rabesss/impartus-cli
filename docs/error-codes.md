<!-- START doctoc generated TOC please keep comment here to allow auto update -->
**Table of Contents**  *generated automatically*

<!---toc start-->

* [Error Codes Reference](#error-codes-reference)
  * [Error Response Format](#error-response-format)
    * [Retry Hints](#retry-hints)
  * [Authentication Errors](#authentication-errors)
  * [Request Validation Errors](#request-validation-errors)
  * [Job Errors](#job-errors)
    * [409 Conflict (Not an Error)](#409-conflict-not-an-error)
  * [Upstream Errors](#upstream-errors)

<!---toc end-->
<!-- END doctoc generated TOC please keep comment here to allow auto update -->

# Error Codes Reference

All API errors use structured JSON responses via `respondWithError` in [`auth.go`](../internal/server/auth.go) and [`server.go`](../internal/server/server.go).

See also: [`api-reference.md`](api-reference.md) for route details, [`websocket-events.md`](websocket-events.md) for event schemas.

## Error Response Format

```json
{
  "success": false,
  "error": {
    "code": "ERROR_CODE",
    "message": "Human readable description",
    "details": {}
  },
  "meta": {
    "command": "commandName",
    "mode": "api"
  }
}
```

`details` is optional and included only when additional context is available (e.g. terminal job status on `JOB_CANNOT_CANCEL`).

### Retry Hints

Some errors include retry hints to guide client retry behavior:

```json
{
  "success": false,
  "error": {
    "code": "UPSTREAM_ERROR",
    "message": "Failed to authenticate with Impartus API",
    "details": {
      "retryable": true,
      "retryAfter": 30
    }
  },
  "meta": { ... }
}
```

| Field | Type | Description |
|-------|------|-------------|
| `retryable` | boolean | `true` if the operation may succeed on retry |
| `retryAfter` | integer | Seconds to wait before retrying (only present when `retryable: true`) |

**Errors with retry hints:**

| Code | Status | retryAfter | Reason |
|------|--------|------------|--------|
| `LOGIN_FAILED` | 502 | 30s | Upstream API may recover |
| `COURSES_FETCH_FAILED` | 502 | 30s | Upstream API may recover |
| `LECTURES_FETCH_FAILED` | 502 | 30s | Upstream API may recover |
| `CANCEL_FAILED` | 500 | 10s | Temporary server issue |

**Errors WITHOUT retry hints (4xx client errors):**
- `INVALID_REQUEST`, `MISSING_PARAMETER`, `INVALID_JOB_CONFIG` (400)
- `MISSING_TOKEN`, `INVALID_TOKEN_FORMAT`, `INVALID_TOKEN`, `AUTH_FAILED` (401)
- `JOB_NOT_FOUND` (404)
- `JOB_CANNOT_CANCEL` (400)
- `INVALID_IDEMPOTENCY_KEY` (400)

Absence of `retryable` field means the error is not retryable.

---

## Authentication Errors

Returned by `authMiddleware` and `loginHandler` in `internal/server/auth.go`.

| Code | Status | Message | Trigger |
|------|--------|---------|---------|
| `MISSING_TOKEN` | 401 | Authorization header required | No `Authorization` header on protected route |
| `INVALID_TOKEN_FORMAT` | 401 | Expected 'Bearer \<token\>' | Header present but not `Bearer <token>` format |
| `INVALID_TOKEN` | 401 | Token is invalid or expired | Token not found in store or past 24h expiry |
| `AUTH_FAILED` | 401 | Invalid username or password | Login credentials don't match `config.json` |
| `TOKEN_GENERATION_FAILED` | 500 | Failed to generate token | `crypto/rand` failure (extremely rare) |

---

## Request Validation Errors

Returned by route handlers in `internal/server/server.go`.

| Code | Status | Message(s) | Trigger |
|------|--------|------------|---------|
| `INVALID_REQUEST` | 400 | Invalid request body | Malformed JSON on `POST /auth/login` or `POST /jobs` |
| `INVALID_REQUEST` | 400 | subjectId must be a valid integer | Non-integer `subjectId`/`subject_id` query param on `GET /lectures` |
| `INVALID_REQUEST` | 400 | sessionId must be a valid integer | Non-integer `sessionId`/`session_id` query param on `GET /lectures` |
| `INVALID_REQUEST` | 400 | startIndex must be >= 1 | `startIndex < 1` on `POST /jobs` |
| `INVALID_REQUEST` | 400 | endIndex must be >= startIndex | `endIndex < startIndex` on `POST /jobs` |
| `MISSING_PARAMETER` | 400 | subjectId and sessionId query parameters required | Missing query params on `GET /lectures` |
| `MISSING_PARAMETER` | 400 | subjectId is required and must be > 0 | Missing or zero `subjectId` on `POST /jobs` |
| `MISSING_PARAMETER` | 400 | sessionId is required and must be > 0 | Missing or zero `sessionId` on `POST /jobs` |
| `MISSING_PARAMETER` | 400 | Job ID is required | Empty `{id}` on `GET /jobs/{id}` or `DELETE /jobs/{id}` |
| `INVALID_IDEMPOTENCY_KEY` | 400 | idempotencyKey must be at most 256 characters | `idempotencyKey` exceeds 256 chars on `POST /jobs` |

---

## Job Errors

| Code | Status | Message | Trigger |
|------|--------|---------|---------|
| `JOB_NOT_FOUND` | 404 | Job not found | Job ID doesn't exist in store |
| `JOB_CANNOT_CANCEL` | 400 | Cannot cancel job in terminal state | DELETE on `completed`/`failed`/`cancelled` job. `details.status` contains the terminal state. |
| `INVALID_JOB_CONFIG` | 400 | *(validation message)* | Invalid `jobConfig` override (bad quality, workers out of range, empty outputPath) |
| `CANCEL_FAILED` | 500 | *(error message)* | Unexpected failure during job cancellation |
| `INVALID_IDEMPOTENCY_KEY` | 400 | idempotencyKey must be at most 256 characters | `idempotencyKey` exceeds 256 chars on `POST /jobs` |

### 409 Conflict (Not an Error)

When an `idempotencyKey` is provided and a job with that key already exists, the API returns `409 Conflict` with a **success** response containing the existing job:

```json
{
  "success": true,
  "data": {
    "job": { ... },
    "duplicate": true
  },
  "meta": {
    "command": "createJob",
    "mode": "api"
  }
}
```

This is not an error condition — it allows safe retry of job creation requests without creating duplicates.

---

## Upstream Errors

Returned when the Impartus API or internal login fails.

| Code | Status | Message | Trigger |
|------|--------|---------|---------|
| `LOGIN_FAILED` | 502 | Failed to authenticate with Impartus API | Upstream login failed |
| `COURSES_FETCH_FAILED` | 502 | Failed to fetch courses from Impartus | Upstream course fetch failed |
| `LECTURES_FETCH_FAILED` | 502 | Failed to fetch lectures from Impartus | Upstream lecture fetch failed |
