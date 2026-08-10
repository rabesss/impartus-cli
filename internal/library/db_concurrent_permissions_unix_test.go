//go:build !windows

package library_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/library"
)

func TestOpenWaitsForConcurrentStateDirectoryPrivacy(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	secured := make(chan error, 1)
	go func() {
		time.Sleep(10 * time.Millisecond)
		secured <- os.Chmod(parent, 0o700)
	}()

	store, err := library.Open(context.Background(), library.Options{Path: filepath.Join(parent, "library.db")})
	if secureErr := <-secured; secureErr != nil {
		t.Fatal(secureErr)
	}
	if err != nil {
		t.Fatalf("Open() error = %v, want convergence after concurrent privacy update", err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})
}
