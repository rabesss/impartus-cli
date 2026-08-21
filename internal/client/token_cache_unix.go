//go:build !windows

package client

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func readTokenCacheFile(path string) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		return nil, unix.Close(fd)
	}
	defer func() { _ = file.Close() }() //nolint:errcheck
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("token cache path must be a regular file, not a symlink or special file")
	}
	if permissions := info.Mode().Perm(); permissions&0o077 != 0 {
		return nil, fmt.Errorf("token cache permissions are %04o, want 0600 or stricter", permissions)
	}
	return io.ReadAll(file)
}

func createTokenCacheTemp(parent, path string) (*os.File, error) {
	temp, err := os.CreateTemp(parent, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return nil, err
	}
	if err := temp.Chmod(tokenCacheFileMode); err != nil {
		name := temp.Name()
		return nil, errors.Join(err, temp.Close(), os.Remove(name))
	}
	return temp, nil
}

func replaceTokenCacheFile(from, to string) error {
	return os.Rename(from, to)
}

func validatePublishedTokenCache(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("published token cache must be a regular file")
	}
	if permissions := info.Mode().Perm(); permissions != tokenCacheFileMode {
		return fmt.Errorf("published token cache permissions are %04o, want 0600", permissions)
	}
	return nil
}
