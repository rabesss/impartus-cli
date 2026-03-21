---
name: go-downloader-worker
description: Implement and verify Go downloader, retrieval, and artifact-generation refactors.
---

# Go Downloader Worker

NOTE: Startup and cleanup are handled by `worker-base`. This skill defines the work procedure.

## When to Use This Skill

Use this skill for features that change Impartus auth/session handling, playlist retrieval/parsing, chunk download/decrypt flow, pipeline/concurrency behavior, progress tracking, or final audio/video artifact generation.

## Work Procedure

1. Read the feature, mission files, and relevant architecture/environment notes before changing code.
2. Map the exact user-visible behavior under change: output artifacts, lecture selection semantics, failure stage reporting, retry behavior, and progress semantics.
3. Add or update failing tests first for the changed behavior. Prefer parser, downloader, pipeline, or client tests that assert the externally visible outcome.
4. Implement the refactor incrementally, preserving output correctness while improving architecture/performance.
5. If concurrency/pipeline behavior changes, verify that ordering and artifact correctness remain intact.
6. Run focused package tests during iteration, then run the required full validators before handoff: `make test`, `make lint`, `go build ./...`.
7. Perform at least one artifact-oriented manual check when feasible, using a lightweight live Impartus flow or a controlled fixture path, and record exactly what output files were produced.
8. Inspect failure-path output/logging for leaked secrets or opaque errors before finishing.

## Example Handoff

```json
{
  "salientSummary": "Refactored the downloader pipeline so playlist retrieval and artifact assembly are cleaner and faster while preserving audio/video output correctness. Added tests for retry bounds and parser/output behavior, then manually verified a representative live flow.",
  "whatWasImplemented": "Updated the downloader/client/pipeline path to improve session reuse, surface stage-specific failures, and preserve correct output ordering under concurrency. Added focused tests for parser boundaries, retries, and artifact path behavior.",
  "whatWasLeftUndone": "",
  "verification": {
    "commandsRun": [
      {
        "command": "go test ./...",
        "exitCode": 0,
        "observation": "All tests passed including new downloader-focused cases."
      },
      {
        "command": "make lint",
        "exitCode": 0,
        "observation": "Touched downloader files are lint-clean."
      },
      {
        "command": "go build ./...",
        "exitCode": 0,
        "observation": "Project builds cleanly after refactor."
      }
    ],
    "interactiveChecks": [
      {
        "action": "Ran a representative audio-only download flow with existing local config.",
        "observed": "Completed without interactive prompts and produced the expected audio artifact at the configured output path."
      }
    ]
  },
  "tests": {
    "added": [
      {
        "file": "internal/downloader/downloader_test.go",
        "cases": [
          {
            "name": "chunk_retry_stops_after_bound",
            "verifies": "Transient failures do not loop indefinitely."
          },
          {
            "name": "audio_only_join_returns_expected_artifact",
            "verifies": "Audio-only mode yields the requested user-visible output."
          }
        ]
      }
    ]
  },
  "discoveredIssues": []
}
```

## When to Return to Orchestrator

- Real Impartus behavior contradicts assumptions in a way that changes mission scope or product semantics.
- Performance gains require changing boundaries or introducing new infrastructure not approved in the mission.
- Live validation cannot proceed because credentials, ffmpeg, or the external service become unavailable.
