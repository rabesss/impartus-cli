---
name: doc-worker
description: Worker for updating documentation files to reflect current codebase state. Reads source files for ground truth, updates MD files.
---

# Doc Worker

NOTE: Startup and cleanup are handled by `worker-base`. This skill defines the WORK PROCEDURE.

## When to Use This Skill

Features that update documentation, CLAUDE.md files, specs, README, AGENTS.md, or any Markdown files to reflect current codebase state. No Go source code changes.

## Required Skills

None.

## Work Procedure

### Step 1: Read Mission Context
1. Read `mission.md` for mission overview
2. Read `AGENTS.md` for boundaries (DO NOT modify .go files)
3. Read `.factory/library/architecture.md` for current architecture understanding
4. Read your assigned feature description — understand which files need updating and what claims need fixing

### Step 2: Read Source Files for Ground Truth
Before writing ANY documentation, read the relevant source files to verify claims:
- `go.mod` — for Go version, dependency versions
- `internal/server/server.go` — for endpoints, response formats, job store, health checks
- `internal/server/auth.go` — for response helpers, envelope format
- `internal/server/job_persistence.go` — for persistence behavior
- `internal/config/config.go` — for Config struct fields, env vars
- `internal/cli/cli.go` — for CLI commands, helpers
- `cmd/main.go` — for entrypoint description
- Any other files referenced in your feature

### Step 3: Read Target Documentation Files
Read ALL files your feature is supposed to fix. Understand what's stale and what needs to change.

### Step 4: Make Targeted Edits
For each file:
1. Make minimal, accurate edits — fix only what's wrong
2. Preserve existing formatting style and conventions
3. DO NOT add content that isn't grounded in source code verification
4. DO NOT modify any `.go` files

### Step 5: Verify Edits
For each file you edited:
1. Re-read the edited file to verify accuracy
2. Run the evidence commands from your feature's verificationSteps
3. Run the evidence commands from validation-contract.md for your assertions

### Step 6: Commit
1. `git add` only the documentation files you changed
2. Commit with descriptive message referencing the feature ID
3. DO NOT stage .go files or any files outside your scope

## Example Handoff

```json
{
  "salientSummary": "Fixed 3 factory library docs: environment.md (Go 1.24.0, golangci-lint v1.64.8), lecture-ordering-convention.md (API now reverses too), architecture.md (removed writeJSON, added not_configured). All grep evidence commands pass.",
  "whatWasImplemented": "Updated .factory/library/environment.md: corrected Go version from 1.26 to 1.24.0, golangci-lint from v1.64.5 to v1.64.8. Rewrote .factory/library/lecture-ordering-convention.md to reflect resolved discrepancy (both CLI and API reverse). Updated .factory/library/architecture.md: removed writeJSON dead code entry, added not_configured upstream status documentation.",
  "whatWasLeftUndone": "",
  "verification": {
    "commandsRun": [
      {"command": "grep -n 'Go 1.24' .factory/library/environment.md", "exitCode": 0, "observation": "Found Go 1.24.0 reference"},
      {"command": "grep 'Go 1.26' .factory/library/environment.md", "exitCode": 1, "observation": "No stale Go 1.26 reference"},
      {"command": "grep 'writeJSON' .factory/library/architecture.md", "exitCode": 1, "observation": "No writeJSON references"}
    ],
    "interactiveChecks": []
  },
  "tests": {
    "added": []
  },
  "discoveredIssues": []
}
```

## When to Return to Orchestrator

- Source file contradicts the expected documentation change (requirements ambiguity)
- A documentation file needs updating that's outside your feature scope
- You discover additional stale documentation not covered by your feature
