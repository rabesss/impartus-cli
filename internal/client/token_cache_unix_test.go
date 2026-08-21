//go:build !windows

package client

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestReadTokenCacheFileRejectsFIFOWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token-fifo")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("create token FIFO: %v", err)
	}

	result := make(chan error, 1)
	go func() {
		_, err := readTokenCacheFile(path)
		result <- err
	}()
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "regular file") {
			t.Fatalf("readTokenCacheFile(FIFO) error = %v, want regular-file rejection", err)
		}
	case <-time.After(time.Second):
		t.Fatal("readTokenCacheFile(FIFO) blocked")
	}
}
