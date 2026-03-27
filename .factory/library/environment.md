# Environment

Environment variables, external dependencies, and setup notes.

**What belongs here:** Required env vars, external API keys/services, dependency quirks, platform-specific notes.
**What does NOT belong here:** Service ports/commands (use `.factory/services.yaml`).

---

## Required Credentials

Credentials come from `config.json` or environment variables:
- `IMPARTUS_USERNAME` / `IMPARTUS_PASSWORD` - Impartus login
- `IMPARTUS_BASE_URL` - Impartus API base URL

## External Dependencies

- Impartus upstream API (requires valid credentials, network access)
- FFmpeg (must be in PATH for video/audio processing)

## Dependency Quirks

- `BaseUrl` field has two forms: `BaseUrl` and `BaseURL` - code normalizes either one
- Token stored in `.token` file (mode 0600) - separate from API auth tokens
