<!-- START doctoc generated TOC please keep comment here to allow auto update -->
**Table of Contents**  *generated automatically*

<!---toc start-->

* [Impartus API Reference](#impartus-api-reference)
  * [Health](#health)
  * [Authentication](#authentication)
    * [Login](#login)
  * [Response formats](#response-formats)
    * [JSON error envelope (`respondWithError`)](#json-error-envelope-respondwitherror)
    * [JSON success envelope (`respondWithSuccess`)](#json-success-envelope-respondwithsuccess)
    * [Direct JSON responses](#direct-json-responses)
  * [Courses](#courses)
  * [Lectures](#lectures)
  * [Jobs](#jobs)
    * [Job object](#job-object)
    * [Create job](#create-job)
    * [List jobs](#list-jobs)
    * [Get job](#get-job)
    * [Cancel job](#cancel-job)
  * [Job Persistence](#job-persistence)
    * [Persisted Data](#persisted-data)
    * [NOT Persisted (runtime-only)](#not-persisted-runtime-only)
    * [Restart Behavior](#restart-behavior)
  * [WebSocket](#websocket)

<!---toc end-->
<!-- END doctoc generated TOC please keep comment here to allow auto update -->

# Impartus API Reference

Base path: `http://localhost:8080/api/v1`

## Health

`GET /health/live`

Returns process liveness without checking configuration, the upstream service, token caches, the filesystem, or FFmpeg. No authentication required.

Success (`200`):
```json
{
  "success": true,
  "data": {
    "status": "ok"
  },
  "meta": {
    "command": "health",
    "mode": "api"
  }
}
```

`GET /health/ready`

Returns dependency readiness. `GET /health` is a compatibility alias that returns the same readiness response. No authentication required.

Success (`200`):
```json
{
  "success": true,
  "data": {
    "status": "ok",
    "config": {
      "status": "ok"
    },
    "upstream": {
      "status": "reachable"
    },
    "ffmpeg": {
      "status": "available"
    }
  },
  "meta": {
    "command": "health",
    "mode": "api"
  }
}
```

Possible `status` values:
- `ok`: All dependencies are healthy
- `degraded`: One or more dependencies have issues

The `config.status` will be `misconfigured` if username, password, or baseUrl are missing.
The `upstream.status` will be `unreachable` if the Impartus API cannot be contacted.
The `ffmpeg.status` will be `not_found` if FFmpeg is not installed or not in PATH.
Any sub-check can be `unknown` if readiness probing fails internally; inspect server logs for details.

Because this endpoint is unauthenticated, configuration health is deliberately aggregate: the response does not reveal which credential fields are present or missing.
Healthy and degraded readiness results are cached for 15 seconds and may be that old. Both readiness paths retain HTTP 200 when degraded; clients must inspect `data.status`.

## Authentication

Protected endpoints require:

```http
Authorization: Bearer <token>
```

Protected routes:
- `GET /ws`
- `GET /courses`
- `GET /lectures`
- `POST /jobs`
- `GET /jobs`
- `GET /jobs/{id}`
- `DELETE /jobs/{id}`

Public routes:
- `GET /health`
- `GET /health/live`
- `GET /health/ready`
- `POST /auth/login`

### Login

`POST /auth/login`

Request:
```json
{
  "username": "uid@hyderabad.bits-pilani.ac.in",
  "password": "your-password"
}
```

Success (`200`):
```json
{
  "success": true,
  "data": {
    "token": "...",
    "expires": "2025-02-12T12:34:56Z"
  }
}
```

## Response formats

### JSON error envelope (`respondWithError`)

Used by auth middleware/login failures and API validation/runtime errors.

```json
{
  "success": false,
  "error": {
    "code": "ERROR_CODE",
    "message": "Human readable message",
    "details": {}
  },
  "meta": {
    "command": "commandName",
    "mode": "api"
  }
}
```

Some errors include retry hints when the operation may succeed on retry:

```json
{
  "success": false,
  "error": {
    "code": "UPSTREAM_ERROR",
    "message": "Failed to fetch courses from Impartus",
    "details": {
      "retryable": true,
      "retryAfter": 30
    }
  },
  "meta": {
    "command": "courses",
    "mode": "api"
  }
}
```

Retry hints are included for:
- `LOGIN_FAILED`, `COURSES_FETCH_FAILED`, `LECTURES_FETCH_FAILED` (502 errors): `retryAfter: 30`
- `CANCEL_FAILED` (500 errors): `retryAfter: 10`
- `RATE_LIMITED` (429 on login): `retryAfter: 60`

4xx client errors do NOT include retry hints (absence means not retryable).

### JSON success envelope (`respondWithSuccess`)

Used where handlers call `respondWithSuccess` (login + delete job + health check).

```json
{
  "success": true,
  "data": {},
  "meta": {
    "command": "commandName",
    "mode": "api"
  }
}
```

### Direct JSON responses

All handlers wrap responses in `{success, data, error, meta}` envelope using `respondWithEnvelope`:
- `GET /courses` → `{success: true, data: [...], meta: {command, mode: 'api'}}`
- `GET /lectures` → `{success: true, data: [...], meta: {command, mode: 'api'}}`
- `POST /jobs`, `GET /jobs`, `GET /jobs/{id}` → `{success: true, data: {...}, meta: {command, mode: 'api'}}`

## Courses

`GET /courses`

Returns the authenticated user's courses as a JSON array.

## Lectures

`GET /lectures?subjectId={subjectId}&sessionId={sessionId}`

Canonical query keys are camelCase: `subjectId` and `sessionId`.

Legacy snake_case aliases `subject_id` and `session_id` are still accepted for backward compatibility, but error messages and new integrations should use the canonical camelCase names.

Returns lectures as a JSON array.

## Jobs

### Job object

```json
{
  "id": "job-9d7e6db6-0df8-4b6c-9e66-e7269c8931ab",
  "subjectId": 678,
  "sessionId": 12345,
  "startIndex": 1,
  "endIndex": 6,
  "status": "running",
  "progress": 52.5,
  "error": "",
  "totalLectures": 6,
  "completedLectures": 3,
  "outputs": ["/path/to/output.mp4"],
  "config": {
    "quality": "720",
    "views": "both",
    "audioOnly": false,
    "audioFormat": "mp3",
    "outputPath": "./downloads",
    "enablePipeline": true,
    "numWorkers": 4,
    "downloadWorkersPerLecture": 4,
    "decryptWorkersPerLecture": 4,
    "slides": false,
    "skipNoAudio": false
  },
  "createdAt": "2025-02-12T12:00:00Z",
  "updatedAt": "2025-02-12T12:01:00Z"
}
```

Notes:
- `id`: opaque identifier beginning with `job-`; the current implementation appends a UUID. Store and compare the complete value without parsing it or inferring creation time or ordering.
- `status`: `pending | running | completed | failed | canceled`
- `progress`: float percentage (0-100)
- `totalLectures`, `completedLectures`, `outputs`, `error` are populated as work advances
- `filteredLectures`: number of selected lectures skipped by `skipNoAudio`; omitted when zero

### Create job

`POST /jobs`

Required fields:
- `subjectId` (int, > 0)
- `sessionId` (int, > 0)
- `startIndex` (int, >= 1, 1-based)
- `endIndex` (int, >= startIndex)

Optional fields:
- `idempotencyKey` (string, max 256 chars): Prevents duplicate job creation. If a job with this key already exists, returns the existing job with 409 Conflict instead of creating a new job. Keys persist across server restarts.

Per-job config overrides are supported in two forms:
1. Preferred nested object: `jobConfig`
2. Backward-compatible top-level fields with same names

Legacy top-level override fields are normalized at the request boundary. New clients should send `jobConfig`.

Supported override keys:
- `quality`
- `views`
- `audioOnly`
- `audioFormat`
- `outputPath`
- `enablePipeline`
- `numWorkers`
- `downloadWorkersPerLecture`
- `decryptWorkersPerLecture`
- `skipNoAudio`

If `jobConfig` is provided, top-level compatibility fields are ignored.

Success (`201`): returns created `Job` object directly.

Duplicate idempotency key (`409 Conflict`):
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

Errors:
- `INVALID_IDEMPOTENCY_KEY` (400): idempotencyKey exceeds 256 characters
- `INVALID_REQUEST` (400): invalid subjectId, sessionId, startIndex, or endIndex

### List jobs

`GET /jobs` → `[]Job`

### Get job

`GET /jobs/{id}` → `Job`

### Cancel job

`DELETE /jobs/{id}`

Cancels a non-terminal job (`pending`/`running`), marks it `canceled`, and stops execution.

Success (`200`):
```json
{
  "success": true,
  "data": {
    "id": "job-9d7e6db6-0df8-4b6c-9e66-e7269c8931ab",
    "status": "canceled"
  }
}
```

Terminal jobs (`completed`/`failed`/`canceled`) return `400` with code `JOB_CANNOT_CANCEL`.
`DELETE` is a cancellation operation; it does not delete job history or downloaded media.

## Job Persistence

Jobs are persisted to a `.jobs.json` file on disk and survive server restarts.

### Persisted Data
- Job ID, subjectId, sessionId, startIndex, endIndex
- Job status, progress, outputs, error message
- Job config (quality, views, audioOnly, etc.)
- Idempotency key (if provided)
- CreatedAt and UpdatedAt timestamps

### NOT Persisted (runtime-only)
- Credentials (username, password, tokens)
- Runtime context (download progress, active connections)

### Restart Behavior
| Original Status | After Restart |
|-----------------|---------------|
| `pending` | Marked as `failed` (cannot be resumed) |
| `running` | Marked as `failed` (cannot resume mid-download) |
| `completed` | Restored as `completed` |
| `failed` | Restored as `failed` |
| `canceled` | Restored as `canceled` |

Idempotency keys are also persisted, so duplicate submissions after restart return the existing job (409 Conflict).

The server retains metadata for the newest 1000 terminal jobs. Pending and running jobs are never pruned while the server is running. On restart, interrupted jobs are first converted to failed and then the 1000-job terminal retention limit is applied. Pruning removes only job metadata and its idempotency-key entry; downloaded media is not deleted.

Persistence writes use atomic file replacement. Progress writes are coalesced, while job creation, cancellation, terminal transitions, and graceful server shutdown are flushed before they are reported as durable.

## WebSocket

Route: `GET /api/v1/ws`

Authentication: `Authorization: Bearer <token>` is required.

Server emits real-time job events:
- `job.started`
- `job.progress`
- `job.completed`
- `job.failed`
- `job.cancelled`

`job.progress` includes execution phase values from the current runtime:
- `initializing`
- `logging_in`
- `fetching_lectures`
- `downloading_slides` (when slides are enabled)
- `fetching_playlists`
- `downloading`
