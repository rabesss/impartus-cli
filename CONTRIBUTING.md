# Contributing

Thanks for helping improve `impartus-cli`.

## Local Setup

```sh
go test ./...
bun --cwd ui install --frozen-lockfile
make build
```

The TUI ships as an adjacent compiled OpenTUI executable, so local UI work
requires Bun 1.3.14 as well as the Go toolchain.

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

## Bumping Go

`go.mod` holds the only Go version: its `go` line (or a `toolchain` line, if
one is ever added). CI, security scans and release builds read it through
`actions/setup-go`, and the Dockerfile's `golang:<version>-bookworm` build image
must match it. To bump Go, change `go.mod` and the Dockerfile tag and digest in
the same PR; `container_build_test.go` fails if they differ. A Dependabot golang
image bump therefore needs the matching `go.mod` change before it can merge.
Moving to a new minor release can also need newer golangci-lint and gosec
releases built with that Go version.

## Code Style

- Follow Go naming conventions.
- Keep functions small and testable.
- Validate user-controlled paths and config values.
- Return actionable errors without exposing private local state.

## Security-sensitive Changes

Auth, local API authorization, path handling, media pipeline invocation, WebSocket payloads, and retry/rate-limit behavior need extra review. See [`docs/review-checklist.md`](docs/review-checklist.md) for the enforced review rubric (security blockers, architecture rules, config checklist, complexity budgets).
