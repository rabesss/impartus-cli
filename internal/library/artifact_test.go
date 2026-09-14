package library_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/artifact"
	"github.com/rabesss/impartus-cli/internal/library"
)

func TestRecordManifestIsIdempotentAndKeepsEveryMaterializedPath(t *testing.T) {
	store := openTestStore(t)
	first := buildTestManifest(t, filepath.Join(t.TempDir(), "lecture.mp4"), "first")
	second := buildTestManifest(t, filepath.Join(t.TempDir(), "講義.mp4"), "second")
	if first.ArtifactID != second.ArtifactID {
		t.Fatalf("same logical lecture produced IDs %q and %q", first.ArtifactID, second.ArtifactID)
	}

	for _, manifest := range []artifact.Manifest{first, first, second} {
		if err := store.RecordManifest(context.Background(), manifest); err != nil {
			t.Fatalf("RecordManifest() error = %v", err)
		}
	}
	record, err := store.GetArtifact(context.Background(), first.ArtifactID)
	if err != nil {
		t.Fatalf("GetArtifact() error = %v", err)
	}
	if record.Manifest.Producer.Version != "second" {
		t.Fatalf("latest manifest producer = %q, want second", record.Manifest.Producer.Version)
	}
	if len(record.Files) != 2 {
		t.Fatalf("materialized files = %+v, want two distinct paths", record.Files)
	}
	for _, file := range record.Files {
		if !file.Present {
			t.Fatalf("newly recorded file marked missing: %+v", file)
		}
	}

	listed, err := store.ListArtifacts(context.Background())
	if err != nil {
		t.Fatalf("ListArtifacts() error = %v", err)
	}
	if len(listed) != 1 || listed[0].Manifest.ArtifactID != first.ArtifactID {
		t.Fatalf("ListArtifacts() = %+v", listed)
	}
}

func TestRecordManifestPersistsSHA256(t *testing.T) {
	store := openTestStore(t)
	path := filepath.Join(t.TempDir(), "lecture.mp4")
	manifest := buildTestManifest(t, path, "digest")
	if err := store.RecordManifest(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	record, err := store.GetArtifact(context.Background(), manifest.ArtifactID)
	if err != nil {
		t.Fatalf("GetArtifact() error = %v", err)
	}
	const expectedSHA256 = "c43403fe022af967a0b859d3e14ea12d6633f4c8ad475816b0c55d85896e8e35"
	if len(record.Files) != 1 || record.Files[0].SHA256 != expectedSHA256 {
		t.Fatalf("recorded sha256 = %+v, want %q", record.Files, expectedSHA256)
	}
	if len(record.Manifest.Files) != 1 || record.Manifest.Files[0].SHA256 != expectedSHA256 {
		t.Fatalf("stored manifest files = %+v, want sha256 %q", record.Manifest.Files, expectedSHA256)
	}
}

func TestVerifyArtifactRefreshesHashAndMarksMissingWithoutDeleting(t *testing.T) {
	store := openTestStore(t)
	path := filepath.Join(t.TempDir(), "lecture.mp4")
	manifest := buildTestManifest(t, path, "verify")
	if err := store.RecordManifest(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}

	verified, err := store.VerifyArtifact(context.Background(), manifest.ArtifactID, library.VerifyOptions{Hash: true})
	if err != nil {
		t.Fatalf("VerifyArtifact() error = %v", err)
	}
	if !verified.OK || len(verified.Files) != 1 || verified.Files[0].Status != library.FilePresent {
		t.Fatalf("verification = %+v", verified)
	}
	const expectedSHA256 = "c43403fe022af967a0b859d3e14ea12d6633f4c8ad475816b0c55d85896e8e35"
	if verified.Files[0].SHA256 != expectedSHA256 {
		t.Fatalf("recorded sha256 = %q, want %q", verified.Files[0].SHA256, expectedSHA256)
	}
	changedMedia := []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'm', 'p', '4', '2'}
	if writeErr := os.WriteFile(path, changedMedia, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	if recordErr := store.RecordManifest(context.Background(), manifest); recordErr != nil {
		t.Fatalf("RecordManifest() after same-path replacement error = %v", recordErr)
	}
	recordedAgain, err := store.GetArtifact(context.Background(), manifest.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	wantChangedSHA := fmt.Sprintf("%x", sha256.Sum256(changedMedia))
	if len(recordedAgain.Files) != 1 || recordedAgain.Files[0].SHA256 != wantChangedSHA {
		t.Fatalf("re-record sha256 = %+v, want %q", recordedAgain.Files, wantChangedSHA)
	}
	refreshed, err := store.VerifyArtifact(context.Background(), manifest.ArtifactID, library.VerifyOptions{Hash: true})
	if err != nil {
		t.Fatalf("VerifyArtifact() after replacement error = %v", err)
	}
	if !refreshed.OK || len(refreshed.Files) != 1 || refreshed.Files[0].SHA256 != wantChangedSHA || refreshed.Files[0].SHA256 == expectedSHA256 {
		t.Fatalf("replacement verification = %+v, want refreshed hash %q", refreshed, wantChangedSHA)
	}

	if removeErr := os.Remove(path); removeErr != nil {
		t.Fatal(removeErr)
	}
	missing, err := store.VerifyArtifact(context.Background(), manifest.ArtifactID, library.VerifyOptions{})
	if err != nil {
		t.Fatalf("VerifyArtifact() after remove error = %v", err)
	}
	if missing.OK || len(missing.Files) != 1 || missing.Files[0].Status != library.FileMissing {
		t.Fatalf("missing verification = %+v", missing)
	}
	record, err := store.GetArtifact(context.Background(), manifest.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Files) != 1 || record.Files[0].Present || record.Files[0].SHA256 != wantChangedSHA {
		t.Fatalf("missing file row was deleted or lost metadata: %+v", record.Files)
	}
}

func TestRecordManifestRechecksContainerSignature(t *testing.T) {
	store := openTestStore(t)
	path := filepath.Join(t.TempDir(), "lecture.mp4")
	manifest := buildTestManifest(t, path, "container-recheck")

	invalid := make([]byte, manifest.Files[0].Bytes)
	copy(invalid, []byte("not-media"))
	if err := os.WriteFile(path, invalid, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordManifest(context.Background(), manifest); err == nil {
		t.Fatal("RecordManifest() error = nil, want container-signature rejection")
	}
}

func TestVerifyArtifactRejectsSymlinkMaterialization(t *testing.T) {
	store := openTestStore(t)
	directory := t.TempDir()
	path := filepath.Join(directory, "lecture.mp4")
	manifest := buildTestManifest(t, path, "verify-symlink")
	if err := store.RecordManifest(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(directory, "target.mp4")
	if err := os.Rename(path, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("create verification symlink: %v", err)
	}

	verified, err := store.VerifyArtifact(context.Background(), manifest.ArtifactID, library.VerifyOptions{Hash: true})
	if err != nil {
		t.Fatalf("VerifyArtifact() error = %v", err)
	}
	if verified.OK || len(verified.Files) != 1 || verified.Files[0].Status != library.FileNotRegular {
		t.Fatalf("symlink verification = %+v, want not_regular", verified)
	}
}

func TestVerifyArtifactCanonicalizesWhitespaceIDBeforeMetadataUpdate(t *testing.T) {
	store := openTestStore(t)
	path := filepath.Join(t.TempDir(), "lecture.mp4")
	manifest := buildTestManifest(t, path, "verify-whitespace")
	if err := store.RecordManifest(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	verified, err := store.VerifyArtifact(context.Background(), " \t"+manifest.ArtifactID+"\n ", library.VerifyOptions{})
	if err != nil {
		t.Fatalf("VerifyArtifact() error = %v", err)
	}
	if verified.ArtifactID != manifest.ArtifactID {
		t.Fatalf("verification artifact ID = %q, want canonical %q", verified.ArtifactID, manifest.ArtifactID)
	}
	record, err := store.GetArtifact(context.Background(), manifest.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Files) != 1 || record.Files[0].Present {
		t.Fatalf("whitespace verification did not persist missing state: %+v", record.Files)
	}
}

func TestVerifyArtifactReportsContainerMismatch(t *testing.T) {
	store := openTestStore(t)
	path := filepath.Join(t.TempDir(), "lecture.mp4")
	manifest := buildTestManifest(t, path, "container-mismatch")
	if err := store.RecordManifest(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	invalid := make([]byte, manifest.Files[0].Bytes)
	copy(invalid, []byte("not-a-container"))
	if err := os.WriteFile(path, invalid, 0o600); err != nil {
		t.Fatal(err)
	}

	verified, err := store.VerifyArtifact(context.Background(), manifest.ArtifactID, library.VerifyOptions{Container: true, Hash: true})
	if err != nil {
		t.Fatalf("VerifyArtifact() error = %v", err)
	}
	if verified.OK || len(verified.Files) != 1 || verified.Files[0].Status != library.FileContainerMismatch {
		t.Fatalf("container verification = %+v, want container_mismatch", verified)
	}
}

func TestVerifyArtifactHashMismatchWithContainerCheck(t *testing.T) {
	store := openTestStore(t)
	path := filepath.Join(t.TempDir(), "lecture.mp4")
	manifest := buildTestManifest(t, path, "hash-mismatch")
	if err := store.RecordManifest(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	verified, err := store.VerifyArtifact(context.Background(), manifest.ArtifactID, library.VerifyOptions{Hash: true})
	if err != nil {
		t.Fatal(err)
	}
	if !verified.OK || verified.Files[0].SHA256 == "" {
		t.Fatalf("initial hash fill = %+v", verified)
	}

	changed := []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'm', 'p', '4', 'x'}
	if writeErr := os.WriteFile(path, changed, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	mismatched, err := store.VerifyArtifact(context.Background(), manifest.ArtifactID, library.VerifyOptions{Hash: true, Container: true})
	if err != nil {
		t.Fatalf("VerifyArtifact() error = %v", err)
	}
	if mismatched.OK || mismatched.Files[0].Status != library.FileHashMismatch {
		t.Fatalf("hash verification = %+v, want hash_mismatch", mismatched)
	}
}

func TestVerifyLatestMaterializationIgnoresHistoricalPaths(t *testing.T) {
	store := openTestStore(t)
	first := buildTestManifest(t, filepath.Join(t.TempDir(), "first.mp4"), "first")
	second := buildTestManifest(t, filepath.Join(t.TempDir(), "second.mp4"), "second")
	if first.ArtifactID != second.ArtifactID {
		t.Fatalf("same lecture produced IDs %q and %q", first.ArtifactID, second.ArtifactID)
	}
	if err := store.RecordManifest(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordManifest(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(first.Files[0].Path); err != nil {
		t.Fatal(err)
	}

	all, err := store.VerifyArtifact(context.Background(), first.ArtifactID, library.VerifyOptions{Hash: true, Container: true})
	if err != nil {
		t.Fatal(err)
	}
	if all.OK {
		t.Fatalf("full verification = %+v, want failure on historical missing path", all)
	}

	latest, err := store.VerifyArtifact(context.Background(), first.ArtifactID, library.VerifyOptions{
		Hash: true, Container: true, LatestMaterialization: true,
	})
	if err != nil {
		t.Fatalf("VerifyArtifact(latest) error = %v", err)
	}
	if !latest.OK || len(latest.Files) != 1 || latest.Files[0].Path != second.Files[0].Path || latest.Files[0].Status != library.FilePresent {
		t.Fatalf("latest verification = %+v, want only current path", latest)
	}
	if latest.Files[0].SHA256 == "" {
		t.Fatal("latest verification did not persist a digest")
	}
}

func TestRecordManifestsRejectsInvalidBatchAtomically(t *testing.T) {
	store := openTestStore(t)
	valid := buildTestManifest(t, filepath.Join(t.TempDir(), "valid.mp4"), "valid")
	invalid := buildTestManifest(t, filepath.Join(t.TempDir(), "invalid.mp4"), "invalid")
	invalid.ArtifactID = "impartus:v1:tampered"
	if err := store.RecordManifests(context.Background(), []artifact.Manifest{valid, invalid}); err == nil {
		t.Fatal("RecordManifests() error = nil, want invalid identity rejection")
	}
	listed, err := store.ListArtifacts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("invalid batch partially committed: %+v", listed)
	}
}

func TestConcurrentReadersAndWritersShareWALDatabase(t *testing.T) {
	databasePath := privateDatabasePath(t)
	first, err := library.Open(context.Background(), library.Options{Path: databasePath})
	if err != nil {
		t.Fatal(err)
	}
	second, err := library.Open(context.Background(), library.Options{Path: databasePath})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := first.Close(); closeErr != nil {
			t.Errorf("close first store: %v", closeErr)
		}
		if closeErr := second.Close(); closeErr != nil {
			t.Errorf("close second store: %v", closeErr)
		}
	})
	manifest := buildTestManifest(t, filepath.Join(t.TempDir(), "lecture.mp4"), "concurrent")
	if err := first.RecordManifest(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}

	var wait sync.WaitGroup
	errorsFound := make(chan error, 12)
	for worker := range 12 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			store := first
			if worker%2 == 1 {
				store = second
			}
			for range 20 {
				if worker%3 == 0 {
					if err := store.RecordManifest(context.Background(), manifest); err != nil {
						errorsFound <- err
						return
					}
					continue
				}
				if _, err := store.GetArtifact(context.Background(), manifest.ArtifactID); err != nil {
					errorsFound <- err
					return
				}
			}
		}()
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Fatalf("concurrent library operation: %v", err)
	}
}

func TestCanceledArtifactCommitLeavesDatabaseUnchanged(t *testing.T) {
	store := openTestStore(t)
	first := buildTestManifest(t, filepath.Join(t.TempDir(), "first.mp4"), "first")
	if err := store.RecordManifest(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	second := buildTestManifest(t, filepath.Join(t.TempDir(), "second.mp4"), "second")
	second.Lecture.TTID = 41
	secondID, err := artifact.NewID(second.Identity())
	if err != nil {
		t.Fatal(err)
	}
	second.ArtifactID = secondID
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if recordErr := store.RecordManifest(ctx, second); recordErr == nil {
		t.Fatal("RecordManifest(canceled) error = nil")
	}
	listed, err := store.ListArtifacts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Manifest.ArtifactID != first.ArtifactID {
		t.Fatalf("canceled commit changed library: %+v", listed)
	}
}

func openTestStore(t *testing.T) *library.Store {
	t.Helper()
	store, err := library.Open(context.Background(), library.Options{Path: privateDatabasePath(t)})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})
	return store
}

func privateDatabasePath(t *testing.T) string {
	t.Helper()
	parent := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(parent, "library.db")
}

func buildTestManifest(t *testing.T, path, version string) artifact.Manifest {
	t.Helper()
	if err := os.WriteFile(path, []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := artifact.Build(artifact.BuildInput{
		Lecture: artifact.Lecture{
			TTID:        40,
			InstituteID: 1,
			SubjectID:   2,
			SessionID:   3,
			SeqNo:       4,
			Topic:       "Test lecture",
		},
		Selection:  artifact.Selection{Views: "left", Quality: "720"},
		Files:      []artifact.FileSpec{{Path: path, Role: "video", View: "left", Container: "mp4"}},
		ProducedAt: time.Date(2026, time.August, 9, 8, 0, 0, 0, time.UTC),
		Producer:   artifact.Producer{Name: "impartus", Version: version},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	return manifest
}
