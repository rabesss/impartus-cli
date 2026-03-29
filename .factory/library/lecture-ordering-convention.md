# Lecture Ordering Convention

## Summary

Both CLI and API surfaces now use **reversed lecture ordering** (newest first) for range selection. The cross-surface discrepancy has been resolved.

## Details

### CLI Behavior
- Lectures are displayed in **reversed order** (newest first)
- `reverseLectures()` in `internal/cli/cli.go:732` reverses the slice before `selectLectureRange()` slices it
- `selectLectureRange()` in `internal/cli/cli.go:740` calls `reverseLectures()` before slicing
- `--start=1` refers to the **last lecture** (newest by date)

### API Behavior  
- Lectures are processed in **reversed order** (newest first)
- `selectJobLectures()` in `internal/server/server.go:1195` reverses lectures to match CLI behavior
- `startIndex=1` refers to the **last lecture** (newest by date)

## Impact

Both surfaces now align: CLI `--start=1 --end=5` selects the same lectures as API `startIndex=1, endIndex=5`.

## Files Involved

- `internal/cli/cli.go:732` - `reverseLectures()` helper
- `internal/cli/cli.go:740` - `selectLectureRange()` with reversal
- `internal/server/server.go:1195` - `selectJobLectures()` with reversal (aligned to CLI)
