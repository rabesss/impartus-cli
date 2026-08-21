# Coverage matrix — impartus-cli dogfood 2026-08-21

Status values: **covered** (readable screenshot exists), **blocked** (not reachable without live Impartus credentials or would persist private lecture metadata), **not applicable**.

Synthetic OpenTUI stills are fixture renders (`createTestRenderer` + `FoundationView`). They are marked **SYNTHETIC** in the PNG banner and are **not** a live logged-in `impartus tui` session. Live `impartus tui` still exits on missing/wrong credentials (ISSUE-002).

| ID | Requested surface / state | Status | Evidence | Notes |
|----|---------------------------|--------|----------|-------|
| 1a | Root help | covered | `screenshots/cli_root_help.png`, `screenshots/issue-003-help-wording.png` | `impartus help` |
| 1b | Version | covered | `screenshots/cli_version.png`, `screenshots/issue-004-empty-build-date.png` | Build Date empty after `make build` |
| 1c | Command discoverability (`--json` capability) | covered | `screenshots/cli_json_capability.png` | `impartus --json` |
| 1d | Invalid command feedback | covered | `screenshots/cli_invalid_command.png` | `unknown command: notacommand`, exit 1 |
| 1e | Non-TTY no-args | covered | `screenshots/cli_nontty_noargs.png` | Help + `interactive TUI requires a terminal`, exit 2 |
| 1f | Subcommand `--help` | covered | `screenshots/cli_download_help_flag.png`, `screenshots/cli_tui_help_flag.png`, `screenshots/cli_library_help_flag.png`, `screenshots/cli_version_help_flag.png`, `screenshots/cli_watch_help_flag.png`, `screenshots/cli_play_help_flag.png`, `screenshots/issue-001-download-help.png`, `screenshots/issue-001-tui-help.png`, `screenshots/issue-001-library-help.png`, `screenshots/issue-001-version-help.png`, `screenshots/issue-001-watch-help.png` | Filed as ISSUE-001 |
| 2a | Missing credentials (CLI) | covered | `screenshots/cli_courses_missing_credentials.png`, `screenshots/cli_download_missing_credentials.png`, `screenshots/cli_play_missing_credentials.png`, `screenshots/cli_serve_missing_credentials.png`, `screenshots/cli_courses_json_error_envelope.png` | `username and password are required` |
| 2b | Missing flags | covered | `screenshots/cli_lectures_missing_flags.png`, `screenshots/cli_watch_missing_targets.png`, `screenshots/cli_download_ttid_conflict.png` | |
| 2c | Published sample / wrong Impartus credentials | covered | `screenshots/cli_sample_wrong_credentials.png`, `screenshots/cli_download_json_wrong_credentials.png` | Placeholders from `sample.config.json` only |
| 2d | TUI auth failure | covered | `screenshots/tui_launch_missing_credentials.png`, `screenshots/tui_sample_wide_initial.png`, `screenshots/issue-002-tui-missing-creds.png`, `screenshots/issue-002-tui-wrong-creds.png`, `screenshots/issue-002-step-1-before.png`, `screenshots/issue-002-step-2-action.png`, `screenshots/issue-002-step-3-result-nocred.png`, `screenshots/issue-002-step-4-result-sample.png`, `videos/issue-002-tui-auth-repro.webm` | Live CLI exits before OpenTUI. Video + step stills. |
| 3 | OpenTUI course overview and navigation | covered | `screenshots/synthetic_opentui_courses_wide.png`, `screenshots/synthetic_opentui_courses_medium.png` | **Synthetic.** Fixtures: Distributed Systems, Compilers. Not a live session. |
| 4a | Lecture workspace, audio-reported row | covered | `screenshots/synthetic_opentui_lectures_audio.png` | **Synthetic.** Inspector shows `audio reported`. |
| 4b | Lecture workspace, probably-no-audio presentation | covered | `screenshots/synthetic_opentui_lectures_noaudio.png` | **Synthetic.** Selected `Visual-only lecture` / `probably no audio`. |
| 4c | Lecture inspector | covered | `screenshots/synthetic_opentui_lectures_audio.png`, `screenshots/synthetic_opentui_lectures_noaudio.png`, `screenshots/synthetic_opentui_download_progress.png` | **Synthetic.** Right-hand inspector pane. |
| 4d | Lecture selection | covered | `screenshots/synthetic_opentui_lectures_audio.png` | **Synthetic.** `> 001 Audio lecture` selected. |
| 4e | Lecture / course filtering | covered | `screenshots/synthetic_opentui_filter_courses.png`, `screenshots/synthetic_opentui_filter_lectures.png` | **Synthetic.** `Filter: compiler` / `Filter: visual` via mockInput `/`. |
| 5 | Playback surface and keyboard controls (paused / muted / speed / volume) | covered | `screenshots/synthetic_opentui_playback_running.png`, `screenshots/synthetic_opentui_playback_paused.png`, `screenshots/synthetic_opentui_playback_muted.png`, `screenshots/synthetic_opentui_playback_speed.png`, `screenshots/synthetic_opentui_playback_volume.png` | **Synthetic** playback operation state. No live mpv media. |
| 6a | Download error without real lecture media (CLI events) | covered | `screenshots/cli_download_events_auth_error.png`, `screenshots/cli_download_events_stdout_pure.png`, `screenshots/cli_download_events_stderr_only.png` | `job.started` then `job.failed`; downloads directory empty. Split streams: stdout is pure NDJSON; human sentence is stderr only. Merged TTY still is **not** evidence of stdout corruption. Original ISSUE-007 withdrawn. |
| 6b | Download error without real lecture media (HTTP job) | covered | `screenshots/api_jobs_failed_download.png`, `screenshots/api_job_detail_upstream_error.png`, `screenshots/issue-006-api-job-generic-error.png`, `screenshots/issue-006-step-1-before.png`, `screenshots/issue-006-step-2-action.png`, `screenshots/issue-006-step-3-result.png`, `videos/issue-006-api-job-error-repro.webm` | Job `failed`, progress 0, no media files. Filed as ISSUE-006. |
| 6c | Download in-progress bar / ETA | covered | `screenshots/synthetic_opentui_download_progress.png` | **Synthetic TUI inspector** `Lecture download 42%`. CLI mpb/ETA was not live-captured (would need a real lecture fetch). |
| 7a | Local library empty | covered | `screenshots/cli_library_empty.png` | CLI: `Library is empty.` |
| 7b | Local library show-missing | covered | `screenshots/cli_library_show_missing.png` | `library artifact not found` |
| 7c | Local library verify empty | covered | `screenshots/cli_library_verify_empty.png`, `screenshots/issue-005-library-verify-silent.png` | Silent exit 0 |
| 7d | Local library populated | blocked | — | CLI `library list` populated needs a completed download or a committed library store. Neither exists in this public dogfood. OpenTUI populated library is 7e. |
| 7e | OpenTUI library pane | covered | `screenshots/synthetic_opentui_library_empty.png`, `screenshots/synthetic_opentui_library_populated.png` | **Synthetic.** Empty: `No downloaded lectures yet`. Populated: `004 Consensus`, 2/2 files. |
| 8a | Diagnostics `doctor` (human) | covered | `screenshots/cli_doctor_missing_config.png`, `screenshots/cli_sample_doctor.png`, `screenshots/synthetic_opentui_diagnostics.png` | CLI WARN config/token when absent; PASS mpv/ffmpeg/library. OpenTUI diagnostics pane is **synthetic** (`[PASS] mpv` / `[WARN] config`). |
| 8b | Diagnostics `doctor --json` | covered | `screenshots/cli_doctor_json.png` | Envelope `success: true`, per-check warn/pass |
| 8c | HTTP health live / ready / alias | covered | `screenshots/api_health_live.png`, `screenshots/api_health_ready.png`, `screenshots/api_health_ready_pretty.png`, `screenshots/api_health_alias.png` | agent-browser |
| 8d | OpenTUI command guide (`?`) | covered | `screenshots/synthetic_opentui_command_guide_wide.png`, `screenshots/synthetic_opentui_command_guide_narrow.png` | **Synthetic.** mockInput `?`. |
| 9a | Loading state (OpenTUI catalog) | covered | `screenshots/synthetic_opentui_loading.png` | **Synthetic.** `Loading current workspace...` |
| 9b | Empty states | covered | `screenshots/cli_library_empty.png`, `screenshots/api_jobs_unauthorized.png`, `screenshots/synthetic_opentui_courses_empty.png`, `screenshots/synthetic_opentui_lectures_empty.png`, `screenshots/synthetic_opentui_library_empty.png` | CLI library empty; unauthenticated jobs; synthetic OpenTUI empty catalog/lectures/library. |
| 9c | Error states | covered | `screenshots/cli_courses_json_error_envelope.png`, `screenshots/api_courses_unauthorized.png`, `screenshots/api_courses_unauthorized_pretty.png`, `screenshots/api_courses_login_failed.png`, `screenshots/api_root_404.png`, `screenshots/api_docs_404.png`, `screenshots/tui_sidecar_missing_bootstrap.png`, `screenshots/tui_missing_sidecar.png`, `screenshots/tui_missing_sidecar_with_config.png`, `screenshots/synthetic_opentui_error_recovery.png` | Live CLI/API errors plus synthetic OpenTUI `Failed to load catalog.` Direct sidecar without parent bootstrap is a live error state. |
| 9d | Recovery states (in-TUI retry) | covered | `screenshots/synthetic_opentui_error_recovery.png` | **Synthetic** chrome: `Press r to retry or esc to return.` Live `impartus tui` still exits instead of offering this surface (ISSUE-002). |
| 10a | Wide OpenTUI layout | covered | `screenshots/synthetic_opentui_courses_wide.png` | **Synthetic** 140×40. Live `screenshots/tui_sample_wide_initial.png` is a wide **terminal** only; OpenTUI never painted there. |
| 10b | Medium OpenTUI layout | covered | `screenshots/synthetic_opentui_courses_medium.png` | **Synthetic** 80×24. |
| 10c | Narrow OpenTUI layout (clipping, emoji width, focus, contrast) | covered | `screenshots/synthetic_opentui_courses_narrow.png`, `screenshots/synthetic_opentui_lectures_narrow.png`, `screenshots/synthetic_opentui_command_guide_narrow.png` | **Synthetic** 40-col. Header clips (`● Conne`); footer glyphs collide. Observation only; not filed (no live TUI session). |

## Totals

| Status | Count |
|--------|-------|
| covered | 35 |
| blocked | 1 |
| not applicable | 0 |
| **rows** | **36** |

The first published matrix reported 22 covered / 14 blocked. Recounting that table as written gives 20 covered / 16 blocked (the 16 blocked IDs: 3, 4a–4e, 5, 6c, 7d, 7e, 8d, 9a, 9d, 10a–10c). This pass covers 15 of those 16 with synthetic OpenTUI stills (plus 6c TUI inspector %). Row **7d** (CLI populated `library list`) remains blocked: no completed public download.

## Capture methods (not mixed up)

| Surface | Method |
|---------|--------|
| CLI and live `impartus tui` attempts | Cursor Cloud XFCE desktop, `xfce4-terminal` on `DISPLAY=:1`, ImageMagick `import -window`. **Not** agent-browser. |
| HTTP JSON | `agent-browser` 0.27.0 session `impartus-api` |
| OpenTUI populated / layout / overlay states | **Synthetic.** Out-of-repo harness `/tmp/impartus-opentui-harness/capture.ts` (not committed). `createTestRenderer` + `FoundationView` with deterministic fixtures; frames converted to PNG with an orange SYNTHETIC banner. |
| Behavioral videos (ISSUE-002, ISSUE-006) | `ffmpeg` x11grab `libvpx` webm on `DISPLAY=:1`. **Not** agent-browser. |
