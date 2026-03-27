# User Testing Guide for Impartus CLI

## Testing Surfaces

### REST API (httptest)
- **Tool**: `go test` with `httptest.NewRecorder` pattern
- **No real server needed**: All API tests use in-memory router + recorder
- **No real upstream needed**: Mock clients for upstream Impartus API
- **Config**: Tests create their own config via `validServerConfig()` helper

### CLI JSON Mode
- **Tool**: `go test` with stdout capture
- **No subprocess needed**: Tests call CLI functions directly

## Validation Concurrency

### Max Concurrent Validators: 5
- API tests are lightweight (in-memory, no I/O)
- CPU-bound only during test execution
- 32GB RAM, 6 cores available
- No shared state between test groups (each creates its own APIServer)

## Flow Validator Guidance: go-test (API)

### Isolation Rules
- Each flow validator runs its own `go test` command
- Tests create their own `APIServer` instances via `NewAPIServer()`
- No shared state between validators
- Each test uses `httptest.NewRecorder()` - no real HTTP listener
- No port conflicts possible

### Shared State Concerns
- **File system**: Some tests may create temp directories. Use `t.TempDir()` pattern.
- **FFmpeg**: Health check tests check system FFmpeg availability. This is read-only and safe for concurrency.

### Constraints
- Do NOT start a real server on port 8080 during tests
- Do NOT modify production config.json
- Do NOT write to the repo directory outside of temp dirs

## Known Issues

### Upstream-dependent tests SKIP without real upstream
- `TestUpstreamCacheReuseOnSubsequentCalls` - requires real upstream login
- `TestUpstreamCacheExpiredTokenRefreshes` - requires real upstream login
- `TestUpstreamCacheConcurrentAccess` - requires real upstream login

These tests use `t.Skip()` when upstream is unavailable. They verify:
- VAL-CACHE-002 (reuse cached token)
- VAL-CACHE-003 (auto-refresh on expiry)
- VAL-CACHE-005 (concurrent requests share cache)

### Passing tests verify remaining assertions
- `TestUpstreamCachePopulatedAfterFirstCall` → VAL-CACHE-001 (first request caches token)
- `TestUpstreamCacheLoginFailureDoesNotPoisonCache` → VAL-CACHE-004 (login failure handled gracefully)
- All health tests → VAL-HLT-001 through VAL-HLT-005

## Test Commands

```bash
# Run all cache tests
go test ./internal/server/... -v -run "TestUpstreamCache"

# Run all health tests
go test ./internal/server/... -v -run "TestHealth"

# Run specific assertion test
go test ./internal/server/... -v -run "TestHealthEndpointReturnsStructuredStatus"
```
