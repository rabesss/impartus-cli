# Environment

Environment variables, external dependencies, and setup notes.

**What belongs here:** required env vars, external services, setup quirks, secret-handling notes.
**What does NOT belong here:** service ports/commands (use `.factory/services.yaml`).

---

- Live Impartus access is required for lightweight integration checks and milestone download validation.
- Existing local config already contains credentials/base URL; mission work must add clear support for environment-based configuration without committing secrets.
- Supported environment variables (defined in `internal/config/config.go`):
  - `IMPARTUS_USERNAME`, `IMPARTUS_PASSWORD` - credentials
  - `IMPARTUS_BASE_URL` - API endpoint
  - `IMPARTUS_DOWNLOAD_LOCATION` - output directory
  - `IMPARTUS_NUM_WORKERS` - download concurrency
  - `IMPARTUS_QUALITY` - video quality (144/450/720)
  - `IMPARTUS_VIEWS` - view selection (first/second/both)
  - `IMPARTUS_AUDIO_ONLY` - audio-only mode
  - `IMPARTUS_AUDIO_FORMAT` - audio format (mp3/m4a/aac/opus)
- `ffmpeg` must be available in `PATH` for download artifact assembly and audio extraction.
- `desloppify` requires Python 3.11+ and maintains local state in `.desloppify/`, which must stay uncommitted.
