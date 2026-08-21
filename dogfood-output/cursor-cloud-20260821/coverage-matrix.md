# Coverage matrix — impartus-cli dogfood 2026-08-21

Status values: **covered** (readable screenshot exists), **blocked** (not reachable without live Impartus credentials or would persist private lecture metadata), **not applicable**.

| ID | Requested surface / state | Status | Evidence | Notes |
|----|---------------------------|--------|----------|-------|
| 1a | Root help | covered | `screenshots/cli_root_help.png`, `screenshots/issue-003-help-wording.png` | `impartus help` |
| 1b | Version | covered | `screenshots/cli_version.png`, `screenshots/issue-004-empty-build-date.png` | Build Date empty after `make build` |
| 1c | Command discoverability (`--json` capability) | covered | `screenshots/cli_json_capability.png` | `impartus --json` |
| 1d | Invalid command feedback | covered | `screenshots/cli_invalid_command.png` | `unknown command: notacommand`, exit 1 |
| 1e | Non-TTY no-args | covered | `screenshots/cli_nontty_noargs.png` | Help + `interactive TUI requires a terminal`, exit 2 |
| 1f | Subcommand `--help` | covered | `screenshots/cli_download_help_flag.png`, `screenshots/cli_tui_help_flag.png`, `screenshots/cli_library_help_flag.png`, `screenshots/cli_version_help_flag.png`, `screenshots/cli_watch_help_flag.png`, `screenshots/cli_play_help_flag.png`, `screenshots/issue-001-*.png` | Filed as ISSUE-001 |
| 2a | Missing credentials (CLI) | covered | `screenshots/cli_courses_missing_credentials.png`, `screenshots/cli_download_missing_credentials.png`, `screenshots/cli_play_missing_credentials.png`, `screenshots/cli_serve_missing_credentials.png`, `screenshots/cli_courses_json_error_envelope.png` | `username and password are required` |
| 2b | Missing flags | covered | `screenshots/cli_lectures_missing_flags.png`, `screenshots/cli_watch_missing_targets.png`, `screenshots/cli_download_ttid_conflict.png` | |
| 2c | Published sample / wrong Impartus credentials | covered | `screenshots/cli_sample_wrong_credentials.png`, `screenshots/cli_download_json_wrong_credentials.png` | Placeholders from `sample.config.json` only |
| 2d | TUI auth failure | covered | `screenshots/tui_launch_missing_credentials.png`, `screenshots/tui_sample_wide_initial.png`, `screenshots/issue-002-*.png` | Exits before OpenTUI |
| 3 | OpenTUI course overview and navigation | blocked | — | `impartus tui` exits on missing/wrong credentials. No documented fixture/demo catalog. Live env credentials were not used (private lecture metadata). |
| 4a | Lecture workspace, audio-reported row | blocked | — | Requires authenticated catalog inside OpenTUI |
| 4b | Lecture workspace, probably-no-audio presentation | blocked | — | Same |
| 4c | Lecture inspector | blocked | — | Same |
| 4d | Lecture selection | blocked | — | Same |
| 4e | Lecture / course filtering | blocked | — | Same |
| 5 | Playback surface and keyboard controls (paused / muted / speed / volume) | blocked | — | Requires live playback; no fixture player. mpv is installed (`doctor` PASS) but play/tui never reached media. |
| 6a | Download error without real lecture media (CLI events) | covered | `screenshots/cli_download_events_auth_error.png`, `screenshots/issue-007-events-duplicate-error.png` | `job.started` then `job.failed`; downloads directory empty |
| 6b | Download error without real lecture media (HTTP job) | covered | `screenshots/api_jobs_failed_download.png`, `screenshots/api_job_detail_upstream_error.png`, `screenshots/issue-006-api-job-generic-error.png` | Job `failed`, progress 0, no media files |
| 6c | Download in-progress bar / ETA | blocked | — | Would require a real (or fixture) lecture fetch. Not performed. |
| 7a | Local library empty | covered | `screenshots/cli_library_empty.png` | `Library is empty.` |
| 7b | Local library show-missing | covered | `screenshots/cli_library_show_missing.png` | `library artifact not found` |
| 7c | Local library verify empty | covered | `screenshots/cli_library_verify_empty.png`, `screenshots/issue-005-library-verify-silent.png` | Silent exit 0 |
| 7d | Local library populated | blocked | — | No completed download; no public populated fixture store |
| 7e | OpenTUI library pane | blocked | — | TUI workspace not entered |
| 8a | Diagnostics `doctor` (human) | covered | `screenshots/cli_doctor_missing_config.png`, `screenshots/cli_sample_doctor.png` | WARN config/token when absent; PASS mpv/ffmpeg/library |
| 8b | Diagnostics `doctor --json` | covered | `screenshots/cli_doctor_json.png` | Envelope `success: true`, per-check warn/pass |
| 8c | HTTP health live / ready / alias | covered | `screenshots/api_health_live.png`, `screenshots/api_health_ready.png`, `screenshots/api_health_ready_pretty.png`, `screenshots/api_health_alias.png` | agent-browser |
| 8d | OpenTUI command guide (`?`) | blocked | — | TUI workspace not entered |
| 9a | Loading state (OpenTUI catalog) | blocked | — | TUI workspace not entered |
| 9b | Empty states | covered | `screenshots/cli_library_empty.png`, `screenshots/api_jobs_unauthorized.png` | Library empty; unauthenticated jobs |
| 9c | Error states | covered | `screenshots/cli_courses_json_error_envelope.png`, `screenshots/api_courses_unauthorized_pretty.png`, `screenshots/api_courses_login_failed.png`, `screenshots/api_root_404.png`, `screenshots/api_docs_404.png`, `screenshots/tui_sidecar_missing_bootstrap.png` | |
| 9d | Recovery states (in-TUI retry) | blocked | — | Parent exits instead of offering retry chrome |
| 10a | Wide OpenTUI layout | blocked | `screenshots/tui_sample_wide_initial.png` shows a wide **terminal** only; OpenTUI chrome never painted | geometry 160x42 attempted |
| 10b | Medium OpenTUI layout | blocked | — | Same auth gate |
| 10c | Narrow OpenTUI layout (clipping, emoji width, focus, contrast) | blocked | — | Same auth gate |

## Totals

| Status | Count |
|--------|-------|
| covered | 22 |
| blocked | 14 |
| not applicable | 0 |
| **rows** | **36** |

## Capture methods (not mixed up)

| Surface | Method |
|---------|--------|
| CLI and `impartus tui` attempts | Cursor Cloud XFCE desktop, `xfce4-terminal` on `DISPLAY=:1`, ImageMagick `import -window`. **Not** agent-browser. |
| HTTP API JSON in Chrome | `agent-browser` 0.27.0 session `impartus-api` |
| Videos | none (no interactive-in-workspace issue was reachable to record) |
