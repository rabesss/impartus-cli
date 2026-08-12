//go:build !windows

package watch

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func openAndTryLock(path string) (*os.File, error) {
	descriptor, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open watch lock: %w", err)
	}
	file := os.NewFile(uintptr(descriptor), path)
	if err := unix.Flock(descriptor, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		closeErr := file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, errors.Join(ErrWatcherRunning, closeErr)
		}
		return nil, errors.Join(fmt.Errorf("acquire watch lock: %w", err), closeErr)
	}
	return file, nil
}

func unlockFile(file *os.File) error {
	if err := unix.Flock(int(file.Fd()), unix.LOCK_UN); err != nil {
		return fmt.Errorf("release watch lock: %w", err)
	}
	return nil
}
