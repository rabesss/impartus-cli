---
name: go-backend-worker
description: Go backend worker for implementing API changes, persistence, caching, and documentation updates in the impartus-go CLI project.
---

# Go Backend Worker

NOTE: Startup and cleanup are handled by `worker-base`. This skill defines the WORK PROCEDURE.

## When to Use This Skill

Features that modify Go source code in `internal/`, `cmd/`, `main.go`, or `docs/`. This includes:
- API handler changes (server.go, auth.go)
- New response helpers or envelope wrappers
- Token caching logic
- Health endpoint enhancements
- Job persistence layer
- Idempotency key support
- Feature flag cleanup
- Documentation updates (manifest, API reference, error codes, etc.)
- Build fixes

## Required Skills

None. All work is done via Go tooling (go test, go build) and file editing.

## Work Procedure

### Step 1: Read Mission Context
1. Read `mission.md` for mission overview and requirements
2. Read `AGENTS.md` for coding conventions, boundaries, and testing patterns
3. Read `.factory/library/architecture.md` for system architecture
4. Read `.factory/library/environment.md` for environment setup notes
5. Read your assigned feature description carefully - understand exactly what needs to change

### Step 2: Understand Existing Code
1. Read ALL relevant source files before making changes. For server work, this means:
   - `internal/server/server.go` - All handlers, routes, job execution
   - `internal/server/auth.go` - Response helpers, auth middleware, token store
   - `internal/server/job_runner.go` - Parallel download runner
   - `internal/cli/cli.go` - CLI commands and JSON envelope
   - `internal/config/config.go` - Config loading and validation
   - `internal/client/client.go` - HTTP client, upstream API calls
   - `internal/client/types.go` - Data types
2. Read existing test files to understand testing patterns:
   - `internal/server/server_test.go` and `server_coverage_test.go`
   - `internal/server/auth_test.go`
   - `internal/cli/cli_test.go`

### Step 3: Write Tests FIRST (Red)
1. Write failing tests BEFORE implementation
2. Use table-driven test pattern: `[]struct{name, want, wantErr}`
3. Use `httptest.NewRecorder()` for API handler testing
4. Test both success and error paths
5. Run: `go test ./internal/server/... -run TestYourFeature` (should FAIL)

### Step 4: Implement (Green)
1. Make the failing tests pass with minimal implementation
2. Follow existing patterns in the codebase (error wrapping, envelope format, etc.)
3. Keep changes focused - don't refactor unrelated code
4. Ensure `go build ./...` passes

### Step 5: Update Existing Tests
1. If your changes break existing tests (e.g., envelope format change), update them
2. Search for tests that assert on old response shapes: `grep -r 'respondWithSuccess\\|writeJSON\\|respondWithError' *_test.go`
3. Run: `go test ./...` - ALL tests must pass

### Step 6: Manual Verification
1. `go build ./...` - no build errors
2. `go test ./...` - all tests pass
3. `go test ./...` - no test failures
4. If documentation changes are part of your feature, verify JSON is valid: `python3 -c "import json; json.load(open('docs/openclaw-manifest.json'))"`

### Step 7: Commit
1. `git add` only files relevant to your feature
2. Commit with descriptive message referencing the feature ID

## Example Handoff

```json
{
  "salientSummary": "Standardized all API responses to use {success, data, error, meta} envelope. Updated respondWithEnvelope helper, replaced 6 writeJSON calls, updated 47 existing tests to expect new shape. All tests pass.",
  "whatWasImplemented": "Created respondWithEnvelope(w, status, command, data) helper in auth.go. Replaced writeJSON calls in healthHandler, coursesHandler, lecturesHandler, createJobHandler, listJobsHandler, getJobHandler. Added meta field to respondWithSuccess and respondWithError. Updated all server tests in server_test.go, server_coverage_test.go, auth_test.go to expect envelope format.",
  "whatWasLeftUndone": "",
  "verification": {
    "commandsRun": [
      {"command": "go build ./...", "exitCode": 0, "observation": "Build succeeded"},
      {"command": "go test ./internal/server/...", "exitCode": 0, "observation": "All 80+ tests pass"},
      {"command": "go test ./internal/cli/...", "exitCode": 0, "observation": "All CLI tests pass"},
      {"command": "go vet ./...", "exitCode": 0, "observation": "No issues"}
    ],
    "interactiveChecks": []
  },
  "tests": {
    "added": [
      {"file": "internal/server/server_test.go", "cases": [{"name": "TestEnvelopeHealth", "verifies": "Health uses envelope"}, {"name": "TestEnvelopeCourses", "verifies": "Courses wrapped in data field"}, {"name": "TestEnvelopeCreateJob", "verifies": "Job creation uses envelope"}]}
    ]
  },
  "discoveredIssues": []
}
```

## When to Return to Orchestrator

- Feature depends on an API endpoint or data model that doesn't exist yet (precondition not met)
- Requirements are ambiguous or contradictory
- Build fails and root cause is in a file outside your feature scope
- Existing test failures that are clearly pre-existing and unrelated to your changes
