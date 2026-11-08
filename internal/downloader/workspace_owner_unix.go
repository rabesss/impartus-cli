//go:build !windows

package downloader

import (
	"errors"
	"os"
	"syscall"
)

// validatePrivateWorkspace rejects a resumable workspace another local user
// could have created or can write to, such as one planted in a shared /tmp.
func validatePrivateWorkspace(info os.FileInfo) error {
	if info.Mode().Perm()&0o077 != 0 {
		return errors.New("workspace is accessible to other users")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("cannot verify workspace owner")
	}
	uid := os.Geteuid()
	// #nosec G115 -- uid is checked non-negative and Unix UIDs are uint32 values.
	if uid < 0 || stat.Uid != uint32(uid) {
		return errors.New("workspace is owned by another user")
	}
	return nil
}
