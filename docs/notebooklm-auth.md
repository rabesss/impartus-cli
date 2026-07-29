# NotebookLM authentication

Impartus does not handle Google credentials. Authenticate once with the
NotebookLM provider CLI on a desktop machine, then let `impartus watch` invoke
that CLI using its existing profile.

Do not put cookies, OAuth tokens, master tokens, or exported browser state in
`config.json`, environment variables intended for Impartus, command arguments,
logs, or this repository.

## `notebooklm-py` (default)

Install the browser-enabled CLI in an isolated environment and complete its
native Google sign-in:

```bash
uv tool install "notebooklm-py[browser]"
notebooklm login
notebooklm auth check --test --json
```

The auth check should return JSON with a successful status. For a named
profile, pass the global profile option before the command:

```bash
notebooklm --profile work login
notebooklm --profile work auth check --test --json
```

Set the same name in `watch.notebooklm.profile`.

For unattended master-token refresh, install both extras on the desktop and run
the provider's native bootstrap:

```bash
uv tool install --force "notebooklm-py[browser,headless]==0.8.0rc1"
notebooklm login --master-token --account you@example.com
notebooklm login --master-token-refresh
```

The headless runtime or container only needs the pinned headless extra:

```bash
uv tool install "notebooklm-py[headless]==0.8.0rc1"
```

Bootstrap the provider profile on the desktop first. If the runtime is on a
different trusted machine, transfer only the provider-owned profile directory
over a secure channel and preserve restrictive permissions:

```bash
chmod -R go-rwx ~/.notebooklm
```

Treat `master_token.json` as a full-account credential. Use a dedicated Google
account where possible, keep the file at mode `0600`, and never pass its value
through Impartus.

## `nlm` (optional)

Install `notebooklm-mcp-cli`, then use its native login:

```bash
uv tool install notebooklm-mcp-cli
nlm login
nlm login --check
```

Named profiles are supported:

```bash
nlm login --profile work
nlm login --check --profile work
```

Configure the provider and profile together:

```json
{
  "watch": {
    "notebooklm": {
      "provider": "nlm",
      "command": "nlm",
      "profile": "work"
    }
  }
}
```

## Verify Impartus

After native login succeeds, validate the complete watch configuration without
downloading:

```bash
impartus watch --check --upload
```

The check verifies ffmpeg, the configured targets, provider binary,
authentication, and NotebookLM source-count guard. Authentication is skipped
when uploads are disabled.

For scheduled operation, prefer a single-cycle invocation under a systemd
timer or cron:

```bash
impartus watch --once --upload
```

Long-running container deployments may instead run `impartus watch --upload`.
If authentication expires, rerun the provider's native login or refresh command
on the machine that owns the profile.
