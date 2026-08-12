package watch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ErrWatcherRunning reports that another process owns the advisory lock.
var ErrWatcherRunning = errors.New("another watcher is already running")

// Lock is an OS-owned advisory lock. The small on-disk file is not state: the
// kernel releases ownership when the descriptor closes or the process dies.
type Lock struct {
	mu     sync.Mutex
	file   *os.File
	closed bool
}

// AcquireLock obtains the non-blocking single-watcher lock at path.
func AcquireLock(path string) (*Lock, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil, errors.New("watch lock path is required")
	}
	absolute, resolveErr := filepath.Abs(filepath.Clean(trimmed))
	if resolveErr != nil {
		return nil, fmt.Errorf("resolve watch lock path: %w", resolveErr)
	}
	parent := filepath.Dir(absolute)
	if mkdirErr := os.MkdirAll(parent, 0o700); mkdirErr != nil {
		return nil, fmt.Errorf("create watch lock directory: %w", mkdirErr)
	}
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		return nil, fmt.Errorf("inspect watch lock directory: %w", err)
	}
	if parentInfo.Mode()&os.ModeSymlink != 0 || !parentInfo.IsDir() {
		return nil, errors.New("watch lock directory must be a real directory")
	}
	if info, statErr := os.Lstat(absolute); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("watch lock path must not be a symlink")
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect watch lock path: %w", statErr)
	}
	file, err := openAndTryLock(absolute)
	if err != nil {
		return nil, err
	}
	return &Lock{file: file}, nil
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
