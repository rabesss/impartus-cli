package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/vbauerster/mpb/v8"

	"github.com/rabesss/impartus-cli/internal/app"
	"github.com/rabesss/impartus-cli/internal/artifact"
	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/downloader"
	"github.com/rabesss/impartus-cli/internal/events"
	"github.com/rabesss/impartus-cli/internal/library"
	"github.com/rabesss/impartus-cli/internal/paths"
	"github.com/rabesss/impartus-cli/internal/secrets"
)

type downloadResult struct {
	Status          string              `json:"status"`
	OutputPaths     []string            `json:"outputPaths"`
	LectureCount    int                 `json:"lectureCount"`
	FilteredCount   int                 `json:"filteredCount,omitempty"`
	TotalLectures   int                 `json:"totalLectures,omitempty"`
	Artifacts       []artifact.Manifest `json:"artifacts"`
	LibraryRecorded bool                `json:"libraryRecorded"`
	Warnings        []string            `json:"-"`
}

// downloadPresentationOptions keeps user-facing output policy at the CLI
// boundary. Machine-readable commands leave progress and warning writers nil
// and discard downloader diagnostics so stdout/stderr stay structured.
type downloadPresentationOptions struct {
	showProgress     bool
	progressOutput   io.Writer
	warningOutput    io.Writer
	diagnosticOutput io.Writer
	eventStream      *downloadEventStream
}

func humanDownloadPresentation() downloadPresentationOptions {
	return downloadPresentationOptions{
		showProgress:   true,
		progressOutput: os.Stdout,
		warningOutput:  os.Stderr,
	}
}

func quietDownloadPresentation() downloadPresentationOptions {
	return downloadPresentationOptions{diagnosticOutput: io.Discard}
}

type downloadExecutionDependencies struct {
	ensureFFmpeg     func() error
	initClient       func(context.Context) (*config.Config, *client.Client, error)
	downloadLectures func(context.Context, *config.Config, *client.Client, client.Lectures, downloadPresentationOptions) (downloadResult, error)
	recordArtifacts  func(context.Context, []artifact.Manifest) error
}

var errDownloadEventDelivery = errors.New("download event delivery failed")
var errDownloadLibraryCommit = errors.New("download library commit failed")

type lectureDownloadRunner interface {
	FetchLecturePlaylists(context.Context, []client.Lecture) ([]client.ParsedPlaylist, error)
	DownloadAndJoinPlaylist(context.Context, client.ParsedPlaylist, *mpb.Progress, *downloader.ProgressTracker) (downloader.JoinResult, error)
}

func defaultDownloadExecutionDependencies() downloadExecutionDependencies {
	return downloadExecutionDependencies{
		ensureFFmpeg:     ensureFFmpeg,
		initClient:       initClient,
		downloadLectures: downloadLectures,
		recordArtifacts:  recordDownloadedArtifacts,
	}
}

func runDownload(args []string) error {
	return runDownloadWithDependencies(args, defaultDownloadExecutionDependencies())
}

func runDownloadWithDependencies(args []string, deps downloadExecutionDependencies) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runDownloadWithDependenciesContext(ctx, args, deps)
}

func runDownloadWithDependenciesContext(ctx context.Context, args []string, deps downloadExecutionDependencies) error {
	if requestedEvents(args) {
		err := runDownloadEventsWithDependenciesContext(ctx, args, os.Stdout, deps, time.Now, "")
		return downloadCommandError(ctx, err)
	}
	_, err := executeDownloadWithDependenciesContext(ctx, args, humanDownloadPresentation(), deps)
	return downloadCommandError(ctx, err)
}

func downloadCommandError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errDownloadEventDelivery) || errors.Is(err, errDownloadLibraryCommit) {
		return events.RedactedError(err)
	}
	if events.IsCancellationForContext(ctx, err) {
		return &exitCodeError{code: 130, err: events.RedactedError(err)}
	}
	return events.RedactedError(err)
}

func runDownloadJSON(args []string) (downloadResult, error) {
	return runDownloadJSONWithSignalDependencies(args, defaultDownloadExecutionDependencies())
}

func runDownloadJSONWithSignalDependencies(args []string, deps downloadExecutionDependencies) (downloadResult, error) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result, err := runDownloadJSONWithDependenciesContext(ctx, args, deps)
	return result, downloadCommandError(ctx, err)
}

func runDownloadJSONWithDependencies(args []string, deps downloadExecutionDependencies) (downloadResult, error) {
	return runDownloadJSONWithDependenciesContext(context.Background(), args, deps)
}

func runDownloadJSONWithDependenciesContext(ctx context.Context, args []string, deps downloadExecutionDependencies) (downloadResult, error) {
	if requestedEvents(args) {
		return downloadResult{}, errors.New("cannot combine --json and --events")
	}
	return executeDownloadWithDependenciesContext(ctx, args, quietDownloadPresentation(), deps)
}

func executeDownloadWithDependencies(args []string, presentation downloadPresentationOptions, deps downloadExecutionDependencies) (downloadResult, error) {
	return executeDownloadWithDependenciesContext(context.Background(), args, presentation, deps)
}

func executeDownloadWithDependenciesContext(ctx context.Context, args []string, presentation downloadPresentationOptions, deps downloadExecutionDependencies) (downloadResult, error) {
	if ctx == nil {
		return downloadResult{}, errors.New("download context is required")
	}
	f, err := parseDownloadFlags(args)
	if err != nil {
		return downloadResult{}, err
	}

	if ffmpegErr := deps.ensureFFmpeg(); ffmpegErr != nil {
		return downloadResult{}, ffmpegErr
	}

	cfg, apiClient, err := deps.initClient(ctx)
	if err != nil {
		return downloadResult{}, err
	}

	cfg, err = applyAndValidateFlags(cfg, f.quality, f.views, f.audioOnly, f.format, f.output, f.skipNoAudio)
	if err != nil {
		return downloadResult{}, err
	}

	if f.includeNoAudio {
		cfg.SkipNoAudio = false
	}

	lectures, err := apiClient.GetLectures(ctx, cfg, client.Course{SubjectID: f.subject, SessionID: f.session})
	if err != nil {
		return downloadResult{}, err
	}

	var selected client.Lectures
	var filteredCount int
	if f.ttidSet {
		selected, filteredCount, err = lectures.SelectForDownloadTTIDInScope(f.ttid, f.subject, f.session, cfg.SkipNoAudio)
	} else {
		selected, filteredCount, err = lectures.SelectForDownload(f.start, f.end, cfg.SkipNoAudio)
	}
	if err != nil {
		return downloadResult{}, err
	}
	if scopeErr := client.ResolveLectureScope(ctx, cfg, apiClient, selected, f.subject, f.session); scopeErr != nil {
		return downloadResult{}, scopeErr
	}

	// Warn about no-audio lectures in the selection (only when not filtering).
	totalLectures := len(selected) + filteredCount
	warnNoAudioLectures(presentation.warningOutput, selected, cfg.SkipNoAudio)

	result, err := deps.downloadLectures(ctx, cfg, apiClient, selected, presentation)
	// Once media has been published, finish the durable library commit even if
	// a signal races with this short post-download transition. The event stream
	// must not report completed media as an unrecorded hard failure merely
	// because its request context was canceled after the producer returned.
	if err == nil || len(result.Artifacts) > 0 {
		result = applyLibraryRecording(context.WithoutCancel(ctx), result, presentation, deps.recordArtifacts)
	}
	result.FilteredCount = filteredCount
	result.TotalLectures = totalLectures
	return result, err
}

func recordDownloadedArtifacts(ctx context.Context, manifests []artifact.Manifest) error {
	store, err := library.Open(ctx, library.Options{})
	if err != nil {
		return err
	}
	recordErr := store.RecordManifests(ctx, manifests)
	return finishCommittedLibraryOperation(recordErr, store.Close)
}

func applyLibraryRecording(
	ctx context.Context,
	result downloadResult,
	presentation downloadPresentationOptions,
	record func(context.Context, []artifact.Manifest) error,
) downloadResult {
	if len(result.Artifacts) == 0 {
		result.LibraryRecorded = true
		return result
	}
	if record == nil {
		result.LibraryRecorded = true
		return result
	}
	if err := record(ctx, result.Artifacts); err != nil {
		result.LibraryRecorded = false
		warning := "download completed but the local library was not updated: " + secrets.ScrubError(err)
		result.Warnings = append(result.Warnings, warning)
		if presentation.warningOutput != nil {
			writeDownloadLibraryWarning(presentation.warningOutput, warning)
		}
		return result
	}
	result.LibraryRecorded = true
	return result
}

func writeDownloadLibraryWarning(output io.Writer, warning string) {
	if _, err := fmt.Fprintf(output, "[WARNING] %s\n", warning); err != nil {
		return
	}
}

// applyAndValidateFlags applies CLI flag overrides to the config and validates them.
// This ensures invalid flag values fail early, before any remote API calls.
func applyAndValidateFlags(cfg *config.Config, quality, views string, audioOnly bool, format, output string, skipNoAudio bool) (*config.Config, error) {
	// Apply flag overrides
	if quality != "" {
		cfg.Quality = quality
	}
	if views != "" {
		cfg.Views = config.NormalizeViews(views)
	}
	if audioOnly {
		cfg.AudioOnly = true
	}
	if format != "" {
		cfg.AudioFormat = format
	}
	if output != "" {
		// CLI --output is a local override: allow absolute paths (the user owns
		// the filesystem) but reject traversal escapes. See docs PR for rationale.
		location, err := paths.ValidateDownloadLocation(output, true)
		if err != nil {
			return nil, err
		}
		cfg.DownloadLocation = location
	}
	if skipNoAudio {
		cfg.SkipNoAudio = true
	}

	// Validate flag override values
	if err := validateFlagOverrides(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func downloadLectures(ctx context.Context, cfg *config.Config, apiClient *client.Client, lectures client.Lectures, presentation downloadPresentationOptions) (downloadResult, error) {
	var d *downloader.Downloader
	if presentation.diagnosticOutput != nil {
		d = downloader.NewWithDiagnosticWriter(cfg, apiClient, presentation.diagnosticOutput)
	} else {
		d = downloader.New(cfg, apiClient)
	}
	return downloadLecturesWithRunner(ctx, cfg, d, lectures, presentation)
}

func downloadLecturesWithRunner(ctx context.Context, cfg *config.Config, d lectureDownloadRunner, lectures client.Lectures, presentation downloadPresentationOptions) (downloadResult, error) {
	if len(lectures) == 0 {
		return downloadResult{}, errors.New("no lectures selected")
	}

	// G301: 0755 is standard for user download directories
	// #nosec G301
	if err := os.MkdirAll(cfg.DownloadLocation, 0o755); err != nil {
		return downloadResult{}, err
	}

	lecturesByScope, err := indexLecturesForArtifacts(lectures, cfg)
	if err != nil {
		return downloadResult{}, err
	}

	playlists, err := d.FetchLecturePlaylists(ctx, lectures)
	if err != nil {
		return downloadResult{}, err
	}
	if len(playlists) == 0 {
		return downloadResult{}, errors.New("no playlists available for selected lectures")
	}
	if associationErr := validatePlaylistAssociations(playlists, lecturesByScope); associationErr != nil {
		return downloadResult{}, associationErr
	}

	p, tracker, err := newDownloadProgress(cfg, presentation, len(playlists), countChunks(playlists, cfg.Views))
	if err != nil {
		return downloadResult{}, err
	}
	if p != nil {
		defer p.Shutdown()
	}
	if tracker != nil {
		defer tracker.Stop()
	}

	outputPaths, artifacts, completedLectures, err := completeLectureDownloads(ctx, cfg, d, playlists, lecturesByScope, p, tracker, presentation.eventStream)
	result := downloadResult{Status: "completed", OutputPaths: outputPaths, LectureCount: completedLectures, Artifacts: artifacts}
	if err != nil {
		result.Status = "failed"
		return result, err
	}

	if tracker != nil {
		tracker.Stop()
	}

	if p != nil {
		p.Wait()
	}
	return result, nil
}

func completeLectureDownloads(
	ctx context.Context,
	cfg *config.Config,
	d lectureDownloadRunner,
	playlists []client.ParsedPlaylist,
	lecturesByScope map[scopedLectureKey]client.Lecture,
	progress *mpb.Progress,
	tracker *downloader.ProgressTracker,
	stream *downloadEventStream,
) ([]string, []artifact.Manifest, int, error) {
	outputPaths := make([]string, 0, len(playlists))
	artifacts := make([]artifact.Manifest, 0, len(playlists))
	for _, playlist := range playlists {
		key := scopedLectureKey{
			instituteID: playlist.InstituteID,
			subjectID:   playlist.SubjectID,
			sessionID:   playlist.SessionID,
			ttid:        playlist.ID,
		}
		lecture, exists := lecturesByScope[key]
		if !exists {
			return outputPaths, artifacts, len(artifacts), fmt.Errorf(
				"playlist is missing from selected scoped lectures: institute=%d subject=%d session=%d ttid=%d",
				key.instituteID,
				key.subjectID,
				key.sessionID,
				key.ttid,
			)
		}
		// Classify unavailable selected media once, before any presentation mode
		// begins work. Event stream v1 cannot publish a nonfatal lecture.failed,
		// and human/JSON modes must not carry a second copy of the same policy.
		skipUnavailable, planErr := skipUnavailablePlaylist(ctx, cfg, playlist, tracker)
		if planErr != nil {
			return outputPaths, artifacts, len(artifacts), planErr
		}
		if skipUnavailable {
			continue
		}
		artifactID, identityErr := downloadArtifactID(lecture, cfg)
		if identityErr != nil {
			return outputPaths, artifacts, len(artifacts), identityErr
		}
		if emitErr := stream.lecture(events.LectureStarted, lecture, artifactID, nil, nil, nil); emitErr != nil {
			return outputPaths, artifacts, len(artifacts), emitErr
		}

		// Route through the shared DownloadAndJoinPlaylist (the same method the
		// server job runner uses) so per-lecture download+join logic has one home.
		joinResult, err := d.DownloadAndJoinPlaylist(ctx, playlist, progress, tracker)
		if err != nil {
			downloadErr := handleLectureDownloadError(ctx, stream, lecture, artifactID, key, err)
			return outputPaths, artifacts, len(artifacts), downloadErr
		}
		paths := joinResult.OutputPaths()
		if len(paths) == 0 {
			contractErr := fmt.Errorf(
				"download and join lecture institute=%d subject=%d session=%d ttid=%d returned no outputs",
				key.instituteID,
				key.subjectID,
				key.sessionID,
				key.ttid,
			)
			emitErr := stream.lecture(events.LectureFailed, lecture, artifactID, nil, nil, map[string]any{"error": events.RedactError(contractErr)})
			return outputPaths, artifacts, len(artifacts), errors.Join(contractErr, emitErr)
		}
		manifest, err := buildDownloadArtifact(lecture, cfg, joinResult, time.Now().UTC())
		if err != nil {
			manifestErr := fmt.Errorf("build artifact manifest for lecture %d: %w", lecture.TTID, err)
			emitErr := stream.lecture(events.LectureFailed, lecture, artifactID, nil, nil, map[string]any{"error": events.RedactError(manifestErr)})
			return outputPaths, artifacts, len(artifacts), errors.Join(manifestErr, emitErr)
		}
		// A successfully built manifest is a fully validated partial result. Add
		// it before event delivery so a later sink or lecture failure cannot
		// discard media that is already safe to record in the local library.
		outputPaths = append(outputPaths, paths...)
		artifacts = append(artifacts, manifest)
		if emitErr := stream.lecture(events.LectureProgress, lecture, artifactID, nil, joinResult.OutputPaths(), map[string]any{"stage": "media_published"}); emitErr != nil {
			return outputPaths, artifacts, len(artifacts), emitErr
		}
		if emitErr := stream.lecture(events.LectureCompleted, lecture, artifactID, &manifest, manifestOutputPaths(manifest), nil); emitErr != nil {
			return outputPaths, artifacts, len(artifacts), emitErr
		}
		if tracker != nil {
			downloader.LectureCompleted(tracker)
		}
	}
	if len(artifacts) == 0 {
		return nil, nil, 0, downloader.ErrNoMediaOutputs
	}
	return outputPaths, artifacts, len(artifacts), nil
}

func skipUnavailablePlaylist(
	ctx context.Context,
	cfg *config.Config,
	playlist client.ParsedPlaylist,
	tracker *downloader.ProgressTracker,
) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	_, err := downloader.PlanJoinResult(cfg, playlist)
	if !errors.Is(err, downloader.ErrNoSelectedMedia) {
		return false, err
	}
	downloader.LectureCompleted(tracker)
	return true, nil
}

func handleLectureDownloadError(
	ctx context.Context,
	stream *downloadEventStream,
	lecture client.Lecture,
	artifactID string,
	key scopedLectureKey,
	cause error,
) error {
	downloadErr := fmt.Errorf(
		"download and join lecture institute=%d subject=%d session=%d ttid=%d: %w",
		key.instituteID,
		key.subjectID,
		key.sessionID,
		key.ttid,
		cause,
	)
	if events.IsCancellationForContext(ctx, downloadErr) {
		return events.RedactedError(downloadErr)
	}
	emitErr := stream.lecture(events.LectureFailed, lecture, artifactID, nil, nil, map[string]any{"error": events.RedactError(downloadErr)})
	return errors.Join(downloadErr, emitErr)
}

// validatePlaylistAssociations rejects unselected and duplicate playlists. The
// server may intentionally omit a selected lecture when it has no playable
// media, so absence is not treated as a coverage failure; LectureCount reports
// the number actually completed.
func validatePlaylistAssociations(playlists []client.ParsedPlaylist, lecturesByScope map[scopedLectureKey]client.Lecture) error {
	seen := make(map[scopedLectureKey]struct{}, len(playlists))
	for _, playlist := range playlists {
		key := scopedLectureKey{
			instituteID: playlist.InstituteID,
			subjectID:   playlist.SubjectID,
			sessionID:   playlist.SessionID,
			ttid:        playlist.ID,
		}
		if _, exists := lecturesByScope[key]; !exists {
			return fmt.Errorf(
				"playlist is missing from selected scoped lectures: institute=%d subject=%d session=%d ttid=%d",
				key.instituteID,
				key.subjectID,
				key.sessionID,
				key.ttid,
			)
		}
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf(
				"duplicate playlist for selected lecture: institute=%d subject=%d session=%d ttid=%d",
				key.instituteID,
				key.subjectID,
				key.sessionID,
				key.ttid,
			)
		}
		seen[key] = struct{}{}
	}

	return nil
}

type scopedLectureKey struct {
	instituteID int
	subjectID   int
	sessionID   int
	ttid        int
}

func indexLecturesForArtifacts(lectures client.Lectures, cfg *config.Config) (map[scopedLectureKey]client.Lecture, error) {
	byScope := make(map[scopedLectureKey]client.Lecture, len(lectures))
	for _, lecture := range lectures {
		key := scopedLectureKey{
			instituteID: lecture.InstituteID,
			subjectID:   lecture.SubjectID,
			sessionID:   lecture.SessionID,
			ttid:        lecture.TTID,
		}
		if _, exists := byScope[key]; exists {
			return nil, fmt.Errorf(
				"duplicate scoped lecture identity institute=%d subject=%d session=%d ttid=%d",
				key.instituteID,
				key.subjectID,
				key.sessionID,
				key.ttid,
			)
		}
		if _, err := downloadArtifactID(lecture, cfg); err != nil {
			return nil, fmt.Errorf("invalid artifact identity for lecture %d: %w", lecture.TTID, err)
		}
		byScope[key] = lecture
	}
	return byScope, nil
}

func downloadArtifactID(lecture client.Lecture, cfg *config.Config) (string, error) {
	return artifact.NewID(artifact.Identity{
		InstituteID: lecture.InstituteID,
		SubjectID:   lecture.SubjectID,
		SessionID:   lecture.SessionID,
		TTID:        lecture.TTID,
		AudioOnly:   cfg.AudioOnly,
		Views:       cfg.Views,
		Quality:     cfg.Quality,
		AudioFormat: cfg.AudioFormat,
	})
}

func buildDownloadArtifact(lecture client.Lecture, cfg *config.Config, result downloader.JoinResult, producedAt time.Time) (artifact.Manifest, error) {
	return app.BuildDownloadArtifact(lecture, cfg, result, producedAt)
}
