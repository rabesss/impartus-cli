# Post-83.3 Desloppify Follow-Up Mission

## Mission Goal

Raise the desloppify strict score beyond 83.3 honestly by following the live queue, not by replaying the already-completed quality mission. This mission should first refresh the triage state with the repo-local desloppify in `.venv`, then execute the highest-leverage follow-on fixes and review refresh work that emerge from that queue.

## Current State (Baseline)

**Desloppify Scores:**
- Strict: 83.3/100
- Objective: 93.6/100
- Verified: 93.3/100
- Target: 95.0/100

**Queue Signals:**
- 51 new review issues not yet triaged
- 3 planning steps queued in `desloppify next`
- 55 stale tracked items outside the live queue
- 6 skipped items

**Known High-Leverage Opportunities:**
1. Complete the pending triage workflow with `.venv/bin/desloppify plan triage`
2. Refresh subjective review coverage, especially AI generated debt, test strategy, and auth consistency
3. Address the high-confidence CLI prompt default issue if it remains prioritized
4. Address top test-health items surfaced by the venv-backed queue

## Constraints

- Use `.venv/bin/desloppify` for all desloppify commands
- Do not use the older global `desloppify` binary on `PATH`
- Use workers for implementation; orchestrator delegates only
- Preserve CLI/API behavior except for genuine bug fixes
- Respect repo guidance in `AGENTS.md`
- All implementation changes must pass `go test ./...`, `make lint`, and `go build ./...`

## Milestones

### Milestone 1: Queue Refresh

Bring desloppify planning state up to date using the repo-local toolchain so the rest of the mission runs off a trustworthy queue.

**Features:**
1. `triage-review-backlog`
2. `refresh-priority-queue`

**Validation:**
- `.venv/bin/desloppify next` stops pointing at incomplete planning stages
- The execution queue is refreshed from the current review backlog
- No code behavior changes are introduced in this milestone

### Milestone 2: High-Leverage Fixes

Execute the best concrete fixes from the refreshed queue, prioritizing real contract bugs and test-health issues that can move score honestly.

**Features:**
1. `fix-cli-prompt-default-contract`
2. `cover-streamutils-module`

**Validation:**
- CLI prompt behavior matches the documented/default branch semantics
- Targeted test-health findings are resolved with real tests
- All tests, lint, and build pass

### Milestone 3: Review Refresh And Rescan

Refresh the most important subjective review dimensions and verify whether the strict score moves beyond 83.3.

**Features:**
1. `refresh-priority-subjective-review`
2. `final-quality-scan`

**Validation:**
- Subjective review coverage is refreshed with the repo-local desloppify workflow
- Final scan records the new strict/objective/verified scores
- Mission ends with clear evidence of either score improvement or a documented plateau

## Environment Setup

```bash
cd /home/ravish/Desktop/clawds-code-crib/impartus-go/impartus

.venv/bin/desloppify status
.venv/bin/desloppify next
go test ./...
make lint
go build ./...
```

## Infrastructure

**Local Services:**
- Existing CLI surface
- Existing API surface when needed for validation

**Off-limits:**
- No new infrastructure, ports, or credentials
- No incompatible API contract changes
- No unsafe score gaming via resolves without real fixes

## Testing Strategy

1. Use focused Go tests first for touched areas
2. Run full `go test ./...` before handoff
3. Run `make lint`
4. Run `go build ./...`
5. Re-run `.venv/bin/desloppify scan --path .` after meaningful batches

## Non-Functional Requirements

- Prefer small, reviewable batches
- Challenge stale review debt before large refactors
- Document any genuine plateau with concrete evidence from the refreshed queue
