package watch

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestWatcherLockAllowsExactlyOneOwnerAndKernelRelease(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "watch.lock")
	first, err := AcquireLock(path)
	if err != nil {
		t.Fatalf("AcquireLock(first) error = %v", err)
	}
	second, err := AcquireLock(path)
	if !errors.Is(err, ErrWatcherRunning) {
		if second != nil {
			if closeErr := second.Close(); closeErr != nil {
				t.Errorf("Close(unexpected second lock) error = %v", closeErr)
			}
		}
		t.Fatalf("AcquireLock(second) error = %v, want ErrWatcherRunning", err)
	}
	if closeErr := first.Close(); closeErr != nil {
		t.Fatalf("Close(first) error = %v", closeErr)
	}
	restarted, err := AcquireLock(path)
	if err != nil {
		t.Fatalf("AcquireLock(after close) error = %v", err)
	}
	if err := restarted.Close(); err != nil {
		t.Fatalf("Close(restarted) error = %v", err)
	}
}

func TestWatcherLockRejectsEmptyPath(t *testing.T) {
	t.Parallel()

	if _, err := AcquireLock(" "); err == nil {
		t.Fatal("AcquireLock(empty) error = nil")
	}
}
