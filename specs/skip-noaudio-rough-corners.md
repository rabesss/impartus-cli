# Skip No-Audio Implementation: Rough Corners & Improvements

## Identified Issues

### 1. Cannot Disable via CLI When Config is True ⚠️ RESOLVED

**Issue:** If `skipNoAudio: true` is set in config.json, there's no way to override it to `false` via CLI flags.

**Current behavior:**
```bash
# Config has skipNoAudio: true
# No CLI flag to disable - only --skip-no-audio to enable
./impartus download -s 3176268 -S 1508 --start 1 --end 10
# This WILL skip noaudio lectures (from config)
```

**Problem:** Users may want to download no-audio lectures intentionally (e.g., for frame extraction) but cannot disable the config setting.

**Solution options:**
1. Add `--include-noaudio` flag that explicitly enables downloading noaudio lectures
2. Add `--skip-noaudio=true|false` as a flag with explicit value
3. Document that users should remove the config option if they want different behavior

**Resolution:** Added `--include-noaudio` flag (see `specs/skip-noaudio-fixes-spec.md`)

---

### 2. Interactive Mode Error Message Is Generic ⚠️ RESOLVED

**Issue:** In interactive mode, when all lectures are filtered out, the error message is generic:

```go
return errors.New("no lectures available after filtering")
```

This doesn't specify WHY filtering occurred (empty topics? noaudio? both?).

**Better message:**
```go
return fmt.Errorf("no lectures available after filtering (empty topics: %d, noaudio: %d)", 
    emptyCount, noaudioCount)
```

**Resolution:** Improved error messages (see `specs/skip-noaudio-fixes-spec.md`)

---

### 3. Missing Unit Test for Interactive Mode

**Issue:** The interactive mode filtering logic is not covered by unit tests.

**Need to add tests for:**
- `TestRunInteractiveSkipsNoAudio` (mock the prompts)
- `TestRunInteractiveWithBothFilters`
- `TestRunInteractiveAllFiltered`

---

### 4. API: FilteredLectures Not Persisted Correctly

**Issue:** In `selectJobLectures`, the `FilteredLectures` count is set on the job:

```go
job.FilteredLectures = totalLectures - len(selected)
```

But this mutation happens inside a function that doesn't return the job. The job is modified in-place, but need to verify this persists correctly.

**Verification needed:** Check if `job.FilteredLectures` is accessible after `selectJobLectures` returns.

---

### 5. No Warning When Downloading NoAudio Lectures ⚠️ RESOLVED

**Issue:** When downloading without `--skip-no-audio`, there's no warning that some lectures have no audio.

**Suggestion:** Add a warning log when noaudio lectures are detected:

```go
if hasNoAudioLectures(selected) {
    fmt.Printf("[WARNING] %d lectures in selection have noaudio=1\n", count)
    fmt.Printf("[INFO] Use --skip-no-audio to filter these out\n")
}
```

**Resolution:** Added warning message (see `specs/skip-noaudio-fixes-spec.md`)

---

### 6. Config File Not Updated for User

**Issue:** The user has `config.json` with credentials, but it doesn't have the new `skipNoAudio` field.

**Note:** The sample.config.json is updated, but existing config.json won't have this option. This is fine - it defaults to false.

---

### 7. Environment Variable Handling in API

**Issue:** The API server doesn't support environment variable overrides for `skipNoAudio` at runtime.

**Current:** Only config file supports `skipNoAudio`.

**Future consideration:** Add `IMPARTUS_SKIP_NO_AUDIO` support to API server (currently only CLI supports this).

---

### 8. Missing Test for Duplicate Filter Application

**Issue:** Need to verify filter is only applied once (not duplicated if both config and flag are set).

**Current logic:**
```go
if *skipNoAudio || cfg.SkipNoAudio {
    selected = filterNoAudioLectures(selected)
}
```

This is correct - filter is applied once, not twice. Good.

---

### 9. WebSocket Events Don't Include Filter Info

**Issue:** When using WebSocket for job progress, the events don't indicate how many lectures were filtered.

**Suggestion:** Add `filteredLectures` to job.progress WebSocket events:

```json
{
  "type": "job.progress",
  "jobId": "...",
  "progress": 50.0,
  "phase": "downloading",
  "filteredLectures": 3
}
```

---

### 10. No Test for Server Filter Count

**Issue:** API test TC-22 needs to verify `filteredLectures` is returned in job status.

**Not implemented yet:** Need integration test for this.

---

## Minor Improvements

### A. Help Text Alignment
The help text has inconsistent spacing:

```go
fmt.Println("  --subject,-s       Subject ID")
fmt.Println("  --session,-S       Session ID")
// vs
fmt.Println("  --audio-only   Audio-only mode")
```

Consider aligning all flags for better readability.

### B. JSON Field Names
The JSON output uses `filteredCount` and `totalLectures`, which are descriptive. Good.

### C. Config Field Name Consistency
`skipNoAudio` in config vs `SkipNoAudio` in code struct. This is Go convention (unexported JSON), so it's fine.

---

## Recommendations Priority

| Priority | Issue | Effort |
|----------|-------|--------|
| HIGH | Cannot disable via CLI when config is true | Medium |
| HIGH | Missing unit tests for interactive mode | Low |
| MEDIUM | Add warning for noaudio lectures | Low |
| MEDIUM | Better error message in interactive mode | Low |
| LOW | WebSocket filter info | Medium |
| LOW | Align help text | Low |

---

## Test Coverage Needed

1. **Unit tests:**
   - `TestFilterNoAudioLectures` ✓ (added)
   - `TestApplyAndValidateFlagsWithSkipNoAudio` (new)
   - `TestExecuteDownloadWithSkipNoAudio` (new, with mocks)

2. **Integration tests:**
   - CLI with real API (requires credentials)
   - API with real download

---

## Summary

The implementation is solid and follows existing patterns. The main concerns are:

1. **UX gap:** Cannot disable via CLI when config enables it
2. **Test coverage:** Interactive mode not tested
3. **User feedback:** No warning when downloading noaudio lectures

These are enhancement opportunities rather than bugs.
