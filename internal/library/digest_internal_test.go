package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

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

	// A re-hash would invoke the stability proof; fail if the pin is re-read.
	oldValidate := validateStableArtifactFile
	validateStableArtifactFile = func(string, *os.File, os.FileInfo) error {
		return errors.New("pinned digest must pass through without a re-hash")
	}
	t.Cleanup(func() { validateStableArtifactFile = oldValidate })

	digested, err := digestManifestFiles(manifest)
	if err != nil {
		t.Fatalf("digestManifestFiles() error = %v", err)
	}
	if len(digested.Files) != 1 || digested.Files[0].SHA256 != pinnedSHA256 {
		t.Fatalf("digestManifestFiles() files = %+v, want pinned %q", digested.Files, pinnedSHA256)
	}
}

func TestRecoverInterruptedJobsSkipsJobWhoseOutputsCannotBeDigested(t *testing.T) {
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

	ctx := context.Background()
	media := []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}
	unstablePath := filepath.Join(t.TempDir(), "unstable.mp4")
	stablePath := filepath.Join(t.TempDir(), "stable.mp4")
	for _, path := range []string{unstablePath, stablePath} {
		if writeErr := os.WriteFile(path, media, 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	newJob := func(id, path string, ttid int) {
		t.Helper()
		expected := ExpectedArtifact{
			Lecture:    artifact.Lecture{TTID: ttid, InstituteID: 1, SubjectID: 2, SessionID: 3},
			Selection:  artifact.Selection{Views: "left", Quality: "720"},
			Files:      []ExpectedFile{{Path: path, Role: "video", View: "left", Container: "mp4"}},
			ProducedAt: time.Date(2026, time.August, 9, 10, 0, 0, 0, time.UTC),
			Producer:   artifact.Producer{Name: "impartus", Version: "test"},
		}
		if createErr := store.CreateJob(ctx, JobSpec{ID: id, Kind: JobKindDownload, Expected: expected}); createErr != nil {
			t.Fatalf("CreateJob(%s) error = %v", id, createErr)
		}
		if startErr := store.StartJob(ctx, id); startErr != nil {
			t.Fatalf("StartJob(%s) error = %v", id, startErr)
		}
	}
	unstableJobID := uuid.NewString()
	stableJobID := uuid.NewString()
	newJob(unstableJobID, unstablePath, 90)
	newJob(stableJobID, stablePath, 91)

	oldValidate := validateStableArtifactFile
	validateStableArtifactFile = func(path string, file *os.File, initial os.FileInfo) error {
		if path == unstablePath {
			return errors.New("injected instability")
		}
		return artifact.ValidateStableCompletedFile(path, file, initial)
	}
	t.Cleanup(func() { validateStableArtifactFile = oldValidate })

	result, err := store.RecoverInterruptedJobs(ctx, JobKindDownload)
	if err != nil {
		t.Fatalf("RecoverInterruptedJobs() error = %v, want per-job containment of digest failures", err)
	}
	if len(result.Pending) != 1 || result.Pending[0] != unstableJobID {
		t.Fatalf("Pending = %+v, want only the unstable job", result.Pending)
	}
	if len(result.Recovered) != 1 || result.Recovered[0] != stableJobID {
		t.Fatalf("Recovered = %+v, want the stable job to recover past the unstable job", result.Recovered)
	}
}
