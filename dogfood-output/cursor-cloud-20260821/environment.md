# Environment — Cursor Cloud dogfood 2026-08-21

## Gates (recorded before testing)

| Gate | Expected | Observed | Result |
|------|----------|----------|--------|
| Repository remote | `https://github.com/rabesss/impartus-cli` | `origin` fetch/push `https://github.com/rabesss/impartus-cli` | pass |
| Starting branch | `feat/exact-lecture-selection-token-cache` | `feat/exact-lecture-selection-token-cache` | pass |
| Initial HEAD | `3fab54fb5beff65a086b607ce16d27cede9285d6` | `3fab54fb5beff65a086b607ce16d27cede9285d6` (`fix: keep audio badges terminal-safe`) | pass |
| Cloud Agent model | Grok 4.6, effort high, fast false | `originalModelName`: `cursor-grok-4.6-high` | pass |

No product files were modified. Testing proceeded only after all gates matched.

## Agent / run identity

| Field | Value |
|-------|-------|
| Cloud Agent bcId | `bc-ea6d17cd-d316-4907-9db3-7acea98e1071` |
| Run URL | https://cursor.com/agents/bc-ea6d17cd-d316-4907-9db3-7acea98e1071 |
| Run name | Impartus-cli dogfood qa |
| Source | sdk |
| Setup | `INSTALL_SUCCEEDED` |
| Environment | Personal `rabesss/impartus-cli` (`environmentPublicId` `0d064216-75f0-11f1-a7d1-d6b4613131ce`) |
| Artifact branch | `cursor/dogfood-report-1071` |
| Base revision | `3fab54fb5beff65a086b607ce16d27cede9285d6` on `feat/exact-lecture-selection-token-cache` |

## Model parameters

| Field | Value |
|-------|-------|
| User-requested | Grok 4.6, effort high, fast false |
| Run metadata | `cursor-grok-4.6-high` |

## Product under test

Public `rabesss/impartus-cli` only. Built in this VM:

```
make build
# go build -o impartus .
# cd ui && bun install --frozen-lockfile && bun run build -- --outfile=../impartus-ui
```

| Binary | Notes |
|--------|--------|
| `impartus` | Go parent, ELF x86-64, reports `Version: 0.1.25` |
| `impartus-ui` | Adjacent OpenTUI sidecar (Bun 1.3.14 / `@opentui/core` 0.5.2) |

Runs used copies under `/tmp/impartus-dogfood-*` so the git worktree was not used as a config/state directory.

## Commands used (credentials unset)

Wrapper `/tmp/impartus-nocred.sh` set `HOME=/tmp/impartus-dogfood-nocred` and **unset** `IMPARTUS_USERNAME`, `IMPARTUS_PASSWORD`, `IMPARTUS_BASE_URL`, `IMPARTUS_TOKEN_CACHE`.

Wrapper `/tmp/impartus-sample.sh` used only the published `sample.config.json` placeholders (not live env credentials).

Exercised:

- `impartus help`, `version`, `--json`, `notacommand`, no-args with stdin `/dev/null`
- `impartus tui`, `tui --help`, `version --help`, `library --help`, `download --help`, `play --help`, `watch --help`, `serve --help`
- `impartus courses`, `courses --json`, `lectures`, `download`, `download -s 1 -S 1`, `download --ttid` conflict, `play`, `watch --once --dry-run`
- `impartus doctor`, `doctor --json`
- `impartus library list`, `library show`, `library verify`
- `impartus serve` (no config: refused; sample config: port 8080)
- `impartus serve --json --port 8081` (non-blocking metadata; port not bound)
- `impartus download -s 1 -S 1 --json` / `--events` with sample placeholders (auth fail, no media)
- Direct `impartus-ui` without bootstrap
- HTTP: `/api/v1/health/live`, `/ready`, `/health`, `/`, `/docs`, `/api/v1/courses`, `/api/v1/jobs`, POST `/api/v1/auth/login` with sample placeholders, POST `/api/v1/jobs` with fake ids (cancelled/failed, empty downloads dir)

`agent-browser skills get dogfood --full` and `agent-browser skills get core` were loaded. Direct binary: `agent-browser` 0.27.0 (`npm i -g` into `~/.npm-global`). Chrome via `agent-browser install` / system `google-chrome`.

## Capture method

| Kind | Method | Honest claim |
|------|--------|----------------|
| Terminal / TUI attempts | Cursor Cloud XFCE + TigerVNC `DISPLAY=:1`, `xfce4-terminal`, ImageMagick `import -window <id>` | Desktop terminal screenshots. **agent-browser did not capture terminals.** |
| HTTP JSON | `agent-browser --session impartus-api` screenshots of `http://127.0.0.1:8080/...` | Browser-accessible API only |
| Videos | none | No in-workspace interactive issue was reachable |

Dogfood skill template/taxonomy: `/home/ubuntu/.npm-global/lib/node_modules/agent-browser/skill-data/dogfood/`.

## Limitations

1. **No live Impartus account in recorded tests.** The Cloud environment had `IMPARTUS_*` set. Those values were never printed, copied into artifacts, or used after the first accidental `courses` probe (discarded; private catalog not screenshotted).
2. **No documented TUI fixture/demo mode** in README/help. OpenTUI populated states are blocked rather than fabricated.
3. **No real lecture download.** Fake subject/session 1 with sample placeholders failed at auth. `downloads/` stayed empty.
4. **OpenTUI layouts, command guide, playback chrome, audio badges, inspector, filters** were not reached. `impartus tui` exits in the Go parent on auth failure.
5. **`doctor --json` screenshot** still clips the first checks at the top of the window; the human `doctor` screenshot is complete.
6. API has no HTML UI; Chrome shows raw JSON (Pretty-print checkbox is the browser JSON viewer, not a product control).
7. agent-browser was used only for loopback HTTP. Exa MCP fetch of skills.sh hit a free-tier rate limit; the skill was loaded from GitHub raw + the installed CLI (`agent-browser skills get …`).

## Out of scope (honored)

Private `impartus-sem5-lecture-worker`, Google Drive, NotebookLM, AGY, Railway, and Obsidian were not opened, called, or dogfooded.
