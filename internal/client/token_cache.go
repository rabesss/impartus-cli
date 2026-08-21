package client

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rabesss/impartus-cli/internal/config"
)

const tokenCacheFileMode os.FileMode = 0o600

func resolvedTokenCachePath(cfg *config.Config) string {
	if cfg == nil {
		return config.DefaultTokenCachePath
	}
	path := strings.TrimSpace(cfg.TokenCachePath)
	if path == "" {
		return config.DefaultTokenCachePath
	}
	return path
}

// readTokenCache reads a token cache only after checking its immediate parent
// and the final entry. The platform-specific reader uses a
// no-follow open where the OS supports it, closing the check/use race for the
// final path component.
func readTokenCache(path string) ([]byte, error) {
	path, err := normalizeTokenCachePath(path)
	if err != nil {
		return nil, err
	}
	parent := filepath.Dir(path)
	parentErr := validateTokenCacheDirectory(parent)
	if parentErr != nil {
		return nil, parentErr
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, invalidTokenCacheTargetError()
	}
	return readTokenCacheFile(path)
}

// writeTokenCache writes an owner-private cache through a same-directory
// temporary file and platform-specific replacement. Existing symlinks and
// special files are rejected, and the temporary file is secured before token
// bytes are written.
func writeTokenCache(path string, token []byte) error {
	path, err := normalizeTokenCachePath(path)
	if err != nil {
		return err
	}
	parent := filepath.Dir(path)
	if mkdirErr := os.MkdirAll(parent, 0o700); mkdirErr != nil { // #nosec G301 -- token parent is intentionally private
		return fmt.Errorf("create token cache directory: %w", mkdirErr)
	}
	if parentErr := validateTokenCacheDirectory(parent); parentErr != nil {
		return parentErr
	}
	if targetErr := validateTokenCacheTarget(path); targetErr != nil {
		return targetErr
	}
	return writeTokenCacheFile(parent, path, token)
}

func writeTokenCacheFile(parent, path string, token []byte) error {
	temp, err := createTokenCacheTemp(parent, path)
	if err != nil {
		return fmt.Errorf("create token cache temporary file: %w", err)
	}
	tempPath := temp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath) //nolint:errcheck // best-effort cleanup after a failed write
		}
	}()

	written, writeErr := temp.Write(token)
	if writeErr == nil && written != len(token) {
		writeErr = io.ErrShortWrite
	}
	if writeErr != nil {
		_ = temp.Close() //nolint:errcheck // preserve the primary write error
		return fmt.Errorf("write token cache: %w", writeErr)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close() //nolint:errcheck // preserve the primary sync error
		return fmt.Errorf("sync token cache: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close token cache temporary file: %w", err)
	}
	if parentErr := validateTokenCacheDirectory(parent); parentErr != nil {
		return parentErr
	}
	if targetErr := validateTokenCacheTarget(path); targetErr != nil {
		return targetErr
	}
	if err := replaceTokenCacheFile(tempPath, path); err != nil {
		return fmt.Errorf("replace token cache atomically: %w", err)
	}
	removeTemp = false
	return validatePublishedTokenCache(path)
}

func validateTokenCacheTarget(path string) error {
	info, statErr := os.Lstat(path)
	if statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return invalidTokenCacheTargetError()
		}
		return nil
	}
	if errors.Is(statErr, os.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("inspect token cache path: %w", statErr)
}

func invalidTokenCacheTargetError() error {
	return errors.New("token cache path must be a regular file, not a symlink or special file; set tokenCachePath or IMPARTUS_TOKEN_CACHE to the regular target path")
}

func normalizeTokenCachePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("token cache path is empty")
	}
	if strings.ContainsRune(path, '\x00') {
		return "", errors.New("token cache path contains a null byte")
	}
	return filepath.Clean(path), nil
}

// validateTokenCacheDirectory requires the immediate cache parent to be a real
// directory. Ancestor symlinks are allowed because supported platforms use
// them for standard locations (for example /var -> /private/var on macOS), and
// callers explicitly choose the cache path. The final cache entry is still
// opened without following symlinks where supported.
func validateTokenCacheDirectory(path string) error {
	// Inspect the caller's immediate parent expression directly. Converting "."
	// through filepath.Abs can reintroduce a shell's logical $PWD symlink and
	// reject the actual current directory even though kernel-relative opens of
	// ".token" never traverse that symlink.
	parent := filepath.Clean(path)
	var info os.FileInfo
	var statErr error
	if parent == "." {
		// The current-directory handle is already resolved by the process. On
		// Windows, Lstat(".") can still report the shell's logical reparse path;
		// Stat follows that representation to the real directory.
		info, statErr = os.Stat(parent)
	} else {
		info, statErr = os.Lstat(parent)
	}
	if statErr != nil {
		return fmt.Errorf("inspect token cache parent: %w", statErr)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("token cache parent must be a real directory")
	}
	return nil
}
