# Impartus API Contracts Specification

> **⚠️ IMPORTANT: DESIGN DRAFT - NOT FULLY IMPLEMENTED**
>
> **Status:** Design Draft
> **Version:** 1.0.0
> **Date:** 2025-02-12
> **Purpose:** Define REST API + WebSocket contracts for transforming impartus-go CLI into a programmable HTTP API with AI-tool integration via OpenClaw
>
> **Implementation Status:**
> - This document is a **design specification** that was **not fully implemented**
> - See `docs/validation-report.md` for a complete discrepancy analysis
> - See `docs/api-reference.md` for **actual** API behavior
> - Many features documented here (structured errors, auth, pagination, extra job fields) are not present in the current implementation

> **Key Differences from Implementation:**
> - Responses use a `{success, data, error, meta}` envelope in both this spec and the implementation; see `docs/api-reference.md` for the current field-level behavior
> - Job IDs are formatted strings (e.g., `job-<timestamp>`) in both this spec and the implementation; the `job-<unixNano>` format used by `fmt.Sprintf("job-%d", time.Now().UnixNano())` in server.go matches the spec's intent — this is no longer a difference
> - Authentication is enforced via Bearer token middleware in the implementation; the broader auth flows described here (login/session lifecycle, token management) are not fully implemented
> - Errors are returned as structured JSON (e.g., `{success: false, error: {...}}`); specific error schemas in this spec may not exactly match the implementation, which uses `respondWithError`
> - Lectures endpoint uses query params as documented here, and the current implementation matches this behavior
> - WebSocket events do not have a `data` wrapper in either this spec or the implementation
> - Timestamps are RFC 3339 in both this spec and the implementation (Go `time.Time` serializes to RFC 3339)
> - Many endpoints and features are still not implemented (see validation report)

---

**For the actual API implementation, refer to:**
- `docs/api-reference.md` - Actual endpoints and request/response formats
- `docs/websocket-events.md` - Actual WebSocket event formats
- `docs/openclaw-manifest.json` - Actual tool manifest

---

## 1. REST API Endpoints

> **⚠️ DESIGN SPEC - NOT FULLY IMPLEMENTED:** The endpoints below are design specifications. See `docs/api-reference.md` for actual implemented behavior.

---

### 1.1 Authentication

#### `POST /api/v1/auth/login`
Authenticate with Impartus credentials and establish a session.

**Request:**
```json
{
  "username": "string",
  "password": "string",
  "base_url": "string"  // Optional: defaults to value from env var
}
```

**Response (200 OK):**
```json
{
  "success": true,
  "data": {
    "token": "string",
    "user_type": 0,
    "message": "string"
  }
}
```

**Error Response (401 Unauthorized):**
```json
{
  "success": false,
  "error": {
    "code": "AUTH_INVALID_CREDENTIALS",
    "message": "Invalid username or password",
    "details": {}
  }
}
```

---

### 1.2 Courses

#### `GET /api/v1/courses`
List all available courses for the authenticated user.

**Query Parameters:**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| limit | int | No | 100 | Max number of courses to return |
| offset | int | No | 0 | Pagination offset |

**Response (200 OK):**
```json
{
  "success": true,
  "data": [
    {
      "session_id": 0,
      "subject_id": 0,
      "subject_name": "string",
      "session_name": "string",
      "professor_name": "string",
      "professor_id": 0,
      "department": "string",
      "department_id": 0,
      "institute": "string",
      "institute_id": 0,
      "video_count": 0,
      "flipped_lectures_count": 0,
      "coverpic": "string"
    }
  ],
  "pagination": {
    "total": 0,
    "limit": 100,
    "offset": 0
  }
}
```

---

### 1.3 Lectures

#### `GET /api/v1/courses/{course_id}/lectures`
List all lectures for a specific course.

**Path Parameters:**
| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| course_id | int | Yes | Subject ID of the course |

**Query Parameters:**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| skip_empty | bool | No | false | Filter out lectures with no content |

**Response (200 OK):**
```json
{
  "success": true,
  "data": [
    {
      "video_id": 0,
      "seq_no": 0,
      "ttid": 0,
      "session_id": 0,
      "subject_id": 0,
      "subject_name": "string",
      "subject_code": "string",
      "topic": "string",
      "professor_name": "string",
      "professor_id": 0,
      "department": "string",
      "department_id": 0,
      "institute": "string",
      "institute_id": 0,
      "classroom_name": "string",
      "classroom_id": 0,
      "start_time": "string",  // ISO 8601
      "end_time": "string",    // ISO 8601
      "actual_duration": 0,     // seconds
      "views": 0,
      "slide_count": 0,
      "document_count": 0,
      "status": 0,
      "type": 0,
      "coverpic": "string",
      "professor_image_url": "string"
    }
  ],
  "pagination": {
    "total": 0,
    "limit": 100,
    "offset": 0
  }
}
```

---

### 1.4 Download Jobs

#### `POST /api/v1/jobs`
Create an async download job for one or more lectures.

**Request:**
```json
{
  "course_id": 0,
  "lectures": [0, 1, 2],  // Array of lecture IDs or "all" for all
  "idempotency_key": "optional-client-generated-idempotency-key",  // Optional: prevents duplicate jobs on retries
  "options": {
    "quality": "720",           // "144" | "450" | "720"
    "views": "both",            // "left" | "right" | "both"
    "audio_only": false,
    "audio_format": "mp3",      // "mp3" | "m4a" | "aac" | "opus"
    "download_slides": false,
    "enable_pipeline": true,
    "download_location": "/path/to/downloads",
    "num_workers": 5
  }
}
```

**Response (201 Created):**
```json
{
  "success": true,
  "data": {
    "job_id": "uuid-v4",
    "status": "queued",
    "created_at": "2025-02-12T10:00:00Z",
    "estimated_completion": "2025-02-12T10:30:00Z"
  }
}
```

---

#### `GET /api/v1/jobs/{job_id}`
Get status and progress of a specific download job.

**Path Parameters:**
| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| job_id | string (UUID) | Yes | Job identifier |

**Response (200 OK):**
```json
{
  "success": true,
  "data": {
    "job_id": "uuid-v4",
    "status": "downloading",  // "queued" | "downloading" | "joining" | "completed" | "failed" | "cancelled"
    "progress": {
      "lectures_total": 10,
      "lectures_completed": 3,
      "chunks_total": 500,
      "chunks_completed": 150,
      "bytes_downloaded": 524288000,
      "percentage": 30
    },
    "current_lecture": {
      "seq_no": 3,
      "topic": "Lecture title",
      "view": "first"
    },
    "started_at": "2025-02-12T10:00:00Z",
    "updated_at": "2025-02-12T10:05:00Z",
    "result": {
      "files": [
        {
          "path": "/path/to/LEC 001 Introduction LEFT VIEW.mp4",
          "size": 52428800,
          "view": "left"
        }
      ],
      "failed_chunks": []
    }
  }
}
```

---

#### `GET /api/v1/jobs`
List all jobs with optional filtering.

**Query Parameters:**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| status | string | No | - | Filter by status |
| limit | int | No | 50 | Max results |
| offset | int | No | 0 | Pagination offset |

**Response (200 OK):**
```json
{
  "success": true,
  "data": [
    {
      "job_id": "uuid-v4",
      "status": "completed",
      "created_at": "2025-02-12T10:00:00Z",
      "progress": {
        "percentage": 100
      }
    }
  ],
  "pagination": {
    "total": 10,
    "limit": 50,
    "offset": 0
  }
}
```

---

#### `DELETE /api/v1/jobs/{job_id}`
Cancel a running or queued download job.

**Response (200 OK):**
```json
{
  "success": true,
  "data": {
    "job_id": "uuid-v4",
    "status": "cancelled",
    "cancelled_at": "2025-02-12T10:15:00Z"
  }
}
```

---

#### `GET /api/v1/jobs/{job_id}/logs`
Get detailed logs for a job (for debugging).

**Query Parameters:**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| limit | int | No | 100 | Max log lines |
| offset | int | No | 0 | Log line offset |

**Response (200 OK):**
```json
{
  "success": true,
  "data": {
    "logs": [
      {
        "timestamp": "2025-02-12T10:00:01Z",
        "level": "info",
        "message": "Starting download for lecture 1"
      }
    ]
  }
}
```

---

### 1.5 Slides

#### `GET /api/v1/lectures/{lecture_id}/slides`
Download lecture slides as PDF.

**Path Parameters:**
| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| lecture_id | int | Yes | Lecture video ID |

**Response (200 OK):**
- Content-Type: `application/pdf`
- Body: PDF binary data

**Error Response (404 Not Found):**
```json
{
  "success": false,
  "error": {
    "code": "SLIDES_NOT_FOUND",
    "message": "No slides available for this lecture",
    "details": {
      "lecture_id": 12345
    }
  }
}
```

---

### 1.6 Health/Status

#### `GET /api/v1/health`
Health check endpoint.

**Response (200 OK):**
```json
{
  "success": true,
  "data": {
    "status": "ok",
    "config": {
      "username": "ok|missing",
      "password": "ok|missing",
      "baseUrl": "ok|missing"
    },
    "upstream": {
      "status": "reachable|unreachable|not_configured"
    },
    "ffmpeg": {
      "status": "available|not_found"
    }
  }
}
```

---

## 2. WebSocket Endpoint

### `GET /ws` (WebSocket)

WebSocket endpoint for real-time job progress updates.

**Connection URL:** `ws://localhost:{port}/ws?job_id={job_id}`

**Event Types:**

#### `job.started`
```json
{
  "event": "job.started",
  "data": {
    "job_id": "uuid-v4",
    "started_at": "2025-02-12T10:00:00Z",
    "lectures_total": 10
  }
}
```

#### `job.progress`
```json
{
  "event": "job.progress",
  "data": {
    "job_id": "uuid-v4",
    "progress": {
      "lectures_completed": 3,
      "lectures_total": 10,
      "chunks_completed": 150,
      "chunks_total": 500,
      "percentage": 30
    },
    "current_lecture": {
      "seq_no": 3,
      "topic": "Current lecture title"
    },
    "speed": {
      "bytes_per_second": 1048576,
      "eta_seconds": 900
    }
  }
}
```

#### `job.lecture_completed`
```json
{
  "event": "job.lecture_completed",
  "data": {
    "job_id": "uuid-v4",
    "lecture": {
      "seq_no": 1,
      "topic": "Completed lecture title"
    },
    "files": [
      {
        "path": "/path/to/output.mp4",
        "size": 52428800
      }
    ]
  }
}
```

#### `job.completed`
```json
{
  "event": "job.completed",
  "data": {
    "job_id": "uuid-v4",
    "completed_at": "2025-02-12T10:30:00Z",
    "result": {
      "total_files": 20,
      "total_bytes": 1048576000,
      "failed_chunks": 5
    }
  }
}
```

#### `job.failed`
```json
{
  "event": "job.failed",
  "data": {
    "job_id": "uuid-v4",
    "failed_at": "2025-02-12T10:15:00Z",
    "error": {
      "code": "DOWNLOAD_ERROR",
      "message": "Failed to download chunk after 3 retries",
      "details": {
        "chunk_id": 42,
        "url": "https://..."
      }
    }
  }
}
```

#### `job.cancelled`
```json
{
  "event": "job.cancelled",
  "data": {
    "job_id": "uuid-v4",
    "cancelled_at": "2025-02-12T10:15:00Z"
  }
}
```

---

## 3. Error Response Format

All error responses follow this structure:

```json
{
  "success": false,
  "error": {
    "code": "ERROR_CODE",
    "message": "Human-readable error message",
    "details": {
      "key": "Additional context for AI self-correction"
    }
  }
}
```

### Error Codes

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `AUTH_INVALID_CREDENTIALS` | 401 | Username or password incorrect |
| `AUTH_TOKEN_EXPIRED` | 401 | Session token expired |
| `AUTH_MISSING_TOKEN` | 401 | No authorization header provided |
| `COURSE_NOT_FOUND` | 404 | Course ID does not exist |
| `LECTURE_NOT_FOUND` | 404 | Lecture ID does not exist |
| `JOB_NOT_FOUND` | 404 | Job ID does not exist |
| `INVALID_REQUEST_BODY` | 400 | Malformed JSON or invalid parameters |
| `INVALID_QUALITY` | 400 | Quality must be 144, 450, or 720 |
| `INVALID_VIEWS` | 400 | Views must be left, right, or both |
| `INVALID_AUDIO_FORMAT` | 400 | Audio format not supported |
| `DOWNLOAD_ERROR` | 500 | Failed to download content |
| `DECRYPTION_ERROR` | 500 | Failed to decrypt chunk |
| `RATE_LIMIT_EXCEEDED` | 429 | Too many requests |
| `INTERNAL_ERROR` | 500 | Unexpected server error |

---

## 4. CLI Flag Specification

CLI flags map directly to API parameters:

| CLI Flag | API Field | Type | Default | Description |
|----------|-----------|------|---------|-------------|
| `-u, --username` | `username` | string | `$IMPARTUS_USER` | Impartus username |
| `-p, --password` | `password` | string | `$IMPARTUS_PASS` | Impartus password |
| `--base-url` | `base_url` | string | `$IMPARTUS_BASE_URL` | API base URL |
| `--quality` | `options.quality` | string | `720` | Video quality |
| `--views` | `options.views` | string | `both` | Camera views |
| `--audio-only` | `options.audio_only` | bool | `false` | Audio extraction mode |
| `--audio-format` | `options.audio_format` | string | `mp3` | Audio output format |
| `--slides` | `options.download_slides` | bool | `false` | Download slides |
| `--output` | `options.download_location` | string | `.` | Output directory |
| `--workers` | `options.num_workers` | int | `5` | Parallel downloads |
| `--no-pipeline` | `options.enable_pipeline` | bool | `true` | Disable pipeline mode |
| `--course-id` | `course_id` | int | - | Specific course |
| `--lecture-ids` | `lectures` | []int | `[]` | Specific lectures |
| `--all-lectures` | `lectures` | string | - | Download all lectures |
| `--api-port` | (server config) | int | `8080` | API server port |
| `--server` | (server mode) | bool | `false` | Run as API server |

---

## 5. Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `IMPARTUS_USER` | No* | - | Username (required if not in CLI/config) |
| `IMPARTUS_PASS` | No* | - | Password (required if not in CLI/config) |
| `IMPARTUS_BASE_URL` | Yes | - | Impartus API base URL |
| `IMPARTUS_API_PORT` | No | `8080` | HTTP API server port |
| `IMPARTUS_DOWNLOAD_DIR` | No | `.` | Default download location |
| `IMPARTUS_TEMP_DIR` | No | `./temp` | Temporary files location |
| `IMPARTUS_WORKERS` | No | `5` | Default number of workers |
| `IMPARTUS_RATE_LIMIT` | No | `10.0` | Download rate limit (req/s) |
| `IMPARTUS_API_RATE_LIMIT` | No | `2.0` | API call rate limit (req/s) |
| `IMPARTUS_LOG_LEVEL` | No | `info` | Log level: debug, info, warn, error |

---

## 6. OpenClaw Tool Manifest

JSON Schema for registering impartus as an AI tool:

```json
{
  "name": "impartus_downloader",
  "version": "1.0.0",
  "description": "Download and decrypt Impartus lecture videos with multi-view support and audio extraction",
  "author": "impartus-go team",
  "homepage": "https://github.com/pnicto/impartus-video-downloader",
  "license": "MIT",

  "api": {
    "base_url": "http://localhost:8080",
    "authentication": {
      "type": "bearer",
      "token_endpoint": "/auth/login"  # example endpoint
    }
  },

  "endpoints": [
    {
      "name": "list_courses",
      "method": "GET",
      "path": "/api/v1/courses",
      "description": "List all available courses",
      "parameters": {
        "limit": { "type": "integer", "default": 100 },
        "offset": { "type": "integer", "default": 0 }
      },
      "response": {
        "type": "array",
        "items": { "$ref": "#/components/schemas/Course" }
      }
    },
    {
      "name": "list_lectures",
      "method": "GET",
      "path": "/api/v1/courses/{course_id}/lectures",
      "description": "List lectures for a specific course",
      "parameters": {
        "course_id": { "type": "integer", "required": true },
        "skip_empty": { "type": "boolean", "default": false }
      },
      "response": {
        "type": "array",
        "items": { "$ref": "#/components/schemas/Lecture" }
      }
    },
    {
      "name": "download_lectures",
      "method": "POST",
      "path": "/api/v1/jobs",
      "description": "Start async download job for lectures",
      "parameters": {
        "course_id": { "type": "integer", "required": true },
        "lectures": {
          "oneOf": [
            { "type": "array", "items": { "type": "integer" } },
            { "type": "string", "enum": ["all"] }
          ]
        },
        "options": { "$ref": "#/components/schemas/DownloadOptions" }
      },
      "response": {
        "type": "object",
        "properties": {
          "job_id": { "type": "string", "format": "uuid" },
          "status": { "type": "string" },
          "estimated_completion": { "type": "string", "format": "date-time" }
        }
      }
    },
    {
      "name": "job_status",
      "method": "GET",
      "path": "/api/v1/jobs/{job_id}",
      "description": "Get download job status and progress",
      "parameters": {
        "job_id": { "type": "string", "format": "uuid", "required": true }
      },
      "response": { "$ref": "#/components/schemas/JobStatus" }
    },
    {
      "name": "cancel_job",
      "method": "DELETE",
      "path": "/api/v1/jobs/{job_id}",
      "description": "Cancel a running download job",
      "parameters": {
        "job_id": { "type": "string", "format": "uuid", "required": true }
      }
    }
  ],

  "websocket": {
    "url": "ws://localhost:8080/ws",
    "events": [
      { "name": "job.started", "description": "Job started processing" },
      { "name": "job.progress", "description": "Progress update" },
      { "name": "job.lecture_completed", "description": "Individual lecture completed" },
      { "name": "job.completed", "description": "All lectures completed" },
      { "name": "job.failed", "description": "Job failed with error" }
    ]
  },

  "components": {
    "schemas": {
      "Course": {
        "type": "object",
        "properties": {
          "session_id": { "type": "integer" },
          "subject_id": { "type": "integer" },
          "subject_name": { "type": "string" },
          "professor_name": { "type": "string" },
          "video_count": { "type": "integer" }
        }
      },
      "Lecture": {
        "type": "object",
        "properties": {
          "video_id": { "type": "integer" },
          "seq_no": { "type": "integer" },
          "ttid": { "type": "integer" },
          "topic": { "type": "string" },
          "subject_name": { "type": "string" },
          "actual_duration": { "type": "integer" },
          "slide_count": { "type": "integer" }
        }
      },
      "DownloadOptions": {
        "type": "object",
        "properties": {
          "quality": { "type": "string", "enum": ["144", "450", "720"], "default": "720" },
          "views": { "type": "string", "enum": ["left", "right", "both"], "default": "both" },
          "audio_only": { "type": "boolean", "default": false },
          "audio_format": { "type": "string", "enum": ["mp3", "m4a", "aac", "opus"], "default": "mp3" },
          "download_slides": { "type": "boolean", "default": false },
          "enable_pipeline": { "type": "boolean", "default": true },
          "num_workers": { "type": "integer", "minimum": 1, "maximum": 50, "default": 5 }
        }
      },
      "JobStatus": {
        "type": "object",
        "properties": {
          "job_id": { "type": "string", "format": "uuid" },
          "status": {
            "type": "string",
            "enum": ["queued", "downloading", "joining", "completed", "failed", "cancelled"]
          },
          "progress": {
            "type": "object",
            "properties": {
              "lectures_total": { "type": "integer" },
              "lectures_completed": { "type": "integer" },
              "chunks_total": { "type": "integer" },
              "chunks_completed": { "type": "integer" },
              "percentage": { "type": "integer", "minimum": 0, "maximum": 100 }
            }
          },
          "result": {
            "type": "object",
            "properties": {
              "files": {
                "type": "array",
                "items": {
                  "type": "object",
                  "properties": {
                    "path": { "type": "string" },
                    "size": { "type": "integer" },
                    "view": { "type": "string" }
                  }
                }
              },
              "failed_chunks": { "type": "array", "items": { "type": "integer" } }
            }
          }
        }
      }
    }
  },

  "examples": [
    {
      "name": "Download all lectures from a course",
      "workflow": [
        { "ref": "list_courses" },
        { "ref": "list_lectures", "bind": { "course_id": "$.courses[0].subject_id" } },
        { "ref": "download_lectures", "params": { "lectures": "all" } },
        { "ref": "job_status", "poll": true, "until": "$.status == 'completed'" }
      ]
    }
  ]
}
```

---

## 7. Implementation Notes

### 7.1 Async Job Processing
- Jobs are queued and processed asynchronously
- Use goroutines for parallel lecture processing
- Maintain job state in memory (persist to disk for recovery)

### 7.2 Rate Limiting
- Respect existing rate limiter implementation
- Apply separate limits for API calls vs. chunk downloads
- WebSocket events bypass rate limiting

### 7.3 Progress Tracking
- Reuse existing `ProgressTracker` for metrics
- Emit WebSocket events on progress updates
- Support HTTP long-polling as fallback

### 7.4 Security
- Require Bearer token for authenticated endpoints
- Sanitize file paths to prevent directory traversal
- Validate all input parameters against existing config validation

### 7.5 Backward Compatibility
- Existing CLI mode remains unchanged
- Add `--server` flag to enable HTTP API mode
- Config file loading works for both modes

---

## Appendix A: Type Mapping

| Go Type | JSON Type | Notes |
|---------|-----------|-------|
| `LoginResponse` | auth response | Direct mapping |
| `Course` | course object | Sanitize subject_name |
| `Lecture` | lecture object | Sanitize topic, subject_name |
| `ParsedPlaylist` | Internal use | Not exposed in API |
| `Config.Quality` | options.quality | Enum: 144, 450, 720 |
| `Config.Views` | options.views | Enum: left, right, both |
| `Config.AudioOnly` | options.audio_only | Boolean |
| `Config.AudioFormat` | options.audio_format | Enum: mp3, m4a, aac, opus |

---

## Appendix B: Migration from CLI to API

| CLI Operation | API Equivalent |
|---------------|----------------|
| `LoginAndSetToken()` | `POST /api/v1/auth/login` |
| `GetCourses()` | `GET /api/v1/courses` |
| `GetLectures(course)` | `GET /api/v1/courses/{id}/lectures` |
| `DownloadPlaylist()` | `POST /api/v1/jobs` (async) |
| Interactive course selection | Client-side UI using API |
| Progress bars | WebSocket events |

---

*End of Specification*
