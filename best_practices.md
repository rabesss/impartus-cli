# Best Practices — Impartus CLI

Repo-specific coding standards for the Qodo Merge review bot (`qodo-code-review`), which does not
read `AGENTS.md`. Mirrors `REVIEW.md`. Keep these in sync.

## Security
- Never log usernames, passwords, bearer tokens, cookies, or full config payloads. Log request IDs, job IDs, lecture IDs, and status summaries instead; redact PII before writing to `api.log`.
- Persisted tokens must stay in `.token` with mode `0600`; never commit `config.json` or `.token`.
- Every protected `/api/v1` route must pass through the auth middleware. Flag any new network-exposed endpoint without authentication.
- Validate external inputs: AES key length (16/24/32 bytes), FFmpeg args via `validateFFmpegArgs`, and output/temp paths (path-traversal risk).

## Error handling
- Wrap errors with `fmt.Errorf("...: %w", err)`; never discard errors silently (including blank assignments and type assertions).
- API failures use `respondWithError(w, status, code, message)`; success/JSON responses use the `{success, data, error, meta}` envelope.

## Concurrency
- Thread `context.Context` through call chains and check `ctx.Done()` in download, decrypt, pipeline, and job-execution loops.
- Close HTTP response bodies; avoid context-less HTTP requests.

## Testing
- Add table-driven tests with descriptive subtest names for new behavior; coverage gate is **40%**.
- Mock the HTTP client for `client`/`downloader` tests; do not assert on timestamps, map ordering, or absolute temp paths.

## Conventions
- Keep initialisms uppercase: `ID`, `URL`, `HTTP`, `JSON`, `API`.
- Stay within complexity budgets: cyclomatic ≤ 15, cognitive ≤ 30, function length ≤ 100 lines / 60 statements, duplicate threshold 100.
- A new config field must be updated in the struct, `ApplyDefaults()`, `Validate()`, `applyEnvOverrides()`, and `sample.config.json`.

## CLI/API contracts
- CLI `--start`/`--end` are 1-based inclusive; verify API job index translation for consistency.
- Normalize view aliases (`first`/`second` ↔ `left`/`right`) at module boundaries.
- Running/pending jobs at shutdown are restored as failed (non-resumable).
