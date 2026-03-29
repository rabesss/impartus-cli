# Skip No-Audio Feature Test Specification

**Feature:** Filter lectures with no audio track during download
**Date:** 2026-03-21
**Status:** Implementation Complete

---

## Test Scope

This spec covers testing for the `--skip-no-audio` flag that filters out lectures marked with `noaudio=1` before downloading.

---

## Test Data: DSP Course

**Course:** SEE IV Sem 2026 Even_Digital Signal Processing
- **subjectId:** 3176268
- **sessionId:** 1508
- **Total lectures:** 35

### Lectures with noaudio=1 (expected):

Based on API data, lectures with `noaudio=1`:
- Lecture index 5: seqNo=31
- Lecture index 6: seqNo=30
- Lecture index 7: seqNo=29
- Lecture index 8: seqNo=28
- Lecture index 9: seqNo=27
- Lecture index 10: seqNo=26
- (and potentially more...)

---

## Test Cases

### Phase 1: CLI Download Tests

#### TC-01: Flag Parsing
```bash
./impartus download --help | grep "skip-no-audio"
```
**Expected:** Flag appears in help text
**Status:** TBD

#### TC-02: Basic Flag Without Args
```bash
./impartus download -s 3176268 -S 1508 --skip-no-audio --start 1 --end 35 --json 2>&1
```
**Expected:** Downloads only lectures with audio (filters out noaudio=1)
**Status:** TBD

#### TC-03: Flag With Audio-Only Mode
```bash
./impartus download -s 3176268 -S 1508 --skip-no-audio --audio-only --format mp3 --start 1 --end 10 --json
```
**Expected:** Filters noaudio lectures, downloads only audio for remaining
**Status:** TBD

#### TC-04: Flag With Quality Override
```bash
./impartus download -s 3176268 -S 1508 --skip-no-audio --quality 720 --start 1 --end 10 --json
```
**Expected:** Filters noaudio lectures, downloads in 720p quality
**Status:** TBD

#### TC-05: Flag With Views Override
```bash
./impartus download -s 3176268 -S 1508 --skip-no-audio --views left --start 1 --end 10 --json
```
**Expected:** Filters noaudio lectures, downloads only left view
**Status:** TBD

---

### Phase 2: Edge Case Tests

#### TC-06: All Lectures Have Audio (No Filtering Needed)
```bash
# Select a range that likely has no noaudio lectures
./impartus download -s 3176268 -S 1508 --skip-no-audio --start 1 --end 5 --json
```
**Expected:** All lectures downloaded, filteredCount=0
**Status:** TBD

#### TC-07: All Lectures Are No-Audio (Complete Filter)
```bash
# Select a range that likely has all noaudio lectures
./impartus download -s 3176268 -S 1508 --skip-no-audio --start 26 --end 31 --json
```
**Expected:** Error message: "no lectures available after filtering (all lectures have noaudio=1 in the selected range)"
**Status:** TBD

#### TC-08: Mixed Range (Some No-Audio)
```bash
./impartus download -s 3176268 -S 1508 --skip-no-audio --start 1 --end 15 --json
```
**Expected:** 
- filteredCount = number of noaudio lectures in range
- totalLectures = 15
- lectureCount = 15 - filteredCount
**Status:** TBD

#### TC-09: Flag Not Set (Default Behavior)
```bash
./impartus download -s 3176268 -S 1508 --start 26 --end 31 --json
```
**Expected:** Attempts to download all lectures (including noaudio=1), no filtering
**Status:** TBD

#### TC-10: Single Lecture Range
```bash
./impartus download -s 3176268 -S 1508 --skip-no-audio --start 5 --end 5 --json
```
**Expected:** If lecture 5 is noaudio, error. Otherwise, download single lecture.
**Status:** TBD

---

### Phase 3: JSON Output Format Tests

#### TC-11: Verify JSON Response Structure
```bash
./impartus download -s 3176268 -S 1508 --skip-no-audio --start 1 --end 10 --json
```
**Expected JSON structure:**
```json
{
  "success": true,
  "data": {
    "status": "completed",
    "outputPaths": ["..."],
    "lectureCount": 8,
    "filteredCount": 2,
    "totalLectures": 10
  },
  "error": null,
  "meta": {
    "command": "download",
    "mode": "json"
  }
}
```
**Status:** TBD

#### TC-12: JSON Error When All Filtered
```bash
./impartus download -s 3176268 -S 1508 --skip-no-audio --start 26 --end 31 --json
```
**Expected:** Error envelope with message about no lectures available after filtering
**Status:** TBD

---

### Phase 4: Config and Environment Tests

#### TC-13: Config File Option
```bash
# Set skipNoAudio: true in config.json
./impartus download -s 3176268 -S 1508 --start 1 --end 10
```
**Expected:** Automatically skips noaudio lectures (no flag needed)
**Status:** TBD

#### TC-14: Environment Variable
```bash
export IMPARTUS_SKIP_NO_AUDIO=true
./impartus download -s 3176268 -S 1508 --start 1 --end 10
```
**Expected:** Environment variable enables filtering
**Status:** TBD

#### TC-15: Flag Overrides Config
```bash
export IMPARTUS_SKIP_NO_AUDIO=true
# Run without flag - should filter
# Run with --skip-no-audio=false - should NOT filter
./impartus download -s 3176268 -S 1508 --skip-no-audio=false --start 1 --end 10 --json
```
**Note:** Need to verify if --skip-no-audio=false works as override
**Status:** TBD

---

### Phase 5: Interactive Mode Tests

#### TC-16: Interactive Mode Prompt
```bash
./impartus
# Select DSP course
# Observe prompt for skipping no-audio lectures
```
**Expected:** Prompt appears after "Skip lectures with titles..." prompt
**Prompt text:** "Skip lectures without audio track? [Y/n]:"
**Status:** TBD

#### TC-17: Interactive Mode - Skip No-Audio
```bash
./impartus
# Select DSP course
# Select lecture range 1-35
# Answer "Y" to skip no-audio
# Answer "n" to skip empty lectures (or default)
```
**Expected:** Downloads only lectures with audio
**Status:** TBD

#### TC-18: Interactive Mode - Don't Skip No-Audio
```bash
./impartus
# Select DSP course
# Select lecture range 26-31
# Answer "n" to skip no-audio
```
**Expected:** Attempts to download all selected lectures
**Status:** TBD

#### TC-19: Interactive Mode - All Filtered Results
```bash
./impartus
# Select DSP course
# Select lecture range 26-31
# Answer "Y" to skip no-audio
```
**Expected:** Error message about no lectures available after filtering
**Status:** TBD

---

### Phase 6: API Server Tests

#### TC-20: API Job Creation with skipNoAudio
```bash
curl -X POST http://localhost:8080/api/v1/jobs \
  -H "Authorization: Bearer <token>" \
  -H 'Content-Type: application/json' \
  -d '{
    "subjectId": 3176268,
    "sessionId": 1508,
    "startIndex": 1,
    "endIndex": 15,
    "jobConfig": {
      "skipNoAudio": true
    }
  }'
```
**Expected:** Job created with skipNoAudio=true in config
**Status:** TBD

#### TC-21: API Job Response Includes skipNoAudio
```bash
curl http://localhost:8080/api/v1/jobs/<job-id> \
  -H "Authorization: Bearer <token>"
```
**Expected:** Response includes `"skipNoAudio": true` in config section
**Status:** TBD

#### TC-22: API Job Response Includes filteredLectures
**Expected:** After job completes/fails, response includes `"filteredLectures": <count>`
**Status:** TBD

#### TC-23: API - All Lectures Filtered
```bash
# Create job with range that has all noaudio lectures
curl -X POST http://localhost:8080/api/v1/jobs \
  -H "Authorization: Bearer <token>" \
  -H 'Content-Type: application/json' \
  -d '{
    "subjectId": 3176268,
    "sessionId": 1508,
    "startIndex": 26,
    "endIndex": 31,
    "jobConfig": {
      "skipNoAudio": true
    }
  }'
```
**Expected:** Job fails with error about no lectures available after filtering
**Status:** TBD

---

## Edge Cases to Verify

### EC-01: Noaudio Field Is 0 vs 1
Verify the lecture API returns `noaudio` as integer (0 or 1), not string.

### EC-02: nil SkipNoAudio in API
If API request doesn't include skipNoAudio, it should default to false.

### EC-03: Interaction with Empty Lecture Filter
Test that skip-no-audio works together with skip-empty (filtering "No class"/"No lecture" topics).

### EC-04: Lecture Count After Both Filters
```bash
./impartus download -s 3176268 -S 1508 --skip-no-audio --start 1 --end 35
```
**Expected:** Filters applied: empty topics + noaudio lectures
**Status:** TBD

### EC-05: WebSocket Events
If using API with WebSocket, verify job.progress events reflect correct total lectures after filtering.

---

## Potential Rough Corners

### RC-01: Boolean Flag Override
Currently the flag is implemented as a boolean with no way to explicitly disable via CLI. If user has `skipNoAudio: true` in config, there's no `--skip-no-audio=false` option to override.

**Suggestion:** Consider using a three-state approach or separate `--include-noaudio` flag.

### RC-02: Missing Unit Test for Interactive Mode Filter
The interactive mode filtering logic needs unit test coverage.

### RC-03: Server-side Filter Count Not Persisted
The `FilteredLectures` count is set on the Job struct but may not persist correctly after job completion.

### RC-04: Config Validation
Need to verify `SkipNoAudio` doesn't cause validation errors when parsed.

---

## Verification Commands

```bash
# Build the binary
cd /home/ravish/Desktop/clawds-code-crib/impartus-go/impartus && go build -o impartus .

# List lectures and check noaudio field
./impartus lectures -s 3176268 -S 1508 --json | jq '.data[] | {seqNo, topic, noaudio}'

# Test with noaudio filter
./impartus download -s 3176268 -S 1508 --skip-no-audio --start 1 --end 35 --json

# Test without filter (control)
./impartus download -s 3176268 -S 1508 --start 1 --end 35 --json
```

---

## Success Criteria

1. All CLI flags parse correctly
2. Filter correctly excludes lectures with noaudio=1
3. JSON output includes filteredCount and totalLectures
4. Error message is clear when all lectures are filtered
5. Interactive mode prompts for skip-no-audio
6. API accepts skipNoAudio in jobConfig
7. API job response includes filteredLectures count
8. Config file option works
9. Environment variable works
10. Edge cases handled gracefully
