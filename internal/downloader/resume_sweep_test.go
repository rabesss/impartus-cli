package downloader

import (
	"fmt"
	"net/http"
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

	info, err := os.Lstat(dir)
	if err != nil {
		t.Fatal(err)
	}
	New(nil, nil).discardWorkspace(dir, info, 5, lock)

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

// A key change must make the chunk removals durable before resume.json
// records the new key, or a power loss could pair old chunks with it.
func TestKeyChangeSyncsChunkRemovalBeforeNewKeyHash(t *testing.T) {
	paths := []string{"/c0.ts", "/c1.ts", "/c2.ts"}
	h := newResumeHarness(t, false, 3)
	h.server.fail["/c1.ts"] = http.StatusNotFound
	if _, err := h.run(t, h.server.playlist(paths...), nil); err == nil {
		t.Fatal("first run error = nil, want failure")
	}
	dirs, err := filepath.Glob(filepath.Join(h.d.config.TempDirLocation, "lecture-*-resume-*"))
	if err != nil || len(dirs) != 1 {
		t.Fatalf("resume workspaces = %v (%v), want one", dirs, err)
	}
	oldMeta, _, err := readResumeMeta(dirs[0])
	if err != nil || oldMeta.KeyHash == "" {
		t.Fatalf("readResumeMeta() = %+v, %v; want a recorded key hash", oldMeta, err)
	}

	var synced int
	saved := syncWorkspaceDirHook
	syncWorkspaceDirHook = func(dir string) error {
		synced++
		if chunks, _ := filepath.Glob(filepath.Join(dir, "*.ts")); len(chunks) != 0 {
			t.Errorf("chunks %v still present at the directory sync", chunks)
		}
		if meta, _, err := readResumeMeta(dir); err != nil || meta.KeyHash != oldMeta.KeyHash {
			t.Errorf("resume.json key hash changed before the directory sync: %+v, %v", meta, err)
		}
		return saved(dir)
	}
	t.Cleanup(func() { syncWorkspaceDirHook = saved })

	delete(h.server.fail, "/c1.ts")
	h.server.mu.Lock()
	h.server.key = []byte("6543210987654321")
	h.server.mu.Unlock()
	if _, err := h.run(t, h.server.playlist(paths...), nil); err != nil {
		t.Fatalf("second run error = %v", err)
	}
	if synced != 1 {
		t.Fatalf("directory syncs = %d, want 1 for the key change", synced)
	}
}

// A workspace renamed away and replaced between validation and locking must
// not be adopted, even when the replacement is private to this user.
func TestResumeRefusesWorkspaceReplacedBeforeLock(t *testing.T) {
	paths := []string{"/c0.ts", "/c1.ts"}
	h := newResumeHarness(t, false, 2)
	h.server.fail["/c1.ts"] = http.StatusNotFound
	if _, err := h.run(t, h.server.playlist(paths...), nil); err == nil {
		t.Fatal("first run error = nil, want failure")
	}
	delete(h.server.fail, "/c1.ts")

	var replacement string
	beforeWorkspaceLockHook = func(dir string) {
		replacement = dir
		if err := os.Rename(dir, dir+".moved"); err != nil {
			t.Error(err)
		}
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Error(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "marker"), []byte("x"), 0o600); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { beforeWorkspaceLockHook = nil })

	if _, err := h.run(t, h.server.playlist(paths...), nil); err != nil {
		t.Fatalf("second run error = %v, want success in a one-off workspace", err)
	}
	if replacement == "" {
		t.Fatal("hook never ran")
	}
	if _, err := os.Stat(filepath.Join(replacement, "marker")); err != nil {
		t.Fatalf("replacement workspace was cleared: %v", err)
	}
	if _, err := os.Stat(filepath.Join(replacement, resumeMetaName)); !os.IsNotExist(err) {
		t.Fatalf("replacement workspace was adopted (resume.json: %v)", err)
	}
}

// A workspace swapped for a symlink after it was locked must never be
// cleared through the link.
func TestKeyChangeRefusesToClearSwappedWorkspace(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "lecture-9-resume-0000000000000009")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	ident, err := os.Lstat(dir)
	if err != nil {
		t.Fatal(err)
	}
	state := &resumeState{dir: dir, ident: ident, meta: resumeMeta{Version: resumeFormatVersion, Fingerprint: "f", KeyHash: "old"}}
	victimDir := filepath.Join(base, "victim")
	if err := os.Mkdir(victimDir, 0o700); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(victimDir, "keep.txt")
	if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(dir, dir+".moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victimDir, dir); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, err := state.verifyKey([]byte("0123456789abcdef")); err == nil {
		t.Fatal("verifyKey() error = nil, want a replaced-workspace error")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("file behind the swapped workspace was removed: %v", err)
	}
}

// Clearing is bound to the validated directory itself, so a directory or
// symlink swapped in at the path after every check is never emptied.
func TestClearWorkspaceIgnoresSwapAfterChecks(t *testing.T) {
	for _, swap := range []string{"symlink", "directory"} {
		t.Run(swap, func(t *testing.T) {
			base := t.TempDir()
			dir := filepath.Join(base, "lecture-9-resume-0000000000000009")
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			ident, err := os.Lstat(dir)
			if err != nil {
				t.Fatal(err)
			}
			victimDir := filepath.Join(base, "victim")
			if err := os.Mkdir(victimDir, 0o700); err != nil {
				t.Fatal(err)
			}
			victim := filepath.Join(victimDir, "keep.txt")
			if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(dir, dir+".moved"); err != nil {
				t.Fatal(err)
			}
			if swap == "symlink" {
				if err := os.Symlink(victimDir, dir); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			} else if err := os.Rename(victimDir, dir); err != nil {
				t.Fatal(err)
			} else {
				victim = filepath.Join(dir, "keep.txt")
			}

			if err := clearWorkspace(dir, ident, false); err == nil {
				t.Fatal("clearWorkspace() error = nil, want a replaced-workspace error")
			}
			if _, err := os.Stat(victim); err != nil {
				t.Fatalf("file in the swapped-in directory was removed: %v", err)
			}
		})
	}
}
