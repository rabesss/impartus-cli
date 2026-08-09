//go:build !windows

package downloader

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSyncAndReplaceReportsPostPublicationSyncAsWarning(t *testing.T) {
	directory := t.TempDir()
	partial := filepath.Join(directory, "lecture.mp4.part")
	final := filepath.Join(directory, "lecture.mp4")
	if err := os.WriteFile(partial, []byte("new media"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(final, []byte("old media"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	publication, err := syncAndReplaceOutputWith(partial, final, func(*os.File) error {
		calls++
		if calls == 2 {
			return errors.New("injected post-publication sync failure")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("syncAndReplaceOutputWith() error = %v, want published success", err)
	}
	if publication.Warning == nil {
		t.Fatal("syncAndReplaceOutputWith() warning = nil")
	}
	contents, err := os.ReadFile(final)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "new media" {
		t.Fatalf("published contents = %q, want new media", contents)
	}
	if _, err := os.Lstat(partial); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial still exists after publication: %v", err)
	}
}
