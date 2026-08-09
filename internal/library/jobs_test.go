package library_test

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
	"github.com/rabesss/impartus-cli/internal/library"
)

func TestRecoverInterruptedJobCommitsValidFinalOutputWithoutNetwork(t *testing.T) {
	databasePath := privateDatabasePath(t)
	outputPath := filepath.Join(t.TempDir(), "recovered.mp4")
	jobID := uuid.NewString()
	expected := library.ExpectedArtifact{
		Lecture:    artifact.Lecture{TTID: 40, InstituteID: 1, SubjectID: 2, SessionID: 3, SeqNo: 4, Topic: "Recovery"},
		Selection:  artifact.Selection{Views: "left", Quality: "720"},
		Files:      []library.ExpectedFile{{Path: outputPath, Role: "video", View: "left", Container: "mp4"}},
		ProducedAt: time.Date(2026, time.August, 9, 10, 0, 0, 0, time.UTC),
		Producer:   artifact.Producer{Name: "impartus", Version: "test"},
	}

	store, err := library.Open(context.Background(), library.Options{Path: databasePath})
	if err != nil {
		t.Fatal(err)
	}
	if createErr := store.CreateJob(context.Background(), library.JobSpec{ID: jobID, Kind: "watch", Expected: expected}); createErr != nil {
		t.Fatalf("CreateJob() error = %v", createErr)
	}
	if startErr := store.StartJob(context.Background(), jobID); startErr != nil {
		t.Fatalf("StartJob() error = %v", startErr)
	}
	if closeErr := store.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}

	store, err = library.Open(context.Background(), library.Options{Path: databasePath})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})
	first, err := store.RecoverInterruptedJobs(context.Background())
	if err != nil {
		t.Fatalf("RecoverInterruptedJobs() missing output error = %v", err)
	}
	if len(first.Recovered) != 0 || len(first.Pending) != 1 || first.Pending[0] != jobID {
		t.Fatalf("missing-output recovery = %+v", first)
	}

	if writeErr := os.WriteFile(outputPath, []byte("completed media"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	second, err := store.RecoverInterruptedJobs(context.Background())
	if err != nil {
		t.Fatalf("RecoverInterruptedJobs() completed output error = %v", err)
	}
	if len(second.Recovered) != 1 || second.Recovered[0] != jobID || len(second.Pending) != 0 {
		t.Fatalf("completed-output recovery = %+v", second)
	}
	job, err := store.Job(context.Background(), jobID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != library.JobCompleted || job.CompletedArtifactID == "" {
		t.Fatalf("recovered job = %+v", job)
	}
	if _, err := store.GetArtifact(context.Background(), job.CompletedArtifactID); err != nil {
		t.Fatalf("recovered artifact missing: %v", err)
	}
}

func TestCompleteJobRejectsUnexpectedOutputPath(t *testing.T) {
	store := openTestStore(t)
	expectedPath := filepath.Join(t.TempDir(), "expected.mp4")
	unexpectedPath := filepath.Join(t.TempDir(), "unexpected.mp4")
	for _, path := range []string{expectedPath, unexpectedPath} {
		if err := os.WriteFile(path, []byte("completed media"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	expected := library.ExpectedArtifact{
		Lecture:    artifact.Lecture{TTID: 50, InstituteID: 1, SubjectID: 2, SessionID: 3},
		Selection:  artifact.Selection{Views: "left", Quality: "720"},
		Files:      []library.ExpectedFile{{Path: expectedPath, Role: "video", View: "left", Container: "mp4"}},
		ProducedAt: time.Date(2026, time.August, 9, 10, 0, 0, 0, time.UTC),
		Producer:   artifact.Producer{Name: "impartus", Version: "test"},
	}
	jobID := uuid.NewString()
	if err := store.CreateJob(context.Background(), library.JobSpec{ID: jobID, Kind: "watch", Expected: expected}); err != nil {
		t.Fatal(err)
	}
	if err := store.StartJob(context.Background(), jobID); err != nil {
		t.Fatal(err)
	}
	manifest, err := artifact.Build(artifact.BuildInput{
		Lecture:    expected.Lecture,
		Selection:  expected.Selection,
		Files:      []artifact.FileSpec{{Path: unexpectedPath, Role: "video", View: "left", Container: "mp4"}},
		ProducedAt: expected.ProducedAt,
		Producer:   expected.Producer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if completeErr := store.CompleteJob(context.Background(), jobID, manifest); completeErr == nil {
		t.Fatal("CompleteJob() error = nil, want unexpected output rejection")
	}
	job, err := store.Job(context.Background(), jobID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != library.JobRunning {
		t.Fatalf("rejected completion changed job status to %q", job.Status)
	}
	if _, err := store.GetArtifact(context.Background(), manifest.ArtifactID); !errors.Is(err, library.ErrArtifactNotFound) {
		t.Fatalf("rejected completion committed artifact: %v", err)
	}
}

func TestCompleteJobEnforcesExpectedSHA256(t *testing.T) {
	store := openTestStore(t)
	outputPath := filepath.Join(t.TempDir(), "expected.mp4")
	if err := os.WriteFile(outputPath, []byte("completed media"), 0o600); err != nil {
		t.Fatal(err)
	}
	const expectedSHA256 = "03537391546a15a1fdb224f2a1c4acad82f63895734245521f18158460a7dba8"
	expected := library.ExpectedArtifact{
		Lecture:    artifact.Lecture{TTID: 51, InstituteID: 1, SubjectID: 2, SessionID: 3},
		Selection:  artifact.Selection{Views: "left", Quality: "720"},
		Files:      []library.ExpectedFile{{Path: outputPath, Role: "video", View: "left", Container: "mp4", SHA256: expectedSHA256}},
		ProducedAt: time.Date(2026, time.August, 9, 10, 0, 0, 0, time.UTC),
		Producer:   artifact.Producer{Name: "impartus", Version: "test"},
	}
	jobID := uuid.NewString()
	if err := store.CreateJob(context.Background(), library.JobSpec{ID: jobID, Kind: "download", Expected: expected}); err != nil {
		t.Fatal(err)
	}
	if err := store.StartJob(context.Background(), jobID); err != nil {
		t.Fatal(err)
	}
	withoutHash, err := artifact.Build(artifact.BuildInput{
		Lecture:    expected.Lecture,
		Selection:  expected.Selection,
		Files:      []artifact.FileSpec{{Path: outputPath, Role: "video", View: "left", Container: "mp4"}},
		ProducedAt: expected.ProducedAt,
		Producer:   expected.Producer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if completeErr := store.CompleteJob(context.Background(), jobID, withoutHash); completeErr == nil {
		t.Fatal("CompleteJob() without pinned SHA error = nil")
	}
	withHash, err := artifact.Build(artifact.BuildInput{
		Lecture:    expected.Lecture,
		Selection:  expected.Selection,
		Files:      []artifact.FileSpec{{Path: outputPath, Role: "video", View: "left", Container: "mp4", SHA256: expectedSHA256}},
		ProducedAt: expected.ProducedAt,
		Producer:   expected.Producer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if completeErr := store.CompleteJob(context.Background(), jobID, withHash); completeErr != nil {
		t.Fatalf("CompleteJob() with expected SHA error = %v", completeErr)
	}
}

func TestCompleteJobRejectsPendingLifecycleState(t *testing.T) {
	store := openTestStore(t)
	outputPath := filepath.Join(t.TempDir(), "pending.mp4")
	if err := os.WriteFile(outputPath, []byte("completed media"), 0o600); err != nil {
		t.Fatal(err)
	}
	expected := library.ExpectedArtifact{
		Lecture:    artifact.Lecture{TTID: 52, InstituteID: 1, SubjectID: 2, SessionID: 3},
		Selection:  artifact.Selection{Views: "left", Quality: "720"},
		Files:      []library.ExpectedFile{{Path: outputPath, Role: "video", View: "left", Container: "mp4"}},
		ProducedAt: time.Date(2026, time.August, 9, 10, 0, 0, 0, time.UTC),
		Producer:   artifact.Producer{Name: "impartus", Version: "test"},
	}
	jobID := uuid.NewString()
	if err := store.CreateJob(context.Background(), library.JobSpec{ID: jobID, Kind: "download", Expected: expected}); err != nil {
		t.Fatal(err)
	}
	manifest, err := artifact.Build(artifact.BuildInput{
		Lecture:    expected.Lecture,
		Selection:  expected.Selection,
		Files:      []artifact.FileSpec{{Path: outputPath, Role: "video", View: "left", Container: "mp4"}},
		ProducedAt: expected.ProducedAt,
		Producer:   expected.Producer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if completeErr := store.CompleteJob(context.Background(), jobID, manifest); !errors.Is(completeErr, library.ErrJobTransition) {
		t.Fatalf("CompleteJob(pending) error = %v, want ErrJobTransition", completeErr)
	}
	job, err := store.Job(context.Background(), jobID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != library.JobPending {
		t.Fatalf("rejected pending completion changed status to %q", job.Status)
	}
}

func TestCompleteJobIdempotencyStillValidatesManifestContract(t *testing.T) {
	store := openTestStore(t)
	directory := t.TempDir()
	expectedPath := filepath.Join(directory, "expected.mp4")
	unexpectedPath := filepath.Join(directory, "unexpected.mp4")
	for _, path := range []string{expectedPath, unexpectedPath} {
		if err := os.WriteFile(path, []byte("completed media"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	expected := library.ExpectedArtifact{
		Lecture:    artifact.Lecture{TTID: 53, InstituteID: 1, SubjectID: 2, SessionID: 3},
		Selection:  artifact.Selection{Views: "left", Quality: "720"},
		Files:      []library.ExpectedFile{{Path: expectedPath, Role: "video", View: "left", Container: "mp4"}},
		ProducedAt: time.Date(2026, time.August, 9, 10, 0, 0, 0, time.UTC),
		Producer:   artifact.Producer{Name: "impartus", Version: "test"},
	}
	jobID := uuid.NewString()
	if err := store.CreateJob(context.Background(), library.JobSpec{ID: jobID, Kind: "download", Expected: expected}); err != nil {
		t.Fatal(err)
	}
	if err := store.StartJob(context.Background(), jobID); err != nil {
		t.Fatal(err)
	}
	valid, err := artifact.Build(artifact.BuildInput{
		Lecture: expected.Lecture, Selection: expected.Selection,
		Files:      []artifact.FileSpec{{Path: expectedPath, Role: "video", View: "left", Container: "mp4"}},
		ProducedAt: expected.ProducedAt, Producer: expected.Producer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if completeErr := store.CompleteJob(context.Background(), jobID, valid); completeErr != nil {
		t.Fatalf("CompleteJob(valid) error = %v", completeErr)
	}
	invalid, err := artifact.Build(artifact.BuildInput{
		Lecture: expected.Lecture, Selection: expected.Selection,
		Files:      []artifact.FileSpec{{Path: unexpectedPath, Role: "video", View: "left", Container: "mp4"}},
		ProducedAt: expected.ProducedAt, Producer: expected.Producer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if completeErr := store.CompleteJob(context.Background(), jobID, invalid); completeErr == nil {
		t.Fatal("CompleteJob(completed, unexpected path) error = nil")
	}
	record, err := store.GetArtifact(context.Background(), valid.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Files) != 1 || record.Files[0].Path != expectedPath {
		t.Fatalf("rejected idempotent completion mutated artifact files: %+v", record.Files)
	}
}

func TestJobFailureAndCancellationAreDurableTerminalStates(t *testing.T) {
	store := openTestStore(t)
	expected := library.ExpectedArtifact{
		Lecture:    artifact.Lecture{TTID: 60, InstituteID: 1, SubjectID: 2, SessionID: 3},
		Selection:  artifact.Selection{Views: "left", Quality: "720"},
		Files:      []library.ExpectedFile{{Path: filepath.Join(t.TempDir(), "expected.mp4"), Role: "video", View: "left", Container: "mp4"}},
		ProducedAt: time.Date(2026, time.August, 9, 10, 0, 0, 0, time.UTC),
		Producer:   artifact.Producer{Name: "impartus", Version: "test"},
	}
	failedID := uuid.NewString()
	canceledID := uuid.NewString()
	for _, jobID := range []string{failedID, canceledID} {
		if err := store.CreateJob(context.Background(), library.JobSpec{ID: jobID, Kind: "watch", Expected: expected}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.StartJob(context.Background(), failedID); err != nil {
		t.Fatal(err)
	}
	secret := "https://example.test/media?token=do-not-store"
	if err := store.FailJob(context.Background(), failedID, errors.New("download failed: "+secret)); err != nil {
		t.Fatal(err)
	}
	if err := store.CancelJob(context.Background(), canceledID); err != nil {
		t.Fatal(err)
	}
	failed, err := store.Job(context.Background(), failedID)
	if err != nil {
		t.Fatal(err)
	}
	canceled, err := store.Job(context.Background(), canceledID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != library.JobFailed || strings.Contains(failed.ErrorSummary, "do-not-store") || !strings.Contains(failed.ErrorSummary, "REDACTED") {
		t.Fatalf("failed job = %+v", failed)
	}
	if canceled.Status != library.JobCanceled || canceled.FinishedAt == nil {
		t.Fatalf("canceled job = %+v", canceled)
	}
	if err := store.StartJob(context.Background(), failedID); !errors.Is(err, library.ErrJobTerminal) {
		t.Fatalf("StartJob(terminal) error = %v", err)
	}
}

func TestCreateJobRejectsInvalidExpectedOutputBeforeWork(t *testing.T) {
	store := openTestStore(t)
	expected := library.ExpectedArtifact{
		Lecture:    artifact.Lecture{TTID: 70, InstituteID: 1, SubjectID: 2, SessionID: 3},
		Selection:  artifact.Selection{Views: "left", Quality: "720"},
		Files:      []library.ExpectedFile{{Path: filepath.Join(t.TempDir(), "lecture.mp4"), Role: "audio", View: "left", Container: "mp4"}},
		ProducedAt: time.Date(2026, time.August, 9, 10, 0, 0, 0, time.UTC),
		Producer:   artifact.Producer{Name: "impartus", Version: "test"},
	}
	if err := store.CreateJob(context.Background(), library.JobSpec{ID: uuid.NewString(), Kind: "watch", Expected: expected}); err == nil {
		t.Fatal("CreateJob() error = nil, want role/selection rejection")
	}
	jobs, err := store.ListJobs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatalf("invalid job was persisted: %+v", jobs)
	}
}
