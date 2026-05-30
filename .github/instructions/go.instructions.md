---
applyTo: "**/*.go"
---
# Go review conventions

- Wrap errors with `fmt.Errorf("...: %w", err)`; do not ignore returned errors (blank assignments and unchecked type assertions are flagged).
- Thread `context.Context` and check `ctx.Done()` in long-running loops (download/decrypt/pipeline/job execution); avoid context-less HTTP requests and close response bodies.
- Keep initialisms uppercase: `ID`, `URL`, `HTTP`, `JSON`, `API`.
- Prefer table-driven tests with descriptive `t.Run` subtest names; mock the HTTP client for `client`/`downloader` tests.
- Respect complexity budgets: cyclomatic ≤ 15, cognitive ≤ 30, function length ≤ 100 lines / 60 statements; US spelling.
- Never log secrets (passwords, tokens, cookies) or full config; flag new `/api/v1` endpoints lacking auth middleware.
