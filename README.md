<!-- START doctoc generated TOC please keep comment here to allow auto update -->
**Table of Contents**  *generated automatically*

<!---toc start-->

* [Impartus CLI](#impartus-cli)
  * [Features](#features)
  * [Quick Start](#quick-start)
    * [Install](#install)
* [Install from source](#install-from-source)
* [Or build from source](#or-build-from-source)
    * [Requirements](#requirements)
    * [Configuration](#configuration)
  * [CLI Usage](#cli-usage)
    * [Interactive Mode](#interactive-mode)
    * [Deterministic JSON Mode](#deterministic-json-mode)
* [Get capability metadata](#get-capability-metadata)
* [List courses](#list-courses)
* [List lectures](#list-lectures)
    * [Command Reference](#command-reference)
    * [Download Flags](#download-flags)
* [Download lectures 1-5 from course](#download-lectures-1-5-from-course)
* [Download audio only](#download-audio-only)
* [Download to custom directory](#download-to-custom-directory)
    * [API Server](#api-server)
* [Default port 8080](#default-port-8080)
* [Custom port](#custom-port)
* [JSON metadata (non-blocking)](#json-metadata-non-blocking)
  * [API Usage](#api-usage)
    * [Authentication](#authentication)
* [Login](#login)
    * [Endpoints](#endpoints)
    * [Create Download Job](#create-download-job)
    * [WebSocket Connection](#websocket-connection)
    * [WebSocket Events](#websocket-events)
  * [Development](#development)
    * [Build & Test](#build--test)
* [Build](#build)
* [Run tests](#run-tests)
* [Run linter](#run-linter)
* [Run pre-commit hooks](#run-pre-commit-hooks)
* [Quality gate scan](#quality-gate-scan)
    * [Makefile Targets](#makefile-targets)
    * [Code Quality](#code-quality)
* [Install golangci-lint](#install-golangci-lint)
* [Install pre-commit](#install-pre-commit)
    * [Running Tests](#running-tests)
* [All tests](#all-tests)
* [With coverage](#with-coverage)
* [Verbose](#verbose)
  * [Architecture](#architecture)
    * [Package Structure](#package-structure)
    * [Key Components](#key-components)
  * [Contributing](#contributing)
    * [Pull Request Process](#pull-request-process)
    * [Code Style](#code-style)
    * [Testing Requirements](#testing-requirements)
  * [License](#license)
  * [Acknowledgments](#acknowledgments)
    * [Dependencies](#dependencies)
  * [Documentation](#documentation)

<!---toc end-->
<!-- END doctoc generated TOC please keep comment here to allow auto update -->


# Impartus CLI

[![Build Status](https://github.com/rabesss/impartus-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/rabesss/impartus-cli/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/rabesss/impartus-cli)](https://goreportcard.com/report/github.com/rabesss/impartus-cli)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A Go-based CLI and HTTP API server for downloading lecture videos from Impartus platforms. Features interactive mode for humans and deterministic JSON mode for automation and AI agents.

## Features

- **Interactive CLI Mode** - Guided download flow with course/lecture selection
- **Deterministic JSON Mode** - Machine-readable output for automation and AI agent integration
- **HTTP API with WebSocket Events** - REST API with real-time job progress updates
- **Multi-View Video Processing** - Support for instructor/dual-view video streams
- **AES Encrypted Chunk Handling** - Automatic decryption of Impartus video chunks
- **Pipeline Parallelization** - Concurrent download + decrypt for faster throughput
- **Progress Tracking with ETA** - Real-time progress bars with speed and time estimates
- **Rate Limiting** - Configurable API and download rate limits
- **Slide Download Support** - Download lecture slides alongside video content

## Quick Start

### Install

```bash
# Install from source
go install github.com/rabesss/impartus-cli@latest

# Or run the container package
docker run --rm ghcr.io/rabesss/impartus-cli:main --help

# Or download the latest release asset
gh release download --repo rabesss/impartus-cli --pattern 'impartus_*_linux_amd64.tar.gz'

# Or build from source
git clone https://github.com/rabesss/impartus-cli
cd impartus-cli
go build -o impartus .
```

### Requirements

- **Go 1.25+** - Go toolchain for building
- **FFmpeg** - Required for video processing (must be in `PATH`)
- **Impartus Account** - Valid credentials for your institution's Impartus platform

### Configuration

1. Copy the sample configuration:

```bash
cp sample.config.json config.json
```

2. Edit `config.json` with your credentials:

```json
{
  "username": "your_impartus_email@example.com",
  "password": "your_impartus_password",
  "baseUrl": "https://a.impartus.com/api",
  "quality": "720",
  "views": "both",
  "downloadLocation": "./downloads"
}
```

#### Configuration Options

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `username` | string | Yes | - | Impartus username (email) |
| `password` | string | Yes | - | Impartus password |
| `baseUrl` | string | Yes | - | Impartus API base URL |
| `quality` | string | No | `"144"` | Video quality: `144`, `450`, `720` |
| `views` | string | No | `"both"` | Views: `left`, `right`, `both`, `first`, `second` |
| `downloadLocation` | string | No | `"./downloads"` | Output directory |
| `tempDirLocation` | string | No | `"./temp"` | Temporary directory |
| `slides` | bool | No | `false` | Download slides alongside video |
| `audioOnly` | bool | No | `false` | Download audio only |
| `audioFormat` | string | No | `"mp3"` | Audio format: `mp3`, `m4a`, `aac`, `opus` |
| `numWorkers` | int | No | `5` | Concurrent workers (1-50) |
| `rateLimit` | float | No | `50` | Download rate limit (req/sec) |
| `apiRateLimit` | float | No | `2` | API rate limit (req/sec) |
| `enablePipeline` | bool | No | `false` | Enable concurrent download+decrypt |
| `downloadWorkersPerLecture` | int | No | `3` | Download workers per lecture (1-10) |
| `decryptWorkersPerLecture` | int | No | `2` | Decrypt workers per lecture (1-10) |
| `httpTimeout` | string | No | `"10m"` | HTTP timeout for chunks (30s-60m) |
| `enableJitter` | bool | No | `true` | Add random delays to reduce load |
| `skipNoAudio` | bool | No | `false` | Skip lectures with no audio track |
| `progressTracking` | object | No | see below | Progress bar tracking configuration |

#### Progress Tracking Options

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable progress bar display |
| `showSpeed` | bool | `false` | Show download speed |
| `showETA` | bool | `false` | Show estimated time remaining |
| `updateInterval` | string | `"2s"` | Progress update interval (500ms-10s) |
| `speedWindowSize` | int | `10` | Speed calculation window (3-30 samples) |

#### Environment Variables

All configuration values can be overridden via environment variables:

```bash
export IMPARTUS_USERNAME="your_email"
export IMPARTUS_PASSWORD="your_password"
export IMPARTUS_BASE_URL="https://a.impartus.com/api"
```

#### Validation Rules

- `username` and `password` are required
- `baseUrl` must be a valid URL
- `quality` must be one of: `144`, `450`, `720`
- `views` must be one of: `left`, `right`, `both`, `first`, `second`
- `numWorkers` must be between 1-50
- `rateLimit` must be between 0.1-100
- `httpTimeout` must be between 30s-60m

## CLI Usage

### Interactive Mode

Run without arguments for guided download:

```bash
./impartus
```

This launches an interactive workflow:
1. Log in with configured credentials
2. Select course from list
3. Select session/lecture range
4. Download with progress tracking

### Deterministic JSON Mode

Pass `--json` for machine-readable output:

```bash
# Get capability metadata
./impartus --json

# List courses
./impartus courses --json

# List lectures
./impartus lectures -s 123 -S 456 --json
```

Response envelope:

```json
{
  "success": true,
  "data": {},
  "error": null,
  "meta": {
    "command": "courses",
    "mode": "json"
  }
}
```

On failure, `success` is `false`, `data` is `null`, and `error.message` contains the error text.

### Command Reference

| Command | Description |
|---------|-------------|
| `impartus` | Interactive mode (guided download) |
| `impartus --json` | Capability metadata |
| `impartus help` | Show usage information |
| `impartus version` | Show version and build date |
| `impartus courses` | List available courses |
| `impartus lectures -s ID -S ID` | List lectures for subject/session |
| `impartus download [flags]` | Download lectures |
| `impartus serve [--port PORT]` | Start HTTP API server |

### Download Flags

```bash
./impartus download --subject 123 --session 456 [flags]
```

| Flag | Short | Description |
|------|-------|-------------|
| `--subject` | `-s` | Subject ID (required) |
| `--session` | `-S` | Session ID (required) |
| `--start` | | Start lecture index (1-based) |
| `--end` | | End lecture index (1-based, inclusive) |
| `--quality` | | Quality: `144`, `450`, `720` |
| `--views` | | Views: `left`, `right`, `both`, `first`, `second` |
| `--audio-only` | | Audio-only mode |
| `--format` | | Audio format: `mp3`, `m4a`, `aac`, `opus` |
| `--output` | `-o` | Output directory |
| `--json` | | JSON output (non-blocking) |

**Examples:**

```bash
# Download lectures 1-5 from course
./impartus download -s 123-S 456--start 1 --end 5# Download in 720p quality
./impartus download -s 123 -S 456 --quality 720
# Download audio only
./impartus download -s 123 -S 456 --audio-only --format mp3
# Download to custom directory
./impartus download -s 123 -S 456 -o /path/to/output```

### API Server

Start the HTTP API server with job persistence:

```bash
# Jobs are persisted to .jobs.json and survive server restarts
./impartus serve
```

**Job Persistence:** Jobs are automatically saved to `.jobs.json`. Running/pending jobs at shutdown are restored as failed (non-resumable). Completed/failed/canceled jobs are restored with their preserved state.

```bash
# Default port 8080
./impartus serve

# Custom port
./impartus serve --port 9090
# JSON metadata (non-blocking)
./impartus serve --json
```

## API Usage

### Authentication

```bash
# Login
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"your_user", "password":"your_pass"}'# Response
{
  "success": true,
  "data": {
    "token": "eyJ...",
    "expires": "2025-02-12T12:34:56Z"
  }
}
```

Use the token for authenticated requests:

```bash
curl -H "Authorization: Bearer <token>" http://localhost:8080/api/v1/courses
```

### Endpoints

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| `GET` | `/api/v1/health` | No | Health check |
| `POST` | `/api/v1/auth/login` | No | Authenticate |
| `GET` | `/api/v1/courses` | Yes | List courses |
| `GET` | `/api/v1/lectures` | Yes | List lectures |
| `POST` | `/api/v1/jobs` | Yes | Create download job |
| `GET` | `/api/v1/jobs` | Yes | List all jobs |
| `GET` | `/api/v1/jobs/{id}` | Yes | Get job status |
| `DELETE` | `/api/v1/jobs/{id}` | Yes | Cancel job |
| `GET` | `/api/v1/ws` | Yes | WebSocket events |

### Health Endpoint

```bash
# Check API health
curl http://localhost:8080/api/v1/health
```

**Response:** Returns a structured `{success, data, error, meta}` envelope with sub-checks for config, upstream, and FFmpeg status:

```json
{
  "success": true,
  "data": {
    "status": "ok",
    "config": {
      "status": "ok",
      "username": "ok",
      "password": "ok",
      "baseUrl": "ok"
    },
    "upstream": {
      "status": "reachable"
    },
    "ffmpeg": {
      "status": "available"
    }
  },
  "error": null,
  "meta": {
    "command": "health",
    "mode": "api"
  }
}
```

Status values:
- `config.status`: `ok` (all fields set) or `misconfigured` (missing fields)
- `upstream.status`: `reachable` (server responds), `unreachable` (TCP/HTTP fails), or `not_configured` (no baseUrl)
- `ffmpeg.status`: `available` (in PATH) or `not_found`
- Overall `status`: `ok` (all sub-checks pass) or `degraded` (one or more sub-checks fail)

### Create Download Job

**Idempotency Key Support:** Pass an optional `idempotencyKey` field to prevent duplicate job creation on network retries. If a job with the same key already exists, returns the existing job with HTTP 409 Conflict.

```bash
curl -X POST http://localhost:8080/api/v1/jobs \
  -H "Authorization: Bearer <token>" \
  -H 'Content-Type: application/json' \
  -d '{
    "subjectId": 123,
    "sessionId": 456,
    "startIndex": 1,
    "endIndex": 5,
    "idempotencyKey": "unique-identifier-here",
    "jobConfig": {
      "quality": "720",
      "views": "both",
      "enablePipeline": true,
      "numWorkers": 6
    }
  }'
```

**Note:** API uses 1-based indexing for `startIndex` and `endIndex` (inclusive), matching CLI `--start` and `--end`.

### WebSocket Connection

Connect to receive real-time job updates:

```javascript
import WebSocket from 'ws';

const ws = new WebSocket('ws://localhost:8080/api/v1/ws', {
  headers: {
    Authorization: `Bearer ${token}`
  }
});

ws.onmessage = (event) => {
  const data = JSON.parse(event.data);
  console.log(`Event: ${data.type}`, data);
};
```

### WebSocket Events

| Event | Description |
|-------|-------------|
| `job.started` | Job began execution |
| `job.progress` | Progress update (includes phase and percentage) |
| `job.completed` | Job finished successfully |
| `job.failed` | Job failed with error |
| `job.cancelled` | Job was cancelled |

See [`docs/websocket-events.md`](docs/websocket-events.md) for complete event schemas.

## Development

### Build & Test

```bash
# Build
make build

# Run tests
make test

# Run linter
make lint

# Run pre-commit hooks
make pre-commit

# Quality gate scan
make quality-gate
```

### Makefile Targets

| Target | Description |
|--------|-------------|
| `make build` | Build the impartus binary |
| `make test` | Run tests with coverage |
| `make lint` | Run golangci-lint |
| `make pre-commit-install` | Install pre-commit hooks |
| `make pre-commit` | Run pre-commit on all files |
| `make clean` | Clean build artifacts |
| `make install` | Install to `$GOPATH/bin` |
| `make run-cli` | Run CLI interactive mode |
| `make run-api` | Start API server on port 8080 |
| `make quality-gate` | Run quality gate scan |
| `make docs` | Generate docs TOC and validate AGENTS.md |
| `make docs-toc` | Generate documentation table of contents |
| `make agents-md-validate` | Validate AGENTS.md references |
| `make security` | Run all security scans (gitleaks, gosec, trivy, govulncheck) |
| `make security-gitleaks` | Run secret scanning |
| `make security-gosec` | Run Go security analysis |
| `make security-trivy` | Run vulnerability scanning |
| `make security-govulncheck` | Run Go vulnerability check |

### Code Quality

- **golangci-lint** - Comprehensive linting with `.golangci.yml`
- **pre-commit** - Git hooks for formatting and validation
- **CI/CD** - Automated testing and linting on push/PR

Install development tools:

```bash
# Install golangci-lint
curl -sSfL https://raw.githubusercontent.com/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin

# Install pre-commit
pip install pre-commit
pre-commit install
```

### Running Tests

```bash
# All tests
go test ./...

# With coverage
go test ./... -cover -coverprofile=coverage.out
go tool cover -func=coverage.out

# Verbose
go test ./... -v
```

## Architecture

This project is **CLI-first, API-secondary**: the CLI is the primary execution path, and the API is started from `impartus serve` when needed.

### Package Structure

```
impartus/
├── main.go                 # Root entrypoint
├── cmd/impartus/main.go    # Module-style entrypoint
├── internal/
│   ├── cli/                 # Command routing and implementations
│   ├── config/              # Configuration parsing and validation
│   ├── client/              # Impartus API client, auth, HTTP helpers
│   ├── downloader/          # Playlist parsing, chunk download/decrypt, ffmpeg
│   └── server/              # HTTP API, auth middleware, jobs, WebSocket
├── docs/                    # Documentation
└── config.json              # User configuration
```

### Key Components

- **`internal/cli`** - CLI command routing and interactive/deterministic modes
- **`internal/config`** - Configuration loading, defaults, and validation
- **`internal/client`** - Impartus API HTTP client with authentication
- **`internal/downloader`** - Video pipeline: playlist parsing, chunk download, AES decryption, FFmpeg join
- **`internal/server`** - HTTP API server with bearer-token auth, background jobs, and WebSocket broadcasting

For detailed flow diagrams, see [`docs/architecture.md`](docs/architecture.md).

## Contributing

### Pull Request Process

1. Fork the repository
2. Create a feature branch (`git checkout -b feat/amazing-feature`)
3. Make changes and ensure tests pass (`make test`)
4. Run linter (`make lint`)
5. Commit with conventional commits
6. Push and create a pull request

### Code Style

- Follow [Go naming conventions](https://go.dev/doc/effective_go#names)
- Run `golangci-lint run --timeout 5m` before committing
- Install pre-commit hooks: `pre-commit install`
- Max cyclomatic complexity: 15 per function
- Max cognitive complexity: 30 per function
- Max function length: 100 lines

### Testing Requirements

- All new features require tests
- Minimum coverage threshold: 40%
- Run `go test ./... -cover` to verify coverage

## License

MIT License - see [LICENSE](LICENSE) for details.

## Acknowledgments

### Dependencies

- [gorilla/mux](https://github.com/gorilla/mux) - HTTP router
- [gorilla/websocket](https://github.com/gorilla/websocket) - WebSocket implementation
- [vbauerster/mpb](https://github.com/vbauerster/mpb) - Progress bars
- [google/uuid](https://github.com/google/uuid) - UUID generation
- [golang.org/x/time](https://pkg.go.dev/golang.org/x/time) - Rate limiting

## Documentation

- [`docs/architecture.md`](docs/architecture.md) - Architecture and flow diagrams
- [`docs/api-reference.md`](docs/api-reference.md) - REST API documentation
- [`docs/websocket-events.md`](docs/websocket-events.md) - WebSocket event schemas
- [`docs/error-codes.md`](docs/error-codes.md) - Error code reference
- [`docs/runbooks.md`](docs/runbooks.md) - Incident response and troubleshooting
- [`docs/openclaw-manifest.json`](docs/openclaw-manifest.json) - OpenClaw agent integration
