// Package lockfile provides a non-blocking, kernel-released advisory lock.
package lockfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ErrLocked reports that another process already owns the advisory lock.
var ErrLocked = errors.New("lockfile already held")

// Lock is an OS-owned advisory lock. The small on-disk file is not state: the
// kernel releases ownership when the descriptor closes or the process dies.
type Lock struct {
	mu     sync.Mutex
	file   *os.File
	closed bool
}

// SanitizeName replaces ":" so an artifact ID can be a lock filename.
func SanitizeName(id string) string {
	return strings.ReplaceAll(strings.TrimSpace(id), ":", "_")
}

// Path is directory plus SanitizeName(id) with a .lock suffix.
func Path(directory, id string) string {
	return filepath.Join(directory, SanitizeName(id)+".lock")
}

// TryAcquire takes the exclusive lock at path or returns ErrLocked.
func TryAcquire(path string) (*Lock, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil, errors.New("lock path is required")
	}
	absolute, resolveErr := filepath.Abs(filepath.Clean(trimmed))
	if resolveErr != nil {
		return nil, fmt.Errorf("resolve lock path: %w", resolveErr)
	}
	parent := filepath.Dir(absolute)
	if mkdirErr := os.MkdirAll(parent, 0o700); mkdirErr != nil {
		return nil, fmt.Errorf("create lock directory: %w", mkdirErr)
	}
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		return nil, fmt.Errorf("inspect lock directory: %w", err)
	}
	if parentInfo.Mode()&os.ModeSymlink != 0 || !parentInfo.IsDir() {
		return nil, errors.New("lock directory must be a real directory")
	}
	if info, statErr := os.Lstat(absolute); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("lock path must not be a symlink")
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect lock path: %w", statErr)
	}
	file, err := openAndTryLock(absolute)
	if err != nil {
		return nil, err
	}
	return &Lock{file: file}, nil
}

// Acquire retries TryAcquire until wait elapses. wait <= 0 tries once.
func Acquire(path string, wait time.Duration) (*Lock, error) {
	deadline := time.Now().Add(wait)
	for {
		lock, err := TryAcquire(path)
		if err == nil || !errors.Is(err, ErrLocked) {
			return lock, err
		}
		if wait <= 0 || !time.Now().Before(deadline) {
			return nil, ErrLocked
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Close releases lock ownership. It is safe to call more than once.
func (lock *Lock) Close() error {
	if lock == nil {
		return nil
	}
	lock.mu.Lock()
	defer lock.mu.Unlock()
	if lock.closed {
		return nil
	}
	lock.closed = true
	if lock.file == nil {
		return nil
	}
	return errors.Join(unlockFile(lock.file), lock.file.Close())
}
