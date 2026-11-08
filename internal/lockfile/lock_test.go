package lockfile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestTryAcquireAllowsExactlyOneOwnerAndKernelRelease(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "artifact.lock")
	first, err := TryAcquire(path)
	if err != nil {
		t.Fatalf("TryAcquire(first) error = %v", err)
	}
	second, err := TryAcquire(path)
	if !errors.Is(err, ErrLocked) {
		if second != nil {
			if closeErr := second.Close(); closeErr != nil {
				t.Errorf("Close(unexpected second lock) error = %v", closeErr)
			}
		}
		t.Fatalf("TryAcquire(second) error = %v, want ErrLocked", err)
	}
	if closeErr := first.Close(); closeErr != nil {
		t.Fatalf("Close(first) error = %v", closeErr)
	}
	restarted, err := TryAcquire(path)
	if err != nil {
		t.Fatalf("TryAcquire(after close) error = %v", err)
	}
	if err := restarted.Close(); err != nil {
		t.Fatalf("Close(restarted) error = %v", err)
	}
}

func TestTryAcquireRejectsEmptyPath(t *testing.T) {
	t.Parallel()

	if _, err := TryAcquire(" "); err == nil {
		t.Fatal("TryAcquire(empty) error = nil")
	}
}

func TestSanitizeNameReplacesColons(t *testing.T) {
	t.Parallel()

	id := "impartus:v1:abc-DEF_123"
	got := SanitizeName(id)
	if got != "impartus_v1_abc-DEF_123" {
		t.Fatalf("SanitizeName(%q) = %q", id, got)
	}
	dir := t.TempDir()
	path := Path(dir, id)
	if filepath.Base(path) != "impartus_v1_abc-DEF_123.lock" {
		t.Fatalf("Path() = %q", path)
	}
}

func TestAcquireFallsBackAfterDeadline(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "busy.lock")
	held, err := TryAcquire(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := held.Close(); closeErr != nil {
			t.Errorf("Close(held) error = %v", closeErr)
		}
	})

	started := time.Now()
	lock, err := Acquire(context.Background(), path, 80*time.Millisecond)
	elapsed := time.Since(started)
	if lock != nil {
		if closeErr := lock.Close(); closeErr != nil {
			t.Errorf("Close(unexpected waiter) error = %v", closeErr)
		}
	}
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("Acquire() error = %v, want ErrLocked", err)
	}
	if elapsed < 80*time.Millisecond {
		t.Fatalf("Acquire returned after %s, want at least the wait deadline", elapsed)
	}
}

func TestAcquireZeroWaitTriesOnce(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "once.lock")
	held, err := TryAcquire(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := held.Close(); closeErr != nil {
			t.Errorf("Close(held) error = %v", closeErr)
		}
	})

	started := time.Now()
	lock, err := Acquire(context.Background(), path, 0)
	if lock != nil {
		if closeErr := lock.Close(); closeErr != nil {
			t.Errorf("Close(unexpected waiter) error = %v", closeErr)
		}
	}
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("Acquire(0) error = %v, want ErrLocked", err)
	}
	if time.Since(started) > 50*time.Millisecond {
		t.Fatalf("Acquire(0) waited %s", time.Since(started))
	}
}

func TestAcquireReturnsWhenContextIsCanceled(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "cancel.lock")
	held, err := TryAcquire(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := held.Close(); closeErr != nil {
			t.Errorf("Close(held) error = %v", closeErr)
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	started := time.Now()
	lock, err := Acquire(ctx, path, time.Second)
	if lock != nil {
		if closeErr := lock.Close(); closeErr != nil {
			t.Errorf("Close(unexpected waiter) error = %v", closeErr)
		}
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Acquire() error = %v, want context.Canceled", err)
	}
	if time.Since(started) > 200*time.Millisecond {
		t.Fatalf("Acquire ignored cancellation for %s", time.Since(started))
	}
}

func TestHoldsPathDetectsMovedOrReplacedFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot rename a directory holding an open file")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "workspace", ".lock")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	lock, err := TryAcquire(path)
	if err != nil {
		t.Fatalf("TryAcquire() error = %v", err)
	}
	if !lock.HoldsPath(path) {
		t.Fatal("HoldsPath() = false right after acquiring")
	}

	moved := filepath.Join(dir, "trash")
	if err := os.Rename(filepath.Dir(path), moved); err != nil {
		t.Fatal(err)
	}
	if lock.HoldsPath(path) {
		t.Fatal("HoldsPath() = true after its directory was moved away")
	}
	if !lock.HoldsPath(filepath.Join(moved, ".lock")) {
		t.Fatal("HoldsPath() = false for the file's new location")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if lock.HoldsPath(path) {
		t.Fatal("HoldsPath() = true for a different file at the same path")
	}

	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if lock.HoldsPath(filepath.Join(moved, ".lock")) {
		t.Fatal("HoldsPath() = true after Close")
	}
	var nilLock *Lock
	if nilLock.HoldsPath(path) {
		t.Fatal("nil HoldsPath() = true")
	}
}
