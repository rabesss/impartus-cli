# Contributing

Thanks for helping improve `impartus-cli`.

## Local Setup

```sh
go test ./...
go build -o impartus .
```

If you use the Makefile:

```sh
make test
make lint
make security
```

## PR Guidelines

- Keep PRs small and focused.
- Run `go test ./...` before opening a PR.
- Update README/docs when changing commands, flags, JSON output, API endpoints, WebSocket events, or config fields.
- Keep deterministic `--json` output stable for automation and agent workflows.
- Do not commit account details, local configs, generated job state, downloaded media, or private logs.
- Prefer tests with local fixtures and mocked upstream responses.

## Code Style

- Follow Go naming conventions.
- Keep functions small and testable.
- Validate user-controlled paths and config values.
- Return actionable errors without exposing private local state.

## Security-sensitive Changes

Auth, local API authorization, path handling, media pipeline invocation, WebSocket payloads, and retry/rate-limit behavior need extra review.