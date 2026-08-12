//go:build !windows

package artifact

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func openCompletedFileDescriptor(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0) // #nosec G304 -- caller supplies a normalized output path
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		return nil, errors.Join(os.ErrInvalid, unix.Close(fd))
	}
	return file, nil
}
