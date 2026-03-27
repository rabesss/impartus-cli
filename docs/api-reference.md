<!-- START doctoc generated TOC please keep comment here to allow auto update -->
**Table of Contents**  *generated automatically*

<!---toc start-->

* [Impartus API Reference](#impartus-api-reference)
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
  * [WebSocket](#websocket)

<!---toc end-->
<!-- END doctoc generated TOC please keep comment here to allow auto update -->


# Impartus API Reference

Base path: `http://localhost:8080/api/v1`

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
  }
}
```

### JSON success envelope (`respondWithSuccess`)

Used where handlers call `respondWithSuccess` (currently login + delete job).

```json
{
  "success": true,
  "data": {}
}
```

### Direct JSON responses

Some handlers write JSON directly (not wrapped):
- `GET /health` → `{ "status": "ok" }`
- `GET /courses` → `[]client.Course`
- `GET /lectures` → `[]client.Lecture`
- `POST /jobs`, `GET /jobs`, `GET /jobs/{id}` → `Job` / `[]Job`

## Courses

`GET /courses`

Returns the authenticated user's courses as a JSON array.

## Lectures

`GET /lectures?subject_id={subjectId}&session_id={sessionId}`

Also accepts camelCase query keys `subjectId` and `sessionId`.

Returns lectures as a JSON array.

## Jobs

### Job object

```json
{
  "id": "job-1739366400000000000",
  "subjectId": 678,
  "sessionId": 12345,
  "startIndex": 0,
  "endIndex": 5,
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
    "decryptWorkersPerLecture": 2,
    "slides": false
  },
  "createdAt": "2025-02-12T12:00:00Z",
  "updatedAt": "2025-02-12T12:01:00Z"
}
```

Notes:
- `status`: `pending | running | completed | failed | canceled`
- `progress`: float percentage (0-100)
- `totalLectures`, `completedLectures`, `outputs`, `error` are populated as work advances

### Create job

`POST /jobs`

Required fields:
- `subjectId` (int, > 0)
- `sessionId` (int, > 0)
- `startIndex` (int, >= 0)
- `endIndex` (int, >= startIndex)

Per-job config overrides are supported in two forms:
1. Preferred nested object: `jobConfig`
2. Backward-compatible top-level fields with same names

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

If `jobConfig` is provided, top-level compatibility fields are ignored.

Success (`201`): returns created `Job` object directly.

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
    "id": "job-1739366400000000000",
    "status": "canceled"
  }
}
```

Terminal jobs (`completed`/`failed`/`canceled`) return `400` with code `JOB_CANNOT_CANCEL`.

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
