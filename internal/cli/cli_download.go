package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
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

type downloadFlags struct {
	subject        int
	session        int
	start          int
	end            int
	quality        string
	views          string
	audioOnly      bool
	format         string
	output         string
	skipNoAudio    bool
	includeNoAudio bool
	events         bool
}

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
	if requestedEvents(args) {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		err := runDownloadEventsWithDependenciesContext(ctx, args, os.Stdout, defaultDownloadExecutionDependencies(), time.Now, "")
		return downloadCommandError(err)
	}
	_, err := executeDownload(args, humanDownloadPresentation())
	return err
}

func downloadCommandError(err error) error {
	if errors.Is(err, errDownloadEventDelivery) {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &exitCodeError{code: 130, err: err}
	}
	return err
}

func runDownloadJSON(args []string) (downloadResult, error) {
	return runDownloadJSONWithDependencies(args, defaultDownloadExecutionDependencies())
}

func runDownloadJSONWithDependencies(args []string, deps downloadExecutionDependencies) (downloadResult, error) {
	if requestedEvents(args) {
		return downloadResult{}, errors.New("cannot combine --json and --events")
	}
	return executeDownloadWithDependencies(args, quietDownloadPresentation(), deps)
}

func parseDownloadFlags(args []string) (downloadFlags, error) {
	fs := flag.NewFlagSet("download", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var f downloadFlags
	fs.IntVar(&f.subject, "subject", 0, "Subject ID")
	fs.IntVar(&f.subject, "s", 0, "Subject ID")
	fs.IntVar(&f.session, "session", 0, "Session ID")
	fs.IntVar(&f.session, "S", 0, "Session ID")
	fs.IntVar(&f.start, "start", 0, "Start lecture index (1-based)")
	fs.IntVar(&f.end, "end", 0, "End lecture index (1-based)")
	fs.StringVar(&f.quality, "quality", "", "Video quality override")
	fs.StringVar(&f.views, "views", "", "Views override: left/right/both or first/second/both")
	fs.BoolVar(&f.audioOnly, "audio-only", false, "Enable audio-only mode")
	fs.StringVar(&f.format, "format", "", "Audio format override")
	fs.StringVar(&f.output, "output", "", "Output directory override")
	fs.StringVar(&f.output, "o", "", "Output directory override")
	fs.BoolVar(&f.skipNoAudio, "skip-no-audio", false, "Skip lectures with no audio track")
	fs.BoolVar(&f.includeNoAudio, "include-noaudio", false, "Include lectures with no audio track (overrides --skip-no-audio)")
	fs.BoolVar(&f.events, "events", false, "Emit newline-delimited JSON lifecycle events")

	if err := fs.Parse(args); err != nil {
		return downloadFlags{}, err
	}
	if fs.NArg() > 0 {
		return downloadFlags{}, errors.New("download does not accept positional arguments")
	}
	if f.subject <= 0 || f.session <= 0 {
		return downloadFlags{}, errors.New("download requires --subject/-s and --session/-S")
	}
	return f, nil
}

type downloadEventStream struct {
	writer  *events.Writer
	jobID   string
	now     func() time.Time
	started bool
}

func newDownloadEventStream(output io.Writer, jobID string, now func() time.Time) *downloadEventStream {
	if strings.TrimSpace(jobID) == "" {
		jobID = "job-" + uuid.NewString()
	}
	if now == nil {
		now = time.Now
	}
	return &downloadEventStream{writer: events.NewWriter(output), jobID: jobID, now: now}
}

func (stream *downloadEventStream) start() error {
	if stream.started {
		return nil
	}
	stream.started = true
	return stream.writer.Emit(events.Event{
		Type: events.JobStarted, JobID: stream.jobID, Command: "download",
		Status: "running", Timestamp: stream.now().UTC(),
	})
}

func (stream *downloadEventStream) finish(result downloadResult, cause error) error {
	if cause != nil {
		return stream.failResult(result, cause)
	}
	if !result.LibraryRecorded {
		cause = errors.New("download completed but the local library commit did not complete")
		return stream.failResult(result, cause)
	}
	for index := range result.Artifacts {
		manifest := result.Artifacts[index]
		if err := stream.writer.Emit(events.Event{
			Type: events.ArtifactCommitted, JobID: stream.jobID, Command: "download", ArtifactID: manifest.ArtifactID,
			Status: "completed", Timestamp: stream.now().UTC(), Artifact: &manifest,
			Outputs: manifestOutputPaths(manifest),
		}); err != nil {
			return stream.failResult(result, err)
		}
	}
	if err := stream.writer.Emit(events.Event{
		Type: events.JobCompleted, JobID: stream.jobID, Command: "download",
		Status: "completed", Timestamp: stream.now().UTC(), Outputs: artifactOutputPaths(result.Artifacts),
		Details: map[string]any{
			"lectureCount": result.LectureCount, "libraryRecorded": result.LibraryRecorded,
			"filteredCount": result.FilteredCount, "totalLectures": result.TotalLectures,
		},
	}); err != nil {
		return stream.failResult(result, err)
	}
	return nil
}

func (stream *downloadEventStream) fail(cause error) error {
	return stream.failResult(downloadResult{}, cause)
}

func (stream *downloadEventStream) failResult(result downloadResult, cause error) error {
	event := events.Failure(stream.jobID, "download", cause, stream.now())
	if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		event = events.Cancellation(stream.jobID, "download", cause, stream.now())
	}
	event.Outputs = artifactOutputPaths(result.Artifacts)
	event.Artifacts = append([]artifact.Manifest(nil), result.Artifacts...)
	event.Details = map[string]any{
		"lectureCount": result.LectureCount, "libraryRecorded": result.LibraryRecorded,
		"filteredCount": result.FilteredCount, "totalLectures": result.TotalLectures,
	}
	emitErr := stream.writer.Emit(event)
	if emitErr != nil {
		return errors.Join(events.RedactedError(cause), errDownloadEventDelivery, events.RedactedError(emitErr))
	}
	return events.RedactedError(cause)
}

func (stream *downloadEventStream) lecture(eventType string, lecture client.Lecture, artifactID string, manifest *artifact.Manifest, outputs []string, details any) error {
	if stream == nil {
		return nil
	}
	event := events.Event{
		Type: eventType, JobID: stream.jobID, Command: "download", Timestamp: stream.now().UTC(),
		Lecture:    &events.Lecture{TTID: lecture.TTID, SeqNo: lecture.SeqNo, Topic: lecture.Topic},
		ArtifactID: artifactID, Artifact: manifest, Outputs: append([]string(nil), outputs...), Details: details,
	}
	if err := stream.writer.Emit(event); err != nil {
		return errors.Join(errDownloadEventDelivery, events.RedactedError(err))
	}
	return nil
}

func runDownloadEventsWithDependenciesContext(ctx context.Context, args []string, output io.Writer, deps downloadExecutionDependencies, now func() time.Time, jobID string) error {
	stream := newDownloadEventStream(output, jobID, now)
	if err := stream.start(); err != nil {
		return stream.fail(err)
	}
	presentation := quietDownloadPresentation()
	presentation.eventStream = stream
	result, err := executeDownloadWithDependenciesContext(ctx, args, presentation, deps)
	return stream.finish(result, err)
}

func emitDownloadResultEvents(output io.Writer, jobID string, result downloadResult, cause error, now func() time.Time) error {
	stream := newDownloadEventStream(output, jobID, now)
	if err := stream.start(); err != nil {
		return stream.fail(err)
	}
	return stream.finish(result, cause)
}

func requestedEvents(args []string) bool {
	enabled := false
	valueFlags := map[string]bool{
		"--subject": true, "-subject": true, "-s": true,
		"--session": true, "-session": true, "-S": true,
		"--start": true, "-start": true, "--end": true, "-end": true,
		"--quality": true, "-quality": true, "--views": true, "-views": true,
		"--format": true, "-format": true, "--output": true, "-output": true, "-o": true,
		"--interval": true, "-interval": true,
	}
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if argument == "--" {
			break
		}
		if valueFlags[argument] {
			index++
			continue
		}
		// flag.FlagSet stops parsing immediately before the first positional
		// argument. Keep this pre-parser aligned so a later --events token cannot
		// change the output mode of a parse error the real parser never reached.
		if argument == "" || argument == "-" || !strings.HasPrefix(argument, "-") {
			break
		}
		if argument == "--events" || argument == "-events" {
			enabled = true
			continue
		}
		prefix := "--events="
		if strings.HasPrefix(argument, "-events=") && !strings.HasPrefix(argument, prefix) {
			prefix = "-events="
		}
		if !strings.HasPrefix(argument, prefix) {
			continue
		}
		value, err := strconv.ParseBool(strings.TrimPrefix(argument, prefix))
		if err == nil {
			enabled = value
		}
	}
	return enabled
}

func manifestOutputPaths(manifest artifact.Manifest) []string {
	paths := make([]string, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		paths = append(paths, file.Path)
	}
	return paths
}

func artifactOutputPaths(manifests []artifact.Manifest) []string {
	count := 0
	for _, manifest := range manifests {
		count += len(manifest.Files)
	}
	output := make([]string, 0, count)
	for _, manifest := range manifests {
		output = append(output, manifestOutputPaths(manifest)...)
	}
	return output
}

func executeDownload(args []string, presentation downloadPresentationOptions) (downloadResult, error) {
	return executeDownloadWithDependencies(args, presentation, defaultDownloadExecutionDependencies())
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

	selected, filteredCount, err := lectures.SelectForDownload(f.start, f.end, cfg.SkipNoAudio)
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
			if errors.Is(err, downloader.ErrNoSelectedMedia) {
				if emitErr := stream.lecture(events.LectureSkipped, lecture, artifactID, nil, nil, map[string]any{"reason": "no selected media"}); emitErr != nil {
					return outputPaths, artifacts, len(artifacts), emitErr
				}
				if tracker != nil {
					downloader.LectureCompleted(tracker)
				}
				continue
			}
			downloadErr := fmt.Errorf(
				"download and join lecture institute=%d subject=%d session=%d ttid=%d: %w",
				key.instituteID,
				key.subjectID,
				key.sessionID,
				key.ttid,
				err,
			)
			emitErr := stream.lecture(events.LectureFailed, lecture, artifactID, nil, nil, map[string]any{"error": events.RedactError(downloadErr)})
			return outputPaths, artifacts, len(artifacts), errors.Join(downloadErr, emitErr)
		}
		if emitErr := stream.lecture(events.LectureProgress, lecture, artifactID, nil, joinResult.OutputPaths(), map[string]any{"stage": "media_published"}); emitErr != nil {
			return outputPaths, artifacts, len(artifacts), emitErr
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

func newDownloadProgress(cfg *config.Config, presentation downloadPresentationOptions, totalLectures, totalChunks int) (*mpb.Progress, *downloader.ProgressTracker, error) {
	if !presentation.showProgress || !cfg.ProgressTracking.Enabled {
		return nil, nil, nil
	}

	progressOptions := []mpb.ContainerOption{mpb.WithWidth(70)}
	if presentation.progressOutput != nil {
		progressOptions = append(progressOptions, mpb.WithOutput(presentation.progressOutput))
	}
	p := mpb.New(progressOptions...)

	var updateInterval time.Duration
	if cfg.ProgressTracking.UpdateInterval != "" {
		var err error
		updateInterval, err = time.ParseDuration(cfg.ProgressTracking.UpdateInterval)
		if err != nil {
			p.Shutdown()
			return nil, nil, fmt.Errorf("invalid progressTracking.updateInterval: %w", err)
		}
	}

	tracker := downloader.NewProgressTrackerWithOptions(totalLectures, totalChunks, p, downloader.ProgressTrackerOptions{
		ShowSpeed:       cfg.ProgressTracking.ShowSpeed,
		ShowETA:         cfg.ProgressTracking.ShowETA,
		SampleInterval:  updateInterval,
		SpeedWindowSize: cfg.ProgressTracking.SpeedWindowSize,
	})
	return p, tracker, nil
}
