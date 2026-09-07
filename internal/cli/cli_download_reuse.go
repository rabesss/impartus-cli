package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/rabesss/impartus-cli/internal/artifact"
	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/events"
	"github.com/rabesss/impartus-cli/internal/library"
	"github.com/rabesss/impartus-cli/internal/lockfile"
)

const (
	lectureOutcomeReused             = "reused"
	lectureOutcomeDownloaded         = "downloaded"
	lectureOutcomeVerificationFailed = "verification_failed"
	reuseReasonNotFound              = "not_found"
	reuseReasonLockBusy              = "lock_busy"
)

type reuseHit struct {
	lecture    client.Lecture
	artifactID string
	manifest   artifact.Manifest
}

type reuseDecision struct {
	hit      *reuseHit
	download bool
	lock     *lockfile.Lock
	outcome  lectureOutcome
	warning  string
}

type reusePartition struct {
	hits     []reuseHit
	download client.Lectures
	planned  []lectureOutcome
	locks    []*lockfile.Lock
	warnings []string
}

func (partition *reusePartition) close() error {
	if partition == nil {
		return nil
	}
	var err error
	for _, lock := range partition.locks {
		err = errors.Join(err, lock.Close())
	}
	partition.locks = nil
	return err
}

func executeReuseVerifiedDownload(
	ctx context.Context,
	cfg *config.Config,
	apiClient *client.Client,
	selected client.Lectures,
	presentation downloadPresentationOptions,
	deps downloadExecutionDependencies,
) (result downloadResult, err error) {
	partition, err := partitionReuseVerifiedLectures(ctx, cfg, selected, presentation, deps)
	defer func() {
		err = errors.Join(err, partition.close())
	}()
	if err != nil {
		return result, err
	}
	if emitErr := emitReusedLectureEvents(presentation.eventStream, partition.hits); emitErr != nil {
		return result, emitErr
	}

	result = downloadResult{
		Status:          "completed",
		OutputPaths:     reusedOutputPaths(partition.hits),
		LectureCount:    len(partition.hits),
		Artifacts:       reusedManifests(partition.hits),
		LibraryRecorded: len(partition.hits) > 0,
		Outcomes:        append([]lectureOutcome(nil), partition.planned...),
		Warnings:        append([]string(nil), partition.warnings...),
	}
	if len(partition.download) == 0 {
		return result, nil
	}
	if ffmpegErr := deps.ensureFFmpeg(); ffmpegErr != nil {
		return result, ffmpegErr
	}

	downloaded, downloadErr := deps.downloadLectures(ctx, cfg, apiClient, partition.download, presentation)
	result.Outcomes = finalizeDownloadOutcomes(partition.planned, downloaded.Artifacts, downloadErr)
	result.OutputPaths = append(reusedOutputPaths(partition.hits), downloaded.OutputPaths...)
	result.Artifacts = append(reusedManifests(partition.hits), downloaded.Artifacts...)
	result.LectureCount = len(partition.hits) + downloaded.LectureCount
	if downloadErr != nil {
		result.Status = "failed"
	}
	if downloadErr == nil || len(downloaded.Artifacts) > 0 {
		recorded := applyLibraryRecording(context.WithoutCancel(ctx), downloadResult{
			Artifacts: downloaded.Artifacts,
			Warnings:  result.Warnings,
		}, presentation, deps.recordArtifacts)
		result.Warnings = recorded.Warnings
		result.LibraryRecorded = recorded.LibraryRecorded
		result.Artifacts = append(reusedManifests(partition.hits), recorded.Artifacts...)
	}
	return result, downloadErr
}

func partitionReuseVerifiedLectures(
	ctx context.Context,
	cfg *config.Config,
	selected client.Lectures,
	presentation downloadPresentationOptions,
	deps downloadExecutionDependencies,
) (*reusePartition, error) {
	partition := &reusePartition{planned: make([]lectureOutcome, 0, len(selected))}
	store, err := openReuseLibrary(ctx, deps)
	if err != nil {
		return partition, err
	}
	defer closeLibraryStore(store)

	lockDir, err := reuseLockDirectory(deps)
	if err != nil {
		return partition, err
	}

	for _, lecture := range selected {
		decision, decideErr := decideReusableLecture(ctx, cfg, lecture, store, lockDir, deps.artifactLockWait)
		if decideErr != nil {
			return partition, decideErr
		}
		applyReuseDecision(partition, presentation, lecture, decision)
	}
	return partition, nil
}

func applyReuseDecision(partition *reusePartition, presentation downloadPresentationOptions, lecture client.Lecture, decision reuseDecision) {
	partition.planned = append(partition.planned, decision.outcome)
	if decision.hit != nil {
		partition.hits = append(partition.hits, *decision.hit)
		writeReuseHumanLine(presentation, decision.hit.artifactID, lecture.TTID)
		return
	}
	if decision.warning != "" {
		partition.warnings = append(partition.warnings, decision.warning)
		if presentation.warningOutput != nil {
			writeDownloadLibraryWarning(presentation.warningOutput, decision.warning)
		}
	}
	if decision.lock != nil {
		partition.locks = append(partition.locks, decision.lock)
	}
	if decision.download {
		partition.download = append(partition.download, lecture)
	}
}

func decideReusableLecture(
	ctx context.Context,
	cfg *config.Config,
	lecture client.Lecture,
	store *library.Store,
	lockDir string,
	wait time.Duration,
) (reuseDecision, error) {
	artifactID, identityErr := downloadArtifactID(lecture, cfg)
	if identityErr != nil {
		return reuseDecision{}, fmt.Errorf("invalid artifact identity for lecture %d: %w", lecture.TTID, identityErr)
	}
	lock, lockErr := lockfile.Acquire(lockfile.Path(lockDir, artifactID), wait)
	if errors.Is(lockErr, lockfile.ErrLocked) {
		return downloadReuseDecision(lecture, artifactID, reuseReasonLockBusy, nil), nil
	}
	if lockErr != nil {
		return reuseDecision{}, lockErr
	}
	return verifyReusableLecture(ctx, lecture, artifactID, store, lock)
}

func verifyReusableLecture(
	ctx context.Context,
	lecture client.Lecture,
	artifactID string,
	store *library.Store,
	lock *lockfile.Lock,
) (reuseDecision, error) {
	record, getErr := store.GetArtifact(ctx, artifactID)
	if errors.Is(getErr, library.ErrArtifactNotFound) {
		return downloadReuseDecision(lecture, artifactID, reuseReasonNotFound, lock), nil
	}
	if getErr != nil {
		return reuseDecision{}, errors.Join(getErr, lock.Close())
	}
	verified, verifyErr := store.VerifyArtifact(ctx, artifactID, library.VerifyOptions{
		Hash: true, Container: true, LatestMaterialization: true,
	})
	if verifyErr != nil {
		return reuseDecision{}, errors.Join(verifyErr, lock.Close())
	}
	if verified.OK {
		if closeErr := lock.Close(); closeErr != nil {
			return reuseDecision{}, closeErr
		}
		return reuseDecision{
			hit: &reuseHit{lecture: lecture, artifactID: artifactID, manifest: record.Manifest},
			outcome: lectureOutcome{
				TTID: lecture.TTID, ArtifactID: artifactID, Outcome: lectureOutcomeReused, Paths: manifestOutputPaths(record.Manifest),
			},
		}, nil
	}
	reason := firstVerificationFailure(verified)
	decision := downloadReuseDecision(lecture, artifactID, reason, lock)
	decision.warning = fmt.Sprintf("verification failed for artifact %s (%s); downloading", artifactID, reason)
	return decision, nil
}

func downloadReuseDecision(lecture client.Lecture, artifactID, reason string, lock *lockfile.Lock) reuseDecision {
	return reuseDecision{
		download: true,
		lock:     lock,
		outcome: lectureOutcome{
			TTID: lecture.TTID, ArtifactID: artifactID, Outcome: lectureOutcomeDownloaded, Reason: reason,
		},
	}
}

func openReuseLibrary(ctx context.Context, deps downloadExecutionDependencies) (*library.Store, error) {
	if deps.openLibrary != nil {
		return deps.openLibrary(ctx)
	}
	return library.Open(ctx, library.Options{})
}

func reuseLockDirectory(deps downloadExecutionDependencies) (string, error) {
	if deps.artifactLockDir != "" {
		return deps.artifactLockDir, nil
	}
	databasePath, err := library.DefaultPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(databasePath), "locks"), nil
}

func firstVerificationFailure(verified library.Verification) string {
	for _, file := range verified.Files {
		if file.Status != library.FilePresent && file.Status != "" {
			return string(file.Status)
		}
	}
	return lectureOutcomeVerificationFailed
}

func reusedManifests(hits []reuseHit) []artifact.Manifest {
	manifests := make([]artifact.Manifest, 0, len(hits))
	for _, hit := range hits {
		manifests = append(manifests, hit.manifest)
	}
	return manifests
}

func reusedOutputPaths(hits []reuseHit) []string {
	paths := make([]string, 0, len(hits))
	for _, hit := range hits {
		paths = append(paths, manifestOutputPaths(hit.manifest)...)
	}
	return paths
}

func writeReuseHumanLine(presentation downloadPresentationOptions, artifactID string, ttid int) {
	if presentation.progressOutput == nil {
		return
	}
	if _, err := fmt.Fprintf(presentation.progressOutput, "reusing verified artifact %s for ttid %d\n", artifactID, ttid); err != nil {
		return
	}
}

func emitReusedLectureEvents(stream *downloadEventStream, hits []reuseHit) error {
	for _, hit := range hits {
		if emitErr := stream.lecture(events.LectureStarted, hit.lecture, hit.artifactID, nil, nil, nil); emitErr != nil {
			return emitErr
		}
		if emitErr := stream.lecture(
			events.LectureCompleted,
			hit.lecture,
			hit.artifactID,
			&hit.manifest,
			manifestOutputPaths(hit.manifest),
			map[string]any{"stage": "artifact_reused"},
		); emitErr != nil {
			return emitErr
		}
	}
	return nil
}

func finalizeDownloadOutcomes(planned []lectureOutcome, downloaded []artifact.Manifest, downloadErr error) []lectureOutcome {
	completed := make(map[int]artifact.Manifest, len(downloaded))
	for _, manifest := range downloaded {
		completed[manifest.Lecture.TTID] = manifest
	}
	out := append([]lectureOutcome(nil), planned...)
	for index, outcome := range out {
		if outcome.Outcome == lectureOutcomeReused {
			continue
		}
		if manifest, ok := completed[outcome.TTID]; ok {
			out[index].Outcome = lectureOutcomeDownloaded
			out[index].Paths = manifestOutputPaths(manifest)
			continue
		}
		if downloadErr != nil && isVerificationFailureReason(outcome.Reason) {
			out[index].Outcome = lectureOutcomeVerificationFailed
		}
	}
	return out
}

func isVerificationFailureReason(reason string) bool {
	switch library.FileStatus(reason) {
	case library.FilePresent:
		return false
	case library.FileMissing, library.FileNotRegular, library.FileSizeMismatch, library.FileHashMismatch, library.FileUnreadable, library.FileContainerMismatch:
		return true
	default:
		return reason == lectureOutcomeVerificationFailed
	}
}
