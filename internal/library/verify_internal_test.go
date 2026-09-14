//go:build !windows

package library

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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

	result := verifyArtifactFile(ArtifactFile{Path: path, Bytes: int64(len(original))}, VerifyOptions{Hash: true})
	if result.Status != FileNotRegular {
		t.Fatalf("verifyArtifactFile() = %+v, want path-swap rejection", result)
	}
	if result.SHA256 != "" {
		t.Fatalf("verifyArtifactFile() SHA256 = %q, want no unpublished hash after path swap", result.SHA256)
	}
}

func TestDigestManifestFilesRejectsPathSwapBeforePublication(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lecture.mp4")
	original := []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := (ExpectedArtifact{
		Lecture:    artifact.Lecture{TTID: 4, InstituteID: 1, SubjectID: 2, SessionID: 3},
		Selection:  artifact.Selection{Views: "left", Quality: "720"},
		Files:      []ExpectedFile{{Path: path, Role: "video", View: "left", Container: "mp4"}},
		ProducedAt: time.Date(2026, time.August, 9, 8, 0, 0, 0, time.UTC),
		Producer:   artifact.Producer{Name: "impartus", Version: "test"},
	}).buildManifest()
	if err != nil {
		t.Fatalf("buildManifest() error = %v", err)
	}

	oldValidate := validateStableArtifactFile
	validateStableArtifactFile = func(stablePath string, file *os.File, initial os.FileInfo) error {
		replacement := path + ".replacement"
		// Same size, different bytes: only the stability proof can catch this.
		changed := []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'm', 'p', '4', '2'}
		if err := os.WriteFile(replacement, changed, 0o600); err != nil {
			return err
		}
		if err := os.Rename(replacement, path); err != nil {
			return err
		}
		return artifact.ValidateStableCompletedFile(stablePath, file, initial)
	}
	t.Cleanup(func() { validateStableArtifactFile = oldValidate })

	if _, err := digestManifestFiles(manifest); err == nil {
		t.Fatal("digestManifestFiles() error = nil, want path-swap rejection")
	}
}
