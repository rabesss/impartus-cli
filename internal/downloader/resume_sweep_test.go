package downloader

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/lockfile"
)

func TestSweepResumeWorkspaces(t *testing.T) {
	base := t.TempDir()
	now := time.Now()
	makeWorkspace := func(name, fingerprint string, age time.Duration) string {
		t.Helper()
		dir := filepath.Join(base, name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if fingerprint != "" {
			meta := filepath.Join(dir, resumeMetaName)
			data := fmt.Sprintf(`{"version":1,"fingerprint":%q}`, fingerprint)
			if err := os.WriteFile(meta, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(meta, now.Add(-age), now.Add(-age)); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	const old, recent = 8 * 24 * time.Hour, 6 * 24 * time.Hour
	makeWorkspace("lecture-1-resume-0000000000000001", "fp1", old)           // stale: removed
	makeWorkspace("lecture-2-resume-0000000000000002", "fp2", recent)        // fresh: kept
	locked := makeWorkspace("lecture-3-resume-0000000000000003", "fp3", old) // stale but in use: kept
	makeWorkspace("lecture-4-resume-0000000000000004", "", old)              // no metadata: kept
	makeWorkspace(".lecture-5-trash-00000000000000aa", "", 0)                // trash: removed
	makeWorkspace("lecture-7-resume-0000000000000007", "fp7", old)           // current: kept
	makeWorkspace("lecture-9-resume-0000000000000009", "fp9-old", recent)    // same lecture, old playlist: removed
	makeWorkspace("lecture-10-resume-000000000000000a", "fp10", recent)      // other lecture: kept
	makeWorkspace("unrelated", "fpx", old)                                   // not ours: kept
	symlinkTarget := makeWorkspace(filepath.Join("..", filepath.Base(t.TempDir()), "target"), "fp6", old)
	if err := os.Symlink(symlinkTarget, filepath.Join(base, "lecture-6-resume-0000000000000006")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	lock, err := lockfile.TryAcquire(filepath.Join(locked, resumeLockName))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }() //nolint:errcheck

	var logs strings.Builder
	d := NewWithDiagnosticWriter(nil, nil, &logs)
	d.sweepResumeWorkspaces(base, 9, "fp9-new", "lecture-7-resume-0000000000000007", now)

	want := []string{
		"lecture-10-resume-000000000000000a",
		"lecture-2-resume-0000000000000002",
		"lecture-3-resume-0000000000000003",
		"lecture-4-resume-0000000000000004",
		"lecture-6-resume-0000000000000006",
		"lecture-7-resume-0000000000000007",
		"unrelated",
	}
	if got := dirNames(t, base); !slicesEqual(got, want) {
		t.Fatalf("after sweep = %v, want %v (logs: %s)", got, want, logs.String())
	}
	if _, err := os.Stat(filepath.Join(symlinkTarget, resumeMetaName)); err != nil {
		t.Fatalf("symlink target was touched: %v", err)
	}
}

// When the workspace cannot be renamed away (Windows refuses while the lock
// is open), it is emptied under the lock instead of being renamed after the
// lock is released, which could remove a newer download's chunks.
func TestDiscardWorkspaceEmptiesItWhenRenameFails(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs Unix permissions that bind the current user")
	}
	base := t.TempDir()
	dir := filepath.Join(base, "lecture-5-resume-0000000000000005")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{resumeMetaName, "5_left_0000.ts", "5_left_0001.ts.part"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	lock, err := lockfile.TryAcquire(filepath.Join(dir, resumeLockName))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(base, 0o500); err != nil { // renames in base now fail
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(base, 0o700) }) //nolint:errcheck

	New(nil, nil).discardWorkspace(dir, 5, lock)

	if got := dirNames(t, dir); !slicesEqual(got, []string{resumeLockName, resumeMetaName}) {
		t.Fatalf("workspace after failed rename = %v, want only lock and metadata", got)
	}
	relock, err := lockfile.TryAcquire(filepath.Join(dir, resumeLockName))
	if err != nil {
		t.Fatalf("lock still held after discard: %v", err)
	}
	_ = relock.Close() //nolint:errcheck
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
