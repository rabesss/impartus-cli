//go:build !windows

package client

import (
	"io"
	"os"

	"golang.org/x/sys/unix"
)

func readTokenCacheFile(path string) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		return nil, unix.Close(fd)
	}
	defer func() { _ = file.Close() }() //nolint:errcheck
	return io.ReadAll(file)
}
