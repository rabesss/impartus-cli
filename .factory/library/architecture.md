# Architecture

How the impartus-go CLI/API system works.

**What belongs here:** Components, relationships, data flows, invariants.
**What does NOT belong here:** Service ports/commands (use `.factory/services.yaml`).

---

## System Overview

Impartus CLI is a Go application for downloading video lectures from the Impartus educational platform. It has two modes: CLI (interactive and JSON) and API server (REST + WebSocket).

## Key Components

### Entry Points
- `main.go` → `cli.Execute(version, date)`
- CLI mode: commands routed in `internal/cli/cli.go`
- API mode: `impartus serve` starts HTTP server via `internal/server/server.go`

### Package Map
| Package | Responsibility |
|---------|---------------|
| `cli` | CLI commands, JSON envelope, interactive mode |
| `client` | HTTP client, upstream API calls, playlist parsing |
| `config` | Config loading (file + env vars), validation, defaults |
| `downloader` | Chunk download, AES decryption, FFmpeg operations, pipeline |
| `server` | REST API handlers, WebSocket, job management, auth |

### Data Flow (API Mode)
1. Client sends request to API server (port 8080)
2. Auth middleware validates Bearer token (in-memory token store)
3. Handler creates upstream client, logs in to Impartus, fetches data
4. Response returned via envelope helpers

### Response Helpers (auth.go)
- `respondWithSuccess(w, data)` → `{success: true, data: {...}}`
- `respondWithError(w, status, code, msg, ...details)` → `{success: false, error: {code, message, details}}`
- `writeJSON(w, status, payload)` → raw JSON (being replaced by envelope in this mission)

### Job Lifecycle
1. POST /jobs creates job in memory (JobStore)
2. Job runs asynchronously via goroutine
3. Progress broadcast via WebSocket
4. State transitions: pending → running → completed/failed/canceled

### CLI JSON Envelope
```go
type jsonEnvelope struct {
    Success bool     `json:"success"`
    Data    any      `json:"data"`
    Error   *jsonErr `json:"error"`
    Meta    jsonMeta `json:"meta"`
}
```

## Key Invariants
- API auth tokens are crypto/rand 32-byte, base64url encoded, 24h expiry
- Job IDs are `job-{unixNano}` format
- Lecture indices are 1-based in CLI and API (startIndex, endIndex)
- Upstream Impartus token stored in `.token` file (mode 0600)
- Config loaded from `config.json` with env var overrides
