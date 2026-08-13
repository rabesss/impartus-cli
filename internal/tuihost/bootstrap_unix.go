//go:build !windows

package tuihost

import (
	"fmt"
	"os"
	"syscall"
)

func secureBootstrapDirectory(path string) error {
	if err := os.Chmod(path, 0o700); err != nil { // #nosec G302 -- owner-only directory permissions are intentional.
		return fmt.Errorf("secure private OpenTUI bootstrap directory: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect private OpenTUI bootstrap directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		return errorsForUnsafeBootstrap("directory")
	}
	return validateBootstrapOwner(info, "directory")
}

func secureBootstrapFile(file *os.File) error {
	if file == nil {
		return errorsForUnsafeBootstrap("file")
	}
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("secure private OpenTUI bootstrap: %w", err)
	}
	return nil
}

func validateBootstrapFile(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect private OpenTUI bootstrap descriptor: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return errorsForUnsafeBootstrap("file")
	}
	if ownerErr := validateBootstrapOwner(info, "file"); ownerErr != nil {
		return ownerErr
	}
	pathInfo, err := os.Lstat(file.Name())
	if err != nil {
		return fmt.Errorf("inspect private OpenTUI bootstrap path: %w", err)
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, pathInfo) {
		return errorsForUnsafeBootstrap("file path")
	}
	return nil
}

func validateBootstrapOwner(info os.FileInfo, label string) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || os.Geteuid() < 0 {
		return fmt.Errorf("cannot verify private OpenTUI bootstrap %s owner", label)
	}
	// #nosec G115 -- effective UID is non-negative and Unix UIDs are uint32.
	if stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("private OpenTUI bootstrap %s must be owned by the current user", label)
	}
	return nil
}

func errorsForUnsafeBootstrap(label string) error {
	return fmt.Errorf("private OpenTUI bootstrap %s has unsafe type or permissions", label)
}
