# Consolidated Impartus lecture product: approved execution plan

Status: proposed for external review  
Owner: Impartus maintainers  
Planned from: `origin/main` at `70401a18765dcadb1cdba9201863564abd13014c`  
Supersedes: the product boundary in PR #139 and untracked Plan 001 on that branch

## Outcome

Impartus remains one Go application and owns the complete lecture workflow:

- Impartus authentication, course and lecture browsing, filtering, playback,
  streaming, downloading, local library, playback history, and download jobs;
- a Go-native Bubble Tea TUI in the existing binary;
- a native mpv window controlled over mpv JSON IPC;
- a versioned artifact contract for automation.

NotebookLM remains a separate product. `notebooklm-py` owns Google auth,
notebooks, sources, uploads, processing, generation, and NotebookLM reads.
Hermes composes both products through the artifact contract. No NotebookLM
package, credential, subprocess, configuration, or protocol remains in
Impartus.

The work is complete only after the CLI/TUI is usable locally with a real
Impartus account, a lecture can be streamed and downloaded, its artifact is
recorded, and the same artifact can be uploaded by the separate NotebookLM
side without a duplicate on a repeated run. Unit tests alone are not the
product acceptance gate.

## Evidence and corrections to the initial proposal

The live code and PR were inspected before this plan:

- `origin/main` already has a secure-enough starting point for playback:
  `internal/downloader/play.go` binds an ephemeral loopback listener, uses an
  unguessable path token, fetches the AES key with the authenticated Go client,
  splits the views, decrypts segments, and returns a local HLS URL.
- `internal/cli/cli_play.go` launches mpv synchronously with inherited terminal
  I/O. It has no JSON IPC, observed playback state, or reusable player session.
- `download --json` already has a stable outer envelope and a backward-
  compatible `downloadResult`, but it only exposes output paths and counts.
- `internal/server` is a separate HTTP/WebSocket product surface. It is not the
  TUI backend and is frozen during this work.
- PR #139 is 29 files and about 5.8k added lines. Its checks are green, but the
  branch embeds NotebookLM policy and still has an unresolved failure in which
  a failed ambiguity-state save can roll state back and permit a duplicate
  source add.
- `notebooklm-py` v0.8.0 already owns file upload and a stable JSON/exit-code
  contract. A successful file add returns after the byte upload is finalized;
  `source wait` owns processing-status policy.

Warp CLI with `claude-5-opus-high` completed the exact-repository architecture
brainstorm on 2026-08-07. It selected one Go process, Bubble Tea v2, native mpv
over JSON IPC, a stable artifact manifest, a pure-Go SQLite library, and a
separate NotebookLM boundary. A fresh follow-up on 2026-08-08 was submitted to
the same model but Warp returned `QuotaLimit` after all three retries and did
no work. The completed artifact is therefore the Opus input to this plan; Droid
Kimi K3 Max and Grok 4.5 High must independently review the committed plan and
every production diff.

Two corrections are deliberate:

1. A stable artifact identity is not a media content hash. `artifactId`
   identifies the logical Impartus output; each file may separately carry a
   content SHA-256. This avoids changing the join key when the same lecture is
   deterministically re-encoded while still allowing integrity verification.
2. A shell pipeline is not an exactly-once upload mechanism. NotebookLM has no
   server-side idempotency-key API. The Hermes-side bridge must persist an
   ambiguous state before invoking `notebooklm source add`, reconcile by the
   stable title token, and refuse to re-add while the outcome is uncertain.
   This is fail-closed, effectively-once behavior; the documentation must not
   claim mathematically exactly-once delivery.

## Architecture and ownership

```text
                    one Go binary: impartus
  +----------+       +--------------------------+
  | CLI/TUI  | ----> | internal/app services    |
  +----------+       | catalog, playback, jobs  |
                     +------+---------+---------+
                            |         |
                    +-------v--+   +--v----------------+
                    | Impartus |   | playback proxy    |
                    | client   |   | loopback HLS/AES  |
                    +----+-----+   +---------+----------+
                         |                   |
                    Impartus API       native mpv window
                                      JSON IPC socket 0600

  download completion
          |
          v
  manifest v1 + library.db + optional NDJSON events
          |
          v
  Hermes bridge  ---->  notebooklm-py CLI  ----> NotebookLM
  durable reconcile     explicit notebook ID      Google auth
```

Package rules:

- `internal/tui` owns only view state, layout, key bindings, and translation
  between application events and `tea.Msg` values. It does not import
  `net/http`, `os/exec`, or `database/sql`.
- `internal/app` is a small orchestration seam used by the TUI and new CLI
  paths. It wraps existing `client` and `downloader` behavior instead of
  rewriting them.
- `internal/player` owns mpv process supervision and JSON IPC.
- The existing loopback HLS proxy remains in `internal/downloader` during the
  first player PR. It may move mechanically to `internal/stream` only after the
  app/player API is green; no duplicate proxy implementation is allowed.
- `internal/library` owns pure-Go SQLite migrations and the artifact, playback,
  and download-job records. Remote catalog caching is deferred until measured
  latency proves it useful.
- `internal/server` is frozen. The TUI must not call it or require a daemon.
- `internal/notebooklm` and NotebookLM-specific watch state never land on the
  replacement branch.

## Stable contracts

### Artifact manifest v1

Every completed lecture download produces one artifact. A multi-lecture command
returns an array of artifacts while preserving all existing `downloadResult`
fields.

```json
{
  "schemaVersion": 1,
  "artifactId": "impartus:v1:BASE64URL_SHA256",
  "lecture": {
    "ttid": 12345,
    "subjectId": 67,
    "sessionId": 8,
    "seqNo": 12,
    "topic": "Topic",
    "startTime": "upstream value",
    "durationSeconds": 3600,
    "professor": "Name",
    "institute": "Institute",
    "noAudio": false
  },
  "selection": {
    "views": "both",
    "quality": "720",
    "audioOnly": false,
    "audioFormat": "mp3"
  },
  "files": [
    {
      "path": "/absolute/path/to/file.mkv",
      "role": "video",
      "view": "both",
      "container": "mkv",
      "bytes": 90604400,
      "sha256": "optional lowercase hex"
    }
  ],
  "producedAt": "RFC3339 UTC",
  "producer": {"name": "impartus", "version": "0.1.20"}
}
```

Canonical `artifactId` input is a length-delimited, versioned encoding of:

`instituteId, subjectId, sessionId, ttid, normalized views, normalized quality,
audioOnly, normalized audioFormat`.

It excludes path, title, timestamps, producer version, output bytes, and map
iteration order. IDs are computed in one package with golden vectors. A file
hash is optional in interactive use because hashing a large lecture adds a full
disk read; `library verify --hash` fills or rechecks it explicitly.

Compatibility rules:

- the outer `{success,data,error,meta}` envelope is unchanged;
- `status`, `outputPaths`, `lectureCount`, `filteredCount`, and `totalLectures`
  remain present with their current meaning;
- `artifacts` is additive in `data`;
- JSON field names and v1 semantics cannot change without a schema version;
- human-mode output remains human-readable and stdout never mixes progress with
  machine JSON.

### Event stream v1

`--events` selects NDJSON and is mutually exclusive with the one-envelope
`--json` mode. Every record includes `schemaVersion`, `event`, `timestamp`, and
`jobId`; lecture events include `artifactId`. The terminating event is
`job.completed` with the complete artifact array. Event names in scope are:

- `job.started`, `lecture.started`, `lecture.progress`,
  `lecture.completed`, `lecture.failed`, `job.completed`, `job.cancelled`.

Human diagnostics go to stderr. Cancellation must emit a terminal event when
stdout is still writable. The event stream is implemented with the generic
watcher, not in the first manifest PR.

### mpv IPC contract

- Unix socket: `$XDG_RUNTIME_DIR/impartus/mpv-<pid>-<random>.sock`, parent mode
  0700, socket mode 0600 after creation. A private 0700 temp runtime directory
  is the fallback when `XDG_RUNTIME_DIR` is absent.
- Windows: an isolated named pipe behind a build-tagged transport. Unsupported
  functionality fails clearly; Linux behavior must not silently be compiled
  into Windows.
- mpv starts with no upstream credential and no Impartus URL in argv. It only
  receives the loopback URL.
- One reader goroutine owns newline-delimited JSON decoding. Commands carry a
  monotonically increasing integer `request_id`; pending replies are bounded
  and removed on response, context cancellation, timeout, or disconnect.
- Observed properties: `pause`, `time-pos`, `duration`, `volume`, `mute`,
  `speed`, `playlist-pos`, `core-idle`, and `eof-reached`/end-file events.
- Shutdown sends `quit`, waits a bounded interval, then kills and reaps the
  process group. Socket cleanup is idempotent.
- mpv uses `--no-terminal`; Bubble Tea exclusively owns the terminal.

### Local library contract

The database lives at `$XDG_STATE_HOME/impartus/library.db` with a private
0700 parent and 0600 file. Use `modernc.org/sqlite`, WAL, foreign keys, and a
busy timeout. Embedded forward-only migrations are keyed by `PRAGMA
user_version` and execute transactionally.

Initial tables are deliberately narrow:

- `artifacts`: manifest JSON plus indexed identity, lecture, path-presence, and
  verification columns;
- `playback`: artifact/lecture resume position, duration, completion, and last
  played time;
- `jobs`: download lifecycle, attempts, error summary, cancellation request,
  and timestamps.

Do not cache the remote catalog in this project phase. Do not write progress to
SQLite for every media chunk; coalesce progress updates. Never delete an
artifact row just because its file is missing—`library verify` marks it missing.

### Hermes/NotebookLM bridge contract

The bridge is a separate Hermes skill/script, not an Impartus package. It
accepts a manifest plus an explicit full notebook UUID and profile/home
selection. It invokes the pinned `notebooklm-py` CLI with `--json`, validates
the documented JSON error codes, and never relies on selected-notebook context.

For each `(profile, notebookId, artifactId)` it stores:

- state: `pending`, `ambiguous`, `uploaded`, or `manual_review`;
- exact source title token, source ID when known, attempt count, and sanitized
  error class;
- an atomic durable commit before and after the remote mutation.

The title contains an exact, reversible token such as
`[impartus:<artifactId>]`. On first use the bridge lists sources and refuses an
ambiguous partial match. Immediately before `source add`, it commits
`ambiguous`. On exit 0 it records the returned source ID. On timeout, crash, or
non-zero after invocation, the next run lists exact-title matches and calls
`source wait` for a unique source. READY becomes `uploaded`; missing remains
ambiguous for a bounded reconciliation window before an explicit retry;
multiple matches, processing failure, or an unclassifiable response becomes
`manual_review`. It never deletes a NotebookLM source automatically.

The bridge stores no Google cookie itself and scrubs CLI output. Authentication
continues to live in `NOTEBOOKLM_HOME`, `NOTEBOOKLM_PROFILE`, or
`NOTEBOOKLM_AUTH_JSON` as supported by notebooklm-py.

## Dependency-ordered pull requests

Each PR is cut from the previous green exact head when stacking is necessary.
Every PR is independently useful, contains docs and tests for its contract, and
keeps production changes near 800 changed lines when practical. Generated
goldens, migrations, and fixtures are excluded from that soft limit.

### PR 1 — `feat(cli): add stable download artifact manifest v1`

Likely files:

- `internal/artifact/{manifest.go,id.go,files.go,*_test.go}`;
- `internal/cli/cli_download.go` and focused tests;
- `internal/buildinfo` only if producer version injection needs a seam;
- `docs/api-reference.md`, `docs/architecture.md`, and this plan.

Work:

1. Introduce typed manifest and deterministic ID golden vectors.
2. Preserve the existing result while adding per-lecture artifacts.
3. Map each playlist/join result to its original lecture by TTID and fail if the
   mapping is missing or duplicated.
4. Stat outputs, normalize absolute paths, infer role/view/container from the
   typed join result rather than filename heuristics, and reject missing outputs.
5. Add exact JSON golden tests for video left/right/both and audio formats.

Acceptance:

- legacy JSON fields and human output tests remain unchanged;
- identical logical inputs produce identical IDs across runs and map order;
- different TTID/view/quality/audio settings produce different IDs;
- partial/missing output files fail before a completed manifest is emitted;
- `go test ./... -count=1`, `go test -race ./...`, `go build ./...`,
  `CGO_ENABLED=0 go build ./...`, `make lint`, and `git diff --check` pass.

### PR 2 — `feat(playback): supervise mpv over private JSON IPC`

Likely files:

- `internal/player/{client.go,process.go,transport_unix.go,
  transport_windows.go,*_test.go}`;
- `internal/app/{catalog.go,playback.go,service.go,*_test.go}`;
- minimal adaptations in `internal/cli/cli_play.go`;
- hardening tests in `internal/downloader/play_test.go`;
- `internal/cli/cli_doctor.go` and docs.

Work:

1. Build the bounded JSON IPC client against a fake Unix-socket mpv.
2. Add process supervision, readiness polling, events, controls, graceful quit,
   kill fallback, and reaping.
3. Route existing `play` through `internal/app` while preserving flags and
   sequential playback behavior.
4. Add `doctor` checks for mpv, ffmpeg, config permissions, and writable state/
   runtime directories.
5. Harden the loopback server with exact host/path checks, idempotent cleanup,
   and surfaced upstream auth failure. Do not duplicate or broadly rewrite it.

Acceptance:

- fake mpv covers replies, out-of-order request IDs, property events, malformed
  JSON, disconnect, command timeout, cancellation, and silent process death;
- no credential appears in mpv argv, logs, or errors;
- cancellation leaves no child process or socket;
- existing `play` tests and all baseline gates pass;
- an opt-in local smoke launches real mpv with `--vo=null`, observes state,
  pauses/seeks, quits, and verifies the child is reaped.

### PR 3 — `feat(library): persist artifacts, playback, and download jobs`

Likely files:

- `internal/library/{db.go,migrate.go,artifact.go,playback.go,jobs.go,
  migrations/*.sql,*_test.go}`;
- `internal/cli/cli_library.go` and focused dispatch/JSON tests;
- small hooks in `internal/app` and download completion;
- config/path docs without adding secrets.

Work:

1. Add pure-Go SQLite and transactional migrations.
2. Commit completed manifests idempotently and expose `library list`, `show`,
   and `verify` with human and JSON modes.
3. Record coalesced playback positions and offer resume through the app service.
4. Persist download job lifecycle. Mark interrupted running jobs recoverable on
   startup; do not pretend chunk-level resume exists yet.
5. Add `library repair` only if a real corruption fixture proves a safe action;
   otherwise `doctor` reports and stops.

Acceptance:

- migration from empty and repeated migration are deterministic;
- downgrade/newer-schema input fails closed without changing the DB;
- concurrent reader/writer, cancellation, disk-full/write error, duplicate
  manifest, moved/missing file, and path-with-non-ASCII tests pass;
- `library verify` never deletes user data;
- `go test -race ./...` and `CGO_ENABLED=0 go build ./...` pass.

### PR 4 — `feat(tui): browse, play, download, and resume lectures`

Dependencies: PRs 1–3.

Likely files:

- `internal/tui/{model.go,update.go,view.go,keys.go,styles.go,
  messages.go,*_test.go}`;
- `internal/cli/cli_tui.go` and root dispatch tests;
- `internal/app` additions only where a real TUI use case requires them;
- `go.mod`, README, API/reference docs.

Work:

1. Use Bubble Tea v2, Bubbles, and Lip Gloss v2 in the existing binary.
2. Ship an explicit `impartus tui`; no-arg launches it only on a real TTY.
   Preserve the old prompt UI temporarily as `impartus classic` for one release.
3. Implement course list, lecture list, details, filtering, responsive layout,
   generated help, loading/error/empty states, and dependency diagnostics.
4. `enter` plays through the app/player service; transport keys use IPC. `d`
   downloads through the app service and commits the artifact/library job.
5. Add a library view and resume prompt. Keep queue management, themes, mouse,
   config editing, and terminal-cell video out of the first complete product.

Acceptance:

- deterministic model/update tests cover navigation, filter, back/quit,
  cancellation, play events, download events, errors, and terminal resizes;
- golden views cover 40x10, 80x24, and a wide terminal without panic or secret
  rendering;
- a PTY test confirms alternate-screen restoration after normal quit, error,
  signal cancellation, and panic recovery;
- no TUI package directly performs HTTP, subprocess, or SQL work;
- real local browse/play/download is completed with user credentials.

### PR 5 — `feat(watch): add generic durable lecture auto-download`

Dependencies: PRs 1 and 3. This is the functionality retained from #139.

Likely files:

- a fresh, NotebookLM-free `internal/watch` package and tests;
- generic watch target config/validation;
- `internal/cli/cli_watch.go`, dispatch and JSON/NDJSON tests;
- sample config and operational docs.

Work:

1. Port only the polling, target, per-cycle budget, retry/backoff, failure
   isolation, and crash-safe download concepts from #139.
2. Use the library job table and artifact ID. Terminal success is a validated,
   committed download manifest—not an upload.
3. Add `--once`, `--dry-run`, and `--events`. Do not add `--upload`, `--check`,
   notebook routing, or deletion-after-upload.
4. Treat interrupted downloads as recoverable. Chunk-level resumability is a
   separately reviewed optimization, not a false promise in this PR.

Acceptance:

- repeated cycles do not redownload a committed artifact unless explicitly
  forced;
- crash before file completion, crash after file completion but before DB
  commit, corrupt/newer DB, partial target failure, auth expiry, rate limiting,
  cancellation, and concurrent-instance lock tests pass;
- NDJSON is valid and terminal events are not dropped;
- no source file or configuration key contains `notebooklm`, notebook/source
  IDs, Google auth, or provider process execution.

### Separate Hermes patch — `impartus-notebooklm` composition skill

This is not an Impartus PR. Install notebooklm-py v0.8.0's own `notebooklm`
skill into the intended Hermes instance, then add a small composition skill and
tested script under that Hermes instance. If an existing version-controlled
Hermes workspace is discovered, use a focused PR there; otherwise keep the
patch local and record exact files/checks without creating a new public repo.

Work:

1. Accept manifest JSON by file or stdin and require explicit notebook/profile.
2. Validate schema v1, allowed local media path, file size, and optional hash.
3. Implement the durable ambiguity/reconciliation contract above using fake
   `notebooklm` process fixtures and an allowlisted environment.
4. Add `doctor`, `plan`, and `sync` operations. `plan` is read-only; `sync`
   performs the upload. No automatic source deletion exists.
5. Run a live upload twice and prove the second run returns the same source ID
   or a reconciled no-op. Simulate timeout-after-remote-acceptance with the fake
   CLI and prove no second add occurs.

Acceptance:

- invalid manifest, missing file, hash mismatch, auth failure, rate limit,
  provider JSON noise, timeout, subprocess crash, unique READY match, missing
  match, PROCESSING timeout, failed source, and duplicate-title cases are tested;
- durable state is private and atomic; errors contain no cookies or raw auth;
- live `notebooklm auth check --test --json` passes for the selected profile;
- a real Impartus artifact reaches the intended notebook and is READY;
- a repeated bridge run does not create a duplicate.

## PR #139 preserve/delete and transition

Do not merge, force-push, or close #139 before replacement evidence exists.

Preserve by reimplementation on fresh branches:

- polling schedule, target list, `--once`, `--dry-run`, per-cycle budget,
  retry/backoff, target failure isolation, corrupt-state fail-closed behavior,
  atomic/durable transition discipline, and the fake end-to-end harness shape;
- all useful lessons about ambiguous external mutation, but in the owner that
  can perform NotebookLM reconciliation.

Delete from Impartus:

- all `internal/notebooklm` code and tests;
- provider enum/pins, NotebookLM env allowlist, auth proxy/docs/scripts;
- notebook/source routing config, `--upload`, `--check`, delete-after-upload,
  source reconciliation fields, and provider-specific status names;
- phone/cloud-agent authentication workarounds and their commit archaeology.

Transition gate:

1. PR 1 is open with a reviewed artifact contract.
2. The Hermes bridge passes fake ambiguity tests and a real repeated upload.
3. PR 5 is open or its exact generic-watch diff is ready and green.
4. Update #139's title/body with replacement links and a concise preserve/delete
   table, reply to the unresolved duplicate comment with the ownership fix,
   then close #139 as superseded. Never claim its old head was merged.

## Complete test matrix

| Area | Required evidence |
|---|---|
| Baseline | tests, race, build, cgo-free build, lint, diff check |
| Artifact | golden IDs/JSON, legacy fields, all view/audio shapes, missing output, Unicode path, large-file stat |
| Proxy | token path, wrong host/path, missing view, segment bounds, upstream failure, cleanup, cancellation |
| mpv IPC | fake server, request matching, events, malformed JSON, disconnect, timeout, process death, quit/kill/reap |
| App | fake catalog/player/downloader/library; cancellation and errors remain typed and scrubbed |
| SQLite | empty/idempotent/newer migration, WAL concurrency, locked/disk-full, duplicate artifact, missing file |
| TUI | model transitions, narrow/wide goldens, resize, PTY ownership/restoration, auth/dependency/error states |
| Watch | dry run, repeat cycle, crash windows, auth/rate limit, per-target isolation, single-instance lock, NDJSON |
| Bridge | manifest validation, JSON error codes, pre-add durable ambiguity, reconciliation, duplicate and manual-review paths |
| Live local | auth, catalog, play/control, download, manifest, library/resume, watch once, NotebookLM upload, repeated no-op |
| Hosted | all required GitHub checks green on each exact head; reviewer findings reproduced before changes |

No live credential is placed in a test fixture, PR body, log, command argv,
terminal capture, or durable review artifact. Live evidence records only IDs
needed to prove behavior, redacted paths, exit codes, and hashes/fingerprints.

## Stop conditions

Stop the affected PR and report instead of improvising if:

- a contract change would remove or reinterpret an existing JSON field;
- artifact identity cannot be mapped unambiguously to the source lecture;
- a proposed player path exposes Impartus credentials to mpv or another process;
- mpv cannot be reaped or the terminal cannot be restored reliably;
- a SQLite migration is destructive, non-transactional, or requires cgo;
- a generic watcher needs any NotebookLM read or credential;
- the bridge cannot distinguish a unique exact-token source or cannot persist
  `ambiguous` before starting the provider command;
- live auth targets the wrong Impartus or Google account;
- a review finds a correctness/security blocker that is not reproduced and
  resolved on the exact head;
- hosted CI is unavailable: record `CI unavailable`, run the exact local matrix,
  and do not imply hosted success.

## Rollback and compatibility

- Every new command is additive. `classic` preserves the old prompt flow during
  the first TUI release.
- The manifest is additive to the download result. Old JSON consumers continue
  to read their fields.
- The player can temporarily fall back to the current blocking mpv launch if IPC
  startup fails before playback and the user explicitly selects that fallback;
  it may not fall back after an ambiguous command or leak the TTY to two owners.
- Library migrations are forward-only, but the pre-library binary remains able
  to download/play because the DB is additive local state.
- The generic watcher is independently stoppable and has no daemon requirement.
- Closing #139 is reversible through its Git branch and commit history; none of
  its provider code is destroyed locally.
- The Hermes bridge can be disabled without affecting Impartus. It never deletes
  a source automatically, so fail-closed manual recovery remains possible.

## Completion checklist

- [ ] Kimi K3 Max and Grok 4.5 High review this committed plan; every valid
      blocker is incorporated and both return an approval/ship verdict.
- [ ] PR 1 artifact contract implemented, locally qualified, reviewed, pushed,
      and hosted checks green.
- [ ] PR 2 mpv IPC/app/doctor implemented with fake and real-mpv evidence.
- [ ] PR 3 library/history/jobs implemented with migration and race evidence.
- [ ] PR 4 TUI supports real browse/play/control/download/library/resume.
- [ ] PR 5 generic watcher preserves automatic download without NotebookLM.
- [ ] Hermes composition skill/bridge installed and fake ambiguity suite green.
- [ ] Real Impartus lecture streamed, controlled, downloaded, manifested, and
      recorded locally without exposing credentials.
- [ ] Real manifest uploaded to the intended NotebookLM notebook and READY;
      repeated run proves reconciled no-op/no duplicate.
- [ ] PR #139 description updated, valid review concerns accounted for, and PR
      closed as superseded only after replacement links/evidence exist.
- [ ] Droid Kimi K3 Max and Grok 4.5 High review every exact committed production
      diff; findings are reproduced locally and all valid blockers fixed.
- [ ] Every final exact head has tests, race, build, cgo-free build, lint, diff
      check, hosted check, and review status recorded separately.

