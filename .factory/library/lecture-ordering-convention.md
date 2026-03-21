# Lecture Ordering Convention

## Summary

CLI and API surfaces use **different lecture orderings** for range selection. This is a known cross-surface discrepancy that must be considered when implementing features that span both surfaces.

## Details

### CLI Behavior
- Lectures are displayed in **reversed order** (newest first)
- `selectLectureRange()` in `internal/cli/cli.go:513` calls `reverseLectures()` before slicing
- `--start=1` refers to the **last lecture** (newest by date)

### API Behavior  
- Lectures are processed in **original order** (oldest first)
- `selectJobLectures()` in `internal/server/server.go:762` does NOT reverse lectures
- `startIndex=1` refers to the **first lecture** (oldest by date)

## Impact

This affects **VAL-CROSS-002**: "CLI range selection and API job ranges refer to the same lecture slice"

Currently, CLI `--start=1 --end=5` selects different lectures than API `startIndex=1, endIndex=5`.

## Resolution Required

Either:
1. Align API to CLI semantics (reverse lectures before selection in `selectJobLectures`)
2. Document the difference and use different index bases
3. Explicitly document that API indices are 1-based in original order while CLI indices are 1-based in reversed order

## Files Involved

- `internal/cli/cli.go:513` - `selectLectureRange()` with reversal
- `internal/server/server.go:762` - `selectJobLectures()` without reversal
