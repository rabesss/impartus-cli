// Package paths validates filesystem path overrides supplied at untrusted
// boundaries (the CLI --output flag and the API job outputPath field).
//
// It is deliberately NOT applied to config-file or environment values, which
// are trusted and routinely use absolute paths (e.g. "/tmp/...", t.TempDir()).
package paths

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ValidateDownloadLocation validates an untrusted download-location override
// and returns the cleaned path, or an error describing why it was rejected.
//
// Traversal (".." components that escape the base) is always rejected. Absolute
// paths are rejected unless allowAbsolute is true: the local CLI permits them
// (the invoking user already owns the filesystem), while remote API overrides
// do not (an untrusted client must not control an absolute output path).
func ValidateDownloadLocation(path string, allowAbsolute bool) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", errors.New("download location cannot be empty")
	}
	cleaned := filepath.Clean(trimmed)
	if filepath.IsAbs(cleaned) && !allowAbsolute {
		return "", fmt.Errorf("absolute download locations are not allowed via the API: %q", cleaned)
	}
	if hasTraversal(cleaned) {
		return "", fmt.Errorf("download location must not escape its base directory: %q", cleaned)
	}
	return cleaned, nil
}

// hasTraversal reports whether the cleaned path contains a ".." component,
// which would resolve above the intended base directory.
func hasTraversal(p string) bool {
	for _, part := range strings.Split(filepath.ToSlash(p), "/") {
		if part == ".." {
			return true
		}
	}
	return false
}
