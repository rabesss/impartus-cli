//go:build windows

package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsPersistenceWriteThroughReplacement(t *testing.T) {
	dir := t.TempDir()
	from := filepath.Join(dir, "from.json")
	to := filepath.Join(dir, "to.json")
	if err := os.WriteFile(from, []byte("new"), 0o600); err != nil {
		t.Fatalf("write replacement: %v", err)
	}
	if err := os.WriteFile(to, []byte("old"), 0o600); err != nil {
		t.Fatalf("write destination: %v", err)
	}
	if err := replacePersistenceFile(from, to); err != nil {
		t.Fatalf("write-through replacement: %v", err)
	}
	got, err := os.ReadFile(to)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if string(got) != "new" {
		t.Fatalf("destination = %q, want new", got)
	}
	if _, err := os.Stat(from); !os.IsNotExist(err) {
		t.Fatalf("source still exists after replacement: %v", err)
	}
}
