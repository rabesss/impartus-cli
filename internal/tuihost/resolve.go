package tuihost

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ResolveExecutable returns the explicit development override or the OpenTUI
// sidecar installed beside the running Go executable.
func ResolveExecutable(override string) (string, error) {
	candidate := strings.TrimSpace(override)
	if candidate == "" {
		parent, err := os.Executable()
		if err != nil {
			return "", fmt.Errorf("resolve impartus executable: %w", err)
		}
		name := "impartus-ui"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		candidate = filepath.Join(filepath.Dir(parent), name)
	}
	absolute, err := filepath.Abs(filepath.Clean(candidate))
	if err != nil {
		return "", fmt.Errorf("resolve OpenTUI frontend path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("OpenTUI frontend is not installed beside impartus; expected %s", filepath.Base(absolute))
		}
		return "", fmt.Errorf("inspect OpenTUI frontend: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("inspect OpenTUI frontend: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("OpenTUI frontend must be a regular file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return "", errors.New("OpenTUI frontend is not executable")
	}
	return resolved, nil
}
