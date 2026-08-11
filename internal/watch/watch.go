// Package watch owns generic lecture polling and durable auto-download. It
// stops at the committed Impartus artifact boundary and has no downstream
// provider or upload responsibility.
package watch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/rabesss/impartus-cli/internal/artifact"
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
	RecoverInterruptedJobs(context.Context, string) (library.RecoveryResult, error)
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
	retryable map[string][]library.Job
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
	options.Once = options.Once || options.Force
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
	return result, errors.Join(failures...)
}

func (watcher *Watcher) targetLectures(ctx context.Context, target config.WatchTarget) (client.Lectures, error) {
	lectures, err := watcher.source.GetLectures(ctx, watcher.cfg, client.Course{SubjectID: target.SubjectID, SessionID: target.SessionID})
	if err != nil {
		return nil, fmt.Errorf("list lectures for %s: %w", targetLabel(target), err)
	}
	if len(lectures) == 0 {
		return client.Lectures{}, nil
	}
	selected, _, selectionErr := lectures.SelectForDownload(0, 0, watcher.cfg.SkipNoAudio)
	if errors.Is(selectionErr, client.ErrNoLecturesAfterFiltering) {
		return client.Lectures{}, nil
	}
	if selectionErr != nil {
		return nil, fmt.Errorf("select lectures for %s: %w", targetLabel(target), selectionErr)
	}
	var catalog client.CourseCatalog
	if source, ok := watcher.source.(CourseSource); ok {
		catalog = retryingCourseCatalog{watcher: watcher, source: source}
	}
	if err := client.ResolveLectureScope(ctx, watcher.cfg, catalog, selected, target.SubjectID, target.SessionID); err != nil {
		return nil, fmt.Errorf("resolve lecture scope for %s: %w", targetLabel(target), err)
	}
	return selected, nil
}

type lectureOutcome struct {
	New, Skipped, Downloaded int
	Attempted                bool
	Outputs                  []string
	Artifacts                []artifact.Manifest
}

func (watcher *Watcher) inspectAndProcess(ctx context.Context, target config.WatchTarget, lecture client.Lecture, withinBudget bool) (lectureOutcome, error) {
	artifactID, identityErr := watcher.artifactIDForLecture(target, lecture)
	if identityErr != nil {
		return lectureOutcome{}, watcher.lectureFailure(target, lecture, "", identityErr)
	}
	skipped, skipErr := watcher.skipCommitted(ctx, target, lecture, artifactID)
	if skipped {
		return lectureOutcome{Skipped: 1}, skipErr
	}
	if skipErr != nil {
		return lectureOutcome{}, skipErr
	}
	outcome := lectureOutcome{New: 1}
	if !withinBudget {
		outcome.Skipped = 1
		return outcome, nil
	}
	outcome.Attempted = true
	lecture, playlist, expected, resolvedArtifactID, resolveErr := watcher.resolveLecture(ctx, target, lecture)
	if resolveErr != nil {
		return outcome, watcher.lectureFailure(target, lecture, "", resolveErr)
	}
	if resolvedArtifactID != artifactID {
		return outcome, watcher.lectureFailure(target, lecture, "", errors.New("resolved playlist changed lecture artifact identity"))
	}
	if watcher.options.DryRun {
		return outcome, nil
	}
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
	return true, nil
}

func targetLabel(target config.WatchTarget) string {
	if label := strings.TrimSpace(target.Label); label != "" {
		return label
	}
	return fmt.Sprintf("subject=%d session=%d", target.SubjectID, target.SessionID)
}
