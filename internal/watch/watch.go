// Package watch owns generic lecture polling and durable auto-download. It
// stops at the committed Impartus artifact boundary and has no downstream
// provider or upload responsibility.
package watch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/rabesss/impartus-cli/internal/artifact"
	"github.com/rabesss/impartus-cli/internal/buildinfo"
	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/downloader"
	"github.com/rabesss/impartus-cli/internal/events"
	"github.com/rabesss/impartus-cli/internal/library"
)

// ErrEventDelivery marks an event-stream failure that must stop a watcher
// immediately instead of being retried as a transient upstream cycle error.
var ErrEventDelivery = errors.New("watch event delivery failed")

// ErrDurableState marks a local library transition whose result may be
// ambiguous. The watcher must stop so startup recovery can reconcile it before
// any further download is attempted.
var ErrDurableState = errors.New("watch durable state transition failed")

// LectureSource lists lectures for one configured course target.
type LectureSource interface {
	GetLectures(context.Context, *config.Config, client.Course) (client.Lectures, error)
}

// CourseSource resolves omitted institute IDs from the authenticated catalog.
// Production clients implement this optional capability; test and alternate
// sources only need it when lecture payloads omit institute scope.
type CourseSource interface {
	GetCourses(context.Context, *config.Config) (client.Courses, error)
}

// Producer resolves playlists and atomically publishes their final media.
type Producer interface {
	FetchLecturePlaylists(context.Context, []client.Lecture) ([]client.ParsedPlaylist, error)
	DownloadAndJoinPlaylist(context.Context, client.ParsedPlaylist) (downloader.JoinResult, error)
}

// JobStore is the durable artifact and local-job boundary used by the watcher.
type JobStore interface {
	RecoverInterruptedJobs(context.Context) (library.RecoveryResult, error)
	ListJobs(context.Context) ([]library.Job, error)
	CreateJob(context.Context, library.JobSpec) error
	StartJob(context.Context, string) error
	FailJob(context.Context, string, error) error
	CancelJob(context.Context, string) error
	CompleteJob(context.Context, string, artifact.Manifest) error
	GetArtifact(context.Context, string) (library.ArtifactRecord, error)
	VerifyArtifact(context.Context, string, library.VerifyOptions) (library.Verification, error)
}

// Options controls one watcher process without adding provider-specific state.
type Options struct {
	Targets             []config.WatchTarget
	Once                bool
	DryRun              bool
	Force               bool
	Interval            time.Duration
	MaxRetries          int
	MaxLecturesPerCycle int
	RetryBackoff        func(int) time.Duration
	Emitter             events.Emitter
	Log                 io.Writer
	Now                 func() time.Time
	JobID               string
	StartupRecovery     *library.RecoveryResult
	DeferTerminal       bool
}

// CycleResult summarizes one completed polling cycle.
type CycleResult struct {
	Listed     int                 `json:"listed"`
	New        int                 `json:"new"`
	Skipped    int                 `json:"skipped"`
	Downloaded int                 `json:"downloaded"`
	Failed     int                 `json:"failed"`
	Recovered  []string            `json:"recovered,omitempty"`
	DryRun     bool                `json:"dryRun,omitempty"`
	Outputs    []string            `json:"outputs,omitempty"`
	Artifacts  []artifact.Manifest `json:"artifacts,omitempty"`
	Errors     []string            `json:"errors,omitempty"`
}

// Watcher polls generic course targets and commits completed local artifacts.
type Watcher struct {
	cfg       *config.Config
	source    LectureSource
	producer  Producer
	store     JobStore
	options   Options
	retryable map[string]library.Job
}

// New constructs a watcher from explicit source, producer, and durable-store boundaries.
func New(cfg *config.Config, source LectureSource, producer Producer, store JobStore, options Options) *Watcher {
	return &Watcher{cfg: cfg, source: source, producer: producer, store: store, options: normalizeOptions(cfg, options)}
}

type downloaderAdapter struct{ inner *downloader.Downloader }

func (adapter downloaderAdapter) FetchLecturePlaylists(ctx context.Context, lectures []client.Lecture) ([]client.ParsedPlaylist, error) {
	return adapter.inner.FetchLecturePlaylists(ctx, lectures)
}

func (adapter downloaderAdapter) DownloadAndJoinPlaylist(ctx context.Context, playlist client.ParsedPlaylist) (downloader.JoinResult, error) {
	return adapter.inner.DownloadAndJoin(ctx, playlist)
}

// NewFromDownloader wires the production Impartus client and downloader into a watcher.
func NewFromDownloader(cfg *config.Config, apiClient *client.Client, store JobStore, options Options) *Watcher {
	producer := downloaderAdapter{inner: downloader.NewWithDiagnosticWriter(cfg, apiClient, options.Log)}
	return New(cfg, apiClient, producer, store, options)
}

func normalizeOptions(cfg *config.Config, options Options) Options {
	options.Targets = normalizedTargets(cfg, options.Targets)
	options.Interval = normalizedInterval(cfg, options.Interval)
	options.MaxRetries = normalizedPositive(options.MaxRetries, watchRetries(cfg), 3)
	options.MaxLecturesPerCycle = normalizedPositive(options.MaxLecturesPerCycle, watchCycleBudget(cfg), 3)
	if options.RetryBackoff == nil {
		options.RetryBackoff = func(attempt int) time.Duration { return time.Duration(attempt*attempt) * time.Second }
	}
	if options.Emitter == nil {
		options.Emitter = discardEmitter{}
	}
	if options.Log == nil {
		options.Log = io.Discard
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if strings.TrimSpace(options.JobID) == "" {
		options.JobID = "job-" + uuid.NewString()
	}
	return options
}

func normalizedTargets(cfg *config.Config, targets []config.WatchTarget) []config.WatchTarget {
	if len(targets) == 0 && cfg != nil {
		return append([]config.WatchTarget(nil), cfg.Watch.Targets...)
	}
	return append([]config.WatchTarget(nil), targets...)
}

func normalizedInterval(cfg *config.Config, interval time.Duration) time.Duration {
	if interval > 0 {
		return interval
	}
	if cfg != nil {
		configured, parseErr := time.ParseDuration(cfg.Watch.PollInterval)
		if parseErr == nil && configured > 0 {
			return configured
		}
	}
	return 5 * time.Minute
}

func normalizedPositive(explicit, configured, fallback int) int {
	if explicit > 0 {
		return explicit
	}
	if configured > 0 {
		return configured
	}
	return fallback
}

func watchRetries(cfg *config.Config) int {
	if cfg == nil {
		return 0
	}
	return cfg.Watch.MaxRetries
}

func watchCycleBudget(cfg *config.Config) int {
	if cfg == nil {
		return 0
	}
	return cfg.Watch.MaxLecturesPerCycle
}

type discardEmitter struct{}

func (discardEmitter) Emit(events.Event) error { return nil }

// Run performs one cycle or polls until cancellation according to Options.
func (watcher *Watcher) Run(ctx context.Context) (CycleResult, error) {
	if ctx == nil {
		return CycleResult{}, errors.New("watch context is required")
	}
	if err := watcher.validate(); err != nil {
		return CycleResult{}, watcher.finish(err, CycleResult{DryRun: watcher.options.DryRun})
	}
	recovery, err := watcher.recover(ctx)
	result := CycleResult{DryRun: watcher.options.DryRun, Recovered: append([]string(nil), recovery.Recovered...)}
	if err != nil {
		return result, watcher.finish(err, result)
	}
	if err := watcher.loadRetryableJobs(ctx); err != nil {
		return result, watcher.finish(err, result)
	}
	if err := watcher.emit(events.Event{
		Type: events.JobStarted, Status: "running",
		Details: map[string]any{"recovered": recovery.Recovered, "pending": recovery.Pending},
	}); err != nil {
		return result, watcher.finish(err, result)
	}
	if err := watcher.emitRecoveredArtifacts(recovery.Artifacts); err != nil {
		return result, watcher.finish(err, result)
	}
	for {
		cycle, cycleErr := watcher.RunCycle(ctx)
		cycle.Recovered = append([]string(nil), result.Recovered...)
		result = cycle
		if isFatalCycleError(cycleErr) {
			return result, watcher.finish(cycleErr, result)
		}
		if watcher.options.Once {
			return result, watcher.finish(cycleErr, result)
		}
		if cycleErr != nil {
			watcher.logf("watch cycle failed; retrying after %s: %s", watcher.options.Interval, events.RedactError(cycleErr))
		}
		timer := time.NewTimer(watcher.options.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return result, watcher.finish(ctx.Err(), result)
		case <-timer.C:
		}
	}
}

func (watcher *Watcher) recover(ctx context.Context) (library.RecoveryResult, error) {
	if watcher.options.DryRun {
		return library.RecoveryResult{}, nil
	}
	if watcher.options.StartupRecovery == nil {
		recovery, err := watcher.store.RecoverInterruptedJobs(context.WithoutCancel(ctx))
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
			Type: events.ArtifactCommitted,
			Target: &events.Target{
				SubjectID: manifest.Lecture.SubjectID,
				SessionID: manifest.Lecture.SessionID,
			},
			Lecture: &events.Lecture{
				TTID: manifest.Lecture.TTID, SeqNo: manifest.Lecture.SeqNo, Topic: manifest.Lecture.Topic,
			},
			Artifact: &manifest,
			Details: map[string]any{
				"libraryJobId": item.JobID, "artifactId": manifest.ArtifactID, "recovered": true,
			},
		}); err != nil {
			return err
		}
	}
	return nil
}

func (watcher *Watcher) validate() error {
	if watcher == nil || watcher.cfg == nil || watcher.source == nil || watcher.store == nil {
		return errors.New("watcher is not fully configured")
	}
	if watcher.producer == nil {
		return errors.New("watch producer is required")
	}
	if len(watcher.options.Targets) == 0 {
		return errors.New("watch targets are required")
	}
	return nil
}

func (watcher *Watcher) loadRetryableJobs(ctx context.Context) error {
	jobs, err := watcher.store.ListJobs(ctx)
	if err != nil {
		return durableStateError("load recoverable watch jobs", err)
	}
	watcher.retryable = make(map[string]library.Job)
	for _, job := range jobs {
		if job.Kind != "watch" || (job.Status != library.JobPending && job.Status != library.JobRecoverable) {
			continue
		}
		if _, exists := watcher.retryable[job.LogicalArtifactID]; !exists {
			watcher.retryable[job.LogicalArtifactID] = job
		}
	}
	return nil
}

// RunCycle polls every target once while enforcing one global download budget.
func (watcher *Watcher) RunCycle(ctx context.Context) (CycleResult, error) {
	result := CycleResult{DryRun: watcher.options.DryRun}
	remaining := watcher.options.MaxLecturesPerCycle
	var failures []error
	for _, target := range watcher.options.Targets {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		selected, err := watcher.targetLectures(ctx, target)
		result.Listed += len(selected)
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, events.RedactError(err))
			failures = append(failures, err)
			continue
		}
		for _, lecture := range selected {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			if err := watcher.emitLecture(events.LectureDiscovered, target, lecture, "", nil); err != nil {
				return result, err
			}
			outcome, processErr := watcher.inspectAndProcess(ctx, target, lecture, remaining > 0)
			result.New += outcome.New
			result.Skipped += outcome.Skipped
			result.Downloaded += outcome.Downloaded
			result.Outputs = append(result.Outputs, outcome.Outputs...)
			result.Artifacts = append(result.Artifacts, outcome.Artifacts...)
			if outcome.Attempted {
				remaining--
			}
			if processErr != nil {
				fatal := isFatalCycleError(processErr)
				ctxErr := ctx.Err()
				if ctxErr != nil && !fatal {
					return result, ctxErr
				}
				result.Failed++
				result.Errors = append(result.Errors, events.RedactError(processErr))
				failures = append(failures, processErr)
				if fatal {
					return result, errors.Join(processErr, ctxErr)
				}
			}
		}
	}
	if err := watcher.emit(events.Event{
		Type: events.CycleCompleted, Status: statusForFailures(failures),
		Details: map[string]any{"listed": result.Listed, "new": result.New, "skipped": result.Skipped, "downloaded": result.Downloaded, "failed": result.Failed, "dryRun": result.DryRun},
	}); err != nil {
		return result, err
	}
	return result, errors.Join(failures...)
}

func (watcher *Watcher) targetLectures(ctx context.Context, target config.WatchTarget) (client.Lectures, error) {
	lectures, err := watcher.source.GetLectures(ctx, watcher.cfg, client.Course{SubjectID: target.SubjectID, SessionID: target.SessionID})
	if err != nil {
		return nil, fmt.Errorf("list lectures for %s: %w", targetLabel(target), err)
	}
	selected := lectures.Reverse().FilterNoAudio()
	scoped, err := watcher.resolveTargetScope(ctx, target, selected)
	if err != nil {
		return selected, fmt.Errorf("resolve lecture scope for %s: %w", targetLabel(target), err)
	}
	return scoped, nil
}

type lectureOutcome struct {
	New, Skipped, Downloaded int
	Attempted                bool
	Outputs                  []string
	Artifacts                []artifact.Manifest
}

func (watcher *Watcher) inspectAndProcess(ctx context.Context, target config.WatchTarget, lecture client.Lecture, withinBudget bool) (lectureOutcome, error) {
	lecture, localArtifactID, identityErr := watcher.localArtifactID(target, lecture)
	if identityErr != nil {
		return lectureOutcome{}, watcher.lectureFailure(target, lecture, "", identityErr)
	}
	if localArtifactID != "" {
		skipped, skipErr := watcher.skipCommitted(ctx, target, lecture, localArtifactID)
		if skipped {
			return lectureOutcome{Skipped: 1}, skipErr
		}
		if skipErr != nil {
			return lectureOutcome{}, skipErr
		}
	}
	outcome := lectureOutcome{New: 1}
	if !watcher.options.DryRun && !withinBudget {
		outcome.Skipped = 1
		emitErr := watcher.emitLecture(events.LectureSkipped, target, lecture, localArtifactID, map[string]any{"reason": "cycle_budget"})
		return outcome, emitErr
	}
	lecture, playlist, expected, artifactID, resolveErr := watcher.resolveLecture(ctx, target, lecture)
	if resolveErr != nil {
		return lectureOutcome{}, watcher.lectureFailure(target, lecture, "", resolveErr)
	}
	if localArtifactID == "" || localArtifactID != artifactID {
		skipped, skipErr := watcher.skipCommitted(ctx, target, lecture, artifactID)
		if skipped {
			return lectureOutcome{Skipped: 1}, skipErr
		}
		if skipErr != nil {
			return lectureOutcome{}, skipErr
		}
	}
	if watcher.options.DryRun {
		return outcome, nil
	}
	outcome.Attempted = true
	manifest, downloadErr := watcher.downloadLecture(ctx, target, lecture, playlist, expected, artifactID)
	if manifest.ArtifactID != "" {
		outcome.Downloaded = 1
		outcome.Outputs = manifestPaths(manifest)
		outcome.Artifacts = []artifact.Manifest{manifest}
	}
	if downloadErr != nil {
		return outcome, downloadErr
	}
	return outcome, nil
}

// localArtifactID derives the logical artifact identity from catalog data so a
// verified local commit can be skipped without resolving an obsolete or
// temporarily unavailable remote playlist. Some upstream catalog responses do
// not include instituteId; those fall back to the post-resolution check.
func (watcher *Watcher) localArtifactID(target config.WatchTarget, lecture client.Lecture) (client.Lecture, string, error) {
	if !scopeMatches(target.SubjectID, lecture.SubjectID) {
		return lecture, "", fmt.Errorf("subject scope mismatch for lecture %d", lecture.TTID)
	}
	if !scopeMatches(target.SessionID, lecture.SessionID) {
		return lecture, "", fmt.Errorf("session scope mismatch for lecture %d", lecture.TTID)
	}
	if lecture.SubjectID == 0 {
		lecture.SubjectID = target.SubjectID
	}
	if lecture.SessionID == 0 {
		lecture.SessionID = target.SessionID
	}
	if lecture.TTID <= 0 {
		return lecture, "", errors.New("lecture ttid must be positive")
	}
	if lecture.InstituteID <= 0 {
		return lecture, "", nil
	}
	id, err := artifact.NewID(artifact.Identity{
		InstituteID: lecture.InstituteID,
		SubjectID:   lecture.SubjectID,
		SessionID:   lecture.SessionID,
		TTID:        lecture.TTID,
		AudioOnly:   watcher.cfg.AudioOnly,
		Views:       watcher.cfg.Views,
		Quality:     watcher.cfg.Quality,
		AudioFormat: watcher.cfg.AudioFormat,
	})
	return lecture, id, err
}

func (watcher *Watcher) resolveLecture(
	ctx context.Context,
	target config.WatchTarget,
	lecture client.Lecture,
) (client.Lecture, client.ParsedPlaylist, library.ExpectedArtifact, string, error) {
	if lecture.SubjectID == 0 {
		lecture.SubjectID = target.SubjectID
	}
	if lecture.SessionID == 0 {
		lecture.SessionID = target.SessionID
	}
	playlists, err := watcher.fetchPlaylists(ctx, lecture)
	if err != nil {
		return lecture, client.ParsedPlaylist{}, library.ExpectedArtifact{}, "", fmt.Errorf("resolve lecture playlist: %w", err)
	}
	if len(playlists) != 1 {
		return lecture, client.ParsedPlaylist{}, library.ExpectedArtifact{}, "", fmt.Errorf("expected one playlist for lecture %d, got %d", lecture.TTID, len(playlists))
	}
	lecture, playlist, scopeErr := normalizeScope(target, lecture, playlists[0])
	if scopeErr != nil {
		return lecture, playlist, library.ExpectedArtifact{}, "", scopeErr
	}
	playlist.Title = watchScopedTitle(lecture, playlist.Title)
	plan, planErr := downloader.PlanJoinResult(watcher.cfg, playlist)
	if planErr != nil {
		return lecture, playlist, library.ExpectedArtifact{}, "", planErr
	}
	expected := expectedArtifact(lecture, watcher.cfg, plan, watcher.options.Now().UTC())
	artifactID, identityErr := artifact.NewID(expectedIdentity(expected))
	if identityErr != nil {
		return lecture, playlist, expected, "", identityErr
	}
	return lecture, playlist, expected, artifactID, nil
}

func (watcher *Watcher) skipCommitted(ctx context.Context, target config.WatchTarget, lecture client.Lecture, artifactID string) (bool, error) {
	if watcher.options.Force {
		return false, nil
	}
	committed, committedErr := watcher.committed(ctx, artifactID)
	if committedErr != nil {
		return false, watcher.lectureFailure(target, lecture, "", committedErr)
	}
	if !committed {
		return false, nil
	}
	emitErr := watcher.emitLecture(events.LectureSkipped, target, lecture, artifactID, map[string]any{"reason": "artifact_committed"})
	return true, emitErr
}

func (watcher *Watcher) fetchPlaylists(ctx context.Context, lecture client.Lecture) ([]client.ParsedPlaylist, error) {
	var result []client.ParsedPlaylist
	err := watcher.retry(ctx, func() error {
		var err error
		result, err = watcher.producer.FetchLecturePlaylists(ctx, []client.Lecture{lecture})
		return err
	})
	return result, err
}

func (watcher *Watcher) downloadLecture(ctx context.Context, target config.WatchTarget, lecture client.Lecture, playlist client.ParsedPlaylist, expected library.ExpectedArtifact, artifactID string) (artifact.Manifest, error) {
	jobID := uuid.NewString()
	if reusable, exists := watcher.retryable[artifactID]; exists {
		if expectedPathsEqual(reusable.Expected, expected) {
			jobID = reusable.ID
			expected = reusable.Expected
		} else {
			if err := watcher.store.FailJob(context.WithoutCancel(ctx), reusable.ID, errors.New("recoverable job was superseded by a new output plan")); err != nil {
				return artifact.Manifest{}, watcher.lectureFailure(target, lecture, reusable.ID, durableStateError("fail superseded recoverable watch job", err))
			}
			delete(watcher.retryable, artifactID)
		}
	}
	if reusable, exists := watcher.retryable[artifactID]; !exists || reusable.ID != jobID {
		if err := watcher.store.CreateJob(context.WithoutCancel(ctx), library.JobSpec{ID: jobID, Kind: "watch", Expected: expected}); err != nil {
			return artifact.Manifest{}, watcher.lectureFailure(target, lecture, jobID, durableStateError("create durable watch job", err))
		}
	}
	if err := watcher.store.StartJob(context.WithoutCancel(ctx), jobID); err != nil {
		return artifact.Manifest{}, watcher.lectureFailure(target, lecture, jobID, durableStateError("start durable watch job", err))
	}
	delete(watcher.retryable, artifactID)
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
		return artifact.Manifest{}, watcher.lectureFailure(target, lecture, jobID, errors.Join(err, failErr))
	}
	manifest, err := manifestFromExpected(expected, joined)
	if err != nil {
		failErr := durableStateError("fail watch job after manifest validation", watcher.store.FailJob(context.WithoutCancel(ctx), jobID, err))
		return artifact.Manifest{}, watcher.lectureFailure(target, lecture, jobID, errors.Join(err, failErr))
	}
	if err := watcher.store.CompleteJob(context.WithoutCancel(ctx), jobID, manifest); err != nil {
		return artifact.Manifest{}, watcher.lectureFailure(target, lecture, jobID, durableStateError("commit durable watch artifact", err))
	}
	if err := watcher.emit(events.Event{
		Type:     events.ArtifactCommitted,
		Target:   &events.Target{SubjectID: target.SubjectID, SessionID: target.SessionID, Label: target.Label},
		Lecture:  &events.Lecture{TTID: lecture.TTID, SeqNo: lecture.SeqNo, Topic: lecture.Topic},
		Artifact: &manifest, Details: map[string]any{"libraryJobId": jobID, "artifactId": manifest.ArtifactID},
	}); err != nil {
		return manifest, err
	}
	return manifest, nil
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

func (watcher *Watcher) finish(cause error, result CycleResult) error {
	safeCause := events.RedactedError(cause)
	if watcher.options.DeferTerminal {
		return safeCause
	}
	event := events.Event{Type: events.JobCompleted, Status: "completed", Outputs: append([]string(nil), result.Outputs...), Details: result}
	if cause != nil {
		if (errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded)) && !isFatalCycleError(cause) {
			event = events.Cancellation(watcher.options.JobID, "watch", cause, watcher.options.Now())
		} else {
			event = events.Failure(watcher.options.JobID, "watch", cause, watcher.options.Now())
		}
	}
	event.Details = result
	return errors.Join(safeCause, watcher.emit(event))
}

func (watcher *Watcher) emit(event events.Event) error {
	event.SchemaVersion = events.SchemaVersion
	event.JobID = watcher.options.JobID
	event.Command = "watch"
	if event.Timestamp.IsZero() {
		event.Timestamp = watcher.options.Now().UTC()
	}
	if err := watcher.options.Emitter.Emit(event); err != nil {
		return errors.Join(ErrEventDelivery, events.RedactedError(err))
	}
	return nil
}

func (watcher *Watcher) emitLecture(eventType string, target config.WatchTarget, lecture client.Lecture, artifactID string, details any) error {
	event := events.Event{
		Type:    eventType,
		Target:  &events.Target{SubjectID: target.SubjectID, SessionID: target.SessionID, Label: target.Label},
		Lecture: &events.Lecture{TTID: lecture.TTID, SeqNo: lecture.SeqNo, Topic: lecture.Topic},
		Details: details,
	}
	if artifactID != "" {
		if values, ok := details.(map[string]any); ok {
			values["artifactId"] = artifactID
		} else {
			event.Details = map[string]any{"artifactId": artifactID}
		}
	}
	return watcher.emit(event)
}

func (watcher *Watcher) lectureFailure(target config.WatchTarget, lecture client.Lecture, jobID string, cause error) error {
	details := map[string]any{"error": events.RedactError(cause)}
	if jobID != "" {
		details["libraryJobId"] = jobID
	}
	if err := watcher.emitLecture(events.LectureFailed, target, lecture, "", details); err != nil {
		return errors.Join(events.RedactedError(cause), err)
	}
	return events.RedactedError(cause)
}

func (watcher *Watcher) logf(format string, values ...any) {
	_, _ = fmt.Fprintf(watcher.options.Log, format+"\n", values...) //nolint:errcheck // diagnostics are best-effort
}

func normalizeScope(target config.WatchTarget, lecture client.Lecture, playlist client.ParsedPlaylist) (client.Lecture, client.ParsedPlaylist, error) {
	if lecture.TTID <= 0 || playlist.ID != lecture.TTID {
		return lecture, playlist, errors.New("playlist does not match its source lecture")
	}
	if !scopeMatches(target.SubjectID, lecture.SubjectID, playlist.SubjectID) {
		return lecture, playlist, fmt.Errorf("subject scope mismatch for lecture %d", lecture.TTID)
	}
	if !scopeMatches(target.SessionID, lecture.SessionID, playlist.SessionID) {
		return lecture, playlist, fmt.Errorf("session scope mismatch for lecture %d", lecture.TTID)
	}
	instituteID := firstPositive(lecture.InstituteID, playlist.InstituteID)
	if !scopeMatches(instituteID, lecture.InstituteID, playlist.InstituteID) {
		return lecture, playlist, fmt.Errorf("institute scope mismatch for lecture %d", lecture.TTID)
	}
	lecture.SubjectID, lecture.SessionID, lecture.InstituteID = target.SubjectID, target.SessionID, instituteID
	playlist.SubjectID, playlist.SessionID, playlist.InstituteID = lecture.SubjectID, lecture.SessionID, instituteID
	if lecture.SeqNo == 0 {
		lecture.SeqNo = playlist.SeqNo
	}
	if playlist.SeqNo == 0 {
		playlist.SeqNo = lecture.SeqNo
	}
	if strings.TrimSpace(lecture.Topic) == "" {
		lecture.Topic = playlist.Title
	}
	if strings.TrimSpace(playlist.Title) == "" {
		playlist.Title = lecture.Topic
	}
	return lecture, playlist, nil
}

func (watcher *Watcher) resolveTargetScope(ctx context.Context, target config.WatchTarget, lectures client.Lectures) (client.Lectures, error) {
	scoped, institutes, missingInstitute, err := applyTargetScope(target, lectures)
	if err != nil || !missingInstitute {
		return scoped, err
	}
	instituteID := onlyInstitute(institutes)
	if instituteID == 0 {
		instituteID, err = watcher.catalogInstitute(ctx, target)
		if err != nil {
			return nil, err
		}
	}
	for index := range scoped {
		if scoped[index].InstituteID == 0 {
			scoped[index].InstituteID = instituteID
		}
	}
	return scoped, nil
}

func applyTargetScope(target config.WatchTarget, lectures client.Lectures) (client.Lectures, map[int]struct{}, bool, error) {
	scoped := append(client.Lectures(nil), lectures...)
	institutes := make(map[int]struct{})
	missingInstitute := false
	for index := range scoped {
		lecture := &scoped[index]
		if lecture.SubjectID > 0 && lecture.SubjectID != target.SubjectID {
			return nil, nil, false, fmt.Errorf("subject scope mismatch for lecture %d", lecture.TTID)
		}
		if lecture.SessionID > 0 && lecture.SessionID != target.SessionID {
			return nil, nil, false, fmt.Errorf("session scope mismatch for lecture %d", lecture.TTID)
		}
		lecture.SubjectID = target.SubjectID
		lecture.SessionID = target.SessionID
		if lecture.InstituteID > 0 {
			institutes[lecture.InstituteID] = struct{}{}
		} else {
			missingInstitute = true
		}
	}
	if len(institutes) > 1 {
		return nil, nil, false, errors.New("ambiguous institute scope for selected lectures")
	}
	return scoped, institutes, missingInstitute, nil
}

func (watcher *Watcher) catalogInstitute(ctx context.Context, target config.WatchTarget) (int, error) {
	catalog, ok := watcher.source.(CourseSource)
	if !ok {
		return 0, errors.New("cannot resolve missing institute scope: course catalog is unavailable")
	}
	var courses client.Courses
	if err := watcher.retry(ctx, func() error {
		var catalogErr error
		courses, catalogErr = catalog.GetCourses(ctx, watcher.cfg)
		return catalogErr
	}); err != nil {
		return 0, fmt.Errorf("resolve missing institute scope from course catalog: %w", err)
	}
	institutes := matchingInstitutes(courses, target)
	if len(institutes) > 1 {
		return 0, errors.New("ambiguous institute scope for selected lectures")
	}
	instituteID := onlyInstitute(institutes)
	if instituteID == 0 {
		return 0, fmt.Errorf("cannot resolve institute scope for subject=%d session=%d", target.SubjectID, target.SessionID)
	}
	return instituteID, nil
}

func matchingInstitutes(courses client.Courses, target config.WatchTarget) map[int]struct{} {
	institutes := make(map[int]struct{})
	for _, course := range courses {
		if course.SubjectID == target.SubjectID && course.SessionID == target.SessionID && course.InstituteID > 0 {
			institutes[course.InstituteID] = struct{}{}
		}
	}
	return institutes
}

func onlyInstitute(institutes map[int]struct{}) int {
	for instituteID := range institutes {
		return instituteID
	}
	return 0
}

func watchScopedTitle(lecture client.Lecture, title string) string {
	return fmt.Sprintf(
		"inst-%d_sub-%d_sess-%d_ttid-%d %s",
		lecture.InstituteID,
		lecture.SubjectID,
		lecture.SessionID,
		lecture.TTID,
		strings.TrimSpace(title),
	)
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func scopeMatches(expected int, values ...int) bool {
	if expected <= 0 {
		return false
	}
	for _, value := range values {
		if value > 0 && value != expected {
			return false
		}
	}
	return true
}

func expectedArtifact(lecture client.Lecture, cfg *config.Config, plan downloader.JoinResult, producedAt time.Time) library.ExpectedArtifact {
	role := "video"
	if cfg.AudioOnly {
		role = "audio"
	}
	files := make([]library.ExpectedFile, 0, 3)
	for _, output := range []struct{ path, view, container string }{
		{plan.LeftOutput, "left", plan.LeftContainer},
		{plan.RightOutput, "right", plan.RightContainer},
		{plan.BothOutput, "both", plan.BothContainer},
	} {
		if output.path != "" {
			files = append(files, library.ExpectedFile{Path: output.path, Role: role, View: output.view, Container: output.container})
		}
	}
	return library.ExpectedArtifact{
		Lecture: artifact.Lecture{
			TTID: lecture.TTID, InstituteID: lecture.InstituteID, SubjectID: lecture.SubjectID,
			SessionID: lecture.SessionID, SeqNo: lecture.SeqNo, Topic: lecture.Topic,
			StartTime: lecture.StartTime, DurationSeconds: lecture.ActualDuration,
			Professor: lecture.ProfessorName, Institute: lecture.Institute, NoAudio: lecture.NoAudio == 1,
		},
		Selection: artifact.Selection{Views: cfg.Views, Quality: cfg.Quality, AudioOnly: cfg.AudioOnly, AudioFormat: cfg.AudioFormat},
		Files:     files, ProducedAt: producedAt, Producer: artifact.Producer{Name: "impartus", Version: buildinfo.Version},
	}
}

func expectedIdentity(expected library.ExpectedArtifact) artifact.Identity {
	return artifact.Identity{
		InstituteID: expected.Lecture.InstituteID, SubjectID: expected.Lecture.SubjectID,
		SessionID: expected.Lecture.SessionID, TTID: expected.Lecture.TTID,
		AudioOnly: expected.Selection.AudioOnly, Views: expected.Selection.Views,
		Quality: expected.Selection.Quality, AudioFormat: expected.Selection.AudioFormat,
	}
}

func manifestFromExpected(expected library.ExpectedArtifact, joined downloader.JoinResult) (artifact.Manifest, error) {
	role := "video"
	if expected.Selection.AudioOnly {
		role = "audio"
	}
	files := make([]artifact.FileSpec, 0, 3)
	for _, output := range []struct{ path, view, container string }{
		{joined.LeftOutput, "left", joined.LeftContainer},
		{joined.RightOutput, "right", joined.RightContainer},
		{joined.BothOutput, "both", joined.BothContainer},
	} {
		if output.path != "" {
			files = append(files, artifact.FileSpec{Path: output.path, Role: role, View: output.view, Container: output.container})
		}
	}
	return artifact.Build(artifact.BuildInput{
		Lecture: expected.Lecture, Selection: expected.Selection, Files: files,
		ProducedAt: expected.ProducedAt, Producer: expected.Producer,
	})
}

func expectedPathsEqual(left, right library.ExpectedArtifact) bool {
	if len(left.Files) != len(right.Files) || left.Selection != right.Selection {
		return false
	}
	for index := range left.Files {
		leftPath, leftErr := filepath.Abs(filepath.Clean(left.Files[index].Path))
		rightPath, rightErr := filepath.Abs(filepath.Clean(right.Files[index].Path))
		if leftErr != nil || rightErr != nil {
			return false
		}
		if leftPath != rightPath || left.Files[index].Role != right.Files[index].Role ||
			left.Files[index].View != right.Files[index].View || left.Files[index].Container != right.Files[index].Container {
			return false
		}
	}
	return true
}

func manifestPaths(manifest artifact.Manifest) []string {
	paths := make([]string, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		paths = append(paths, file.Path)
	}
	return paths
}

func targetLabel(target config.WatchTarget) string {
	if label := strings.TrimSpace(target.Label); label != "" {
		return label
	}
	return fmt.Sprintf("subject=%d session=%d", target.SubjectID, target.SessionID)
}

func statusForFailures(failures []error) string {
	if len(failures) > 0 {
		return "failed"
	}
	return "completed"
}

func durableStateError(operation string, err error) error {
	if err == nil {
		return nil
	}
	wrapped := fmt.Errorf("%s: %w", operation, err)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return wrapped
	}
	return errors.Join(ErrDurableState, wrapped)
}

func isFatalCycleError(err error) bool {
	return errors.Is(err, ErrEventDelivery) || errors.Is(err, ErrDurableState)
}
