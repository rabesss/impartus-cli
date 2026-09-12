package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/artifact"
)

func TestVerifyArtifactFillsLegacyEmptySHA256(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := Open(context.Background(), Options{Path: filepath.Join(parent, "library.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})
	path := filepath.Join(t.TempDir(), "lecture.mp4")
	if writeErr := os.WriteFile(path, []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	manifest, err := (ExpectedArtifact{
		Lecture:    artifact.Lecture{TTID: 4, InstituteID: 1, SubjectID: 2, SessionID: 3},
		Selection:  artifact.Selection{Views: "left", Quality: "720"},
		Files:      []ExpectedFile{{Path: path, Role: "video", View: "left", Container: "mp4"}},
		ProducedAt: time.Date(2026, time.August, 9, 8, 0, 0, 0, time.UTC),
		Producer:   artifact.Producer{Name: "impartus", Version: "legacy"},
	}).buildManifest()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if recordErr := store.RecordManifest(ctx, manifest); recordErr != nil {
		t.Fatal(recordErr)
	}
	cleared, err := store.database.ExecContext(ctx, "UPDATE artifact_files SET sha256 = '' WHERE artifact_id = ?", manifest.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	if rows, rowsErr := cleared.RowsAffected(); rowsErr != nil || rows != 1 {
		t.Fatalf("cleared %d rows, err = %v, want exactly one legacy row", rows, rowsErr)
	}

	verified, err := store.VerifyArtifact(ctx, manifest.ArtifactID, VerifyOptions{Hash: true})
	if err != nil {
		t.Fatalf("VerifyArtifact() error = %v", err)
	}
	const expectedSHA256 = "c43403fe022af967a0b859d3e14ea12d6633f4c8ad475816b0c55d85896e8e35"
	if !verified.OK || len(verified.Files) != 1 || verified.Files[0].Status != FilePresent || verified.Files[0].SHA256 != expectedSHA256 {
		t.Fatalf("legacy verification = %+v, want present with %q", verified, expectedSHA256)
	}
	record, err := store.GetArtifact(ctx, manifest.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Files) != 1 || record.Files[0].SHA256 != expectedSHA256 {
		t.Fatalf("stored sha256 after legacy fill = %+v, want %q", record.Files, expectedSHA256)
	}
}
