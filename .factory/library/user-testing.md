# User Testing

Validation surfaces, setup notes, and concurrency guidance.

**What belongs here:** validation surfaces, tool choices, runtime setup notes, resource cost classification.

---

## Validation Surface

- **Primary surface:** CLI in non-interactive / JSON-oriented agent mode.
- **Secondary surface:** local API on port `8080`.
- **External integration:** lightweight live Impartus login/listing flows during implementation; real download checks at milestone validation when relevant.
- Dry run status before mission start:
  - `go build ./...` passed
  - `make test` passed
  - `./impartus --json` passed
  - local API health check on `8080` passed
  - lightweight live `./impartus courses --json` passed
  - `make lint` failed with substantial existing debt

## Validation Concurrency

- Machine profile observed: 6 CPUs and roughly 35 GB available memory headroom during dry run.
- Recommended max concurrent validators:
  - CLI surface: 3
  - API surface: 2
- Reasoning: the representative checks consumed negligible local resources, but live Impartus flows are network-bound and may be sensitive to rate limiting/session behavior, so keep concurrency conservative.

---

## Flow Validator Guidance: CLI Surface

This guidance applies to all flow validators testing CLI assertions.

### Isolation Rules

- CLI tests are stateless and can run concurrently without data isolation requirements.
- Each validator invokes the `./impartus` binary directly; no shared server process.
- Avoid tests that depend on external network state (real Impartus credentials) unless specifically required by the assertion.
- Do not modify the user's real config file; use environment variables or temporary config files for config precedence tests.

### Testing Tool

- Use `tuistory` skill for terminal automation when interactive behavior verification is needed.
- For non-interactive JSON output checks, direct `./impartus` invocations via Execute tool are sufficient.

### Commands Available

- `./impartus --json` - JSON help envelope (VAL-CLI-001)
- `./impartus --json version` or `./impartus version --json` - order independence (VAL-CLI-002)
- `./impartus <unknown> --json` - unknown command handling (VAL-CLI-003)
- `./impartus serve --json --port <n>` - declarative serve (VAL-CLI-005)
- `./impartus lectures --json` - missing flag validation (VAL-CLI-006)
- `./impartus download --json --subject 1 --session 2 --quality invalid` - argument validation (VAL-CLI-007)
- `./impartus download --json --subject 1 --session 2 --start X --end Y` - range semantics (VAL-CLI-008)
- `./impartus <cmd> extra_arg --json` - extra positional handling (VAL-CLI-009)
- Environment variable tests for config precedence (VAL-CLI-010, VAL-CLI-011)
- `./impartus download --json ...` success payload checks (VAL-CLI-012)
- `./impartus download --json ... --views left` - view normalization (VAL-CLI-013)

### Evidence Capture

- Save terminal output for each assertion to evidence directory.
- Include exact command invocations and full JSON responses.
- For assertions requiring "no prompt" behavior, capture and verify stdin was not consumed.

---

## Flow Validator Guidance: API Surface

This guidance applies to all flow validators testing API assertions.

### Isolation Rules

- API tests share a single running server on port 8080.
- All validators read from the same config.json for Impartus credentials.
- Do not create/modify/delete real jobs on the production Impartus system unless specifically testing that flow.
- Use mock/stub data where possible for validation testing.

### Testing Tool

- Use `curl` commands via Execute tool for API testing.
- For WebSocket testing, use `wscat` or a simple Node.js WebSocket client.

### Commands Available

- Health check: `curl -s http://localhost:8080/api/v1/health`
- Login: `curl -s -X POST http://localhost:8080/api/v1/auth/login -H "Content-Type: application/json" -d '{"username":"...","password":"..."}'`
- Courses: `curl -s -H "Authorization: Bearer <token>" http://localhost:8080/api/v1/courses`
- Lectures: `curl -s -H "Authorization: Bearer <token>" "http://localhost:8080/api/v1/lectures?subject_id=1&session_id=2"`
- Jobs: `curl -s -H "Authorization: Bearer <token>" http://localhost:8080/api/v1/jobs`
- Create job: `curl -s -X POST -H "Authorization: Bearer <token>" -H "Content-Type: application/json" http://localhost:8080/api/v1/jobs -d '{"subjectId":1,"sessionId":2,"startIndex":0,"endIndex":1}'`
- WebSocket: Connect to `ws://localhost:8080/api/v1/ws` with Bearer auth

### Evidence Capture

- Save curl output (headers with `-i` for request ID checks).
- Include full request/response pairs.
- For WebSocket tests, capture event transcripts.
