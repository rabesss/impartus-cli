//go:build !windows

package library

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rabesss/impartus-cli/internal/artifact"
)

func TestVerifyArtifactFileRejectsPathSwapBeforePublication(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lecture.mp4")
	original := []byte("original media")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	oldValidate := validateStableArtifactFile
	validateStableArtifactFile = func(stablePath string, file *os.File, initial os.FileInfo) error {
		replacement := path + ".replacement"
		if err := os.WriteFile(replacement, []byte("changed media!"), 0o600); err != nil {
			return err
		}
		if err := os.Rename(replacement, path); err != nil {
			return err
		}
		return artifact.ValidateStableCompletedFile(stablePath, file, initial)
	}
	t.Cleanup(func() { validateStableArtifactFile = oldValidate })

	result := verifyArtifactFile(ArtifactFile{Path: path, Bytes: int64(len(original))}, VerifyOptions{})
	if result.Status != FileNotRegular {
		t.Fatalf("verifyArtifactFile() = %+v, want path-swap rejection", result)
	}
}
