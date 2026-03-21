---
name: quality-gate-worker
description: Integrate and verify desloppify-driven quality gates and repo automation surfaces.
---

# Quality Gate Worker

NOTE: Startup and cleanup are handled by `worker-base`. This skill defines the work procedure.

## When to Use This Skill

Use this skill for features that introduce or refine `desloppify`, quality-gate automation, local state handling, workflow commands, or repo contract surfaces related to code-quality scoring.

## Work Procedure

1. Read the feature, mission files, and current repo automation surfaces before making changes.
2. Identify the exact quality-gate workflow to add or update: installation, commands, local state handling, score reporting, or enforcement threshold.
3. Write or update failing tests/checks first where feasible for any scripted/config behavior you introduce.
4. Implement the workflow so it is safe for local use, does not commit `.desloppify/` internals, and is easy for workers/validators to run.
5. Run the relevant quality-gate commands yourself, including a real `desloppify` invocation, and record the resulting score/output.
6. Run repo validators before handoff: `make test`, `make lint`, `go build ./...`.
7. If the score does not exceed 80, either improve the touched area enough to cross the threshold or return a clear handoff explaining the blocker and remaining debt.

## Example Handoff

```json
{
  "salientSummary": "Integrated desloppify into the repo workflow as a required post-change quality gate and verified it runs locally without committing .desloppify state. Recorded the resulting score and aligned automation surfaces with the mission threshold.",
  "whatWasImplemented": "Added repo automation and local setup for desloppify, ensured `.desloppify/` stays out of commits, and wired a clear quality-gate command flow that workers and validators can run after refactors. Verified the real tool output and captured the current score.",
  "whatWasLeftUndone": "",
  "verification": {
    "commandsRun": [
      {
        "command": "python3 -m pip install --user \"desloppify[full]\"",
        "exitCode": 0,
        "observation": "Installed desloppify locally for mission use."
      },
      {
        "command": "desloppify scan --path .",
        "exitCode": 0,
        "observation": "Generated a current quality scan for the repo."
      },
      {
        "command": "desloppify next",
        "exitCode": 0,
        "observation": "Reported the current prioritized quality backlog and overall score."
      },
      {
        "command": "make test && make lint && go build ./...",
        "exitCode": 0,
        "observation": "Repo validators still pass after workflow integration."
      }
    ],
    "interactiveChecks": [
      {
        "action": "Inspected git status after running desloppify.",
        "observed": "Only intended workflow/config changes were present; `.desloppify/` state was not staged for commit."
      }
    ]
  },
  "tests": {
    "added": []
  },
  "discoveredIssues": []
}
```

## When to Return to Orchestrator

- `desloppify` cannot be installed or run in this environment.
- The repo cannot reach the required score threshold without broader scope changes that need replanning.
- Local state handling or workflow integration would violate mission boundaries or conflict with existing automation unexpectedly.
