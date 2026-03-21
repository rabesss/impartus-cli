---
name: go-integration-worker
description: Implement and verify OpenClaw-facing API, manifest, and cross-surface contract alignment.
---

# Go Integration Worker

NOTE: Startup and cleanup are handled by `worker-base`. This skill defines the work procedure.

## When to Use This Skill

Use this skill for features that change API route behavior, response/error shapes, OpenClaw manifests/docs that define runtime truth, websocket lifecycle semantics, request correlation behavior, or cross-surface consistency between CLI and API.

## Work Procedure

1. Read the feature, mission files, and relevant library notes.
2. Identify the exact client-visible contract being changed: route path/method/auth, JSON shape, headers, websocket payloads, config surface, or CLI/API semantic alignment.
3. Write or update failing tests first for the contract being changed. Prefer exact response-shape and header assertions over broad status-only tests.
4. Implement runtime changes and update the OpenClaw-facing manifest/docs that define runtime truth in the repo.
5. Start the local API on port `8080` for manual verification and verify representative public/protected routes, including headers and error shapes.
6. If websocket behavior changed, capture at least one representative lifecycle transcript or equivalent observation.
7. Run full validators before handoff: `make test`, `make lint`, `go build ./...`.
8. Compare equivalent CLI and API flows when the feature affects shared semantics such as ranges, views, or config resolution.

## Example Handoff

```json
{
  "salientSummary": "Aligned the local API and OpenClaw-facing contract so response shapes, request-correlation behavior, and websocket lifecycle semantics match the documented runtime truth. Added targeted tests and manually verified representative routes on port 8080.",
  "whatWasImplemented": "Updated server handlers, route metadata, and OpenClaw-facing contract files to reflect the implemented response/error/header behavior. Added tests for headers, route semantics, and client-visible JSON structures, then verified public and protected API flows manually.",
  "whatWasLeftUndone": "",
  "verification": {
    "commandsRun": [
      {
        "command": "go test ./internal/server ./internal/cli ./internal/config",
        "exitCode": 0,
        "observation": "Targeted server/contract tests passed."
      },
      {
        "command": "make test",
        "exitCode": 0,
        "observation": "Full test suite passed."
      },
      {
        "command": "make lint",
        "exitCode": 0,
        "observation": "Lint passed for touched integration files."
      },
      {
        "command": "go build ./...",
        "exitCode": 0,
        "observation": "Project builds cleanly."
      }
    ],
    "interactiveChecks": [
      {
        "action": "Started `./impartus serve --port 8080` and called `/api/v1/health` plus representative protected routes.",
        "observed": "Observed the documented response shapes and request-correlation behavior on the local API surface."
      }
    ]
  },
  "tests": {
    "added": [
      {
        "file": "internal/server/server_test.go",
        "cases": [
          {
            "name": "health_includes_expected_request_id_behavior",
            "verifies": "Request correlation remains client-usable."
          },
          {
            "name": "cancel_emits_stable_client_visible_state",
            "verifies": "Cancelation semantics are deterministic for clients."
          }
        ]
      }
    ]
  },
  "discoveredIssues": []
}
```

## When to Return to Orchestrator

- A required contract change would break agreed OpenClaw semantics and needs a product decision.
- Cross-surface alignment reveals a larger architectural mismatch that should be decomposed into new features.
- Local API/manual validation cannot proceed within the approved boundaries.
