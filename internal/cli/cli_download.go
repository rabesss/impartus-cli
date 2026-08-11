package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/vbauerster/mpb/v8"

	"github.com/rabesss/impartus-cli/internal/artifact"
	"github.com/rabesss/impartus-cli/internal/buildinfo"
	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/downloader"
	"github.com/rabesss/impartus-cli/internal/paths"
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
}

type downloadResult struct {
	Status        string              `json:"status"`
	OutputPaths   []string            `json:"outputPaths"`
	LectureCount  int                 `json:"lectureCount"`
	FilteredCount int                 `json:"filteredCount,omitempty"`
	TotalLectures int                 `json:"totalLectures,omitempty"`
	Artifacts     []artifact.Manifest `json:"artifacts"`
}

// downloadPresentationOptions keeps user-facing output policy at the CLI
// boundary. Machine-readable commands leave progress and warning writers nil
// and discard downloader diagnostics so stdout/stderr stay structured.
type downloadPresentationOptions struct {
	showProgress     bool
	progressOutput   io.Writer
	warningOutput    io.Writer
	diagnosticOutput io.Writer
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
}

type lectureDownloadRunner interface {
	FetchLecturePlaylists(context.Context, []client.Lecture) ([]client.ParsedPlaylist, error)
	DownloadAndJoinPlaylist(context.Context, client.ParsedPlaylist, *mpb.Progress, *downloader.ProgressTracker) (downloader.JoinResult, error)
}

func defaultDownloadExecutionDependencies() downloadExecutionDependencies {
	return downloadExecutionDependencies{
		ensureFFmpeg:     ensureFFmpeg,
		initClient:       initClient,
		downloadLectures: downloadLectures,
	}
}

func runDownload(args []string) error {
	_, err := executeDownload(args, humanDownloadPresentation())
	return err
}

func runDownloadJSON(args []string) (downloadResult, error) {
	return runDownloadJSONWithDependencies(args, defaultDownloadExecutionDependencies())
}

func runDownloadJSONWithDependencies(args []string, deps downloadExecutionDependencies) (downloadResult, error) {
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

func executeDownload(args []string, presentation downloadPresentationOptions) (downloadResult, error) {
	return executeDownloadWithDependencies(args, presentation, defaultDownloadExecutionDependencies())
}

func executeDownloadWithDependencies(args []string, presentation downloadPresentationOptions, deps downloadExecutionDependencies) (downloadResult, error) {
	f, err := parseDownloadFlags(args)
	if err != nil {
		return downloadResult{}, err
	}

	if ffmpegErr := deps.ensureFFmpeg(); ffmpegErr != nil {
		return downloadResult{}, ffmpegErr
	}

	ctx := context.Background()
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
	if err != nil {
		return downloadResult{}, err
	}
	result.FilteredCount = filteredCount
	result.TotalLectures = totalLectures
	return result, nil
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

	outputPaths, artifacts, completedLectures, err := completeLectureDownloads(ctx, cfg, d, playlists, lecturesByScope, p, tracker)
	if err != nil {
		return downloadResult{}, err
	}

	if tracker != nil {
		tracker.Stop()
	}

	if p != nil {
		p.Wait()
	}
	return downloadResult{Status: "completed", OutputPaths: outputPaths, LectureCount: completedLectures, Artifacts: artifacts}, nil
}

func completeLectureDownloads(
	ctx context.Context,
	cfg *config.Config,
	d lectureDownloadRunner,
	playlists []client.ParsedPlaylist,
	lecturesByScope map[scopedLectureKey]client.Lecture,
	progress *mpb.Progress,
	tracker *downloader.ProgressTracker,
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
			return nil, nil, 0, fmt.Errorf(
				"playlist is missing from selected scoped lectures: institute=%d subject=%d session=%d ttid=%d",
				key.instituteID,
				key.subjectID,
				key.sessionID,
				key.ttid,
			)
		}
		// Route through the shared DownloadAndJoinPlaylist (the same method the
		// server job runner uses) so per-lecture download+join logic has one home.
		joinResult, err := d.DownloadAndJoinPlaylist(ctx, playlist, progress, tracker)
		if err != nil {
			return nil, nil, 0, fmt.Errorf(
				"download and join lecture institute=%d subject=%d session=%d ttid=%d: %w",
				key.instituteID,
				key.subjectID,
				key.sessionID,
				key.ttid,
				err,
			)
		}
		paths := joinResult.OutputPaths()
		if len(paths) == 0 {
			continue
		}
		manifest, err := buildDownloadArtifact(lecture, cfg, joinResult, time.Now().UTC())
		if err != nil {
			return nil, nil, 0, fmt.Errorf("build artifact manifest for lecture %d: %w", lecture.TTID, err)
		}
		outputPaths = append(outputPaths, paths...)
		artifacts = append(artifacts, manifest)
		if tracker != nil {
			downloader.LectureCompleted(tracker)
		}
	}
	if len(artifacts) == 0 {
		return nil, nil, 0, errors.New("no media outputs available for selected lectures")
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
		if _, err := artifact.NewID(artifact.Identity{
			InstituteID: lecture.InstituteID,
			SubjectID:   lecture.SubjectID,
			SessionID:   lecture.SessionID,
			TTID:        lecture.TTID,
			AudioOnly:   cfg.AudioOnly,
			Views:       cfg.Views,
			Quality:     cfg.Quality,
			AudioFormat: cfg.AudioFormat,
		}); err != nil {
			return nil, fmt.Errorf("invalid artifact identity for lecture %d: %w", lecture.TTID, err)
		}
		byScope[key] = lecture
	}
	return byScope, nil
}

func buildDownloadArtifact(lecture client.Lecture, cfg *config.Config, result downloader.JoinResult, producedAt time.Time) (artifact.Manifest, error) {
	role := "video"
	if cfg.AudioOnly {
		role = "audio"
	}

	fileSpecs := make([]artifact.FileSpec, 0, 3)
	for _, output := range []struct {
		path      string
		view      string
		container string
	}{
		{path: result.LeftOutput, view: "left", container: result.LeftContainer},
		{path: result.RightOutput, view: "right", container: result.RightContainer},
		{path: result.BothOutput, view: "both", container: result.BothContainer},
	} {
		if strings.TrimSpace(output.path) == "" {
			continue
		}
		fileSpecs = append(fileSpecs, artifact.FileSpec{
			Path:      output.path,
			Role:      role,
			View:      output.view,
			Container: output.container,
		})
	}

	return artifact.Build(artifact.BuildInput{
		Lecture: artifact.Lecture{
			TTID:            lecture.TTID,
			InstituteID:     lecture.InstituteID,
			SubjectID:       lecture.SubjectID,
			SessionID:       lecture.SessionID,
			SeqNo:           lecture.SeqNo,
			Topic:           lecture.Topic,
			StartTime:       lecture.StartTime,
			DurationSeconds: lecture.ActualDuration,
			Professor:       lecture.ProfessorName,
			Institute:       lecture.Institute,
			NoAudio:         lecture.NoAudio == 1,
		},
		Selection: artifact.Selection{
			Views:       cfg.Views,
			Quality:     cfg.Quality,
			AudioOnly:   cfg.AudioOnly,
			AudioFormat: cfg.AudioFormat,
		},
		Files:      fileSpecs,
		ProducedAt: producedAt,
		Producer: artifact.Producer{
			Name:    "impartus",
			Version: buildinfo.Version,
		},
	})
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
