# Environment — Cursor Cloud dogfood 2026-08-21

## Gates (recorded before testing)

| Gate | Expected | Observed | Result |
|------|----------|----------|--------|
| Repository remote | `https://github.com/rabesss/impartus-cli` | `origin` fetch/push `https://github.com/rabesss/impartus-cli` | pass |
| Starting branch | `feat/exact-lecture-selection-token-cache` | `feat/exact-lecture-selection-token-cache` | pass |
| Initial HEAD | `3fab54fb5beff65a086b607ce16d27cede9285d6` | `3fab54fb5beff65a086b607ce16d27cede9285d6` (`fix: keep audio badges terminal-safe`) | pass |
| Cloud Agent model | Grok 4.6, effort high, fast false | `originalModelName`: `cursor-grok-4.6-high` | pass |

No product files were modified. Testing proceeded only after all gates matched. This evidence-quality correction also left product source, tests, and config untouched; only `dogfood-output/cursor-cloud-20260821/` changed.

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
| Correction pass | Same model request (Grok 4.6 high, fast false) |

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
| Terminal / live TUI attempts | Cursor Cloud XFCE + TigerVNC `DISPLAY=:1`, `xfce4-terminal`, ImageMagick `import -window <id>` | Desktop terminal screenshots. **agent-browser did not capture terminals.** |
| HTTP JSON | `agent-browser --session impartus-api` screenshots of `http://127.0.0.1:8080/...` | Browser-accessible API only |
| Split-stream ISSUE-007 check | `impartus download -s 1 -S 1 --events > /tmp/issue007/stdout.txt 2> /tmp/issue007/stderr.txt` then display each file in a terminal and `import -window` | stdout and stderr were never merged for this conclusion |
| OpenTUI workspace / layouts / overlays | Temporary harness **outside the repo** (see below) | **Synthetic.** Not a live Impartus login. Every PNG has an orange SYNTHETIC banner. |
| Behavioral videos | `ffmpeg -f x11grab` on `DISPLAY=:1`, `libvpx` webm | ISSUE-002 and ISSUE-006 only. **Not** agent-browser. |

Dogfood skill template/taxonomy: `/home/ubuntu/.npm-global/lib/node_modules/agent-browser/skill-data/dogfood/`.

## OpenTUI synthetic harness (correction pass)

Live `impartus tui` never reaches OpenTUI without credentials. To close the OpenTUI coverage gap **without live credentials**, an out-of-repo harness reused the product's existing view and deterministic fixture *shapes* (synthetic names only).

| Item | Value |
|------|--------|
| Harness directory | `/tmp/impartus-opentui-harness` (**not committed**) |
| Entry | `/tmp/impartus-opentui-harness/capture.ts` |
| Product code | symlink `src` → `/workspace/ui/src` (read-only reuse of `FoundationView`) |
| Dependencies | symlink `node_modules` → `/workspace/ui/node_modules` |
| Renderer | `@opentui/core/testing` `createTestRenderer` |
| Frames | `/tmp/impartus-opentui-harness/frames/*.txt` (not committed) |
| PNG conversion | Python/PIL: dark terminal canvas + orange banner `SYNTHETIC OpenTUI fixture render — not a live Impartus session` |
| Working directory | Must be the harness directory so `@opentui/core` is a **single copy**. Importing `ui/src/view.ts` from a script that also imported `@opentui/core/testing` from a second resolution path produced empty panels and `remove expects a renderable child object`; mockInput (`?`, `/`) failed. |

Fixture catalog used in frames (synthetic only; not live probe data):

- Courses: Distributed Systems (Dr. Rao), Compilers (Dr. Sen), session name Monsoon 2026
- Lectures: Audio lecture (`noAudio: false`, ttid 101), Visual-only lecture (`noAudio: true`, ttid 102)
- Playback/library: Consensus, Room 7, artifact id `impartus:v1:synthetic:distributed-systems:consensus`
- Numeric ids: institute 1, session 2, subject 3/4, ttid 101/102/105

Product `ui/test/view.test.ts` was read **only** to reuse constructor shapes. Live-catalog-like names from that file were **not** copied into frames or screenshots.

Keyboard overlays (`/` filter, `?` command guide) used `setup.mockInput.pressKey` / `typeText` on the test renderer, not a live TTY.

## Split-stream correction (original ISSUE-007)

Command (sample placeholders, no live credentials):

```
impartus download -s 1 -S 1 --events
```

Redirected to `/tmp/issue007/stdout.txt` and `/tmp/issue007/stderr.txt`.

| Stream | Contents |
|--------|----------|
| stdout | Two JSONL lines: `job.started`, `job.failed` with `"error":"wrong credentials please retry"` inside the JSON object |
| stderr | One human line: `wrong credentials please retry` |

Conclusion: NDJSON stdout is pure. The duplicate human sentence is stderr only. Original ISSUE-007 (stdout corruption / impact on event parsers) is withdrawn. A merged TTY screenshot must not be used to infer stream corruption.

## Video capture (ISSUE-002, ISSUE-006)

| Video | Path | Method |
|-------|------|--------|
| TUI auth exits | `videos/issue-002-tui-auth-repro.webm` | xfce4-terminal + paced `/tmp/issue002-repro.sh` + `ffmpeg -f x11grab` `libvpx` on `DISPLAY=:1` |
| API job generic error | `videos/issue-006-api-job-error-repro.webm` | xfce4-terminal + paced `/tmp/issue006-repro.sh` + same ffmpeg. Window title in the recording says `issue008-repro` (script leftover name); content is ISSUE-006. |

Scripts lived under `/tmp` and were not committed. Tokens and passwords were not printed.

## Limitations

1. **No live Impartus account in recorded tests.** The Cloud environment had `IMPARTUS_*` set. Those values were never printed, copied into artifacts, or used after the first accidental `courses` probe (discarded; private catalog not screenshotted; that probe remains absent from this folder).
2. **OpenTUI populated states in this report are synthetic fixture renders**, not a logged-in `impartus tui` session. Live TUI still exits in the Go parent on auth failure (ISSUE-002).
3. **No real lecture download.** Fake subject/session 1 with sample placeholders failed at auth. `downloads/` stayed empty. CLI populated `library list` (coverage 7d) is still blocked. CLI mpb/ETA in-progress bar was not live-captured; TUI inspector percent is synthetic only (6c).
4. **`doctor --json` screenshot** still clips the first checks at the top of the window; the human `doctor` screenshot is complete.
5. API has no HTML UI; Chrome shows raw JSON (Pretty-print checkbox is the browser JSON viewer, not a product control).
6. agent-browser was used only for loopback HTTP. Exa MCP fetch of skills.sh hit a free-tier rate limit; the skill was loaded from GitHub raw + the installed CLI (`agent-browser skills get …`).
7. ISSUE-002 stills labeled “before” already include the ACTION header because the paced script printed both blocks before the TUI process returned; the video is the primary sequence evidence.

## Out of scope (honored)

Private `impartus-sem5-lecture-worker`, Google Drive, NotebookLM, AGY, Railway, and Obsidian were not opened, called, or dogfooded.
