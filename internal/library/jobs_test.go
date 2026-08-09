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

var validMP4Fixture = []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}

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
	first, err := store.RecoverInterruptedJobs(context.Background(), library.JobKindWatch)
	if err != nil {
		t.Fatalf("RecoverInterruptedJobs() missing output error = %v", err)
	}
	if len(first.Recovered) != 0 || len(first.Pending) != 1 || first.Pending[0] != jobID {
		t.Fatalf("missing-output recovery = %+v", first)
	}

	if writeErr := os.WriteFile(outputPath, []byte("stale unrelated file"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	second, err := store.RecoverInterruptedJobs(context.Background(), library.JobKindWatch)
	if err != nil {
		t.Fatalf("RecoverInterruptedJobs() stale output error = %v", err)
	}
	if len(second.Recovered) != 0 || len(second.Pending) != 1 || second.Pending[0] != jobID {
		t.Fatalf("stale-output recovery = %+v", second)
	}

	validMP4 := []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 0, 0, 0, 0, 'i', 's', 'o', 'm', 'm', 'p', '4', '2'}
	if writeErr := os.WriteFile(outputPath, validMP4, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	third, err := store.RecoverInterruptedJobs(context.Background(), library.JobKindWatch)
	if err != nil {
		t.Fatalf("RecoverInterruptedJobs() completed output error = %v", err)
	}
	if len(third.Recovered) != 1 || third.Recovered[0] != jobID || len(third.Pending) != 0 {
		t.Fatalf("completed-output recovery = %+v", third)
	}
	if len(second.Artifacts) != 1 || second.Artifacts[0].JobID != jobID {
		t.Fatalf("completed-output recovery artifacts = %+v", second.Artifacts)
	}
	if second.Artifacts[0].Manifest.ArtifactID == "" || len(second.Artifacts[0].Manifest.Files) != 1 || second.Artifacts[0].Manifest.Files[0].Path != outputPath {
		t.Fatalf("completed-output recovery manifest = %+v", second.Artifacts[0].Manifest)
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

func TestRecoverInterruptedJobsDoesNotClaimAnotherProducerKind(t *testing.T) {
	store := openTestStore(t)
	expected := library.ExpectedArtifact{
		Lecture:    artifact.Lecture{TTID: 41, InstituteID: 1, SubjectID: 2, SessionID: 3, SeqNo: 5, Topic: "Scoped recovery"},
		Selection:  artifact.Selection{Views: "left", Quality: "720"},
		Files:      []library.ExpectedFile{{Path: filepath.Join(t.TempDir(), "missing.mp4"), Role: "video", View: "left", Container: "mp4"}},
		ProducedAt: time.Date(2026, time.August, 10, 10, 0, 0, 0, time.UTC),
		Producer:   artifact.Producer{Name: "impartus", Version: "test"},
	}
	watchID := uuid.NewString()
	downloadID := uuid.NewString()
	for _, spec := range []library.JobSpec{
		{ID: watchID, Kind: library.JobKindWatch, Expected: expected},
		{ID: downloadID, Kind: library.JobKindDownload, Expected: expected},
	} {
		if err := store.CreateJob(context.Background(), spec); err != nil {
			t.Fatal(err)
		}
		if err := store.StartJob(context.Background(), spec.ID); err != nil {
			t.Fatal(err)
		}
	}

	recovery, err := store.RecoverInterruptedJobs(context.Background(), library.JobKindWatch)
	if err != nil {
		t.Fatal(err)
	}
	if len(recovery.Pending) != 1 || recovery.Pending[0] != watchID {
		t.Fatalf("watch recovery = %+v, want only %s pending", recovery, watchID)
	}
	watchJob, err := store.Job(context.Background(), watchID)
	if err != nil {
		t.Fatal(err)
	}
	downloadJob, err := store.Job(context.Background(), downloadID)
	if err != nil {
		t.Fatal(err)
	}
	if watchJob.Status != library.JobRecoverable || downloadJob.Status != library.JobRunning {
		t.Fatalf("statuses watch=%s download=%s, want recoverable/running", watchJob.Status, downloadJob.Status)
	}
}

func TestCompleteJobRejectsUnexpectedOutputPath(t *testing.T) {
	store := openTestStore(t)
	expectedPath := filepath.Join(t.TempDir(), "expected.mp4")
	unexpectedPath := filepath.Join(t.TempDir(), "unexpected.mp4")
	for _, path := range []string{expectedPath, unexpectedPath} {
		if err := os.WriteFile(path, validMP4Fixture, 0o600); err != nil {
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

func TestCreateJobCanonicalizesExpectedFileViewAliases(t *testing.T) {
	t.Parallel()

	store := openTestStore(t)
	jobID := uuid.NewString()
	expected := library.ExpectedArtifact{
		Lecture:    artifact.Lecture{TTID: 49, InstituteID: 1, SubjectID: 2, SessionID: 3},
		Selection:  artifact.Selection{Views: "first", Quality: "720"},
		Files:      []library.ExpectedFile{{Path: filepath.Join(t.TempDir(), "lecture.mp4"), Role: "video", View: "first", Container: "mp4"}},
		ProducedAt: time.Date(2026, time.August, 11, 10, 0, 0, 0, time.UTC),
		Producer:   artifact.Producer{Name: "impartus", Version: "test"},
	}
	if err := store.CreateJob(context.Background(), library.JobSpec{ID: jobID, Kind: library.JobKindDownload, Expected: expected}); err != nil {
		t.Fatalf("CreateJob() error = %v", err)
	}
	job, err := store.Job(context.Background(), jobID)
	if err != nil {
		t.Fatalf("Job() error = %v", err)
	}
	if job.Expected.Selection.Views != "left" || len(job.Expected.Files) != 1 || job.Expected.Files[0].View != "left" {
		t.Fatalf("canonical expected artifact = %+v", job.Expected)
	}
}

func TestCompleteJobEnforcesExpectedSHA256(t *testing.T) {
	store := openTestStore(t)
	outputPath := filepath.Join(t.TempDir(), "expected.mp4")
	if err := os.WriteFile(outputPath, validMP4Fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	const expectedSHA256 = "c43403fe022af967a0b859d3e14ea12d6633f4c8ad475816b0c55d85896e8e35"
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

func TestCompleteJobCanonicalizesIrrelevantVideoAudioFormat(t *testing.T) {
	store := openTestStore(t)
	outputPath := filepath.Join(t.TempDir(), "expected.mp4")
	if err := os.WriteFile(outputPath, validMP4Fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	expected := library.ExpectedArtifact{
		Lecture:    artifact.Lecture{TTID: 54, InstituteID: 1, SubjectID: 2, SessionID: 3},
		Selection:  artifact.Selection{Views: "left", Quality: "720", AudioFormat: "mp3"},
		Files:      []library.ExpectedFile{{Path: outputPath, Role: "video", View: "left", Container: "mp4"}},
		ProducedAt: time.Date(2026, time.August, 9, 10, 0, 0, 0, time.UTC),
		Producer:   artifact.Producer{Name: "impartus", Version: "test"},
	}
	jobID := uuid.NewString()
	if err := store.CreateJob(context.Background(), library.JobSpec{ID: jobID, Kind: library.JobKindDownload, Expected: expected}); err != nil {
		t.Fatal(err)
	}
	if err := store.StartJob(context.Background(), jobID); err != nil {
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
	if manifest.Selection.AudioFormat != "" {
		t.Fatalf("built video manifest audio format = %q, want canonical empty value", manifest.Selection.AudioFormat)
	}
	if err := store.CompleteJob(context.Background(), jobID, manifest); err != nil {
		t.Fatalf("CompleteJob() error = %v", err)
	}
}

func TestCompleteJobRejectsPendingLifecycleState(t *testing.T) {
	store := openTestStore(t)
	outputPath := filepath.Join(t.TempDir(), "pending.mp4")
	if err := os.WriteFile(outputPath, validMP4Fixture, 0o600); err != nil {
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
		if err := os.WriteFile(path, validMP4Fixture, 0o600); err != nil {
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
	failure := "download failed: " + secret + " upstream auth=body-secret signature=signed-secret\n" +
		`auth: Digest username="alice", response="digest-secret"` + "\n" +
		"X-Api-Key: api-secret"
	if err := store.FailJob(context.Background(), failedID, errors.New(failure)); err != nil {
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
	if failed.Status != library.JobFailed || strings.Contains(failed.ErrorSummary, "do-not-store") ||
		strings.Contains(failed.ErrorSummary, "body-secret") || strings.Contains(failed.ErrorSummary, "signed-secret") || strings.Contains(failed.ErrorSummary, "digest-secret") || strings.Contains(failed.ErrorSummary, "api-secret") ||
		!strings.Contains(failed.ErrorSummary, "REDACTED") {
		t.Fatalf("failed job = %+v", failed)
	}
	if canceled.Status != library.JobCanceled || canceled.FinishedAt == nil {
		t.Fatalf("canceled job = %+v", canceled)
	}
	if err := store.StartJob(context.Background(), failedID); !errors.Is(err, library.ErrJobTerminal) {
		t.Fatalf("StartJob(terminal) error = %v", err)
	}
}

func TestJobFailureRedactsHeadersAndBareAssignments(t *testing.T) {
	t.Parallel()

	store := openTestStore(t)
	expected := library.ExpectedArtifact{
		Lecture:    artifact.Lecture{TTID: 61, InstituteID: 1, SubjectID: 2, SessionID: 3},
		Selection:  artifact.Selection{Views: "left", Quality: "720"},
		Files:      []library.ExpectedFile{{Path: filepath.Join(t.TempDir(), "expected.mp4"), Role: "video", View: "left", Container: "mp4"}},
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
	if err := store.FailJob(context.Background(), jobID, errors.New(`Authorization: Token auth-secret upload_token=body-secret response={"token":"json-secret"}`)); err != nil {
		t.Fatal(err)
	}
	job, err := store.Job(context.Background(), jobID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(job.ErrorSummary, "auth-secret") || strings.Contains(job.ErrorSummary, "body-secret") || strings.Contains(job.ErrorSummary, "json-secret") || !strings.Contains(job.ErrorSummary, "REDACTED") {
		t.Fatalf("durable failure summary was not fully redacted: %q", job.ErrorSummary)
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
