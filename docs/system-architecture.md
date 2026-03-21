<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/ktechhub/doctoc)*

<!---toc start-->

* [Impartus-Go System Architecture](#impartus-go-system-architecture)
  * [System Overview](#system-overview)
    * [Package Relationships](#package-relationships)
  * [Core Components](#core-components)
    * [1. CLI Entry Flow](#1-cli-entry-flow)
    * [2. HTTP API Request Flow](#2-http-api-request-flow)
    * [3. Download Pipeline Architecture](#3-download-pipeline-architecture)
    * [4. Job Execution State Machine](#4-job-execution-state-machine)
    * [5. WebSocket Event Flow](#5-websocket-event-flow)
    * [6. Configuration Resolution Flow](#6-configuration-resolution-flow)
  * [Key Packages](#key-packages)
    * [internal/config](#internalconfig)
    * [internal/client](#internalclient)
    * [internal/downloader](#internaldownloader)
    * [internal/server](#internalserver)
    * [internal/cli](#internalcli)
  * [Data Structures](#data-structures)
    * [Config Struct Relationships](#config-struct-relationships)
    * [Download Pipeline Data Flow](#download-pipeline-data-flow)
  * [Error Handling](#error-handling)
    * [Error Propagation Patterns](#error-propagation-patterns)
    * [Retry Logic with Exponential Backoff](#retry-logic-with-exponential-backoff)
    * [Error Response Format (API)](#error-response-format-api)
  * [Rate Limiting](#rate-limiting)
  * [Progress Tracking](#progress-tracking)

<!---toc end-->

<!-- END doctoc generated TOC please keep comment here to allow auto update -->
# Impartus-Go System Architecture

This document provides a comprehensive overview of the impartus-go CLI architecture, designed for developers and AI agents working with the codebase.

## System Overview

Impartus-go is a Go-based video downloader CLI for Impartus lectures. It supports both interactive CLI mode and a JSON mode for AI agent integration, plus an HTTP API server with WebSocket events for programmatic access.

```mermaid
graph TB
    subgraph "User Interfaces"
        CLI[CLI Mode]
        JSON[JSON Mode]
        API[HTTP API Server]
    end

    subgraph "Core Layer"
        Router[Command Router]
        Config[Config Package]
    end

    subgraph "Client Layer"
        Auth[Authentication]
        HTTPClient[HTTP Client]
        APICalls[API Methods]
    end

    subgraph "Download Pipeline"
        Playlist[Playlist Fetcher]
        Chunks[Chunk Downloader]
        Decrypt[AES Decryptor]
        FFmpeg[FFmpeg Joiner]
    end

    subgraph "Server Layer"
        Routes[HTTP Routes]
        Middleware[Auth Middleware]
        JobStore[Job Store]
        WSHub[WebSocket Hub]
    end

    CLI --> Router
    JSON --> Router
    API --> Routes

    Router --> Config
    Router --> Auth
    Router --> APICalls

    Routes --> Middleware
    Middleware --> JobStore
    JobStore --> WSHub

    Config --> HTTPClient
    Auth --> HTTPClient
    HTTPClient --> APICalls

    APICalls --> Playlist
    Playlist --> Chunks
    Chunks --> Decrypt
    Decrypt --> FFmpeg

    Config -.->|Rate Limits| Chunks
    JobStore -.->|Job Execution| Playlist
```

### Package Relationships

| Package | Depends On | Purpose |
|---------|------------|---------|
| `cli` | `config`, `client`, `downloader`, `server` | Command-line interface and JSON envelope output |
| `client` | `config` | HTTP client for Impartus API, authentication, playlist fetching |
| `downloader` | `config`, `client` | Download pipeline, rate limiting, decryption, FFmpeg integration |
| `server` | `config`, `client`, `downloader` | HTTP API server, job management, WebSocket events |
| `config` | - | Configuration loading, validation, environment overrides |

## Core Components

### 1. CLI Entry Flow

The CLI supports two modes: interactive (default) and JSON (for AI agents). The `--json` flag enables machine-readable output.

```mermaid
flowchart TD
    A[main.go] --> B[cli.Execute]
    B --> C{Args Present?}
    C -->|No| D[Interactive Mode]
    C -->|Yes| E{--json Flag?}
    E -->|Yes| F[executeJSON]
    E -->|No| G[Command Routing]

    D --> H[Get Courses]
    H --> I[Select Course]
    I --> J[Get Lectures]
    J --> K[Select Range]
    K --> L[Download]

    F --> M{Command}
    M -->|courses| N[getCourses JSON]
    M -->|lectures| O[getLectures JSON]
    M -->|download| P[runDownloadJSON]
    M -->|serve| Q[Serve JSON Envelope]
    M -->|version| R[Version JSON]

    G --> S{Command}
    S -->|courses| T[runCourses]
    S -->|lectures| U[runLectures]
    S -->|download| V[runDownload]
    S -->|serve| W[runServe]

    P --> X[executeDownload]
    V --> X
    L --> X

    X --> Y[FFmpeg Check]
    Y --> Z[Init Client]
    Z --> AA[Validate Flags]
    AA --> AB[Fetch Playlists]
    AB --> AC[Download Chunks]
    AC --> AD[Decrypt & Join]
    AD --> AE[Output Files]

    N --> AF[Emit JSON Envelope]
    O --> AF
    Q --> AF
    R --> AF
```

**Key Functions:**

| Function | Location | Purpose |
|----------|----------|---------|
| `Execute()` | `cli/cli.go` | Entry point, routes to interactive or command mode |
| `executeJSON()` | `cli/cli.go` | Handles `--json` flag, outputs JSON envelopes |
| `runInteractive()` | `cli/cli.go` | Interactive course/lecture selection |
| `runServe()` | `cli/cli.go` | Starts HTTP API server |
| `downloadLectures()` | `cli/cli.go` | Orchestrates the download pipeline |

### 2. HTTP API Request Flow

The API server uses middleware for authentication and request tracing.

```mermaid
sequenceDiagram
    participant Client
    participant Middleware as Request ID + CORS + Auth
    participant Router as Gorilla Mux Router
    participant Handler as Route Handler
    participant Store as Job Store
    participant WSHub as WebSocket Hub
    participant Downloader as Downloader Package
    participant Impartus as Impartus API

    Client->>Middleware: HTTP Request
    Note over Middleware: X-Request-ID header added/propagated

    alt Protected Route
        Middleware->>Middleware: Check Bearer Token
        alt Invalid/Expired Token
            Middleware-->>Client: 401 Unauthorized
        end
    end

    Middleware->>Router: Forward Request

    alt POST /jobs
        Router->>Handler: createJobHandler
        Handler->>Handler: Validate Request Body
        Handler->>Handler: Merge Config with Options
        Handler->>Store: CreateJob()
        Store->>Store: Generate Job ID
        Store->>Store: Set Status: pending
        Handler-->>Client: 201 Created + Job JSON
        Handler->>Downloader: executeJob (async goroutine)
        loop Progress Updates
            Downloader->>WSHub: Broadcast job.progress
            WSHub->>Client: WebSocket Event
        end
        Downloader->>WSHub: Broadcast job.completed
    else GET /jobs/{id}
        Router->>Handler: getJobHandler
        Handler->>Store: GetJob(id)
        Store-->>Handler: Job Status
        Handler-->>Client: Job JSON
    else DELETE /jobs/{id}
        Router->>Handler: deleteJobHandler
        Handler->>Store: CancelJob(id)
        Store->>Store: Update Status: cancelled
        Store->>Handler: Cancelled Job
        Handler->>WSHub: Broadcast job.cancelled
        Handler-->>Client: 200 OK
    else GET /courses
        Router->>Handler: coursesHandler
        Handler->>Impartus: LoginAndSetToken
        Handler->>Impartus: GetCourses
        Impartus-->>Handler: Courses List
        Handler-->>Client: Courses JSON
    else GET /lectures
        Router->>Handler: lecturesHandler
        Handler->>Handler: Parse Query Params
        Handler->>Impartus: LoginAndSetToken
        Handler->>Impartus: GetLectures
        Impartus-->>Handler: Lectures List
        Handler-->>Client: Lectures JSON
    end
```

**Authentication Middleware Flow:**

```mermaid
flowchart TD
    A[Request] --> B{OPTIONS Method?}
    B -->|Yes| C[Return 200 OK]
    B -->|No| D{Authorization Header?}
    D -->|No| E[401 MISSING_TOKEN]
    D -->|Yes| F{Bearer Prefix?}
    F -->|No| G[401 INVALID_TOKEN_FORMAT]
    F -->|Yes| H[Extract Token]
    H --> I{Token Valid?}
    I -->|No| J[401 INVALID_TOKEN]
    I -->|Yes| K[Continue to Handler]
```

### 3. Download Pipeline Architecture

The download pipeline supports both sequential and parallel (pipelined) processing modes.

```mermaid
flowchart TB
    subgraph "Input Stage"
        A[Lecture Selection] --> B[Get Stream Infos]
        B --> C[Select Quality Stream]
        C --> D[Fetch M3U8 Playlist]
    end

    subgraph "Parsing Stage"
        D --> E[Parse Playlist]
        E --> F{Multiple Views?}
        F -->|Yes| G[First View URLs + Second View URLs]
        F -->|No| H[Single View URLs]
        E --> I[Extract AES Key URL]
    end

    subgraph "Key Resolution"
        I --> J[Fetch Encryption Key]
        J --> K[Reverse Bytes]
        K --> L[16-byte AES Key]
    end

    subgraph "Download Stage"
        G --> M{Pipeline Mode?}
        H --> M
        M -->|Yes| N[Parallel Pipeline]
        M -->|No| O[Sequential Download]

        N --> P[Download Workers Pool]
        P --> Q[Decrypt Workers Pool]
        O --> R[Rate-Limited Chunks]
    end

    subgraph "Processing Stage"
        Q --> S[Downloaded TS Files]
        R --> S
        S --> T[AES-128-CBC Decryption]
        T --> U[Decrypted TS Files]
    end

    subgraph "Join Stage"
        U --> V{Audio Only?}
        V -->|Yes| W[FFmpeg Audio Extract]
        V -->|No| X{Views Mode}
        W --> Y[Audio Output Files]
        X -->|both| Z[FFmpeg Multi-View Join]
        X -->|left/right| AA[FFmpeg Single View Join]
        Z --> AB[MKV Output]
        AA --> AC[MP4 Output]
        Y --> AD[MP3/M4A Output]
    end

    subgraph "Output Stage"
        AB --> AE[Download Directory]
        AC --> AE
        AD --> AE
        AE --> AF[Cleanup Temp Files]
    end

    subgraph "Rate Limiting"
        AG[Rate Limiter] -.->|WaitForDownload| P
        AG -.->|WaitForDownload| R
        AG -.->|WaitForAPI| B
        AG -.->|WaitForAPI| J
    end
```

**Pipeline vs Sequential Mode:**

```mermaid
flowchart LR
    subgraph "Sequential Mode"
        A1[Chunk 1 Download] --> B1[Chunk 1 Decrypt]
        B1 --> A2[Chunk 2 Download]
        A2 --> B2[Chunk 2 Decrypt]
        B2 --> A3[Chunk 3 Download]
    end

    subgraph "Pipeline Mode"
        C1[Download Worker 1] --> D1[Decrypt Worker]
        C2[Download Worker 2] --> D1
        C3[Download Worker 3] --> D1
        C1 -.->|Concurrent| C2
        C2 -.->|Concurrent| C3
    end
```

**Pipeline Worker Architecture:**

```mermaid
flowchart TD
    subgraph "LecturePipeline"
        A[Download Queue] --> B[Download Worker 1]
        A --> C[Download Worker 2]
        A --> D[Download Worker N]
        
        B --> E[Downloaded Chunks Channel]
        C --> E
        D --> E
        
        E --> F[Decrypt Worker 1]
        E --> G[Decrypt Worker 2]
        E --> H[Decrypt Worker M]
        
        F --> I[Decrypted Chunks Channel]
        G --> I
        H --> I
        
        I --> J[Result Collector]
        J --> K[First View Chunks]
        J --> L[Second View Chunks]
    end

    M[Config] -->|DownloadWorkersPerLecture| A
    M -->|DecryptWorkersPerLecture| E
```

### 4. Job Execution State Machine

```mermaid
stateDiagram-v2
    [*] --> pending: CreateJob()
    
    pending --> running: executeJob() starts
    pending --> cancelled: CancelJob() before start
    
    running --> running: Progress Updates
    running --> completing: All Downloads Finish
    running --> failed: Error Occurs
    running --> cancelled: CancelJob() during execution
    
    completing --> completed: Output Files Written
    
    completed --> [*]
    failed --> [*]
    cancelled --> [*]

    note right of pending
        Job created, waiting for execution
        JobStore.CreateJob()
    end note
    
    note right of running
        Active download in progress
        Progress: 0-100%
        Phase transitions:
        - initializing
        - logging_in
        - fetching_lectures
        - downloading_slides
        - fetching_playlists
        - downloading
    end note
    
    note right of failed
        Error stored in job.Error
        WebSocket: job.failed event
    end note
    
    note right of cancelled
        context.CancelFunc() called
        WebSocket: job.cancelled event
    end note
```

**Job Lifecycle Events:**

| Phase | Progress Range | WebSocket Event |
|-------|----------------|-----------------|
| `initializing` | 0-2% | `job.started` |
| `logging_in` | 2-8% | `job.progress` |
| `fetching_lectures` | 8-15% | `job.progress` |
| `downloading_slides` | 15-25% | `job.progress` (optional) |
| `fetching_playlists` | 25-30% | `job.progress` |
| `downloading` | 30-95% | `job.progress` |
| `completing` | 95-100% | `job.progress` |
| `completed` | 100% | `job.completed` |

### 5. WebSocket Event Flow

```mermaid
sequenceDiagram
    participant Client as API Client
    participant Server as API Server
    participant Hub as WebSocket Hub
    participant Store as Job Store
    participant Runner as Job Runner

    Client->>Server: WebSocket Upgrade Request
    Note over Server: Authorization: Bearer <token>
    Server->>Hub: Register Connection
    Hub-->>Client: Connection Established

    loop Event Broadcasting
        Runner->>Store: Update Job Progress
        Store->>Hub: Broadcast Event
        Hub->>Client: WebSocket Message
    end

    rect rgb(200, 230, 200)
        Note right of Runner: Job Started
        Runner->>Store: UpdateJob("running", 2, "")
        Store->>Hub: Broadcast job.started
        Hub->>Client: {"type":"job.started","jobId":"...","status":"running","timestamp":...}
    end

    rect rgb(200, 200, 230)
        Note right of Runner: Progress Updates
        Runner->>Store: SetLectureProgress(completed, total)
        Store->>Hub: Broadcast job.progress
        Hub->>Client: {"type":"job.progress","progress":50,"phase":"downloading","details":{...}}
    end

    rect rgb(230, 200, 200)
        Note right of Runner: Job Completed
        Runner->>Store: SetOutputs(outputs)
        Store->>Store: UpdateJob("completed", 100, "")
        Store->>Hub: Broadcast job.completed
        Hub->>Client: {"type":"job.completed","status":"completed","outputs":[...]}
    end

    Client->>Server: Close Connection
    Server->>Hub: Unregister Connection
```

**Event Types:**

| Event | Trigger | Fields |
|-------|---------|--------|
| `job.started` | Job begins execution | `type`, `jobId`, `status`, `timestamp` |
| `job.progress` | Periodic updates | `type`, `jobId`, `status`, `progress`, `phase`, `details?`, `timestamp` |
| `job.completed` | All downloads finish | `type`, `jobId`, `status`, `progress`, `outputs[]`, `timestamp` |
| `job.failed` | Unrecoverable error | `type`, `jobId`, `status`, `error`, `timestamp` |
| `job.cancelled` | User cancellation | `type`, `jobId`, `status`, `progress?`, `timestamp` |

### 6. Configuration Resolution Flow

```mermaid
flowchart TD
    A[Start] --> B{Config Path Provided?}
    B -->|Yes| C[Parse Custom Config]
    B -->|No| D{./config.json Exists?}
    D -->|Yes| E[Parse Default Config]
    D -->|No| F[Empty Config]

    C --> G[Apply Defaults]
    E --> G
    F --> G

    G --> H[Apply Environment Overrides]
    
    H --> I[IMPARTUS_USERNAME]
    H --> J[IMPARTUS_PASSWORD]
    H --> K[IMPARTUS_BASE_URL]
    H --> L[IMPARTUS_QUALITY]
    H --> M[IMPARTUS_VIEWS]
    H --> N[IMPARTUS_DOWNLOAD_LOCATION]
    H --> O[IMPARTUS_TEMP_DIR]
    H --> P[IMPARTUS_AUDIO_ONLY]
    H --> Q[IMPARTUS_NUM_WORKERS]
    H --> R[IMPARTUS_RATE_LIMIT]
    H --> S[IMPARTUS_API_RATE_LIMIT]

    I --> T[Build Canonical Fields]
    J --> T
    K --> T
    L --> T
    M --> T

    T --> U[Validate Core Fields]
    U --> V{Username & Password Set?}
    V -->|No| W[Error: Required Fields Missing]
    V -->|Yes| X{BaseUrl Set?}
    X -->|No| Y[Error: BaseUrl Required]
    X -->|Yes| Z{Quality Valid?}

    Z -->|No| AA[Error: Invalid Quality]
    Z -->|Yes| AB{Views Valid?}

    AB -->|No| AC[Error: Invalid Views]
    AB -->|Yes| AD{Workers in Range?}

    AD -->|No| AE[Error: Workers Out of Range]
    AD -->|Yes| AF[Config Ready]

    W --> AG[Return Error]
    Y --> AG
    AA --> AG
    AC --> AG
    AE --> AG
    AF --> AH[Return Config]
```

**Configuration Priority:**

1. **CLI Flags** (highest) - `--quality`, `--views`, `--output`, etc.
2. **Environment Variables** - `IMPARTUS_*`
3. **Config File** - `./config.json`
4. **Defaults** (lowest) - Applied after file/env

**Validation Rules:**

| Field | Valid Values | Default |
|-------|--------------|---------|
| `quality` | `144`, `450`, `720` | Required |
| `views` | `left`, `right`, `both`, `first`, `second` | Required |
| `numWorkers` | 1-50 | 5 |
| `downloadWorkersPerLecture` | 1-10 | 3 |
| `decryptWorkersPerLecture` | 1-10 | 2 |
| `rateLimit` | 0.1-100 RPS | 10 |
| `apiRateLimit` | 0.1-20 RPS | 2 |
| `httpTimeout` | 30s-60m | 10m |
| `audioFormat` | `mp3`, `m4a`, `aac`, `opus` (audio-only) | `mp3` |

## Key Packages

### internal/config

**Responsibilities:**
- Load configuration from JSON file
- Apply environment variable overrides
- Validate configuration values
- Provide defaults for missing values

**Key Types:**

```go
type Config struct {
    Username         string
    Password         string
    BaseUrl          string
    Quality          string
    Views            string
    DownloadLocation string
    TempDirLocation  string
    Token            string
    NumWorkers       int
    AudioOnly        bool
    AudioFormat      string
    RateLimit        float64
    APIRateLimit     float64
    EnableJitter     bool
    EnablePipeline   bool
    DownloadWorkersPerLecture int
    DecryptWorkersPerLecture  int
    ProgressTracking ProgressConfig
    HTTPTimeout      string
}

type ProgressConfig struct {
    Enabled         bool
    ShowSpeed       bool
    ShowETA         bool
    UpdateInterval  string
    SpeedWindowSize int
}
```

**Key Functions:**

| Function | Purpose |
|----------|---------|
| `LoadResolved(path)` | Load config from file, apply env overrides and defaults |
| `Load(path)` | Parse and validate config file |
| `Parse(path)` | Parse JSON config file |
| `Validate()` | Validate all config fields |
| `ApplyDefaults()` | Set default values for missing fields |

### internal/client

**Responsibilities:**
- HTTP client for Impartus API
- Authentication and token management
- Course and lecture data retrieval
- Playlist fetching and parsing

**Key Types:**

```go
type Client struct {
    HTTPClient        *http.Client
    UserAgentProvider func() string
    token             string
}

type Course struct {
    SubjectID   int
    SessionID   int
    SubjectName  string
    ProfessorName string
    // ... additional fields
}

type Lecture struct {
    Ttid    int
    Topic   string
    SeqNo   int
    // ... additional fields
}

type ParsedPlaylist struct {
    KeyURL           string
    Title            string
    FirstViewURLs    []string
    SecondViewURLs   []string
    Id               int
    SeqNo            int
    HasMultipleViews bool
}
```

**Key Methods:**

| Method | Purpose |
|--------|---------|
| `LoginAndSetToken(ctx, cfg)` | Authenticate and store bearer token |
| `GetCourses(ctx, cfg)` | Fetch available courses |
| `GetLectures(ctx, cfg, course)` | Fetch lectures for a course |
| `GetPlaylists(ctx, cfg, lectures)` | Fetch and parse M3U8 playlists |
| `GetAuthorizedWithToken(ctx, url, token)` | Make authenticated GET request |

### internal/downloader

**Responsibilities:**
- Download pipeline orchestration
- Rate limiting for API and download requests
- AES-128-CBC decryption
- FFmpeg video/audio joining
- Progress tracking

**Key Types:**

```go
type Downloader struct {
    config      *config.Config
    client      *client.Client
    rateLimiter *RateLimiter
    maxRetries  int
    ffmpegPath  string
}

type LecturePipeline struct {
    config           PipelineConfig
    downloadQueue    chan ChunkTask
    downloadedChunks chan DownloadedChunk
    decryptedChunks  chan DecryptedChunk
    // ...
}

type RateLimiter struct {
    downloadLimiter *rate.Limiter
    apiLimiter      *rate.Limiter
    jitterEnabled   bool
}

type ProgressTracker struct {
    totalLectures     int32
    completedLectures int32
    totalChunks       int32
    completedChunks   int32
    speedSamples      []SpeedSample
    // ...
}
```

**Key Functions:**

| Function | Purpose |
|----------|---------|
| `New(cfg, client)` | Create downloader with config |
| `FetchLecturePlaylists(ctx, lectures)` | Get M3U8 playlists for lectures |
| `DownloadPlaylist(ctx, playlist, progress, tracker)` | Download chunks for one lecture |
| `JoinLectureOutput(file)` | Join downloaded chunks with FFmpeg |
| `decryptChunk(filePath, key)` | AES-128-CBC decryption |

### internal/server

**Responsibilities:**
- HTTP API server
- Job management and execution
- WebSocket hub for real-time events
- Authentication middleware
- Token management

**Key Types:**

```go
type APIServer struct {
    cfg        *config.Config
    jobStore   *JobStore
    wsHub      *WSHub
    tokenStore *TokenStore
    upgrader   websocket.Upgrader
    router     *mux.Router
    port       string
}

type Job struct {
    ID                string
    SubjectID         int
    SessionID         int
    StartIndex        int
    EndIndex          int
    Status            string
    Progress          float64
    Error             string
    TotalLectures     int
    CompletedLectures  int
    Outputs           []string
    Config            JobRuntimeConfig
    CreatedAt         time.Time
    UpdatedAt         time.Time
    ctx               context.Context
    cancel            context.CancelFunc
}

type JobStore struct {
    jobs map[string]*Job
    mu   sync.RWMutex
}

type WSHub struct {
    clients map[*websocket.Conn]bool
    mu      sync.Mutex
}

type TokenStore struct {
    tokens map[string]TokenInfo
    mu     sync.RWMutex
}
```

**Key HTTP Handlers:**

| Handler | Route | Purpose |
|---------|-------|---------|
| `healthHandler` | `GET /health` | Health check endpoint |
| `loginHandler` | `POST /auth/login` | Authenticate and get token |
| `coursesHandler` | `GET /courses` | List available courses |
| `lecturesHandler` | `GET /lectures` | List lectures for course |
| `createJobHandler` | `POST /jobs` | Create download job |
| `listJobsHandler` | `GET /jobs` | List all jobs |
| `getJobHandler` | `GET /jobs/{id}` | Get job status |
| `deleteJobHandler` | `DELETE /jobs/{id}` | Cancel job |
| `websocketHandler` | `GET /ws` | WebSocket connection |

### internal/cli

**Responsibilities:**
- Command-line argument parsing
- Interactive mode prompts
- JSON envelope output
- Flag validation and override

**Key Types:**

```go
type jsonEnvelope struct {
    Success bool     `json:"success"`
    Data    any      `json:"data"`
    Error   *jsonErr `json:"error"`
    Meta    jsonMeta `json:"meta"`
}

type downloadResult struct {
    Status       string   `json:"status"`
    OutputPaths  []string `json:"outputPaths"`
    LectureCount int      `json:"lectureCount"`
}
```

**Key Functions:**

| Function | Purpose |
|----------|---------|
| `Execute(version, date)` | Main CLI entry point |
| `executeJSON(args, version, date)` | Handle `--json` mode |
| `runInteractive()` | Interactive course/lecture selection |
| `runDownload(args)` | CLI download command |
| `runServe(args, version)` | Start API server |

## Data Structures

### Config Struct Relationships

```mermaid
classDiagram
    class Config {
        +string Username
        +string Password
        +string BaseUrl
        +string Quality
        +string Views
        +string DownloadLocation
        +string TempDirLocation
        +string Token
        +int NumWorkers
        +bool AudioOnly
        +string AudioFormat
        +float64 RateLimit
        +float64 APIRateLimit
        +bool EnableJitter
        +bool EnablePipeline
        +int DownloadWorkersPerLecture
        +int DecryptWorkersPerLecture
        +ProgressConfig ProgressTracking
        +string HTTPTimeout
        +ApplyDefaults()
        +Validate() error
    }

    class ProgressConfig {
        +bool Enabled
        +bool ShowSpeed
        +bool ShowETA
        +string UpdateInterval
        +int SpeedWindowSize
    }

    Config --> ProgressConfig : contains

    class Job {
        +string ID
        +int SubjectID
        +int SessionID
        +int StartIndex
        +int EndIndex
        +string Status
        +float64 Progress
        +string Error
        +int TotalLectures
        +int CompletedLectures
        +[]string Outputs
        +JobRuntimeConfig Config
        +time.Time CreatedAt
        +time.Time UpdatedAt
    }

    class JobRuntimeConfig {
        +string Quality
        +string Views
        +bool AudioOnly
        +string AudioFormat
        +string OutputPath
        +bool EnablePipeline
        +int NumWorkers
        +int DownloadWorkersPerLecture
        +int DecryptWorkersPerLecture
        +bool Slides
    }

    Job --> JobRuntimeConfig : contains
```

### Download Pipeline Data Flow

```mermaid
flowchart LR
    subgraph "Input Types"
        A[Lecture]
        B[StreamInfo]
    end

    subgraph "Processing Types"
        C[ParsedPlaylist]
        D[DownloadedPlaylist]
        E[M3U8File]
        F[JoinResult]
    end

    A -->|GetStreamInfos| B
    A -->|FetchLecturePlaylists| C
    C -->|DownloadPlaylist| D
    D -->|CreateTempM3U8File| E
    E -->|JoinLectureOutput| F

    subgraph "Lecture Fields"
        A --- A1[Ttid]
        A --- A2[Topic]
        A --- A3[SeqNo]
    end

    subgraph "ParsedPlaylist Fields"
        C --- C1[KeyURL]
        C --- C2[Title]
        C --- C3[FirstViewURLs]
        C --- C4[SecondViewURLs]
        C --- C5[HasMultipleViews]
    end

    subgraph "DownloadedPlaylist Fields"
        D --- D1[FirstViewChunks]
        D --- D2[SecondViewChunks]
        D --- D3[Playlist]
    end

    subgraph "JoinResult Fields"
        F --- F1[LeftOutput]
        F --- F2[RightOutput]
        F --- F3[BothOutput]
    end
```

## Error Handling

### Error Propagation Patterns

```mermaid
flowchart TD
    A[Function Call] --> B{Returns Error?}
    B -->|No| C[Continue Execution]
    B -->|Yes| D{Error Type?}

    D -->|Network Error| E{Retry Count?}
    E -->|< Max Retries| F[Exponential Backoff]
    F --> G[Retry]
    G --> A
    E -->|= Max Retries| H[Return Wrapped Error]

    D -->|Validation Error| I[Return Error Immediately]

    D -->|Context Cancellation| J[Stop Processing]
    J --> K[Return Context Error]

    D -->|Terminal Error| L[Mark Job Failed]
    L --> M[Broadcast job.failed]

    C --> N[Success Path]
```

### Retry Logic with Exponential Backoff

```go
// downloadWithRetry implements exponential backoff
func (d *Downloader) downloadWithRetry(ctx context.Context, url string, 
    id, chunk int, view string, maxRetries int, tracker *ProgressTracker) (string, error) {
    var lastErr error
    baseDelay := 1 * time.Second
    
    for attempt := 0; attempt < maxRetries; attempt++ {
        filePath, bytesDownloaded, err := d.downloadURL(ctx, url, id, chunk, view)
        if err == nil {
            if tracker != nil {
                ChunkCompleted(tracker, bytesDownloaded)
            }
            return filePath, nil
        }
        
        lastErr = err
        if attempt < maxRetries-1 {
            delay := retryDelay(baseDelay, attempt)
            time.Sleep(delay)
        }
    }
    
    return "", fmt.Errorf("failed after %d attempts: %w", maxRetries, lastErr)
}

func retryDelay(baseDelay time.Duration, attempt int) time.Duration {
    // Calculate: baseDelay * 2^attempt
    // Capped at ~6 hours to prevent overflow
    multiplier := int64(math.Pow(2, float64(attempt)))
    return time.Duration(int64(baseDelay) * multiplier)
}
```

### Error Response Format (API)

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

**Common Error Codes:**

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `MISSING_TOKEN` | 401 | No Authorization header |
| `INVALID_TOKEN_FORMAT` | 401 | Missing Bearer prefix |
| `INVALID_TOKEN` | 401 | Token expired or invalid |
| `AUTH_FAILED` | 401 | Invalid credentials |
| `MISSING_PARAMETER` | 400 | Required parameter missing |
| `INVALID_REQUEST` | 400 | Malformed request body |
| `JOB_NOT_FOUND` | 404 | Job ID doesn't exist |
| `JOB_CANNOT_CANCEL` | 400 | Job in terminal state |
| `LOGIN_FAILED` | 502 | Authentication with Impartus failed |
| `COURSES_FETCH_FAILED` | 502 | Failed to fetch courses |
| `LECTURES_FETCH_FAILED` | 502 | Failed to fetch lectures |

## Rate Limiting

The system implements rate limiting at two levels:

1. **API Rate Limit** - Controls requests to Impartus API
2. **Download Rate Limit** - Controls chunk download concurrency

```mermaid
flowchart TD
    A[Request] --> B{Request Type}
    B -->|API Call| C[WaitForAPI]
    B -->|Download| D[WaitForDownload]

    C --> E[API Rate Limiter]
    D --> F[Download Rate Limiter]

    E --> G{Token Available?}
    F --> H{Token Available?}

    G -->|Yes| I[Execute Request]
    G -->|No| J[Wait]

    H -->|Yes| K[Execute Download]
    H -->|No| L[Wait]

    I --> M{Jitter Enabled?}
    K --> M

    M -->|Yes| N[Random Delay -200ms to +200ms]
    M -->|No| O[Return]

    N --> O
    J --> G
    L --> H
```

## Progress Tracking

The progress tracker provides real-time feedback during downloads:

```mermaid
flowchart LR
    A[Download Start] --> B[Initialize ProgressTracker]
    B --> C[Start Update Loop]

    C --> D[Update Every 2s]
    D --> E[Sample Bytes Downloaded]
    E --> F[Calculate Speed]
    F --> G[Estimate ETA]
    G --> H[Update Progress Bar]

    H --> I{Download Complete?}
    I -->|No| D
    I -->|Yes| J[Stop Tracker]
    J --> K[Final Update]

    L[Chunk Completed] --> M[Update Bytes]
    M --> N[Update Completed Count]
```

**Metrics Tracked:**

| Metric | Formula |
|--------|---------|
| Progress % | `(completedChunks / totalChunks) * 100` |
| Speed | `Δbytes / Δtime` over sliding window |
| ETA | `remainingBytes / speed` |
| Elapsed | `time.Since(startTime)` |

---

*This documentation reflects the architecture as of the current implementation. For API-specific details, see [api-reference.md](api-reference.md). For WebSocket event specifications, see [websocket-events.md](websocket-events.md).*
