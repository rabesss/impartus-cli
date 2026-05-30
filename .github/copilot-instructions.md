# Copilot Instructions — Impartus CLI

Repository custom instructions for GitHub Copilot code review and chat. Kept short on purpose
(Copilot reads only the first ~4,000 characters per instruction file). See `REVIEW.md` for the full guide.

This is a **CLI-first, API-secondary** Go 1.25 application that downloads Impartus lecture videos
(CLI mode, HTTP API via `impartus serve`, and deterministic `--json` mode).

## Security (review priority)
- Never log usernames, passwords, bearer tokens, cookies, or full config. Use request/job/lecture IDs and status summaries; redact PII before writing to `api.log`.
- Keep `.token` at mode `0600`; never commit `config.json` or `.token`.
- Every protected `/api/v1` route must use the auth middleware. Flag any new network-exposed endpoint without authentication.

## Go conventions
- Wrap errors with `fmt.Errorf("...: %w", err)`; never discard errors silently.
- Honor `context.Context` cancellation (`ctx.Done()`) in download, decrypt, pipeline, and job loops.
- Keep initialisms uppercase: `ID`, `URL`, `HTTP`, `JSON`, `API`.
- Stay within lint budgets: cyclomatic ≤ 15, cognitive ≤ 30, function length ≤ 100 lines / 60 statements.
- API/JSON responses use the `{success, data, error, meta}` envelope; failures use `respondWithError`.

## Testing
- New features and bug fixes require table-driven tests; coverage gate is 40% (`go test ./...`).

## Conventions & gotchas
- PR titles must follow Conventional Commits (validated by `lint-pr.yml`); the title becomes the squash commit.
- CLI `--start`/`--end` are 1-based inclusive; verify API job index translation for consistency.
- Normalize view aliases (`first`/`second` ↔ `left`/`right`) at module boundaries.
- A new config field must be updated in the struct, `ApplyDefaults()`, `Validate()`, `applyEnvOverrides()`, and `sample.config.json`.
- Comment only on issues the PR introduces or touches.
