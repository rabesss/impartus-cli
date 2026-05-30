# REVIEW.md — Code Review Guide for Impartus CLI

> Canonical PR review instructions for this repository. Primary consumer is **Kilo Code**
> (`app.kilo.ai/code-reviews`), which reads this `REVIEW.md` from the PR **base** branch — so edits
> take effect only after they merge to `main`, and the "Use REVIEW.md" toggle must be enabled in the
> Kilo dashboard. The same guidance is mirrored into the per-tool config files under "Reviewer
> routing" so every bot reviews consistently. Keep this file in sync with `AGENTS.md`.

## Purpose

Impartus CLI is a **CLI-first, API-secondary** Go application for downloading lecture videos from
Impartus platforms. It exposes three surfaces:

- **CLI mode** — interactive and flag-driven commands (the primary execution path).
- **HTTP API mode** — REST + WebSocket server started via `impartus serve`.
- **JSON mode** — deterministic machine-readable output (`--json`) for automation and AI agents.

Review changes through the lens of: correctness of the download/decrypt/join pipeline, safety of
credential handling, and stability of the CLI/JSON/API contracts that downstream automation depends on.

## Architecture

```
main.go / cmd/impartus/main.go → internal/cli.Execute(version, date)
        → command handlers → client.Client (Impartus HTTP/auth)
        → downloader.Downloader (chunk download → AES-128 decrypt → FFmpeg join)
        → server.APIServer (REST + WebSocket job orchestration, `impartus serve` only)
```

Data flow: auth → courses → lectures → playlists → rate-limited chunk download → AES-CBC decrypt →
progress tracking → FFmpeg join → output files (MP4/MKV/audio).

## Folder structure

- `main.go`, `cmd/impartus/main.go` — binary entrypoints; both call `internal/cli.Execute`.
- `internal/buildinfo` — build-time version/date metadata.
- `internal/cli` — command routing, flag parsing, JSON envelope, interactive mode.
- `internal/client` — Impartus HTTP client, auth/token, course/lecture fetch, playlist parsing.
- `internal/config` — config load/defaults/validation (file + env), singleton via `Get()`.
- `internal/downloader` — chunk download, AES decrypt, m3u8 parsing, FFmpeg/mpv, rate limiting, progress.
- `internal/server` — REST API, auth middleware, job store/runner/executor/persistence, WebSocket hub, rate limiter.
- `docs/` — architecture, API reference, websocket events, error codes, runbooks.
- `.github/` — workflows (CI, release, security, lint-pr, packages), issue/PR templates, reviewer configs.

## Stack

- **Go 1.25.0** (pinned in `go.mod`; CI uses `actions/setup-go@v6` with `go-version-file: go.mod`).
- Deps: `gorilla/mux`, `gorilla/websocket`, `google/uuid`, `vbauerster/mpb/v8`, `golang.org/x/time/rate`.
- External runtime tools: **FFmpeg** (required for video join) and **mpv** (required for `play`); both must be on `PATH`.
- Build/release: Makefile targets, `release-please` (squash-merge), GHCR Docker images.

## Testing

- Run `go test ./...`; coverage gate is **40% minimum** (`go test ./... -cover`).
- Prefer **table-driven tests** with descriptive subtest names (`t.Run`).
- New features and bug fixes **must** include tests. Mock the HTTP client for `client`/`downloader` tests.
- Don't assert on unstable output (timestamps, map ordering, absolute temp paths).
- Flag PRs that add behavior without tests or that drop coverage below the gate.

## Code style and conventions

- Format with `gofmt -s` + `goimports` (local prefix `github.com/rabesss/impartus-cli`). Lint with `golangci-lint run` (config `.golangci.yml`, v2 schema).
- **Error handling:** wrap with `fmt.Errorf("...: %w", err)`; never silently discard errors (`errcheck` checks blank assignments and type assertions). API errors use `respondWithError(w, status, code, msg)`.
- **Naming:** Go conventions; keep initialisms uppercase — `ID`, `URL`, `HTTP`, `JSON`, `API` (revive `var-naming`).
- **Context:** thread `context.Context` through call chains and honor cancellation (`ctx.Done()`); `noctx` forbids context-less HTTP requests.
- **JSON envelope:** all JSON/API responses use `{success, data, error, meta}`; build via `newSuccessEnvelope` / `newJSONError`.
- **Complexity budgets (will fail CI):** cyclomatic ≤ 15 (`gocyclo`), cognitive ≤ 30 (`gocognit`), function length ≤ 100 lines / 60 statements (`funlen`), duplicate-code threshold 100 (`dupl`). US spelling (`misspell`).

## Security (highest priority)

- **Never log** usernames, passwords, bearer tokens, cookies, or full config payloads. Prefer request IDs, job IDs, lecture IDs, and status summaries. Redact/omit user-identifying fields before writing to `api.log`.
- Tokens persist in `.token` with mode **0600**; `config.json` and `.token` are gitignored — flag any code that weakens these or commits secrets.
- Every protected `/api/v1` route must pass through the auth middleware. **Flag any new network-exposed endpoint that lacks authentication/authorization.**
- Validate external inputs (FFmpeg args via `validateFFmpegArgs`, AES key length 16/24/32, playlist URLs). Watch for path traversal in output/temp file naming.
- `gosec` runs in CI (G104 handled by `errcheck`); don't add blanket `//nolint:gosec` without justification.

## PR-specific rules

- **PR titles must follow [Conventional Commits](https://www.conventionalcommits.org/)** — validated by `lint-pr.yml`. The title becomes the squash-commit message that `release-please` parses.
  - Types: `feat`, `fix`, `chore`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `revert`.
  - Scopes: `cli`, `api`, `downloader`, `config`, `server`, `ci`, `deps`, `security`, `lint`, `test`, `docs`.
- Keep changes scoped to one module unless a cross-module contract change is required; update `internal/CLAUDE.md` when interfaces/contracts in `internal/**` change.
- When config fields change, update **all five touchpoints**: struct, `ApplyDefaults()`, `Validate()`, `applyEnvOverrides()`, and `sample.config.json`.
- User-facing changes need a `CHANGELOG`/docs update; endpoint changes need `docs/api-reference.md` + `docs/websocket-events.md` updates.

## Common pitfalls

- **Index base confusion:** CLI `--start`/`--end` are **1-based inclusive**. The API job index translation must be verified against the handler/docs — confirm consistency rather than assuming, as CLI and API use different bases.
- **View aliases:** normalize `first`/`second` ↔ `left`/`right` at module boundaries, not ad hoc deep in call paths.
- Missing `ctx` cancellation checks in download/decrypt workers and job execution → goroutine leaks / unstoppable jobs.
- Unclosed HTTP response bodies (`bodyclose`) and writes to nil progress trackers.
- Assuming FFmpeg/mpv exist — handle the "not found on PATH" path gracefully.
- Restart semantics: running/pending jobs at shutdown are restored as **failed (non-resumable)**; don't claim resumability.

## Out of scope (do not flag)

- Pre-existing issues unrelated to the PR's diff — comment only on what the PR introduces or touches.
- Generated/vendored artifacts and local tooling state: `coverage*.out`, `*.test`, `downloads/`, `temp/`, `.beads/`, `.desloppify/`, `.factory/`, `.omx/`, `droid-wiki/`.
- Cosmetic preferences already enforced (or deliberately not) by `gofmt`/`golangci-lint`.
- Speculative scaling/abstraction beyond the change's scope.

## Reviewer routing

This repo is reviewed by multiple AI bots; each reads a different config (all derived from this guide):

| Reviewer | Config file it reads |
|----------|----------------------|
| Kilo Code (`kilo-code-bot`) | `REVIEW.md` (this file) + `AGENTS.md` |
| OpenAI Codex (`chatgpt-codex-connector`) | `AGENTS.md` → "Review Guidelines" |
| CodeRabbit (`coderabbitai`) | `.coderabbit.yaml` (+ auto-reads `AGENTS.md`) |
| Qodo Merge (`qodo-code-review`) | `.pr_agent.toml` + `best_practices.md` |
| GitHub Copilot (`copilot-pull-request-reviewer`) | `.github/copilot-instructions.md` + `.github/instructions/go.instructions.md` |
| Gemini Code Assist (`gemini-code-assist`) | `.gemini/config.yaml` + `.gemini/styleguide.md` |
| Socket (`socket-security`) | `socket.yml` (supply-chain only) |
| Factory Droid (`factory-droid`) | `AGENTS.md` (native) |
