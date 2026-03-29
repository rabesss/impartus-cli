# Impartus CLI Testing Specification

## Overview

This document specifies the expected behavior of the Impartus CLI based on code analysis and documentation. It serves as a reference for testing the CLI functionality.

**Test Subject:** Impartus CLI (impartus)
**Course Under Test:** SEE IV Sem 2026 Even_Digital Signal Processing
- **subjectId:** 3176268
- **sessionId:** 1508
- **Professor:** Ms. Hilda Mayrose

---

## CLI Commands Specification

### 1. Help/Version Commands

| Command | Expected Behavior |
|---------|-------------------|
| `impartus --json` | Returns capability metadata JSON envelope |
| `impartus --help` | Shows help text to stdout |
| `impartus version` | Shows version and build date |

### 2. Courses Command

```bash
impartus courses --json
```

**Expected Output:** JSON envelope with list of courses containing:
- `subjectName`, `sessionName`, `professorName`
- `subjectId`, `sessionId`
- `videoCount`, `flippedLecturesCount`

### 3. Lectures Command

```bash
impartus lectures -s <subjectId> -S <sessionId> --json
```

**Example:**
```bash
impartus lectures -s 3176268 -S 1508 --json
```

**Expected Output:** JSON envelope with array of lectures containing:
- `ttid` (unique lecture ID), `seqNo` (lecture sequence number)
- `topic`, `startTime`, `endTime`
- `noaudio` (1 if no audio track), `views` (view count)
- `actualDuration` (seconds)

### 4. Download Command

```bash
impartus download -s <subjectId> -S <sessionId> [flags]
```

**Required Flags:**
- `-s, --subject` - Subject ID
- `-S, --session` - Session ID

**Optional Flags:**
- `--start <n>` - Start lecture index (1-based, default: 1)
- `--end <n>` - End lecture index (1-based, inclusive, default: last)
- `--quality <144|450|720>` - Video quality override
- `--views <left|right|both|first|second>` - View selection
- `--audio-only` - Audio-only mode
- `--format <mp3|m4a|aac|opus>` - Audio format when audio-only
- `-o, --output <dir>` - Output directory

**Expected Behavior:**
1. Fetches lectures for subject/session
2. Selects lecture range (1-based indexing, reversed order - newest first)
3. For each lecture:
   - Fetches HLS playlist
   - Downloads encrypted chunks
   - Decrypts chunks using AES-128-CBC
   - Joins chunks via FFmpeg
   - Outputs MP4 (or audio format if audio-only)

---

## Configuration Specification

### config.json Structure

```json
{
  "username": "string (required)",
  "password": "string (required)",
  "baseUrl": "string (required)",
  "quality": "144|450|720 (required, no default)",
  "views": "left|right|both|first|second (required, no default)",
  "downloadLocation": "string (default: ./downloads)",
  "tempDirLocation": "string (default: ./temp)",
  "slides": "boolean (default: false)",
  "audioOnly": "boolean (default: false)",
  "audioFormat": "mp3|m4a|aac|opus (default: mp3)",
  "skipNoAudio": "boolean (default: false)",
  "numWorkers": "integer 1-50 (default: 5)",
  "rateLimit": "float 0.1-100 (default: 10)",
  "apiRateLimit": "float 0.1-20 (default: 2)",
  "enablePipeline": "boolean (default: false)",
  "downloadWorkersPerLecture": "integer 1-10 (default: 3)",
  "decryptWorkersPerLecture": "integer 1-10 (default: 2)",
  "httpTimeout": "duration (default: 10m)",
  "enableJitter": "boolean (default: true)",
  "progressTracking": {
    "enabled": "boolean (default: false)",
    "showSpeed": "boolean (default: false)",
    "showETA": "boolean (default: false)",
    "updateInterval": "duration (default: 2s)",
    "speedWindowSize": "integer 3-30 (default: 10)"
  }
}
```

### Environment Variable Overrides

The following config fields can be overridden via environment variables (as defined in `internal/config/config.go` → `applyEnvOverrides()`):
- `IMPARTUS_USERNAME` – overrides `username`
- `IMPARTUS_PASSWORD` – overrides `password`
- `IMPARTUS_BASE_URL` – overrides `baseUrl`
- `IMPARTUS_QUALITY` – overrides `quality`
- `IMPARTUS_VIEWS` – overrides `views`
- `IMPARTUS_DOWNLOAD_LOCATION` – overrides `downloadLocation`
- `IMPARTUS_TEMP_DIR` – overrides `tempDirLocation`
- `IMPARTUS_AUDIO_ONLY` – overrides `audioOnly`
- `IMPARTUS_AUDIO_FORMAT` – overrides `audioFormat`
- `IMPARTUS_HTTP_TIMEOUT` – overrides `httpTimeout`
- `IMPARTUS_NUM_WORKERS` – overrides `numWorkers`
- `IMPARTUS_RATE_LIMIT` – overrides `rateLimit`
- `IMPARTUS_API_RATE_LIMIT` – overrides `apiRateLimit`
- `IMPARTUS_SKIP_NO_AUDIO` – overrides `skipNoAudio`

Other configuration fields, including nested objects such as `progressTracking`, cannot currently be set via environment variables and must be specified in `config.json`.

---

## Quality and Views Specification

### Quality Levels
- `144` - Lowest quality
- `450` - Medium quality
- `720` - High quality (HD)

### View Types
- `left` / `first` - Instructor view only
- `right` / `second` - whiteboard/slides view only
- `both` - Both views combined into single video

---

## Audio Format Specification

When `--audio-only` is enabled:
- `mp3` - MPEG Audio Layer III
- `m4a` - MPEG-4 Audio
- `aac` - Advanced Audio Coding
- `opus` - Opus Interactive Audio

---

## Testing Plan

### Phase 1: Basic Command Tests

| Test ID | Command | Expected Result |
|---------|---------|-----------------|
| T1.1 | `impartus --json` | Returns capability metadata |
| T1.2 | `impartus version` | Shows version info |
| T1.3 | `impartus courses --json` | Lists all courses |
| T1.4 | `impartus lectures -s 3176268 -S 1508 --json` | Lists DSP lectures |

### Phase 2: Download Tests - Quality Variations

| Test ID | Command | Description |
|---------|---------|-------------|
| T2.1 | `impartus download -s 3176268 -S 1508 --start 1 --end 1 --quality 144` | 144p video |
| T2.2 | `impartus download -s 3176268 -S 1508 --start 1 --end 1 --quality 450` | 450p video |
| T2.3 | `impartus download -s 3176268 -S 1508 --start 1 --end 1 --quality 720` | 720p video |

### Phase 3: Download Tests - View Variations

| Test ID | Command | Description |
|---------|---------|-------------|
| T3.1 | `impartus download -s 3176268 -S 1508 --start 1 --end 1 --views left` | Left/instructor view only |
| T3.2 | `impartus download -s 3176268 -S 1508 --start 1 --end 1 --views right` | Right/whiteboard view only |
| T3.3 | `impartus download -s 3176268 -S 1508 --start 1 --end 1 --views both` | Both views (combined) |
| T3.4 | `impartus download -s 3176268 -S 1508 --start 1 --end 1 --views first` | Legacy 'first' view |
| T3.5 | `impartus download -s 3176268 -S 1508 --start 1 --end 1 --views second` | Legacy 'second' view |

### Phase 4: Audio-Only Tests

| Test ID | Command | Description |
|---------|---------|-------------|
| T4.1 | `impartus download -s 3176268 -S 1508 --start 1 --end 1 --audio-only --format mp3` | MP3 audio |
| T4.2 | `impartus download -s 3176268 -S 1508 --start 1 --end 1 --audio-only --format m4a` | M4A audio |
| T4.3 | `impartus download -s 3176268 -S 1508 --start 1 --end 1 --audio-only --format aac` | AAC audio |
| T4.4 | `impartus download -s 3176268 -S 1508 --start 1 --end 1 --audio-only --format opus` | Opus audio |

### Phase 5: Range and Output Tests

| Test ID | Command | Description |
|---------|---------|-------------|
| T5.1 | `impartus download -s 3176268 -S 1508 --start 1 --end 3` | Download range 1-3 |
| T5.2 | `impartus download -s 3176268 -S 1508 --start 1 --end 1 -o /tmp/test_dsp` | Custom output dir |

### Phase 6: Validation Tests (Expected Failures)

| Test ID | Command | Expected Behavior |
|---------|---------|-------------------|
| T6.1 | `impartus lectures` | Error: missing --subject and --session |
| T6.2 | `impartus download -s 3176268` | Error: missing --session |
| T6.3 | `impartus download -s 3176268 -S 1508 --quality 1080` | Error: invalid quality |
| T6.4 | `impartus download -s 3176268 -S 1508 --views invalid` | Error: invalid views |
| T6.5 | `impartus download -s 3176268 -S 1508 --format wav` | Error: invalid format |

---

## Expected Output File Naming

### Video Output (--audio-only=false)
- `LEC <SeqNo> <Topic> LEFT VIEW.mp4`
- `LEC <SeqNo> <Topic> RIGHT VIEW.mp4`
- `LEC <SeqNo> <Topic>.mp4` (combined when views=both)

### Audio Output (--audio-only=true)
- `LEC <SeqNo> <Topic> LEFT VIEW.<format>`
- `LEC <SeqNo> <Topic> RIGHT VIEW.<format>`
- `LEC <SeqNo> <Topic>.<format>` (combined when views=both)

Note: When topic is "No Topic Entered", the filename will reflect that.

---

## Error Handling Expectations

| Scenario | Expected Error |
|----------|---------------|
| Missing credentials | "username and password are required" |
| Invalid quality | "invalid quality value: must be one of: 144, 450, 720" |
| Invalid views | "invalid views value: must be one of: first, second, both, left, right" |
| Invalid audio format | "invalid audioFormat value: must be one of: mp3, m4a, aac, opus" |
| FFmpeg not found | "please add ffmpeg to your path" |
| No lectures in range | "invalid lecture range" |

---

## Notes

1. **Lecture Order:** Lectures are displayed in reverse chronological order (newest first). Index 1 is the most recent lecture.

2. **No-Audio Lectures:** Some lectures may have `noaudio: 1` flag - these may have different behavior in audio-only mode.

3. **Rate Limiting:** The API has rate limiting configured. The CLI implements jitter and rate limiting to avoid being blocked.

4. **Pipeline Mode:** When `enablePipeline: true` in config, downloads and decryption run concurrently.
