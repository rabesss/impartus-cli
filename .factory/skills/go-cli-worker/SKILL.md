---
name: go-cli-worker
description: Implement and verify agent-facing Go CLI contract features.
---

# Go CLI Worker

NOTE: Startup and cleanup are handled by `worker-base`. This skill defines the work procedure.

## When to Use This Skill

Use this skill for features that change CLI contract behavior, JSON output/error shapes, config loading and precedence, argument validation, or agent-facing invocation semantics.

## Work Procedure

1. Read the feature, `mission.md`, mission `AGENTS.md`, and the relevant repo library notes before touching code.
2. Identify the exact CLI contract to preserve or change, including JSON shapes, flag behavior, config/env precedence, and error semantics.
3. Write or update failing tests first for the touched CLI/config behavior. Prefer exact assertions on machine-consumable payload shape and deterministic error text.
4. Implement the CLI/config changes only after the tests fail for the intended reason.
5. Verify non-interactive behavior explicitly. Agent-facing paths must never fall back to stdin prompts.
6. Run focused fast checks during iteration, then run the full required validators before handoff: `make test`, `make lint`, `go build ./...`.
7. Perform manual CLI checks for representative valid and invalid invocations, including at least one JSON-mode success case and one JSON-mode failure case.
8. If config/env behavior changed, verify the precedence matrix with representative local runs and ensure no secrets appear in logs or test fixtures.

## Example Handoff

```json
{
  "salientSummary": "Refactored the CLI agent contract so JSON mode stays non-interactive, env/config precedence is explicit, and invalid agent invocations now return structured errors. Added CLI/config tests and manually verified representative JSON success and failure flows.",
  "whatWasImplemented": "Updated internal/cli and internal/config so agent-facing invocations resolve config deterministically, reject unsupported argument patterns early, and return stable JSON payloads for both success and failure. Added focused tests asserting exact envelope fields and precedence behavior.",
  "whatWasLeftUndone": "",
  "verification": {
    "commandsRun": [
      {
        "command": "go test ./internal/cli ./internal/config",
        "exitCode": 0,
        "observation": "CLI and config tests passed including new JSON envelope and precedence cases."
      },
      {
        "command": "make test",
        "exitCode": 0,
        "observation": "Full Go test suite passed."
      },
      {
        "command": "make lint",
        "exitCode": 0,
        "observation": "Lint passed after refactor and cleanup in touched files."
      },
      {
        "command": "go build ./...",
        "exitCode": 0,
        "observation": "Project builds cleanly."
      }
    ],
    "interactiveChecks": [
      {
        "action": "Ran `./impartus --json` with no subcommand.",
        "observed": "Returned a deterministic JSON capability envelope and did not prompt for input."
      },
      {
        "action": "Ran an invalid JSON-mode command with missing required flags.",
        "observed": "Returned a structured JSON error with deterministic text and no interactive fallback."
      }
    ]
  },
  "tests": {
    "added": [
      {
        "file": "internal/cli/cli_test.go",
        "cases": [
          {
            "name": "json_mode_missing_required_flags_returns_structured_error",
            "verifies": "JSON-mode CLI validation failures remain machine-consumable."
          },
          {
            "name": "env_overrides_config_for_agent_invocation",
            "verifies": "Config precedence is deterministic and testable."
          }
        ]
      }
    ]
  },
  "discoveredIssues": []
}
```

## When to Return to Orchestrator

- The required contract change conflicts with mission-approved CLI/API semantics.
- The feature depends on downloader/API behavior that is not yet stable enough to verify the CLI surface.
- Live Impartus behavior reveals a product decision that needs human judgment rather than a code-only fix.
