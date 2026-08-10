package library

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateExpectedContainerSignature(t *testing.T) {
	t.Parallel()

	validHeaders := map[string][]byte{
		"mp4":  {0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'},
		"m4a":  {0, 0, 0, 24, 'f', 't', 'y', 'p', 'M', '4', 'A', ' '},
		"mkv":  {0x1a, 0x45, 0xdf, 0xa3},
		"mp3":  {'I', 'D', '3'},
		"aac":  {0xff, 0xf1},
		"opus": {'O', 'g', 'g', 'S'},
	}
	for container, header := range validHeaders {
		t.Run(container, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "lecture."+container)
			if err := os.WriteFile(path, header, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := validateExpectedContainerSignature(ExpectedFile{Path: path, Container: container}); err != nil {
				t.Fatalf("validateExpectedContainerSignature() error = %v", err)
			}
		})
	}

	path := filepath.Join(t.TempDir(), "stale.mp4")
	if err := os.WriteFile(path, []byte("unrelated data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateExpectedContainerSignature(ExpectedFile{Path: path, Container: "mp4"}); err == nil {
		t.Fatal("validateExpectedContainerSignature() error = nil for unrelated data")
	}
}
