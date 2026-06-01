# Security Policy

## Scope

This project handles local configuration, authenticated API requests, media processing, and an optional local HTTP API. Security issues are taken seriously.

## Do Not Share Publicly

Do not include real account details, session material, private lecture metadata, private downloaded media, local config files, or prompt logs containing private material in issues, PRs, screenshots, logs, or test fixtures.

## Sensitive Areas

Changes to these areas need careful review:

- authentication handling
- local API server authorization
- download job creation and idempotency
- WebSocket event payloads
- path handling and output filenames
- FFmpeg invocation and media pipeline behavior
- rate limiting and retry behavior
- logging and error reporting

## Local Config Guidance

Keep `config.json` and generated job/state files private to your user account. Use sample config files for docs and tests. Do not commit real credentials or private downloaded content.

## Reporting

Use GitHub private vulnerability reporting when available. If unavailable, open a minimal public issue asking for a private channel, without including sensitive details.