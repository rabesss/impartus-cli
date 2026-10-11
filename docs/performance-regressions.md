# Performance regression checks

The `Performance regression` job runs on pull requests, main pushes and manual
dispatch. It uses synthetic media and loopback services, without Impartus
credentials, remote providers, FFmpeg joining or real lectures.

## Blocking checks

- AES-CBC decryption of fixed 64 KiB and 1 MiB buffers must preserve every
  plaintext byte. Each operation resets the caller buffer, invokes production
  `DecryptAESInPlace`, and checks a fixed SHA-256 oracle. Proposed ceilings are
  2,048 allocated bytes and five allocations per operation, with Go 1.26.5.
  The initial local measurement was 1,040 bytes and three allocations for both
  sizes. Copying the complete buffer exceeds the ceiling while preserving
  correct plaintext. This measures the helper, not every media pipeline allocation.
- A two-view download must retrieve exactly one key and twelve chunks, reach
  exactly three concurrent chunk requests, deliver 960 encrypted chunk-body
  bytes, preserve 768 ordered plaintext bytes, and leave no encrypted temporary
  files. A bounded barrier detects unintended serialisation or reduced worker
  concurrency. Five/ten-second deadlines are execution safeguards, not speed SLOs.
- For each existing UI layout, 200 arrow inputs must advance through a fixed
  5,000-course catalog and render the expected selected course. Every input is
  checked; a renderer that omits navigation cannot appear faster.

The limits are fixed-scenario regression budgets. They do not certify every
performance path, real-world network throughput, playback or end-user latency.

## Advisory measurements

The report includes decryption time/throughput, UI cold frame and input
p50/p95/p99, and process RSS after each layout. RSS is a point measurement,
not peak memory. There are three separate-process samples per source, with
alternating source order. Timings are descriptive; they are not statistical
speed verdicts and do not fail CI. UI frame fingerprint changes disable UI
timing comparison. Correctness and allocation checks still apply.

## Comparable sources and policy

Both commits are materialized from immutable Git archives. The runner overlays
the same trusted Go fixture and UI benchmark into each, and uses pinned Go/Bun
versions and each source's frozen lockfile. The PR baseline is its exact base
commit; the tested candidate is the checked-out merge commit, labeled separately
from the requested PR head. Main pushes use the prior commit; manual runs use
the checked-out commit's first parent.

The established driver, fixtures and budgets come from the exact base commit.
Candidate changes to those files are reported, and do not raise the active
ceilings. The introduction PR uses an explicitly labeled bootstrap harness
from the candidate for both sources only when none of the three newly added
core files exists in the baseline. Partial harnesses, unavailable source refs,
incomplete measurements and malformed results fail availability (exit 2).
Measured candidate budget violations exit 1; a complete passing run exits 0.

Change budgets deliberately in a calibration PR: keep the existing scenarios
passing, record actual observations on the pinned runner, explain the new
limit and distinguish fixture changes from measured growth. The existing base
policy remains active for that PR; the reviewed new policy applies after merge.
Branch protection is configured separately by the repository owner.

## Local run

On Linux amd64, with Go 1.26.5, Bun 1.3.14 and Python 3.12 or newer:

```sh
python3 -m unittest discover -s scripts/performance -p 'test_*.py'
python3 scripts/performance/run.py \
  --base <full-base-commit-sha> --candidate <full-candidate-commit-sha> \
  --output /var/tmp/impartus-performance-results
```

Results are `results.json` and `summary.md`. The workflow uploads them as an
artifact and appends the summary to its job page. It has read-only repository
permissions, provides no secrets to measured code, and posts no PR comments.
Temporary snapshots and dependencies are removed by the local runner. Use an
isolated reviewed checkout for local candidate execution: the harness is not
a sandbox for hostile source code.
