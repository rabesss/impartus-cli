//go:build !windows

package library

import (
	"fmt"
	"os"
)

func validatePrivateDirectoryPermissions(_ string, info os.FileInfo) error {
	if info.Mode().Perm() != 0o700 {
		return fmt.Errorf("library state directory must have mode 0700, got %04o", info.Mode().Perm())
	}
	return nil
}

func validatePrivateDatabasePermissions(_ string, info os.FileInfo) error {
	if info.Mode().Perm() != 0o600 {
		return fmt.Errorf("library database must have mode 0600, got %04o", info.Mode().Perm())
	}
	return nil
}

func secureNewDatabaseFile(file *os.File) error {
	return file.Chmod(0o600)
}

func secureNewStateDirectory(string) error { return nil }
