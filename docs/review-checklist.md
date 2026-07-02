# Review Checklist

This is the enforced review rubric for `impartus-cli`. Every PR must be checked
against it. It replaces the previously committed (now gitignored) bot-specific
configs with a single, enforceable, in-repo contract. Bot-specific YAML may stay
local, but **policy lives in git**.

Reviewers should block a PR that fails any **BLOCKER** item. **SHOULD** items are
strongly recommended; raise them but do not block on minor cases.

---

## Security (all BLOCKER)

- **No secrets in logs or errors.** Upstream URLs may carry auth tokens in the
  query string (e.g. `fetchvideo?...&token=...`). Any error that can reach a log
  (`log.Printf`, `fmt.Errorf` wrapping a raw URL, handler failure responses) must
  route through `internal/secrets` (`RedactURL`, `SanitizeError`, `ScrubError`).
  `http.Client.Do` returns a `*url.Error` that embeds the full URL — sanitize the
  error itself, not just the explicit `%s`.
- **Log files are owner-only.** `api.log` and any credential-adjacent log must be
  opened with mode `0600` (or `0640` with a documented group). Never `0666`.
- **Untrusted path overrides are validated.** CLI `--output` and API `outputPath`
  go through `internal/paths.ValidateDownloadLocation`. Traversal (`..`) is always
  rejected; absolute paths are rejected for the API and allowed for the local CLI
  (the invoking user owns the filesystem). Trusted config/env values are exempt.
- **Non-loopback binds require opt-in.** `ListenAddr` other than loopback
  (`0.0.0.0`, etc.) must be refused at startup unless `allowRemoteAccess`
  (`IMPARTUS_ALLOW_REMOTE_ACCESS=1`) is set. When exposed, CORS must not send a
  wildcard `Allow-Origin` and the WebSocket `CheckOrigin` must validate origin.
- **Unauthenticated endpoints reveal nothing useful.** `/health` exposes only an
  aggregate `status` (ok/misconfigured), never which credentials are set.
- **Subprocess execution uses argv arrays.** FFmpeg/mpv must be invoked via
  `exec.CommandContext` with discrete args — never a shell. Job persistence and
  `.token` files stay `0600` and exclude credentials.

## Architecture (SHOULD)

- **One orchestration path per domain.** CLI and server share download/join logic
  via `Downloader.DownloadAndJoinPlaylist`; do not reintroduce a parallel
  per-lecture loop. Lecture selection goes through `Lectures.SelectForDownload`.
- **No stringly-typed view conditionals.** Use `config.IncludesLeft()`,
  `IncludesRight()`, `HasBothViews()` instead of raw `Views != "right"` checks.
- **Defaults honor explicit config.** Fields whose zero value is meaningful
  (e.g. `enableJitter`) must default at load time with key/env-presence detection,
  not be force-overridden on every `ApplyDefaults()`.
- **Files stay under ~1000 lines.** Split god files (production or test) by
  concern. New behavior is added to the focused file, not appended to a monolith.

## Testing (SHOULD)

- `go test ./...` and `go build ./...` pass on every PR.
- New security-relevant code ships with a regression test that fails if the
  protection regresses (e.g. a token-leak assertion).
- Tests use local fixtures and mocked upstreams; no network in unit tests.

## Config field checklist

When adding/changing a config field, confirm: JSON tag, env override
(`IMPARTUS_*`), defaulting semantics (does an explicit value survive
`ApplyDefaults`?), validation in `Validate()`, and a README/sample.config entry.

## Complexity budget

- Production files: < ~500 lines preferred, 1000 hard cap.
- Test files: split by concern; avoid single files > 1000 lines.
- Functions: small and single-purpose; prefer extraction over duplication.
