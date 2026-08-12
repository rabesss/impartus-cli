//go:build !windows

package artifact

import (
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestOpenCompletedFileDescriptorDoesNotBlockOnFIFO(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "swapped-output")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		file, err := openCompletedFileDescriptor(path)
		if file != nil {
			err = file.Close()
		}
		done <- err
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("openCompletedFileDescriptor blocked on a FIFO path swap")
	}
}
