<!-- START doctoc generated TOC please keep comment here to allow auto update -->
**Table of Contents**  *generated automatically*

<!---toc start-->

* [Impartus CLI](#impartus-cli)
  * [Features](#features)
  * [Quick Start](#quick-start)
    * [Install](#install)
    * [Requirements](#requirements)
    * [Container usage](#container-usage)
    * [Configuration](#configuration)
  * [CLI Usage](#cli-usage)
    * [Interactive Mode](#interactive-mode)
    * [Deterministic JSON Mode](#deterministic-json-mode)
    * [Command Reference](#command-reference)
    * [Download / Play Flags](#download--play-flags)
    * [API Server](#api-server)
  * [API Usage](#api-usage)
    * [Authentication](#authentication)
    * [Endpoints](#endpoints)
    * [Health Endpoint](#health-endpoint)
    * [Create Download Job](#create-download-job)
    * [WebSocket Connection](#websocket-connection)
    * [WebSocket Events](#websocket-events)
  * [Development](#development)
    * [Build & Test](#build--test)
    * [Makefile Targets](#makefile-targets)
    * [Running Tests](#running-tests)
  * [Architecture](#architecture)
    * [Package Structure](#package-structure)
    * [Key Components](#key-components)
  * [Contributing](#contributing)
  * [License](#license)
  * [Acknowledgments](#acknowledgments)
    * [Dependencies](#dependencies)
  * [Documentation](#documentation)

<!---toc end-->
<!-- END doctoc generated TOC please keep comment here to allow auto update -->

# Impartus CLI

[![Build Status](https://github.com/rabesss/impartus-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/rabesss/impartus-cli/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A Go-based CLI/TUI and HTTP API server for browsing, streaming, and downloading lecture videos from Impartus platforms. The terminal workspace is for humans; deterministic JSON remains available for automation and AI agents.

## Features

- **OpenTUI Terminal Workspace** - A responsive desktop-style course, lecture, playback, download, library, and diagnostics workspace
- **Deterministic JSON Mode** - Machine-readable output for automation and AI agent integration
- **HTTP API with WebSocket Events** - REST API with real-time job progress updates
- **Multi-View Video Processing** - Support for instructor/dual-view video streams
- **AES Encrypted Chunk Handling** - Automatic decryption of Impartus video chunks
- **Pipeline Parallelization** - Concurrent download + decrypt for faster throughput
- **Progress Tracking with ETA** - Real-time progress bars with speed and time estimates
- **Rate Limiting** - Configurable API and download rate limits
- **Slide Download Support** - Download lecture slides alongside video content
- **Supervised mpv Playback** - Private JSON IPC provides playback state and controls without exposing the loopback stream capability in process arguments

## Quick Start

### Install

```bash
# Or run the container package
docker run --rm ghcr.io/rabesss/impartus-cli:main --help

# Download the latest release asset (recommended for the TUI)
gh release download --repo rabesss/impartus-cli --pattern 'impartus_*_linux_amd64.tar.gz'
tar -xzf impartus_*_linux_amd64.tar.gz

# Or build from source
git clone https://github.com/rabesss/impartus-cli
cd impartus-cli
make build
```

`make build` and `make build-go` stamp one current UTC RFC3339 build date into
the Go binary. Reproducible builds can provide an explicit timestamp or the
standard Unix-seconds epoch; an explicit non-empty `BUILD_DATE` takes
precedence:

```bash
make build BUILD_DATE=2026-08-22T10:20:30Z
make build SOURCE_DATE_EPOCH=0
```

For these Make targets, invalid metadata fails the build instead of silently
using the wall clock.
Direct unstamped `go build` binaries report `Build Date: unknown`; JSON version
output keeps the `buildDate` field and uses the same value. A local Docker build
without a `BUILD_DATE` argument likewise displays `unknown` through this runtime
fallback; container label/timestamp selection remains owned by the packaging
workflow.

Keep the release's `impartus` and `impartus-ui` executables together in the
same directory. The Go parent owns credentials, networking, SQLite, mpv, and
downloads; the adjacent OpenTUI executable owns terminal presentation only.
`go install github.com/rabesss/impartus-cli@latest` still builds the headless Go
commands, but cannot install the compiled TUI sidecar.

Linux TUI release sidecars currently target glibc. The Go parent remains
CGO-free and its explicit non-TUI commands continue to work without the
sidecar; musl-only systems should build the UI locally or use those commands.

### Requirements

- **Go 1.25+** - Go toolchain for building (pinned in `go.mod`; Docker images may use a newer patch release)
- **Bun 1.3.14** - Required only when building the OpenTUI frontend from source
- **FFmpeg** - Required for video processing (must be in `PATH`)
- **mpv** - Required for TUI or CLI playback (must be in `PATH`)
- **Impartus Account** - Valid credentials for your institution's Impartus platform

### Container usage

The `--help` command above is a quick image and entrypoint smoke test. For a
real download, mount the configuration read-only and provide writable download
and temporary directories:

```bash
mkdir -p downloads temp
docker run --rm \
  --volume "$PWD/config.json:/work/config.json:ro" \
  --volume "$PWD/downloads:/work/downloads" \
  --volume "$PWD/temp:/work/temp" \
  ghcr.io/rabesss/impartus-cli:main \
  download --subject 123 --session 456
```

The image runs as the non-root `impartus` user. Bind-mounted `downloads` and
`temp` directories retain their host ownership and permissions, so they must be
writable by that container user. API jobs are stored in `/work/.jobs.json`; the
file is ephemeral when the container is removed unless `/work` is persisted.

### Configuration

1. Create a private configuration file:

```bash
make config-init
```

This copies the sample when `config.json` does not exist and sets owner-only
permissions (`0600`). If the file already exists, the command only tightens its
permissions and never overwrites your configuration. You can apply the same
protection manually with `chmod 600 config.json`.

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
| `quality` | string | No | `"720"` | Video quality: `144`, `450`, `720` |
| `views` | string | No | `"both"` | Views: `left`, `right`, `both`, `first`, `second` |
| `downloadLocation` | string | No | `"./downloads"` | Output directory |
| `tokenCachePath` | string | No | `".token"` | Private bearer-token cache path; `IMPARTUS_TOKEN_CACHE` overrides it |
| `tempDirLocation` | string | No | `"./temp"` | Temporary directory |
| `slides` | bool | No | `false` | Download slides alongside video |
| `audioOnly` | bool | No | `false` | Download audio only |
| `audioFormat` | string | No | `"mp3"` | Audio format: `mp3`, `m4a`, `aac`, `opus` |
| `numWorkers` | int | No | `5` | Concurrent lecture workers (1-50); active playlist downloads are bounded by per-lecture media workers to preserve the browser-observed burst envelope |
| `rateLimit` | float | No | `100` | Download rate limit (0.1-100 req/sec) |
| `apiRateLimit` | float | No | `2` | API rate limit (0.1-20 req/sec) |
| `enablePipeline` | bool | No | `true` | Use bounded concurrent download+decrypt; set `false` to keep the legacy serial path |
| `downloadWorkersPerLecture` | int | No | `12` | Download workers per lecture (1-12) |
| `decryptWorkersPerLecture` | int | No | `4` | Decrypt workers per lecture (1-10) |
| `httpTimeout` | string | No | `"10m"` | Timeout for the shared upstream HTTP client, including login, API, playlist, and media requests (30s-60m) |
| `enableJitter` | bool | No | `true` | Add small random delays to API requests |
| `skipNoAudio` | bool | No | `false` | Skip lectures with no audio track |
| `listenAddr` | string | No | `"127.0.0.1"` | API server bind address (loopback only unless `allowRemoteAccess` is set) |
| `allowRemoteAccess` | bool | No | `false` | Permit a non-loopback `listenAddr` (e.g. `0.0.0.0`); required to expose the API on the network |
| `progressTracking` | object | No | see below | Progress bar tracking configuration |
| `watch` | object | No | disabled | Generic durable lecture auto-download configuration |

The token cache is secured before bearer-token bytes are written. Unix uses a
mode-0600 temporary file and a no-follow descriptor read; Windows uses a
protected DACL limited to the current user, SYSTEM, and Administrators plus a
reparse-point-safe handle open. Publication uses an atomic same-directory
replacement, with write-through semantics on Windows. A Windows cache created
by an older release may be rejected once when its DACL is inherited instead of
protected; the next successful sign-in rewrites it with the protected DACL.
On Unix, a pre-existing cache with group or other permission bits is likewise
rejected and rewritten with mode `0600` after the next successful sign-in.
Symlinked token caches are rejected; point `tokenCachePath` or
`IMPARTUS_TOKEN_CACHE` directly at the regular target file instead.

For practical tuning profiles, failure symptoms, and a safe way to benchmark
your connection, see [Download performance tuning](docs/download-performance.md).

#### Progress Tracking Options

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable all progress bars in human-readable mode; JSON mode remains quiet |
| `showSpeed` | bool | `false` | Include download speed in the aggregate progress status |
| `showETA` | bool | `false` | Include estimated time remaining in the aggregate progress status |
| `updateInterval` | string | `"2s"` | Speed-sampling interval (500ms-10s) |
| `speedWindowSize` | int | `10` | Number of samples used for the speed moving average (3-30) |

#### Watch Options

The watcher owns Impartus polling and completed local artifacts only. Configure
one or more course targets; downstream tools consume the committed manifest or
the NDJSON event stream independently.

```json
{
  "watch": {
    "enabled": true,
    "pollInterval": "10m",
    "maxLecturesPerCycle": 3,
    "maxRetries": 3,
    "quality": "144",
    "views": "left",
    "audioFormat": "mp3",
    "targets": [
      {"subjectId": 123, "sessionId": 456, "label": "Algorithms"}
    ]
  }
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Validate and enable configured watch targets |
| `pollInterval` | string | `"5m"` | Delay between cycles, from 5 minutes to 24 hours |
| `maxLecturesPerCycle` | int | `3` | Global new-lecture budget across all targets |
| `maxRetries` | int | `3` | Playlist/download attempts with bounded backoff |
| `targets` | array | none | Unique `subjectId`/`sessionId` pairs with optional display labels |
| `quality` | string | `"144"` | Watch download quality |
| `views` | string | `"left"` | Watch view selection: `left`, `right`, or `both` |
| `audioFormat` | string | `"mp3"` | Watch audio output: `mp3`, `m4a`, `aac`, or `opus` |

Watch downloads are always audio-only and skip lectures marked as having no
audio. There are no remote-provider credentials or routing fields in this
configuration.

#### Environment Variables

Only the settings listed below have environment-variable overrides. Settings absent from this table must be configured in JSON.

| Environment variable | Config field | Notes |
|----------------------|--------------|-------|
| `IMPARTUS_USERNAME` | `username` | Required unless supplied in JSON |
| `IMPARTUS_PASSWORD` | `password` | Required unless supplied in JSON |
| `IMPARTUS_BASE_URL` | `baseUrl` | Required unless supplied in JSON |
| `IMPARTUS_QUALITY` | `quality` | `144`, `450`, or `720` |
| `IMPARTUS_VIEWS` | `views` | `left`, `right`, `both`, `first`, or `second` |
| `IMPARTUS_DOWNLOAD_LOCATION` | `downloadLocation` | Output directory |
| `IMPARTUS_TOKEN_CACHE` | `tokenCachePath` | Private bearer-token cache path; defaults to legacy `.token` |
| `IMPARTUS_TEMP_DIR` | `tempDirLocation` | Temporary directory |
| `IMPARTUS_TEMP_DIR_LOCATION` | `tempDirLocation` | Compatibility alias for the shorter temporary-directory variable |
| `IMPARTUS_AUDIO_FORMAT` | `audioFormat` | `mp3`, `m4a`, `aac`, or `opus` |
| `IMPARTUS_HTTP_TIMEOUT` | `httpTimeout` | Go duration between 30s and 60m |
| `IMPARTUS_LISTEN_ADDR` | `listenAddr` | Non-loopback values also require remote-access opt-in |
| `IMPARTUS_AUDIO_ONLY` | `audioOnly` | Boolean |
| `IMPARTUS_SLIDES` | `slides` | Boolean |
| `IMPARTUS_SKIP_NO_AUDIO` | `skipNoAudio` | Boolean |
| `IMPARTUS_ALLOW_REMOTE_ACCESS` | `allowRemoteAccess` | Boolean |
| `IMPARTUS_ENABLE_PIPELINE` | `enablePipeline` | Boolean; `false` preserves the legacy serial path |
| `IMPARTUS_ENABLE_JITTER` | `enableJitter` | Boolean |
| `IMPARTUS_PROGRESS_TRACKING_ENABLED` | `progressTracking.enabled` | Boolean; controls all progress bars |
| `IMPARTUS_NUM_WORKERS` | `numWorkers` | Integer from 1-50 |
| `IMPARTUS_RATE_LIMIT` | `rateLimit` | Number from 0.1-100 |
| `IMPARTUS_API_RATE_LIMIT` | `apiRateLimit` | Number from 0.1-20 |
| `IMPARTUS_WATCH_ENABLED` | `watch.enabled` | Boolean |
| `IMPARTUS_WATCH_POLL_INTERVAL` | `watch.pollInterval` | Duration from 5m to 24h |
| `IMPARTUS_WATCH_MAX_LECTURES_PER_CYCLE` | `watch.maxLecturesPerCycle` | Positive integer |
| `IMPARTUS_WATCH_MAX_RETRIES` | `watch.maxRetries` | Positive integer |
| `IMPARTUS_WATCH_QUALITY` | `watch.quality` | `144`, `450`, or `720` |
| `IMPARTUS_WATCH_VIEWS` | `watch.views` | `left`, `right`, or `both` |
| `IMPARTUS_WATCH_AUDIO_FORMAT` | `watch.audioFormat` | `mp3`, `m4a`, `aac`, or `opus` |

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

Run without arguments from a real terminal, or invoke the workspace explicitly:

```bash
./impartus
./impartus tui
```

The compiled OpenTUI workspace owns the alternate screen while mpv renders in a
separate native window. It uses a desktop-style three-pane layout on wide
terminals, two panes at medium widths, and a routed single pane on narrow
terminals. It supports live course/lecture browsing, `/` filters, `enter`
playback with recorded resume state, `d` download, `l` library, `!` dependency
diagnostics, and mpv pause/seek/volume/mute/speed/camera controls. Press `?` for
the command guide. It never falls back to blocking legacy mpv.

The Go parent starts a private, authenticated loopback session for each TUI
launch and passes one one-use owner-private bootstrap file to the child. The
capability never appears in argv or the child environment, and the child does
not inherit Impartus credentials.

No arguments launch the TUI only when both stdin and stdout are terminals.
Otherwise, Impartus prints help to stderr and exits 2 rather than consuming a
pipeline.

### Command Help

Every command in the command reference accepts `--help` and `-h`, including
`library list`, `library show`, and `library verify`:

```bash
./impartus courses --help
./impartus library verify -h
```

Explicit help is resolved before command parsing or command startup. A help
token before the argument sentinel (`--`) wins even when another argument is
invalid, so `./impartus download --start bad --help` prints download help and
exits 0 without loading credentials or starting command dependencies. A help
token after `--` remains a literal positional argument.

### Deterministic JSON Mode

Pass `--json` for machine-readable output:

```bash
# Get capability metadata
./impartus --json

# Get command help as one JSON envelope (either flag order is equivalent)
./impartus download --help --json
./impartus download --json --help

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

Root JSON help (`--json`, `--json --help`, or `--help --json`) keeps the root
capability payload returned by `./impartus --json`, including `data.name` and
`data.commands`. Command-specific JSON help writes exactly one successful
envelope with `meta.command` set to `help` and this stable `data` schema:

```json
{
  "command": "download",
  "description": "Download lectures and record completed media in the local library.",
  "usage": [
    "impartus download --subject <id> --session <id> [--ttid <id> | --start <n> --end <n>] [flags]"
  ]
}
```

`--help --json` and `--json --help` are equivalent. The target command is
identified by `data.command`; the envelope metadata deliberately identifies the
operation as `help`. Nested library commands use dotted identifiers:
`library.list`, `library.show`, and `library.verify`.

Successful JSON commands write exactly one response envelope to stdout. They do
not write progress bars or warning text, and successful downloads leave stderr
empty. Failed JSON commands exit non-zero and write exactly one error envelope
to stderr while leaving stdout empty; in that envelope, `success` is `false`,
`data` is `null`, and `error.message` contains the error text.
The diagnostic `doctor` command is the one intentional exception: on failure,
`data` contains the full per-check report so automation can identify the broken
dependency or path while the envelope remains unsuccessful and exits non-zero.

For JSON downloads, `lectureCount` is the number of lectures completed.
`outputPaths` contains the files produced, so one completed lecture can add
multiple paths when multiple views or output forms are requested. The additive
`artifacts` array contains one versioned manifest per completed lecture with its
scoped IDs, normalized media selection, verified absolute files, byte sizes,
and a stable `impartus:v1:` logical identity. Existing automation can continue
using the legacy fields unchanged. `libraryRecorded` reports whether all
manifests were committed to the local SQLite library. If media completed but
that additive commit failed, the command still succeeds, sets the field to
`false`, and places exactly one diagnostic in `meta.warnings` without writing
unstructured JSON-mode stderr.

`selection.views` records the normalized requested view set used for logical
identity. `files` is the authoritative materialization list: an upstream
lecture can expose only one playable camera even when `both` was requested, so
consumers must use each file's `view` rather than infer output completeness from
the selection alone.

### Command Reference

| Command | Description |
|---------|-------------|
| `impartus` | Launch the TUI when stdin/stdout are terminals; otherwise help + exit 2 |
| `impartus tui` | Explicitly launch the course/lecture terminal workspace |
| `impartus --json` | Capability metadata |
| `impartus help` | Show usage information |
| `impartus COMMAND --help` / `impartus COMMAND -h` | Show deterministic command-specific help without starting the command |
| `impartus version` | Show version and build date |
| `impartus courses` | List available courses |
| `impartus lectures -s ID -S ID` | List lectures for subject/session |
| `impartus download [flags]` | Download lectures |
| `impartus watch [flags]` | Poll configured targets and durably download new lectures |
| `impartus play [flags]` | Play lectures in mpv |
| `impartus doctor` | Check mpv, FFmpeg, credential permissions, and private writable state/runtime paths |
| `impartus library list` | List logical artifacts and materialized file counts |
| `impartus library show ARTIFACT_ID` | Show one artifact and every known local path |
| `impartus library verify [--hash] [ARTIFACT_ID]` | Mark missing/changed paths without deleting records; optionally fill or recheck SHA-256 |
| `impartus serve [--port PORT]` | Start HTTP API server |

### Download / Play Flags

```bash
./impartus download --subject 123 --session 456 [flags]
./impartus play --subject 123 --session 456 [flags]
```

| Flag | Short | Description | Applicable To |
|------|-------|-------------|---------------|
| `--subject` | `-s` | Subject ID (required) | Both |
| `--session` | `-S` | Session ID (required) | Both |
| `--start` | | Start lecture index (1-based) | Both |
| `--end` | | End lecture index (1-based, inclusive) | Both |
| `--ttid` | | Exact positive lecture TTID; mutually exclusive with `--start`/`--end` | Download Only |
| `--lecture` | `-l` | Specific lecture index (shortcut for start & end) | Play Only |
| `--mpv-mode` | | `ipc` (supervised default; `legacy` defaults on Windows) or explicit compatibility mode `legacy` | Play Only |
| `--quality` | | Quality: `144`, `450`, `720` | Both |
| `--views` | | Views: `left`, `right`, `both`, `first`, `second` | Both |
| `--audio-only` | | Audio-only mode | Download Only |
| `--format` | | Audio format: `mp3`, `m4a`, `aac`, `opus` | Download Only |
| `--output` | `-o` | Output directory | Download Only |
| `--json` | | JSON output (non-blocking) | Download Only |
| `--events` | | NDJSON lifecycle stream; mutually exclusive with `--json` | Download Only |

**Examples:**

```bash
# Download lectures 1-5 from course
./impartus download -s 123 -S 456 --start 1 --end 5

# Download exactly one lecture by upstream TTID
./impartus download -s 123 -S 456 --ttid 10913022

# Download in 720p quality
./impartus download -s 123 -S 456 --quality 720

# Download audio only
./impartus download -s 123 -S 456 --audio-only --format mp3

# Download to custom directory
./impartus download -s 123 -S 456 -o /path/to/output

# Play lectures 1-5 from course
./impartus play -s 123 -S 456 --start 1 --end 5

# Play a specific lecture
./impartus play -s 123 -S 456 --lecture 3

# Browse, stream, control, download, and resume interactively
./impartus tui

# Diagnose local playback/download prerequisites and private paths
./impartus doctor

# Inspect and non-destructively verify completed local media
./impartus library list
./impartus library show 'impartus:v1:...'
./impartus library verify --hash

# Preview new lectures once without creating media or jobs
./impartus watch --once --dry-run -s 123 -S 456

# Run one durable cycle and stream machine-readable lifecycle events
./impartus watch --once --events -s 123 -S 456

# Temporary compatibility path; IPC failures never select this automatically
./impartus play -s 123 -S 456 --lecture 3 --mpv-mode legacy
```

### Durable Watch

`impartus watch` reads `watch.targets`, or accepts one target through
`--subject/-s` and `--session/-S`. JSON mode, dry-run, and `--once` each run one
cycle. Without them, the command sleeps for `watch.pollInterval` and continues
until signaled.

| Flag | Description |
|------|-------------|
| `--subject,-s` and `--session,-S` | Replace configured targets with one course |
| `--interval` | Override the polling interval |
| `--output,-o` | Override the download directory |
| `--once` | Run one cycle and exit |
| `--dry-run` | Discover and report without media or job mutations |
| `--events` | Emit synchronous NDJSON lifecycle records to stdout |
| `--force` | Explicitly redownload a present committed artifact for one cycle |

The per-cycle budget is global across targets. A failed target is recorded but
does not prevent later targets from running; a one-shot cycle with any failure
exits non-zero and emits one aggregate `job.failed` terminal record. See
[`docs/cli-events.md`](docs/cli-events.md) for the stream contract.
Signal cancellation emits `job.canceled` in events mode and exits 130.

The default `ipc` mode starts mpv idle with user configuration and scripts
disabled, creates an owner-private Unix socket, and sends the tokenized local
stream URL only after that socket is verified. It observes playback state and
reaps mpv on normal exit, cancellation, or forced shutdown. The local HLS proxy
accepts only its exact loopback Host and unguessable session paths. `legacy`
retains the previous blocking process launch as an explicit one-release
compatibility option; an IPC error never falls back to it.

`impartus doctor` may create the application-owned state directory at
`$XDG_STATE_HOME/impartus` (or `~/.local/state/impartus`) with mode `0700` so it
can verify private writable storage. Missing `config.json` and the configured
token-cache file (legacy default `.token`) are warnings because environment-only
configuration and a first login are supported; an existing credential file with
group/world permissions is a
blocking failure. The doctor also opens and migrates the local library, verifies
WAL and private permissions, and fails instead of attempting an automatic
repair when the database is incompatible.

### Local Lecture Library

Completed manifests, every materialized path, playback resume state, and local
download/watch job state live in `$XDG_STATE_HOME/impartus/library.db` (or
`~/.local/state/impartus/library.db`). Unix enforces mode `0700` on the parent
and `0600` on the database. Windows creates a protected DACL limited to the
current user, SYSTEM, and Administrators, rejects broader existing ACLs, and
retains symlink and file-type checks. The pure-Go SQLite
store enables WAL, foreign keys, `synchronous=FULL`, a bounded busy timeout, and
transactional forward-only migrations, so CGO is not required.

Logical artifacts are keyed by the stable manifest identity rather than by a
filename. Re-downloading the same selection elsewhere adds another file row;
it does not discard the older path. `library verify` checks regular-file type
and size and updates presence metadata without deleting user data. Hashing is
deliberately opt-in: `library verify --hash` fills or rechecks SHA-256 metadata.
Playback checkpoints reject far-future timestamps, merge equal-time updates,
and keep completion sticky so out-of-order delivery cannot regress resume.

FFmpeg writes final media to a same-directory `.part` path, syncs it, and then
atomically replaces the final path. Colliding in-process writers to one final
path are serialized, and every pre-publication failure removes its `.part`.
The library's durable job API can record expected final paths before work and
mark interrupted `running` jobs
`recoverable`. When all expected outputs already validate, recovery commits the
artifact without another Impartus fetch; incomplete `.part` files are never
considered complete. Startup recovery must run while the watcher holds its
single-instance lock and before workers start; the recovery method is never an
implicit database-open side effect. Event-mode watcher runs carry each recovered
manifest across preflight and emit it immediately after `job.started`, before
the first lecture cycle. Current one-shot CLI downloads best-effort
record their completed manifests but do not create lifecycle job rows. The
generic watcher creates a UUIDv4 `watch` job before final-output work, marks it
running, and calls the atomic artifact-plus-job completion transaction only
after every expected final file validates. It reuses recoverable jobs after an
interrupted process and skips a present committed artifact on later cycles
unless `--force` is explicit. These local jobs are separate from the existing
HTTP API server's `.jobs.json` compatibility store.

Only one watcher can own the state directory at a time. `watch.lock` is an OS
advisory lock rather than a database flag; closing the process or SIGKILL
releases ownership in the kernel, so the small lock file may safely remain.
Startup acquires that lock and recovers interrupted jobs before login or other
network work.

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
  -d '{"username":"your_user", "password":"your_pass"}'
```

Response:
```json
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
| `GET` | `/api/v1/health/live` | No | Process liveness check |
| `GET` | `/api/v1/health/ready` | No | Cached dependency readiness check |
| `GET` | `/api/v1/health` | No | Compatibility alias for readiness |
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
# Use this for process or container liveness probes (no dependency checks)
curl http://localhost:8080/api/v1/health/live

# Use this for dependency readiness
curl http://localhost:8080/api/v1/health/ready
```

`/api/v1/health/live` returns the standard envelope with `data.status` set to `ok` and performs no configuration, network, token-cache, filesystem, or executable checks.

`/api/v1/health/ready` returns a structured `{success, data, error, meta}` envelope with sub-checks for config, upstream, and FFmpeg status. `/api/v1/health` remains a compatibility alias with the same readiness response:

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
- Any sub-check may report `unknown` if readiness probing fails internally; inspect server logs for details
- Overall `status`: `ok` (all sub-checks pass) or `degraded` (one or more sub-checks fail)

The unauthenticated health response deliberately exposes only aggregate configuration status; it does not reveal which credential fields are present.
Readiness results, including degraded results, are cached for 15 seconds and may be that old. Readiness endpoints retain HTTP 200 when degraded, so callers must inspect `data.status` rather than relying on the HTTP status alone.

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

WebSocket events are live notifications, not a durable event stream. A client
that stops reading may be disconnected when its bounded outbound queue fills.
Reconnect and query `GET /api/v1/jobs/{id}` to recover the current job state;
events that occurred while disconnected are not replayed.

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

```

### Makefile Targets

| Target | Description |
|--------|-------------|
| `make build` | Build the Go parent and adjacent OpenTUI frontend |
| `make test` | Run tests with coverage |
| `make lint` | Run golangci-lint |
| `make pre-commit-install` | Install pre-commit hooks |
| `make pre-commit` | Run pre-commit on all files |
| `make clean` | Clean build artifacts |
| `make install` | Install to `$GOPATH/bin` |
| `make run-cli` | Run CLI interactive mode |
| `make run-api` | Start API server on port 8080 |
| `make docs` | Generate docs table of contents |
| `make docs-toc` | Generate documentation table of contents |
| `make security` | Run all security scans (gitleaks, gosec, trivy, govulncheck) |
| `make security-gitleaks` | Run secret scanning |
| `make security-gosec` | Run Go security analysis |
| `make security-trivy` | Run vulnerability scanning |
| `make security-govulncheck` | Run Go vulnerability check |

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
│   ├── app/                 # Shared catalog/playback orchestration seam
│   ├── tuihost/             # OpenTUI child lifecycle and private bootstrap ownership
│   ├── tuiproto/            # Generated versioned Go/TypeScript session contract
│   ├── tuisession/          # Authenticated loopback projections, operations, and events
│   ├── player/              # Supervised mpv process and bounded JSON IPC
│   ├── library/             # Pure-Go SQLite artifacts, playback, and local jobs
│   ├── events/              # Shared synchronous CLI NDJSON lifecycle contract
│   ├── watch/               # Generic polling, advisory lock, and durable downloads
│   └── server/              # HTTP API, auth middleware, jobs, WebSocket
├── ui/                      # OpenTUI TypeScript frontend compiled by Bun
├── docs/                    # Documentation
└── config.json              # User configuration
```

### Key Components

- **`internal/cli`** - CLI command routing, TTY-aware OpenTUI launch, and deterministic modes
- **`internal/config`** - Configuration loading, defaults, and validation
- **`internal/client`** - Impartus API HTTP client with authentication
- **`internal/downloader`** - Video pipeline: playlist parsing, chunk download, AES decryption, FFmpeg join
- **`internal/app`** - Shared catalog, playback, download, artifact, library, and resume orchestration boundary
- **`internal/tuihost`** - Resolves and supervises the adjacent frontend while preserving terminal cleanup
- **`internal/tuiproto`** - One strict schema generated into matching Go and TypeScript types
- **`internal/tuisession`** - Private per-launch transport that projects safe state and keeps mutable work in Go
- **`ui`** - OpenTUI rendering, responsive routing, filtering, command help, and session-client validation
- **`internal/player`** - Private mpv runtime, process-group supervision, bounded JSON IPC, events, and typed controls
- **`internal/library`** - Private SQLite migrations, artifact paths, verification, resume history, and recoverable local jobs
- **`internal/events`** - Single-terminal NDJSON lifecycle events for automation
- **`internal/watch`** - Provider-neutral polling and durable artifact completion
- **`internal/server`** - HTTP API server with bearer-token auth, background jobs, and WebSocket broadcasting

For detailed flow diagrams, see [`docs/architecture.md`](docs/architecture.md).

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md) for local setup, PR guidelines, and code style expectations.

Security-sensitive changes are described in [`SECURITY.md`](SECURITY.md).

## License

MIT License - see [LICENSE](LICENSE) for details.

## Acknowledgments

### Dependencies

- [gorilla/mux](https://github.com/gorilla/mux) - HTTP router
- [gorilla/websocket](https://github.com/gorilla/websocket) - WebSocket implementation
- [vbauerster/mpb](https://github.com/vbauerster/mpb) - Progress bars
- [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) - Pure-Go SQLite driver
- [google/uuid](https://github.com/google/uuid) - UUID generation
- [golang.org/x/time](https://pkg.go.dev/golang.org/x/time) - Rate limiting

## Documentation

- [`CONTRIBUTING.md`](CONTRIBUTING.md) - Contributing guide
- [`SECURITY.md`](SECURITY.md) - Security policy
- [`docs/architecture.md`](docs/architecture.md) - Architecture and flow diagrams
- [`docs/api-reference.md`](docs/api-reference.md) - REST API documentation
- [`docs/websocket-events.md`](docs/websocket-events.md) - WebSocket event schemas
- [`docs/error-codes.md`](docs/error-codes.md) - Error code reference
- [`docs/runbooks.md`](docs/runbooks.md) - Incident response and troubleshooting
- [`docs/download-performance.md`](docs/download-performance.md) - Download worker and rate-limit tuning
