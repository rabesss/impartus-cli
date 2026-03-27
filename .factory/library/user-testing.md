# User Testing

Testing surface and validation infrastructure for the impartus-go project.

**What belongs here:** Testing tools, surface, validation approach, resource constraints.
**What does NOT belong here:** Test commands (use `.factory/services.yaml`).

---

## Validation Surface

| Surface | Tool | Notes |
|----------|------|-------|
| REST API endpoints | `go test` (httptest.NewRecorder) | No real server needed. Tests use in-process mock HTTP clients. |
| CLI `--json` mode | `go test` (stdout capture) | Tests capture os.Stdout and Go test files. |
| Documentation | manual review | Cross-reference docs against code |

## Validation Concurrency

| Metric | Value |
|--------|-------|
| Machine RAM | 46 GiB total, 32 GiB available |
| CPU cores | 6 |
| Max concurrent validators | 5 (API tests are lightweight, ~300MB RAM each, httptest recorder pattern) |

## Key Testing Patterns

- **Server tests:** `httptest.NewRecorder()` + `mux.Router` - no real HTTP server started
- **CLI tests:** Stdout capture via pipe, test helper functions
- **Config tests:** Direct config struct manipulation
- **No real Impartus credentials needed for testing** - mock the upstream client
- **Table-driven tests:** Standard pattern for all test files

## Important Notes for Validators

- The project has a pre-existing build error in `internal/cli/cli.go:556` (unused variable `totalBeforeFilter`). The first feature fixes this.
- Server tests use `httptest.NewRecorder` - do NOT start a real server.
- CLI `--json` mode is separate from API `--json` mode. CLI tests are in `internal/cli/cli_test.go`.
- After envelope standardization, ALL existing server tests that expected bare arrays need updating to expect envelope format.
