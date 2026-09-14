package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/artifact"
)

func TestDigestManifestFilesRejectsSizeDrift(t *testing.T) {
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

	grown := append(append([]byte(nil), original...), 0)
	if err := os.WriteFile(path, grown, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := digestManifestFiles(manifest); err == nil {
		t.Fatal("digestManifestFiles() error = nil, want size drift rejection")
	} else if !strings.Contains(err.Error(), "size changed") {
		t.Fatalf("digestManifestFiles() error = %v, want size drift rejection", err)
	}
}

func TestDigestManifestFilesKeepsCallerPinnedDigest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lecture.mp4")
	media := []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}
	if err := os.WriteFile(path, media, 0o600); err != nil {
		t.Fatal(err)
	}
	const pinnedSHA256 = "c43403fe022af967a0b859d3e14ea12d6633f4c8ad475816b0c55d85896e8e35"
	manifest, err := (ExpectedArtifact{
		Lecture:    artifact.Lecture{TTID: 4, InstituteID: 1, SubjectID: 2, SessionID: 3},
		Selection:  artifact.Selection{Views: "left", Quality: "720"},
		Files:      []ExpectedFile{{Path: path, Role: "video", View: "left", Container: "mp4", SHA256: pinnedSHA256}},
		ProducedAt: time.Date(2026, time.August, 9, 8, 0, 0, 0, time.UTC),
		Producer:   artifact.Producer{Name: "impartus", Version: "test"},
	}).buildManifest()
	if err != nil {
		t.Fatalf("buildManifest() error = %v", err)
	}
	digested, err := digestManifestFiles(manifest)
	if err != nil {
		t.Fatalf("digestManifestFiles() error = %v", err)
	}
	if len(digested.Files) != 1 || digested.Files[0].SHA256 != pinnedSHA256 {
		t.Fatalf("digestManifestFiles() files = %+v, want pinned %q", digested.Files, pinnedSHA256)
	}
}
