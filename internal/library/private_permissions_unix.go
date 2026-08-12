//go:build !windows

package library

import (
	"fmt"
	"os"
	"syscall"
)

func validatePrivateDirectoryPermissions(path string, info os.FileInfo) error {
	if info.Mode().Perm() != 0o700 {
		return fmt.Errorf("library state directory must have mode 0700, got %04o", info.Mode().Perm())
	}
	return validatePrivateOwner(path, info)
}

func validatePrivateDatabasePermissions(path string, info os.FileInfo) error {
	if info.Mode().Perm() != 0o600 {
		return fmt.Errorf("library database must have mode 0600, got %04o", info.Mode().Perm())
	}
	return validatePrivateOwner(path, info)
}

func validatePrivateOwner(path string, info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("cannot verify owner of private library path %q", path)
	}
	effectiveUID := os.Geteuid()
	if effectiveUID < 0 {
		return fmt.Errorf("cannot determine current owner for private library path %q", path)
	}
	// #nosec G115 -- effectiveUID is checked non-negative and Unix UIDs are uint32 values.
	if stat.Uid != uint32(effectiveUID) {
		return fmt.Errorf("private library path %q must be owned by the current user", path)
	}
	return nil
}

func secureNewDatabaseFile(file *os.File) error {
	return file.Chmod(0o600)
}

func secureNewStateDirectory(string) error { return nil }
