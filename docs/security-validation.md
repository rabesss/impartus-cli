# API Security Validation

## Scope
- Reviewed `internal/server/auth.go` and `internal/server/server.go` for auth bypass, token handling, input validation, error leakage, and race-condition risk.
- Focused only on API-surface security behavior.

## Checks Run
- `go test ./...` ✅ pass
- `go test -race ./...` ✅ pass
- `go vet ./...` ✅ pass

## Findings
### 1) High-confidence auth bypass on WebSocket route (fixed)
- **Issue:** `GET /api/v1/ws` was registered on the public router, allowing unauthenticated clients to connect and receive job lifecycle/progress events.
- **Impact:** Unauthorized information disclosure (job IDs, progress, failure messages, output paths) and passive monitoring of server activity.
- **Fix:** Moved `/api/v1/ws` route into the authenticated subrouter guarded by `authMiddleware`.
- **Validation:** Added `TestWebSocketRouteRequiresAuth` to assert unauthenticated access now returns `401` with `MISSING_TOKEN`.

## Additional Review Notes
- Token generation uses `crypto/rand` with 32-byte entropy and in-memory expiry checks.
- Protected REST endpoints are correctly gated by bearer-token middleware.
- Input validation for login and job creation blocks malformed/invalid core parameters.
- Error responses still include upstream `err.Error()` strings on some handlers; this can leak backend details and should be reduced in a future hardening pass if needed.

## Current Risk Posture (Actionable)
- **Auth bypass risk:** reduced from **high** to **low** for API/WebSocket access control.
- **Residual risk:** **low-to-medium** for error-detail exposure from backend dependency failures.
- **Recommended next step:** replace direct upstream error messages in API responses with stable generic error text while logging detailed errors server-side.
