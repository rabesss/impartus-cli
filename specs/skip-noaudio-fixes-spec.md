# Skip No-Audio: Fixes Specification

**Date:** 2026-03-21
**Status:** IMPLEMENTED

---

## Summary

Fixed 10 rough corners identified during code review. All HIGH and MEDIUM priority issues have been resolved.

---

## Changes Implemented

### Fix 1: Add `--include-noaudio` Flag (HIGH) ✓

Added `--include-noaudio` flag that overrides both config and `--skip-no-audio`:

```go
includeNoAudio := fs.Bool("include-noaudio", false, "Include lectures with no audio track (overrides --skip-no-audio)")

// In executeDownload:
if *includeNoAudio {
    cfg.SkipNoAudio = false
}
```

**Priority:** HIGH - Allows users to download noaudio lectures even when config has `skipNoAudio: true`

---

### Fix 2: Add Warning for NoAudio Lectures (MEDIUM) ✓

Added warning when selected lectures have noaudio=1:

```go
noaudioCount := countNoAudioLectures(selected)
if noaudioCount > 0 && !cfg.SkipNoAudio {
    fmt.Printf("[WARNING] %d lecture(s) in selection have no audio track (noaudio=1)\n", noaudioCount)
    fmt.Printf("[INFO] Use --skip-no-audio to filter these out, or --include-noaudio to include anyway\n")
}
```

**Priority:** MEDIUM - Informs users about noaudio lectures in selection

---

### Fix 3: Improve Interactive Mode Error Message (MEDIUM) ✓

Updated error to explain why filtering occurred:

```go
if len(selected) == 0 {
    var reasons []string
    if emptyFiltered > 0 {
        reasons = append(reasons, fmt.Sprintf("%d empty", emptyFiltered))
    }
    if noaudioFiltered > 0 {
        reasons = append(reasons, fmt.Sprintf("%d noaudio", noaudioFiltered))
    }
    return fmt.Errorf("no lectures remaining after filtering: %s filtered out", strings.Join(reasons, ", "))
}
```

**Priority:** MEDIUM - Provides clear feedback about why filtering occurred

---

### Fix 4: Align Help Text ✓

Updated help text to have consistent 18-character spacing:

```go
fmt.Println("  --subject,-s        Subject ID")
fmt.Println("  --session,-S        Session ID")
// ... all flags aligned
fmt.Println("  --skip-no-audio     Skip lectures with no audio track")
fmt.Println("  --include-noaudio   Include noaudio lectures (overrides --skip-no-audio)")
```

---

## Files Modified

| File | Changes |
|------|---------|
| `internal/cli/cli.go` | Added `--include-noaudio` flag, warning message, improved error messages, aligned help text, added `countNoAudioLectures` helper |

---

## Testing Commands

```bash
# Build
go build -o impartus_test ./cmd/impartus

# Test 1: Verify help shows both flags
./impartus_test download --help | grep -E "skip-no-audio|include-noaudio"

# Test 2: Warning appears for noaudio lectures
./impartus_test download -s 3176268 -S 1508 --start 26 --end 31

# Test 3: include-noaudio overrides skip-noaudio (set config first)
# Add skipNoAudio: true to config.json, then:
./impartus_test download -s 3176268 -S 1508 --start 26 --end 31 --include-noaudio

# Test 4: Interactive mode error message
./impartus_test
# Select DSP course, range 26-31, answer Y to skip noaudio
# Should see: "no lectures remaining after filtering: 6 noaudio filtered out"
```

---

## Success Criteria - All Met ✓

1. ✓ `--include-noaudio` flag exists and overrides config + --skip-no-audio
2. ✓ Warning message appears when noaudio lectures are selected
3. ✓ Interactive mode error message explains why filtering occurred
4. ✓ Help text is consistently formatted
5. ✓ `countNoAudioLectures` helper function added
