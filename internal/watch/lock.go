package watch

import (
	"errors"

	"github.com/rabesss/impartus-cli/internal/lockfile"
)

// ErrWatcherRunning reports that another process owns the advisory lock.
var ErrWatcherRunning = errors.New("another watcher is already running")

// Lock is an OS-owned advisory lock. The small on-disk file is not state: the
// kernel releases ownership when the descriptor closes or the process dies.
type Lock = lockfile.Lock

// AcquireLock obtains the non-blocking single-watcher lock at path.
func AcquireLock(path string) (*Lock, error) {
	lock, err := lockfile.TryAcquire(path)
	if errors.Is(err, lockfile.ErrLocked) {
		return nil, ErrWatcherRunning
	}
	return lock, err
}
