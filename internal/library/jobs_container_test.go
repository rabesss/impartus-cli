package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/artifact"
)

func TestExpectedArtifactBuildManifestRequiresContainerSignature(t *testing.T) {
	t.Parallel()

	build := func(path string) error {
		_, err := (ExpectedArtifact{
			Lecture:    artifact.Lecture{TTID: 4, InstituteID: 1, SubjectID: 2, SessionID: 3},
			Selection:  artifact.Selection{Views: "left", Quality: "720"},
			Files:      []ExpectedFile{{Path: path, Role: "video", View: "left", Container: "mp4"}},
			ProducedAt: time.Now(),
			Producer:   artifact.Producer{Name: "impartus", Version: "test"},
		}).buildManifest()
		return err
	}

	valid := filepath.Join(t.TempDir(), "lecture.mp4")
	if err := os.WriteFile(valid, []byte{0, 0, 0, 24, 'f', 't', 'y', 'p'}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := build(valid); err != nil {
		t.Fatalf("buildManifest() valid error = %v", err)
	}

	stale := filepath.Join(t.TempDir(), "stale.mp4")
	if err := os.WriteFile(stale, []byte("unrelated data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := build(stale); err == nil || !strings.Contains(err.Error(), "does not match container") {
		t.Fatalf("buildManifest() stale error = %v, want container mismatch", err)
	}
}
