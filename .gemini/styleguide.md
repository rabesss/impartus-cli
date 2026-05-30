# Style Guide — Impartus CLI (Gemini Code Assist)

Review criteria for the Gemini Code Assist bot (`gemini-code-assist`). This is a CLI-first Go 1.25
project for downloading Impartus lecture videos (CLI, HTTP API via `impartus serve`, and `--json` mode).
Mirrors `REVIEW.md`; comment only on what the PR introduces or touches.

## Security (highest priority)
- Never log usernames, passwords, bearer tokens, cookies, or full config payloads. Use request IDs, job IDs, lecture IDs, and status summaries; redact PII before writing to `api.log`.
- Persisted tokens stay in `.token` (mode `0600`); never commit `config.json` or `.token`.
- Every protected `/api/v1` route must pass through the auth middleware — flag any new network-exposed endpoint without authentication.
- Validate AES key length (16/24/32 bytes) and FFmpeg args (`validateFFmpegArgs`); watch for path traversal in output/temp paths.

## Error handling
- Wrap errors with `fmt.Errorf("...: %w", err)`; never discard errors silently.
- API failures use `respondWithError`; success/JSON output uses the `{success, data, error, meta}` envelope.

## Concurrency
- Thread `context.Context` and check `ctx.Done()` in download, decrypt, pipeline, and job-execution loops to avoid goroutine leaks and unstoppable jobs.
- Close HTTP response bodies; avoid context-less HTTP requests.

## Testing
- New behavior requires table-driven tests; coverage gate is 40%. Mock the HTTP client for `client`/`downloader` tests.

## Conventions
- Keep initialisms uppercase: `ID`, `URL`, `HTTP`, `JSON`, `API`.
- Complexity budgets: cyclomatic ≤ 15, cognitive ≤ 30, function length ≤ 100 lines / 60 statements; US spelling.
- A new config field must be updated in the struct, `ApplyDefaults()`, `Validate()`, `applyEnvOverrides()`, and `sample.config.json`.

## Gotchas
- CLI `--start`/`--end` are 1-based inclusive; verify API job index translation is consistent.
- Normalize view aliases (`first`/`second` ↔ `left`/`right`) at module boundaries.
- Running/pending jobs at shutdown are restored as failed (non-resumable).
- Handle FFmpeg/mpv being absent from `PATH` gracefully.
