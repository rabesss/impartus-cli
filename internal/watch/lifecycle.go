package watch

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/rabesss/impartus-cli/internal/artifact"
	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/downloader"
	"github.com/rabesss/impartus-cli/internal/events"
	"github.com/rabesss/impartus-cli/internal/library"
)

func (watcher *Watcher) recover(ctx context.Context) (library.RecoveryResult, error) {
	if watcher.options.DryRun {
		return library.RecoveryResult{}, nil
	}
	if watcher.options.StartupRecovery == nil {
		recovery, err := watcher.store.RecoverInterruptedJobs(context.WithoutCancel(ctx), library.JobKindWatch)
		return cloneRecoveryResult(recovery), durableStateError("recover interrupted watch jobs", err)
	}
	return cloneRecoveryResult(*watcher.options.StartupRecovery), nil
}

func cloneRecoveryResult(source library.RecoveryResult) library.RecoveryResult {
	recovery := source
	recovery.Recovered = append([]string(nil), recovery.Recovered...)
	recovery.Pending = append([]string(nil), recovery.Pending...)
	recovery.Skipped = append([]string(nil), recovery.Skipped...)
	recovery.Artifacts = make([]library.RecoveredArtifact, len(source.Artifacts))
	for index, recovered := range source.Artifacts {
		recovery.Artifacts[index] = recovered
		recovery.Artifacts[index].Manifest.Files = append([]artifact.File(nil), recovered.Manifest.Files...)
	}
	return recovery
}

func (watcher *Watcher) emitRecoveredArtifacts(recovered []library.RecoveredArtifact) error {
	for _, item := range recovered {
		manifest := item.Manifest
		if err := watcher.emit(events.Event{
			Type:       events.LectureCompleted,
			ArtifactID: manifest.ArtifactID,
			Target: &events.Target{
				SubjectID: manifest.Lecture.SubjectID,
				SessionID: manifest.Lecture.SessionID,
			},
			Lecture: &events.Lecture{
				TTID: manifest.Lecture.TTID, SeqNo: manifest.Lecture.SeqNo, Topic: manifest.Lecture.Topic,
			},
			Artifact: &manifest,
			Outputs:  manifestPaths(manifest),
			Details: map[string]any{
				"libraryJobId": item.JobID, "recovered": true,
			},
		}); err != nil {
			return err
		}
	}
	return nil
}

func (watcher *Watcher) loadRetryableJobs(ctx context.Context) error {
	jobs, err := watcher.store.ListJobs(ctx)
	if err != nil {
		return durableStateError("load recoverable watch jobs", err)
	}
	watcher.retryable = make(map[string][]library.Job)
	for _, job := range jobs {
		if job.Kind != library.JobKindWatch || (job.Status != library.JobPending && job.Status != library.JobRecoverable) {
			continue
		}
		watcher.retryable[job.LogicalArtifactID] = append(watcher.retryable[job.LogicalArtifactID], job)
	}
	return nil
}

func (watcher *Watcher) downloadLecture(ctx context.Context, target config.WatchTarget, lecture client.Lecture, playlist client.ParsedPlaylist, expected library.ExpectedArtifact, artifactID string) (artifact.Manifest, error) {
	jobID := uuid.NewString()
	reused := false
	for _, candidate := range watcher.retryable[artifactID] {
		if !reused && expectedPathsEqual(candidate.Expected, expected) {
			jobID = candidate.ID
			expected = candidate.Expected
			reused = true
			continue
		}
		if err := watcher.store.FailJob(context.WithoutCancel(ctx), candidate.ID, errors.New("recoverable job was superseded by a new output plan")); err != nil {
			return artifact.Manifest{}, watcher.lectureFailure(ctx, target, lecture, candidate.ID, durableStateError("fail superseded recoverable watch job", err))
		}
	}
	delete(watcher.retryable, artifactID)
	if !reused {
		if err := watcher.store.CreateJob(context.WithoutCancel(ctx), library.JobSpec{ID: jobID, Kind: library.JobKindWatch, Expected: expected}); err != nil {
			return artifact.Manifest{}, watcher.lectureFailure(ctx, target, lecture, jobID, durableStateError("create durable watch job", err))
		}
	}
	if err := watcher.store.StartJob(context.WithoutCancel(ctx), jobID); err != nil {
		return artifact.Manifest{}, watcher.lectureFailure(ctx, target, lecture, jobID, durableStateError("start durable watch job", err))
	}
	if err := watcher.emitLecture(events.LectureStarted, target, lecture, artifactID, map[string]any{"libraryJobId": jobID}); err != nil {
		failErr := durableStateError("fail watch job after event delivery failure", watcher.store.FailJob(context.WithoutCancel(ctx), jobID, err))
		return artifact.Manifest{}, errors.Join(err, failErr)
	}
	var joined downloader.JoinResult
	err := watcher.retry(ctx, func() error {
		var downloadErr error
		joined, downloadErr = watcher.producer.DownloadAndJoinPlaylist(ctx, playlist)
		return downloadErr
	})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			cancelErr := durableStateError("cancel durable watch job", watcher.store.CancelJob(context.WithoutCancel(ctx), jobID))
			return artifact.Manifest{}, errors.Join(ctxErr, cancelErr)
		}
		failErr := durableStateError("fail durable watch job", watcher.store.FailJob(context.WithoutCancel(ctx), jobID, err))
		return artifact.Manifest{}, watcher.lectureFailure(ctx, target, lecture, jobID, errors.Join(err, failErr))
	}
	manifest, err := manifestFromExpected(expected, joined)
	if err != nil {
		failErr := durableStateError("fail watch job after manifest validation", watcher.store.FailJob(context.WithoutCancel(ctx), jobID, err))
		return artifact.Manifest{}, watcher.lectureFailure(ctx, target, lecture, jobID, errors.Join(err, failErr))
	}
	if err := watcher.store.CompleteJob(context.WithoutCancel(ctx), jobID, manifest); err != nil {
		commitErr := durableStateError("commit durable watch artifact", err)
		return artifact.Manifest{}, watcher.lectureFailure(ctx, target, lecture, jobID, commitErr)
	}
	progressErr := watcher.emitLecture(events.LectureProgress, target, lecture, artifactID, map[string]any{
		"libraryJobId": jobID, "stage": "media_published", "outputs": manifestPaths(manifest),
	})
	if progressErr != nil {
		return manifest, progressErr
	}
	if err := watcher.emitLectureCompleted(target, lecture, jobID, artifactID, manifest); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func (watcher *Watcher) emitLectureCompleted(target config.WatchTarget, lecture client.Lecture, jobID, artifactID string, manifest artifact.Manifest) error {
	if err := watcher.emit(events.Event{
		Type:       events.LectureCompleted,
		Target:     &events.Target{SubjectID: target.SubjectID, SessionID: target.SessionID, Label: target.Label},
		Lecture:    &events.Lecture{TTID: lecture.TTID, SeqNo: lecture.SeqNo, Topic: lecture.Topic},
		ArtifactID: artifactID,
		Artifact:   &manifest,
		Outputs:    manifestPaths(manifest),
		Details:    map[string]any{"libraryJobId": jobID},
	}); err != nil {
		return err
	}
	return nil
}

func (watcher *Watcher) committed(ctx context.Context, artifactID string) (bool, error) {
	record, err := watcher.store.GetArtifact(ctx, artifactID)
	if err != nil {
		if errors.Is(err, library.ErrArtifactNotFound) {
			return false, nil
		}
		return false, durableStateError("read committed artifact", err)
	}
	verification, err := watcher.store.VerifyArtifact(ctx, artifactID, library.VerifyOptions{})
	if err != nil {
		return false, durableStateError("verify committed artifact", err)
	}
	statusByPath := make(map[string]library.FileStatus, len(verification.Files))
	for _, file := range verification.Files {
		statusByPath[filepath.Clean(file.Path)] = file.Status
	}
	for _, file := range record.Manifest.Files {
		if statusByPath[filepath.Clean(file.Path)] != library.FilePresent {
			return false, nil
		}
	}
	return len(record.Manifest.Files) > 0, nil
}

func (watcher *Watcher) retry(ctx context.Context, operation func() error) error {
	var failures []error
	for attempt := 1; attempt <= watcher.options.MaxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(failures, err)...)
		}
		err := operation()
		if err == nil {
			return nil
		}
		failures = append(failures, err)
		if attempt == watcher.options.MaxRetries {
			break
		}
		timer := time.NewTimer(watcher.options.RetryBackoff(attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.Join(append(failures, ctx.Err())...)
		case <-timer.C:
		}
	}
	return errors.Join(failures...)
}

func durableStateError(operation string, err error) error {
	if err == nil {
		return nil
	}
	wrapped := fmt.Errorf("%s: %w", operation, err)
	if events.IsCancellation(err) {
		return wrapped
	}
	return errors.Join(ErrDurableState, wrapped)
}

func isFatalCycleError(err error) bool {
	return errors.Is(err, ErrEventDelivery) || errors.Is(err, ErrDurableState)
}
