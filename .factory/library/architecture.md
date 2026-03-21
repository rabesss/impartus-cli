# Architecture

Architectural decisions, patterns discovered, and system constraints.

**What belongs here:** runtime architecture, interface boundaries, normalization rules, invariants.

---

- Primary user surface is the CLI binary; the local API is a secondary integration surface for OpenClaw-style automation.
- This mission must shift the repo toward an agent-first non-interactive contract while preserving video and audio workflows.
- CLI, API, config loading, and downloader behavior currently expose contract mismatches (range indexing, JSON shapes, config/env handling) that must be resolved deliberately rather than patched ad hoc.
- Performance work must preserve correctness of playlist parsing, decryption, artifact assembly, and lecture selection semantics.
- Cross-surface consistency matters: config resolution, view naming, and lecture-slice semantics should not drift between CLI and API paths.

## Downloader Package

- **Rate limiting**: The downloader uses `golang.org/x/time/rate` with configurable RPS limits. `RateLimiter.WaitForDownload` and `RateLimiter.WaitForAPI` calls may block when rate limits are exceeded. Workers spawning download tasks should account for this blocking behavior.
- **Pipeline architecture**: `internal/downloader/pipeline.go` implements a worker pool pattern with separate download and decrypt workers. The `buildOrderedList` function ensures correct chunk ordering despite concurrent processing by iterating chunk IDs 0 to maxID in order.
- **Playlist parsing**: `PlaylistParser` in `internal/downloader/parser.go` handles `#EXT-X-DISCONTINUITY` markers to separate first (left) and second (right) view chunks for multi-view Impartus recordings.
- **Retry behavior**: Downloads use bounded retries (default 3 attempts) with exponential backoff via `retryDelay` function. Failed chunks are tracked but do not abort the entire lecture download.
