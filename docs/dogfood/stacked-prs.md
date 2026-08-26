# Impartus dogfood fix stack

This stack fixes the verified CLI and TUI bugs from the 26 Aug 2026 dogfood runs.
The users of `download`, `watch`, `library`, `tui`, and `play` feel these first.
The rule is red then green. One check per PR.
The PR ids follow in stack order.
PR-token-cache, PR-cli-start, PR-cli-quality, PR-cli-json, PR-cli-help, PR-library-argv, PR-watch-skip, PR-tui-events, PR-tui-download-progress, PR-tui-download-cancel, PR-tui-chrome, PR-tui-nav-focus, PR-tui-filter-slash.

## How to read this

One box is one unit of work. Every box names the evidence that checks it. A nested box is a sub-step of the box above it. Check a box only when its evidence exists, a file, a log line, a screenshot, a test run, or a SHA. The body is a how-to. The appendices explain and record.

The program runs `pstack/skills/poteto-mode/playbooks/autopilot-stack.md`. The operator lands every PR. Nothing auto-merges.

Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

## Program checklist

### Arm the program

- [ ] State the protocol and this plan to the operator, then stop. Start execution only on her explicit go.
- [ ] On her go, arm a `/goal` with this exact text. "/cursor/stores/self/docs/dogfood-stacked-prs.md. Stack PR-token-cache through PR-tui-filter-slash. A PR is verified only when its unit, live, and perf boxes are all checked. The operator lands. Done when the tip is STACK-READY and every issue 189-200 is linked."
- [ ] Read these from trunk at program start. Re-read them at every tick.
  - [ ] `git show origin/main:pstack/skills/poteto-mode/playbooks/autopilot-stack.md`
  - [ ] `git show origin/main:pstack/skills/swarm/SKILL.md`
  - [ ] `git show origin/main:pstack/skills/control-cli/SKILL.md`
  - [ ] `git show origin/main:pstack/skills/poteto-mode/playbooks/opening-a-pr.md`
  - [ ] `git show origin/main:pstack/skills/technical-writing/SKILL.md`
  - [ ] `git show origin/main:pstack/skills/unslop/SKILL.md`
  - [ ] `git show origin/main:pstack/skills/how/SKILL.md`
- [ ] Arm the 30-minute audit tick. In a local session, a real terminal `/loop`. In a cloud root, a cloud-sleeper wake chain. Never leave the cadence to memory.
- [ ] Use this tick prompt, verbatim. "Re-read the execution playbook from trunk and the armed /goal. Audit the operation against both and fix drift in this tick. Probe every active lane and judge progress by side effects only. Stand down a stuck lane and dispatch its replacement now. Then send the operator a status message, whether or not anything changed, with the queue table of PR, owner, state, and head SHA, the verdicts since the last tick, what merged, open operator gates, and blockers."
- [ ] On the operator's hold or stand-down, send every owner a zero-writes order at once.

### Spawn owners

- [ ] Spawn one owner per PR with the full lifecycle the execution playbook names.
- [ ] Follow this dependency graph. Start dependent work only after its parent merges, or base it on the parent branch when the execution playbook stacks.
  - [ ] PR-token-cache is first and branches from `main`. Issue [#193](https://github.com/rabesss/impartus-cli/issues/193).
  - [ ] PR-cli-start after PR-token-cache. Issue [#189](https://github.com/rabesss/impartus-cli/issues/189).
  - [ ] PR-cli-quality after PR-cli-start. Issue [#192](https://github.com/rabesss/impartus-cli/issues/192).
  - [ ] PR-cli-json after PR-cli-quality. Issue [#191](https://github.com/rabesss/impartus-cli/issues/191).
  - [ ] PR-cli-help after PR-cli-json because both can touch `cli_json.go`. Issue [#190](https://github.com/rabesss/impartus-cli/issues/190).
  - [ ] PR-library-argv after PR-cli-help. Issue [#195](https://github.com/rabesss/impartus-cli/issues/195).
  - [ ] PR-watch-skip after PR-library-argv. Issue [#194](https://github.com/rabesss/impartus-cli/issues/194).
  - [ ] PR-tui-events after PR-watch-skip. Issue [#196](https://github.com/rabesss/impartus-cli/issues/196).
  - [ ] PR-tui-download-progress after PR-tui-events. Issue [#197](https://github.com/rabesss/impartus-cli/issues/197).
  - [ ] PR-tui-download-cancel after PR-tui-download-progress. Same issue [#197](https://github.com/rabesss/impartus-cli/issues/197).
  - [ ] PR-tui-chrome after PR-tui-download-cancel. Issue [#198](https://github.com/rabesss/impartus-cli/issues/198).
  - [ ] PR-tui-nav-focus after PR-tui-chrome because both edit `ui/src/view.ts`. Issue [#199](https://github.com/rabesss/impartus-cli/issues/199).
  - [ ] PR-tui-filter-slash after PR-tui-nav-focus because both edit `ui/src/view.ts`. Issue [#200](https://github.com/rabesss/impartus-cli/issues/200).
- [ ] Hold the file boundaries. CLI PRs stay in `internal/cli`, `internal/client`, or `internal/watch` as named below. TUI PRs stay in `ui/src` plus `internal/tuisession` as named below.
- [ ] Hold the review gate. PR-cli-start, PR-cli-help, and every `PR-tui-*` change an interaction. They wait for the operator's review in chat with screenshots and a video before merge.

### PR mechanics, for every PR

- [ ] Open the PR ready, never draft, with `gh pr create` and `draft: false`, or with Graphite `gt` for a stack.
- [ ] Run the repo's lint and typecheck once before the PR-facing push. Push with hooks on.
- [ ] Run `/deslop` before each commit and `/no-comments` before review.
- [ ] Triage every Bugbot and security-reviewer comment per `../references/bugbot-triage.md`.
- [ ] Rebase onto current trunk before babysit and again before the merge-ready report.

### Verdict and merge, for every PR

- [ ] At the merge-ready head SHA, run the swarm per `pstack/skills/swarm/SKILL.md`. One gates lane. The ten live lanes from the PR's **Verify, live** block. The perf lane from its **Verify, perf** block. One audit lane that reads the diff and the receipts and distrusts the PR body.
- [ ] Clean only when every lane is `PASS`. Findings go back to the owner. A new head gets a fresh swarm and a fresh verdict.
- [ ] The root appends the PR to the Graphite stack on a clean verdict. The operator lands it. Restack voids verdicts. Compare `git patch-id` at each verdict SHA against the new head.

### Boot recipe, for every live lane

Each live lane runs on its own cloud VM at the PR head. Drive the CLI and TUI through a real TTY. `control-cli` is not in this repo. Use `./impartus` and `./impartus tui` in `xfce4-terminal`.

- [ ] `git fetch origin <head-branch> && git checkout <head SHA>`.
- [ ] `make build`. Wait for adjacent `impartus` and `impartus-ui`. Install mpv for TUI and play lanes.
- [ ] Export `IMPARTUS_*` from the environment. Set `XDG_STATE_HOME` and `IMPARTUS_TOKEN_CACHE` under `/tmp`. Never store the token under `/opt/cursor/artifacts`.
- [ ] Deliver input only through the TTY. Capture stderr to a log. Screenshot the terminal with `ffmpeg -f x11grab`.
- [ ] Save every screenshot to `/tmp/swarm-<pr-id>/worker-<n>/<slug>.png` and return the paths with the report.

## Refuse a world-readable token cache (PR-token-cache)

**Depends on.** None.

**Files.**

- [ ] Edit `internal/client/token_cache.go`.
- [ ] Edit `internal/client/token_cache_unix.go`.
- [ ] Edit `internal/client/token_cache_test.go` or the unix test file that covers publish.

**Build.**

- [ ] Validate mode before `replaceTokenCacheFile`. Delete the destination on failure. Never leave a 0666 token at the published path.

**You see.**

- [ ] A chmod-noop filesystem rejects login without a leftover cache file. stderr names the mode failure. POSIX `/tmp` still publishes 0600.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] `internal/client` gains a chmod-noop publish test. Run `go test ./internal/client`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Point `IMPARTUS_TOKEN_CACHE` at `/tmp` and run `courses --json`. Save `token-posix-ok.png`. Pass when the cache mode is 0600.
- [ ] Lane 2. Point the cache at a chmod-noop dir and run `courses --json`. Save `token-noop-fail.png`. Pass when the command fails and the dest file is absent.
- [ ] Lane 3. Reuse a good `/tmp` cache for a second `courses --json`. Save `token-reuse.png`. Pass when no second password prompt and mode stays 0600.
- [ ] Lane 4. Run `doctor --json` after a good publish. Save `token-doctor.png`. Pass when the token check is pass.
- [ ] Lane 5. Corrupt the dest to 0644 on POSIX then run a command. Save `token-reject-0644.png`. Pass when validation fails and login does not reuse it.
- [ ] Lane 6. Windows skip. On Unix run `library list` after a failed noop publish. Save `token-library-after-fail.png`. Pass when no cache file appears under the noop path.
- [ ] Lane 7. Run `version --json` with a noop cache path. Save `token-version.png`. Pass when version still works because it does not login.
- [ ] Lane 8. Run `download --help` with a noop cache path. Save `token-help.png`. Pass when help still works because it does not login.
- [ ] Lane 9. After a POSIX success, `stat` the file and screenshot. Save `token-stat.png`. Pass when the screenshot shows 0600.
- [ ] Lane 10. Confirm `/opt/cursor/artifacts` is not used as the cache. Save `token-no-artifacts.png`. Pass when the env points at `/tmp`.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. World-readable token files created.
- [ ] Probe. Publish once on POSIX `/tmp` and once on a chmod-noop dir. Count dest files with mode other than 0600.
- [ ] Baseline. Record the trunk count of leftover 0666 files under the artifacts harness (was 1 in run 1). first.
- [ ] Rule. Head must be 0 leftover 0666 files. Fail if any published token is not 0600 on POSIX.

**Review gate.** None. PR-token-cache is not review-gated.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends the PR to the Graphite stack and the operator lands it.

## Reject a non-positive download start (PR-cli-start)

**Depends on.** PR-token-cache.

**Files.**

- [ ] Edit `internal/cli/cli_download_flags.go`.
- [ ] Edit `internal/cli/cli_download_test.go`.
- [ ] Edit `internal/cli/cli_lecture_flags_test.go` only if the default 0,0 case must stay omitted-flags.

**Build.**

- [ ] When `startSet` or `endSet` is true, require the value `>= 1` inside `parseDownloadFlags`. Do not change `SelectRange` 0-as-default for omitted flags.

**You see.**

- [ ] `./impartus download --start 0 --json` exits non-zero before `initClient`. stderr mentions a positive 1-based index. No `fetchvideo`.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] `TestParseDownloadFlags` rejects `--start 0` and `--end 0`. Run `go test ./internal/cli`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Run `download --subject 1 --session 1 --start 0 --json`. Save `start-start0.png`. Pass when exit is non-zero and there is no fetchvideo.
- [ ] Lane 2. Run `download --end 0 --json` with dummy ids. Save `start-end0.png`. Pass when the same reject.
- [ ] Lane 3. Run `download --start -1 --json`. Save `start-neg.png`. Pass when the same reject.
- [ ] Lane 4. Run `download --help`. Save `start-help.png`. Pass when help still mentions --start as 1-based.
- [ ] Lane 5. Omit --start and --end on a tiny dry path if one exists, else `download --help` again. Save `start-omit.png`. Pass when omitted flags still mean the default range in code comments or help.
- [ ] Lane 6. Run `play --start 0` and compare. Save `start-play.png`. Pass when play remains consistent or is documented if still 0-as-all.
- [ ] Lane 7. Run `download --ttid 0 --json`. Save `start-ttid.png`. Pass when ttid already rejects, still rejects.
- [ ] Lane 8. Run `download --start 1 --end 1 --json` with fake ids. Save `start-one.png`. Pass when failure is missing course, not the start flag.
- [ ] Lane 9. Screenshot the start 0 stderr. Save `start-stderr.png`. Pass when the message names 1-based.
- [ ] Lane 10. Confirm no PID was left fetching. Save `start-noproc.png`. Pass when no impartus download process remains.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. Time to fail `--start 0`.
- [ ] Probe. Time `./impartus download --subject 1 --session 1 --start 0 --json` at trunk and at head.
- [ ] Baseline. Record the trunk trunk time that includes login or fetchvideo (seconds). first.
- [ ] Rule. Head must fail in well under the trunk time and without a network fetch. Fail if head calls fetchvideo.

**Review gate.** The operator reviews before merge.

- [ ] Copy lane 1 screenshots into `/tmp/swarm-PR-cli-start/review-start.png`.
- [ ] Record a 30 to 60 second video of the change on a lane VM. Save it as `/tmp/swarm-PR-cli-start/review.mp4`.
- [ ] Post the screenshots and the video in chat. Stop at merge-ready. Wait for the operator's click.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends the PR to the Graphite stack and the operator lands it.

## Validate quality before login (PR-cli-quality)

**Depends on.** PR-cli-start.

**Files.**

- [ ] Edit `internal/cli/cli_download.go`.
- [ ] Edit `internal/cli/cli_play.go`.
- [ ] Edit `internal/cli/cli_lecture_flags_test.go`.

**Build.**

- [ ] Call `applyAndValidateFlags` before `initClient` in download and play.

**You see.**

- [ ] `./impartus download --quality 1080 --json` fails with the invalid quality message and does not log in.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Add an Execute-level test that bad quality never calls the injected `initClient`. Run `go test ./internal/cli`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Run `download --quality 1080 --json`. Save `quality-dl.png`. Pass when the error is invalid quality.
- [ ] Lane 2. Run `play --quality 1080`. Save `quality-play.png`. Pass when the same reject.
- [ ] Lane 3. Run `download --quality 720 --help`. Save `quality-help.png`. Pass when help still wins.
- [ ] Lane 4. Run `download --quality 144 --json` with fake ids. Save `quality-okq.png`. Pass when failure is missing course, not quality.
- [ ] Lane 5. Run `download --quality banana --json`. Save `quality-banana.png`. Pass when invalid quality.
- [ ] Lane 6. Screenshot download 1080 stderr. Save `quality-shot.png`. Pass when the message lists 144, 450, 720.
- [ ] Lane 7. Run `play --quality 450 --help`. Save `quality-playhelp.png`. Pass when help is printed.
- [ ] Lane 8. Run `download --views weird --json`. Save `quality-views.png`. Pass when views still validate without login if that flag is local.
- [ ] Lane 9. Confirm token cache mtime does not jump on 1080. Save `quality-mtime.png`. Pass when no new token write.
- [ ] Lane 10. Run `version`. Save `quality-ver.png`. Pass when version still works.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. Login attempts on invalid quality.
- [ ] Probe. Run bad quality once. Count `NewLoggedIn` or token-cache writes.
- [ ] Baseline. Record the trunk trunk performs login (at least one token touch). first.
- [ ] Rule. Head must be 0 logins. Fail if the token cache is rewritten.

**Review gate.** None. PR-cli-quality is not review-gated.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends the PR to the Graphite stack and the operator lands it.

## Stop stripping --json after the sentinel (PR-cli-json)

**Depends on.** PR-cli-quality.

**Files.**

- [ ] Edit `internal/cli/cli_json.go`.
- [ ] Edit the test that covers `TestStripGlobalJSONFlag`.

**Build.**

- [ ] Make `stripGlobalJSONFlag` stop at `--`, matching `hasHelpBeforeSentinel`.

**You see.**

- [ ] `./impartus download --subject 1 --session 2 -- --json` is not JSON mode. `--json` before `--` still is.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] `TestStripGlobalJSONFlag` gains `courses -- --json`. Run `go test ./internal/cli`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Run `courses --json`. Save `json-still.png`. Pass when JSON envelope remains.
- [ ] Lane 2. Run `courses -- --json`. Save `json-after.png`. Pass when not JSON mode.
- [ ] Lane 3. Run `help --json`. Save `json-helpjson.png`. Pass when JSON help remains.
- [ ] Lane 4. Run `download --json --help`. Save `json-order.png`. Pass when help still wins.
- [ ] Lane 5. Run `download -- --help`. Save `json-helppos.png`. Pass when help is positional, not command help, matching today.
- [ ] Lane 6. Run `version --json`. Save `json-ver.png`. Pass when JSON version remains.
- [ ] Lane 7. Run `download --subject 1 --session 2 --json -- --json`. Save `json-both.png`. Pass when JSON mode from the flag before the sentinel.
- [ ] Lane 8. Screenshot the after-sentinel human error. Save `json-shot.png`. Pass when stderr is human, not an envelope.
- [ ] Lane 9. Run `library list --json`. Save `json-lib.png`. Pass when JSON library remains.
- [ ] Lane 10. Run `library list -- --json`. Save `json-libafter.png`. Pass when not JSON mode.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. False JSON mode on sentinel args.
- [ ] Probe. Run `download --subject 1 --session 2 -- --json` and inspect `meta.mode`.
- [ ] Baseline. Record the trunk trunk `mode` is json. first.
- [ ] Rule. Head `mode` must not be json. Fail if the envelope is present.

**Review gate.** None. PR-cli-json is not review-gated.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends the PR to the Graphite stack and the operator lands it.

## Make command help list flags (PR-cli-help)

**Depends on.** PR-cli-json.

**Files.**

- [ ] Edit `internal/cli/cli.go`.
- [ ] Edit `internal/cli/cli_help.go`.
- [ ] Edit `internal/cli/cli_helpers.go`.
- [ ] Edit `internal/cli/cli_json.go` only for command help payload flags.
- [ ] Edit `internal/cli/cli_help_test.go`.

**Build.**

- [ ] Dispatch `help <command>` to `showCommandHelp`. Print the flag list already stored in `showHelpTo`. JSON command help includes flags.

**You see.**

- [ ] `./impartus help download` matches `download --help` and names `--start` and `--quality`. Root `--help` lists `--json` as its own flag.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] `TestExecuteHumanCommandHelpMatrix` gains `help download`. Run `go test ./internal/cli`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Run `help download`. Save `help-help-dl.png`. Pass when output is download help, not root.
- [ ] Lane 2. Run `download --help`. Save `help-flag-dl.png`. Pass when flags are listed.
- [ ] Lane 3. Run `help download --json`. Save `help-help-json.png`. Pass when JSON command help, not root capabilities only.
- [ ] Lane 4. Run `help play`. Save `help-help-play.png`. Pass when play help.
- [ ] Lane 5. Run `help`. Save `help-root.png`. Pass when root help remains.
- [ ] Lane 6. Run `--help`. Save `help-rootflag.png`. Pass when root lists --json.
- [ ] Lane 7. Run `help vrfy`. Save `help-unknown.png`. Pass when unknown command, not silent root success if that is the chosen contract.
- [ ] Lane 8. Run `library --help`. Save `help-lib.png`. Pass when library help.
- [ ] Lane 9. Screenshot `download --help`. Save `help-shot.png`. Pass when the shot shows --subject and --start.
- [ ] Lane 10. Run `help tui`. Save `help-tui.png`. Pass when tui help.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. Help bytes that mention --start.
- [ ] Probe. Count `--start` in `download --help` and `help download` stdout.
- [ ] Baseline. Record the trunk trunk count is 0 on `download --help` flag list (usage line only). first.
- [ ] Rule. Head count must be >= 1 in both commands. Fail if either still prints only [flags].

**Review gate.** The operator reviews before merge.

- [ ] Copy lane 1 screenshots into `/tmp/swarm-PR-cli-help/review-help.png`.
- [ ] Record a 30 to 60 second video of the change on a lane VM. Save it as `/tmp/swarm-PR-cli-help/review.mp4`.
- [ ] Post the screenshots and the video in chat. Stop at merge-ready. Wait for the operator's click.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends the PR to the Graphite stack and the operator lands it.

## Validate library argv before opening the store (PR-library-argv)

**Depends on.** PR-cli-help.

**Files.**

- [ ] Edit `internal/cli/cli_library.go`.
- [ ] Edit `internal/cli/cli_help_test.go` or the library execute tests.

**Build.**

- [ ] Switch on `args[0]` before `library.Open`. Unknown names must not mkdir.

**You see.**

- [ ] `XDG_STATE_HOME` stays empty after `library vrfy`. The error is unknown command.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Add a test that a fresh state dir has no `library.db` after `library vrfy`. Run `go test ./internal/cli`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Fresh state dir, run `library vrfy`. Save `library-typo.png`. Pass when no library.db.
- [ ] Lane 2. Run `library list` on empty. Save `library-list.png`. Pass when empty list is allowed and may create the store.
- [ ] Lane 3. Run `library --help`. Save `library-help.png`. Pass when no store from help.
- [ ] Lane 4. Run `library verify --help`. Save `library-vhelp.png`. Pass when no store.
- [ ] Lane 5. Run `library show` with no id. Save `library-show.png`. Pass when arity error. Store exists only if Open still runs for valid verbs.
- [ ] Lane 6. Run `library list extra`. Save `library-extra.png`. Pass when error. Store policy matches the chosen contract.
- [ ] Lane 7. Screenshot the vrfy stderr. Save `library-shot.png`. Pass when unknown command.
- [ ] Lane 8. Run `library verify` on empty. Save `library-verify.png`. Pass when verify of empty is ok.
- [ ] Lane 9. Repeat vrfy with JSON. Save `library-json.png`. Pass when JSON error, no db from the typo path.
- [ ] Lane 10. Confirm `library` with no args still errors first. Save `library-empty.png`. Pass when no db if argv empty.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. Store files created by a typo.
- [ ] Probe. Run `library vrfy` in a fresh `XDG_STATE_HOME` and count `library.db`.
- [ ] Baseline. Record the trunk trunk count is 1. first.
- [ ] Rule. Head count must be 0. Fail if the db exists.

**Review gate.** None. PR-library-argv is not review-gated.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends the PR to the Graphite stack and the operator lands it.

## Stop counting a budgeted lecture as new and skipped (PR-watch-skip)

**Depends on.** PR-library-argv.

**Files.**

- [ ] Edit `internal/watch/watch.go`.
- [ ] Edit `internal/watch/watch_budget_test.go`.

**Build.**

- [ ] When `!withinBudget`, count Skipped and not New. Update `TestWatcherDryRunAppliesGlobalBudgetBeforePlaylistFetch`.

**You see.**

- [ ] Dry-run JSON with budget 3 and 6 new lectures reports new=3 and skipped=3, or an explicit field, never new=6 and skipped=3.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Change the budget test so New+Skipped does not double-count. Run `go test ./internal/watch`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Run a unit dry-run with 2 new and budget 1. Save `watch-unit.png`. Pass when New is 1 and Skipped is 1.
- [ ] Lane 2. Repeat with budget 3 and 3 new. Save `watch-eq.png`. Pass when Skipped is 0 for budget.
- [ ] Lane 3. Repeat with 0 new. Save `watch-none.png`. Pass when both 0 for budget tail.
- [ ] Lane 4. CLI `watch --dry-run --json` if credentials exist. Save `watch-cli.png`. Pass when new + skipped does not exceed listed.
- [ ] Lane 5. Screenshot the JSON cycle object. Save `watch-shot.png`. Pass when the numbers add up.
- [ ] Lane 6. Committed lectures stay skipped without also being new. Save `watch-commit.png`. Pass when already-downloaded is skipped only.
- [ ] Lane 7. Failed playlist still consumes budget as today, without a second New. Save `watch-fail.png`. Pass when the existing fail test is updated if needed.
- [ ] Lane 8. Default budget remains 3. Save `watch-default.png`. Pass when config default is 3.
- [ ] Lane 9. Human dry-run text still runs. Save `watch-human.png`. Pass when no panic.
- [ ] Lane 10. JSON envelope still wraps cycle. Save `watch-env.png`. Pass when meta.command is watch.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. new + skipped versus listed.
- [ ] Probe. On a 6-new dry-run with budget 3, compute new+skipped-listed.
- [ ] Baseline. Record the trunk trunk extra count is 3 (6+3-6). first.
- [ ] Rule. Head extra count must be 0. Fail if new+skipped > listed + already-committed.

**Review gate.** None. PR-watch-skip is not review-gated.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends the PR to the Graphite stack and the operator lands it.

## Keep the TUI event stream alive and print the real error (PR-tui-events)

**Depends on.** PR-watch-skip.

**Files.**

- [ ] Edit `internal/tuisession/events.go`.
- [ ] Edit `ui/src/client.ts`.
- [ ] Edit `ui/src/main.ts`.
- [ ] Edit `internal/tuisession/session_test.go`.
- [ ] Edit `ui/test/client.test.ts`.

**Build.**

- [ ] Write SSE comments on an idle interval. Do not time out the `/events` fetch. Print `inspect(error)` from `main().catch`.

**You see.**

- [ ] A TUI left on the catalog for six minutes still says Connected. A forced fetch failure prints the real error on stderr.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] `go test ./internal/tuisession` covers idle comments. `cd ui && bun test` still parses events.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Launch TUI, wait 6 minutes on the catalog. Save `events-idle.png`. Pass when the process is still up.
- [ ] Lane 2. Screenshot Connected after the wait. Save `events-shot.png`. Pass when status is Connected.
- [ ] Lane 3. Start self-test after the wait. Save `events-st.png`. Pass when self-test completes.
- [ ] Lane 4. Kill comments on a debug build if needed, else SIGTERM the child fetch by not doing that in prod. Save `events-err.png`. Pass when stderr from a failed events() names TimeoutError or the new inspect text in a unit harness.
- [ ] Lane 5. Open help after 6 minutes. Save `events-help.png`. Pass when help still opens.
- [ ] Lane 6. Quit with q. Save `events-quit.png`. Pass when exit 0.
- [ ] Lane 7. Relaunch and press s immediately. Save `events-fast.png`. Pass when self-test 100%.
- [ ] Lane 8. Confirm doctor still passes mpv. Save `events-doc.png`. Pass when mpv pass.
- [ ] Lane 9. Play a lecture for 30s after idle wait. Save `events-play.png`. Pass when mpv opens.
- [ ] Lane 10. Confirm no `terminal frontend failed during interactive renderer` in the idle log. Save `events-nocrash.png`. Pass when the phrase is absent.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. TUI survival over 6 idle minutes.
- [ ] Probe. Launch TUI, sleep 360s, `pgrep impartus-ui`.
- [ ] Baseline. Record the trunk trunk pgrep is empty (process died). first.
- [ ] Rule. Head pgrep must find impartus-ui. Fail if exit 1.

**Review gate.** The operator reviews before merge.

- [ ] Copy lane 1 screenshots into `/tmp/swarm-PR-tui-events/review-events.png`.
- [ ] Record a 30 to 60 second video of the change on a lane VM. Save it as `/tmp/swarm-PR-tui-events/review.mp4`.
- [ ] Post the screenshots and the video in chat. Stop at merge-ready. Wait for the operator's click.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends the PR to the Graphite stack and the operator lands it.

## Publish download progress into the TUI dock (PR-tui-download-progress)

**Depends on.** PR-tui-events.

**Files.**

- [ ] Edit `internal/tuisession/operations.go`.
- [ ] Edit `internal/tuisession/session_test.go`.
- [ ] Edit downloader progress wiring only if `DownloadAndJoin` must gain a callback.

**Build.**

- [ ] Publish `EventTypeOperationProgress` with `Percent` from the existing `ProgressTracker` while `runDownload` runs. Update the test that wanted started then completed only.

**You see.**

- [ ] The dock percent increases while `.ts` chunks appear. It is not stuck at 0 after 30s of fetch.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] `TestDownloadOperationReResolvesLectureAndProducesOneTerminal` allows progress events. Run `go test ./internal/tuisession`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Press d on an audio lecture. Save `dlprog-start.png`. Pass when dock says download running.
- [ ] Lane 2. Wait 15s and screenshot. Save `dlprog-pct.png`. Pass when percent is > 0 or the status text names bytes.
- [ ] Lane 3. Confirm temp dir grew. Save `dlprog-temp.png`. Pass when du is > 0.
- [ ] Lane 4. Open library while running. Save `dlprog-lib.png`. Pass when empty or in-progress, no crash.
- [ ] Lane 5. Open diagnostics while running. Save `dlprog-diag.png`. Pass when mpv still pass.
- [ ] Lane 6. Let it run 30s more. Save `dlprog-more.png`. Pass when percent did not reset to 0 without cause.
- [ ] Lane 7. Cancel is out of scope here. Leave running then q. Save `dlprog-quit.png`. Pass when quit still works.
- [ ] Lane 8. Self-test percent still reaches 100. Save `dlprog-st.png`. Pass when self-test unchanged.
- [ ] Lane 9. Playback percent still moves. Save `dlprog-play.png`. Pass when playback telemetry unchanged.
- [ ] Lane 10. Screenshot the dock. Save `dlprog-shot.png`. Pass when the shot is not 0% after chunks exist.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. Dock percent after 30s of download.
- [ ] Probe. Start d, wait 30s, read percent from a screenshot or event log.
- [ ] Baseline. Record the trunk trunk percent is 0. first.
- [ ] Rule. Head percent must be > 0 when temp dir grew. Fail if still 0 with chunks present.

**Review gate.** The operator reviews before merge.

- [ ] Copy lane 1 screenshots into `/tmp/swarm-PR-tui-download-progress/review-dlprog.png`.
- [ ] Record a 30 to 60 second video of the change on a lane VM. Save it as `/tmp/swarm-PR-tui-download-progress/review.mp4`.
- [ ] Post the screenshots and the video in chat. Stop at merge-ready. Wait for the operator's click.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends the PR to the Graphite stack and the operator lands it.

## Cancel a TUI download with Escape (PR-tui-download-cancel)

**Depends on.** PR-tui-download-progress.

**Files.**

- [ ] Edit `ui/src/main.ts`.
- [ ] Add a ui test next to workspace operations if one can drive `goBack`.

**Build.**

- [ ] If a running operation is `download`, Escape calls `cancelOperation` before changing screen. Keep playback cancel as it is.

**You see.**

- [ ] Escape on the lecture list stops the download. Dock says canceled. Temp dir stops growing.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] `cd ui && bun test`. Add a controller or view test that back on lectures with a running download requests cancel.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Start d, then Escape. Save `dlcancel-esc.png`. Pass when state is canceled.
- [ ] Lane 2. Confirm no new .ts after 5s. Save `dlcancel-stop.png`. Pass when mtime of temp dir is idle.
- [ ] Lane 3. library.db job status is canceled. Save `dlcancel-job.png`. Pass when status canceled.
- [ ] Lane 4. Playback Escape still closes mpv. Save `dlcancel-play.png`. Pass when mpv exits.
- [ ] Lane 5. Escape on courses without an operation still goes nowhere harmful. Save `dlcancel-noop.png`. Pass when catalog remains.
- [ ] Lane 6. Start d, go to library, Escape. Save `dlcancel-lib.png`. Pass when policy is documented and cancel still happens or the dock still offers it.
- [ ] Lane 7. q during download still shuts down. Save `dlcancel-q.png`. Pass when exit 0 or canceled.
- [ ] Lane 8. Screenshot canceled dock. Save `dlcancel-shot.png`. Pass when the word canceled is visible.
- [ ] Lane 9. Start d twice after cancel. Save `dlcancel-again.png`. Pass when a second download can start.
- [ ] Lane 10. s after cancel runs self-test. Save `dlcancel-st.png`. Pass when self-test is not stuck.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. Chunks written after Escape.
- [ ] Probe. Start d, wait 5s, Escape, wait 10s, count new .ts.
- [ ] Baseline. Record the trunk trunk keeps writing after Escape. first.
- [ ] Rule. Head new .ts after Escape must be 0. Fail if the job stays running.

**Review gate.** The operator reviews before merge.

- [ ] Copy lane 1 screenshots into `/tmp/swarm-PR-tui-download-cancel/review-dlcancel.png`.
- [ ] Record a 30 to 60 second video of the change on a lane VM. Save it as `/tmp/swarm-PR-tui-download-cancel/review.mp4`.
- [ ] Post the screenshots and the video in chat. Stop at merge-ready. Wait for the operator's click.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends the PR to the Graphite stack and the operator lands it.

## Show hidden keys and blocked-command reasons (PR-tui-chrome)

**Depends on.** PR-tui-download-cancel.

**Files.**

- [ ] Edit `ui/src/view.ts`.
- [ ] Edit `ui/src/workspace_commands.ts`.
- [ ] Edit `ui/test/view.test.ts`.
- [ ] Edit `ui/test/workspace_commands.test.ts`.

**Build.**

- [ ] Stop slicing help to height-5 without a pager. Footer can keep `keys[0]` if help lists every key. Put `operationAvailability.reason` on the dock when a key is ignored.

**You see.**

- [ ] `?` shows q, l, !, d. A running download plus `s` writes 'An operation is already running' into the dock.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] `bun test` in `ui/` covers help listing download and quit, and a running-operation reason.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Press ? on catalog. Save `chrome-help.png`. Pass when q and d are visible or a pager exists.
- [ ] Lane 2. Screenshot help. Save `chrome-shot.png`. Pass when quit is listed.
- [ ] Lane 3. Start d, press s. Save `chrome-s.png`. Pass when dock names the running operation.
- [ ] Lane 4. Footer still shows a move key. Save `chrome-foot.png`. Pass when up or navigate is present.
- [ ] Lane 5. Palette still opens. Save `chrome-pal.png`. Pass when ctrl+p works.
- [ ] Lane 6. ! diagnostics still opens. Save `chrome-diag.png`. Pass when mpv row exists.
- [ ] Lane 7. l library still opens. Save `chrome-lib.png`. Pass when empty state exists.
- [ ] Lane 8. Escape closes help. Save `chrome-esc.png`. Pass when overlay gone.
- [ ] Lane 9. Wide and compact widths both open help. Save `chrome-wide.png`. Pass when no crash.
- [ ] Lane 10. After cancel, s runs. Save `chrome-after.png`. Pass when self-test 100%.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. Help commands visible versus COMMANDS length.
- [ ] Probe. Count help rows versus commandsForHelp length at 148x44.
- [ ] Baseline. Record the trunk trunk visible count is less than COMMANDS (truncated). first.
- [ ] Rule. Head visible count must be all commands or a pager that can reach q. Fail if q is missing with no pager.

**Review gate.** The operator reviews before merge.

- [ ] Copy lane 1 screenshots into `/tmp/swarm-PR-tui-chrome/review-chrome.png`.
- [ ] Record a 30 to 60 second video of the change on a lane VM. Save it as `/tmp/swarm-PR-tui-chrome/review.mp4`.
- [ ] Post the screenshots and the video in chat. Stop at merge-ready. Wait for the operator's click.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends the PR to the Graphite stack and the operator lands it.

## Restore collection focus after wide g (PR-tui-nav-focus)

**Depends on.** PR-tui-chrome.

**Files.**

- [ ] Edit `ui/src/view.ts`.
- [ ] Edit `ui/test/view.test.ts`.

**Build.**

- [ ] Wide `g` records previous focus. Escape restores collection focus without calling course `goBack`. Reuse overlay `previousFocus`.

**You see.**

- [ ] At 148x44, g then Escape returns `[ACTIVE]` to the course list. `/` is in the footer again.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] `ui/test/view.test.ts` wide g plus Escape restores collection. Run `cd ui && bun test`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Resize to 148x44, press g. Save `nav-g.png`. Pass when Navigation is focused.
- [ ] Lane 2. Press Escape. Save `nav-esc.png`. Pass when collection is ACTIVE.
- [ ] Lane 3. Press /. Save `nav-slash.png`. Pass when filter editor opens.
- [ ] Lane 4. Tab from collection. Save `nav-tab.png`. Pass when inspector becomes ACTIVE.
- [ ] Lane 5. Shift+Tab back. Save `nav-stab.png`. Pass when collection is ACTIVE.
- [ ] Lane 6. g, down, enter library. Save `nav-lib.png`. Pass when library opens.
- [ ] Lane 7. g, enter diagnostics. Save `nav-diag.png`. Pass when diagnostics opens.
- [ ] Lane 8. Compact width uses overlay Escape as today. Save `nav-compact.png`. Pass when overlay closes.
- [ ] Lane 9. Screenshot after Escape. Save `nav-shot.png`. Pass when ACTIVE is Learning workspace.
- [ ] Lane 10. Enter still opens lectures from collection. Save `nav-enter.png`. Pass when lecture list opens.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. Focus after g then Escape.
- [ ] Probe. Drive g, Escape, read the ACTIVE label.
- [ ] Baseline. Record the trunk trunk ACTIVE stays Navigation. first.
- [ ] Rule. Head ACTIVE must be the collection. Fail if Navigation stays focused.

**Review gate.** The operator reviews before merge.

- [ ] Copy lane 1 screenshots into `/tmp/swarm-PR-tui-nav-focus/review-nav.png`.
- [ ] Record a 30 to 60 second video of the change on a lane VM. Save it as `/tmp/swarm-PR-tui-nav-focus/review.mp4`.
- [ ] Post the screenshots and the video in chat. Stop at merge-ready. Wait for the operator's click.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends the PR to the Graphite stack and the operator lands it.

## Stop inserting slash while the filter is open (PR-tui-filter-slash)

**Depends on.** PR-tui-nav-focus.

**Files.**

- [ ] Edit `ui/src/view.ts`.
- [ ] Edit `ui/test/view.test.ts`.

**Build.**

- [ ] `#handleFilterKey` ignores `/` as printable input. Keep backspace, enter, and escape.

**You see.**

- [ ] `/` then `med` matches Medical Devices. The query is `med`, not `/med`.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Add a second-`/` case to `ui/test/view.test.ts`. Run `cd ui && bun test`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Press / then med. Save `filter-med.png`. Pass when matching courses exist.
- [ ] Lane 2. Press / / med. Save `filter-dbl.png`. Pass when query is still med, not /med.
- [ ] Lane 3. Escape keeps the filter. Save `filter-keep.png`. Pass when footer shows Filter med.
- [ ] Lane 4. / then backspace all then enter. Save `filter-clear.png`. Pass when full catalog returns.
- [ ] Lane 5. Filter on diagnostics. Save `filter-diag.png`. Pass when mpv still matches m.
- [ ] Lane 6. Filter on library empty. Save `filter-lib.png`. Pass when no crash.
- [ ] Lane 7. Screenshot /med absence. Save `filter-shot.png`. Pass when no leading slash.
- [ ] Lane 8. Unicode grapheme filter still truncates at 120. Save `filter-graph.png`. Pass when existing test still passes.
- [ ] Lane 9. Enter applies. Save `filter-enter.png`. Pass when editor closes.
- [ ] Lane 10. No TimeoutError from this key. Save `filter-alive.png`. Pass when TUI still up.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. Leading slash in the applied filter after / / med.
- [ ] Probe. Type / / med, Escape, read the footer filter string.
- [ ] Baseline. Record the trunk trunk string is /med. first.
- [ ] Rule. Head string must be med. Fail if it starts with /.

**Review gate.** The operator reviews before merge.

- [ ] Copy lane 1 screenshots into `/tmp/swarm-PR-tui-filter-slash/review-filter.png`.
- [ ] Record a 30 to 60 second video of the change on a lane VM. Save it as `/tmp/swarm-PR-tui-filter-slash/review.mp4`.
- [ ] Post the screenshots and the video in chat. Stop at merge-ready. Wait for the operator's click.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends the PR to the Graphite stack and the operator lands it.

## Close the program

- [ ] Every box above is checked with its evidence.
- [ ] Reply to the operator with the report the execution playbook names.

## Appendix A. Prototype evidence

No new prototype branch. Both dogfood runs already ran the real binary.

Run 1 (CLI, no mpv). `.audit/cli-dogfood.tsv`, `/opt/cursor/artifacts/cli-dogfood/dogfood_cli.py`, findings under `/opt/cursor/artifacts/cli-dogfood`. SHA of the built binary is local to that VM.

Run 2 (TUI+mpv). Agent [Pstack cli dogfooding](https://cursor.com/agents/bc-01a03c93-2ac2-7097-89bb-3bb80797d748). Timeout log `/opt/cursor/artifacts/tui_timeout_crash.log`. Videos `/opt/cursor/artifacts/tui_lecture_playback_mpv_controls.mp4` and `/opt/cursor/artifacts/cli_play_mpv_lecture_video.mp4`.

Unproven. Exact Bun fetch timeout milliseconds. Heartbeat interval. Whether `SelectRange` should change (it must not). Whether watch JSON needs a third field.

## Appendix B. Alternatives rejected

Change `SelectRange` 0-as-default. Rejected because watch and omitted flags use it.

Fold download progress into the events PR. Rejected because the session test that forbids progress events would couple two contracts.

Fold Escape-cancel into wide g. Rejected because one is session policy and one is pane focus.

Autopilot-full. Rejected because the operator asked for a stacked plan she lands.

## Appendix C. Risks

This repo does not vendor pstack. `git show origin/main:pstack/skills/...` will fail until the plugin path is used. Owners read the plugin copies.

`control-cli` is not in this repo. Live lanes use `xfce4-terminal` and `ffmpeg` x11grab.

Do not re-run `--start 0` on a live course until PR-cli-start lands.

Do not point `IMPARTUS_TOKEN_CACHE` at `/opt/cursor/artifacts`.

TUI live lanes need mpv, bun, and a TTY stdout. Redirecting stdout makes `tui` exit 2.

## Appendix D. Links and reading list

Issues [#189](https://github.com/rabesss/impartus-cli/issues/189) through [#200](https://github.com/rabesss/impartus-cli/issues/200). Bodies also live in `/cursor/stores/self/docs/issues/`.

`pstack/skills/how/SKILL.md` on PR-tui-events and PR-cli-start.
`pstack/skills/interrogate/SKILL.md` on PR-tui-events.
Trail `.audit/cli-dogfood.tsv` plus this program's local `decisions.tsv`.
