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
- golangci-lint is installed at `$(go env GOPATH)/bin/golangci-lint` (v1.64.8, built with Go 1.24). The project also uses Go 1.24.0. Use `$(go env GOPATH)/bin/golangci-lint` rather than expecting it on PATH. The `make lint` target handles the fallback automatically.
