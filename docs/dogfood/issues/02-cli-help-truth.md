---
title: [Bug]: CLI help ignores help <command> and omits flags
labels: type: bug,priority: medium,area: cli,area: docs
---

# [Bug]: CLI help ignores help <command> and omits flags

Dogfood. https://cursor.com/agents/bc-01a03c93-2ac2-7097-89bb-3bb80797d748

## Bug Description

Two help lies that users hit in the same breath.

`impartus help download` (and `help download --json`) prints root help and exits 0. `help` is a root command, not a dispatcher. `resolveCommandHelp` only treats `--help` after a real command as command help.

`impartus download --help` then prints a description plus one usage line with `[flags]`. The actual flag list lives only in root `showHelpTo`. Root help mentions `--json` only inside the `--events` line.

## Steps to Reproduce

1. `./impartus help download`
2. `./impartus help download --json`
3. `./impartus download --help`
4. `./impartus --help`

## Expected Behavior

`help download` matches `download --help`. Command help lists `--subject`, `--session`, `--start`, `--quality`, `--json`, and the rest. Root help lists `--json` as a global flag.

## Actual Behavior

Steps 1 and 2 print root usage. Step 3 prints usage with `[flags]` and no flag list. Exit 0.

## Version

0.1.28

## Environment

- OS: Ubuntu (Cursor Cloud Agent XFCE desktop, Linux 6.12)
- Binary: /workspace/impartus 0.1.28, build 2026-08-26T13:59:47Z
- ffmpeg: present
- mpv: absent in run 1, /usr/bin/mpv 0.37.0 in run 2
- bun: absent in run 1, 1.4.0 in run 2
- Config: environment-only IMPARTUS_USERNAME, IMPARTUS_PASSWORD, IMPARTUS_BASE_URL. No config.json.
- State: XDG_STATE_HOME=/tmp/impartus-dogfood/state, IMPARTUS_TOKEN_CACHE=/tmp/impartus-dogfood/token (0600)

## Relevant Logs

See `/opt/cursor/artifacts/cli-dogfood` cases `help_download_subcommand_style` and `help_download_json`. Makefile target `run-cli-download-help` only exercises `download --help`.

## Media and source

- Run 1. https://cursor.com/agents/bc-01a03c93-2ac2-7097-89bb-3bb80797d748
- `internal/cli/cli.go` `executeHuman` `case "help"`
- `internal/cli/cli_help.go` `resolveCommandHelp`, `showCommandHelp`
- `internal/cli/cli_helpers.go` `showHelpTo`
- `internal/cli/cli_json.go` `helpPayload`, `newCommandHelpPayload`

## Grouping

B2 and B3 together. Same help types. Stacked PR `PR-cli-help`.
