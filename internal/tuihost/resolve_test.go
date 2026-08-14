package tuihost_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/tuihost"
)

func TestResolveExecutableValidatesExplicitFrontend(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "impartus-ui")
	mode := os.FileMode(0o755)
	if runtime.GOOS == "windows" {
		mode = 0o600
	}
	if err := os.WriteFile(path, []byte("test"), mode); err != nil {
		t.Fatalf("write frontend: %v", err)
	}
	resolved, err := tuihost.ResolveExecutable("  " + path + "  ")
	if err != nil {
		t.Fatalf("ResolveExecutable() error = %v", err)
	}
	want, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("EvalSymlinks() error = %v", err)
	}
	if resolved != want {
		t.Fatalf("ResolveExecutable() = %q, want %q", resolved, want)
	}
}

func TestResolveExecutableRejectsMissingAndNonExecutableFrontend(t *testing.T) {
	_, err := tuihost.ResolveExecutable(filepath.Join(t.TempDir(), "missing"))
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("missing ResolveExecutable() error = %v", err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	path := filepath.Join(t.TempDir(), "impartus-ui")
	if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
		t.Fatalf("write frontend: %v", err)
	}
	if _, err := tuihost.ResolveExecutable(path); err == nil || !strings.Contains(err.Error(), "not executable") {
		t.Fatalf("non-executable ResolveExecutable() error = %v", err)
	}
}
