# User Testing Guide for Impartus CLI

## Tooling Requirements

### golangci-lint
- **Use v1** (`github.com/golangci/golangci-lint/cmd/golangci-lint@latest`), NOT v2
- The project's `.golangci.yml` is in v1 format and is incompatible with v2 (specifically `output.formats` field)
- For running individual linters, use temp config files with `-c` flag to avoid `--disable-all`/`disable` conflict in the project config
- Full lint suite: `golangci-lint run --timeout 5m` works fine with the project config

### goimports
- Install via: `go install golang.org/x/tools/cmd/goimports@latest`
- Check: `goimports -l <files>` (empty output = properly formatted)

## Validation Concurrency

All assertions for ci-green-2 are shell-based (grep, goimports, golangci-lint, go build, go test). No concurrency issues — all run sequentially.

## Testing Surfaces

### Lint (goimports, golangci-lint)
- **VAL-LINT-001**: `goimports -l internal/metrics/metrics.go internal/sentryhook/sentryhook.go` — expect empty output
- **VAL-LINT-002**: gocyclo on cli.go — use temp config with `min-complexity: 15`
- **VAL-LINT-003**: godox on cli.go — use temp config with keywords: TODO,FIXME,BUG,HACK,NOTE,OPTIMIZE
- **VAL-LINT-004**: Full golangci-lint suite on changed packages

### CI Workflow
- **VAL-WF-001**: Simulate AGENTS.md Makefile target extraction: `grep -oP 'make \K\w+' AGENTS.md | sort -u`
- **VAL-WF-002**: `grep 'action-govet' .github/workflows/ci.yml` — expect empty

### Code Review
- **VAL-REV-001 through VAL-REV-005**: Simple grep checks on specific files

### Build & Test
- **VAL-BLD-001**: `go build ./...`
- **VAL-BLD-002**: `go test ./... -count=1`

## Services

No services need to be started for validation. This is a pure Go CLI project with no running server required for testing.
