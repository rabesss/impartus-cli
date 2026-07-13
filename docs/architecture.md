<!-- START doctoc generated TOC please keep comment here to allow auto update -->
**Table of Contents**  *generated automatically*

<!---toc start-->

* [Architecture](#architecture)
  * [CLI interactive mode flow](#cli-interactive-mode-flow)
  * [CLI deterministic JSON mode flow](#cli-deterministic-json-mode-flow)
  * [CLI play command flow](#cli-play-command-flow)
  * [API authenticated job lifecycle flow](#api-authenticated-job-lifecycle-flow)
  * [Internal package/module boundaries](#internal-packagemodule-boundaries)

<!---toc end-->
<!-- END doctoc generated TOC please keep comment here to allow auto update -->

# Architecture

This project is CLI-first and API-secondary: the CLI is the primary execution path, and the API is started from `impartus serve` when needed.

## CLI interactive mode flow

The default mode (`impartus` with no command) runs an interactive download workflow.

```mermaid
flowchart TD
  A[User runs impartus] --> B[cli.Execute]
  B --> C{Check json flag}
  C -- No --> D[runInteractive]
  D --> E[loadConfig + apply defaults]
  E --> F[client.LoginAndSetToken]
  F --> G[Fetch courses and lectures]
  G --> H[Prompt selection + range]
  H --> I[downloader.FetchLecturePlaylists]
  I --> J[Download/decrypt/join outputs]
  J --> K[Write files to downloads path]
  C -- Yes --> L[Dispatch non-interactive command]
```

## CLI deterministic JSON mode flow

Passing `--json` switches command handling to deterministic response envelopes for automation.

```mermaid
sequenceDiagram
  participant A as Automation/Agent
  participant C as CLI (Execute)
  participant J as executeJSON

  A->>C: impartus --json [command]
  C->>C: stripGlobalJSONFlag(args)
  alt no command provided
    C-->>A: success help envelope
  else command provided
    C->>J: executeJSON(args)
    alt courses / lectures
      J-->>A: success envelope with fetched data
    else download
      J-->>A: run command silently + success envelope
    else serve
      J-->>A: non-blocking ready metadata (no server start)
    else failure/unknown command
      J-->>A: error envelope ({success:false})
    end
  end
```

The stream boundary is part of the JSON-mode contract. Success writes exactly
one envelope to stdout and writes no progress or warning text; successful
downloads leave stderr empty. Failure returns a non-zero exit status, leaves
stdout empty, and writes exactly one error envelope to stderr. For download
results, `lectureCount` counts completed lectures, while `outputPaths` may hold
multiple files for each lecture.

## CLI play command flow

The `play` command streams lectures directly in **mpv** without writing output files or invoking FFmpeg join.

```mermaid
flowchart TD
  A[impartus play -s ID -S ID] --> B[cli.runPlay]
  B --> C[loadConfig + apply defaults]
  C --> D[client.LoginAndSetToken]
  D --> E[Fetch lectures in range]
  E --> F[downloader.PlayLectures via mpv]
  F --> G[Stream HLS to mpv]
```

Requires **mpv** on `PATH`. Supports the same `--start`/`--end` range flags as download (1-based inclusive).

## API authenticated job lifecycle flow

The API lifecycle is token-gated and executes downloads asynchronously in background jobs.

```mermaid
sequenceDiagram
  participant U as API Client
  participant S as APIServer
  participant T as TokenStore
  participant JS as JobStore
  participant W as executeJob goroutine
  participant WS as WebSocket clients

  U->>S: POST /api/v1/auth/login
  S->>T: Store token (24h expiry)
  S-->>U: {success:true,data.token}
  U->>S: POST /api/v1/jobs (Bearer token)
  S->>T: Validate token
  S->>JS: CreateJob(status=pending)
  S-->>U: 201 Created job
  S->>W: go executeJob(jobID)
  W->>JS: Update status/progress (running -> completed/failed/cancelled)
  W-->>WS: Broadcast job.started/progress/completed/failed/cancelled
  U->>S: GET /api/v1/jobs/{id}
  S->>JS: Read current job state
  S-->>U: Job JSON (status, progress, outputs/error)
```

**Upstream token cache:** When handling `/courses`, `/lectures`, and job execution, `APIServer` caches the authenticated Impartus HTTP client and upstream token for approximately **23 hours** (tokens are typically valid for 24h). This avoids re-login on every API request while still refreshing expired entries.

## Internal package/module boundaries

Core boundaries keep command orchestration in `internal/cli`, network access in `internal/client`, media pipeline in `internal/downloader`, and HTTP orchestration in `internal/server`.

```mermaid
flowchart LR
  subgraph Entrypoints
    M1[main.go]
    M2[cmd/impartus/main.go]
  end

  subgraph Internal
    CLI[internal/cli]
    CFG[internal/config]
    CLT[internal/client]
    DL[internal/downloader]
    SRV[internal/server]
  end

  IMP[(Impartus APIs)]
  FS[(Local files + ffmpeg)]

  M1 --> CLI
  M2 --> CLI
  CLI --> CFG
  CLI --> CLT
  CLI --> DL
  CLI --> SRV
  SRV --> CFG
  SRV --> CLT
  SRV --> DL
  CLT --> IMP
  DL --> FS
```
