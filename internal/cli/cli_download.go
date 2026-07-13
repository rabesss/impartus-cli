package cli

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"

	"github.com/vbauerster/mpb/v8"

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
	Status        string   `json:"status"`
	OutputPaths   []string `json:"outputPaths"`
	LectureCount  int      `json:"lectureCount"`
	FilteredCount int      `json:"filteredCount,omitempty"`
	TotalLectures int      `json:"totalLectures,omitempty"`
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

	playlists, err := d.FetchLecturePlaylists(ctx, lectures)
	if err != nil {
		return downloadResult{}, err
	}
	if len(playlists) == 0 {
		return downloadResult{}, errors.New("no playlists available for selected lectures")
	}

	var p *mpb.Progress
	if presentation.showProgress {
		progressOptions := []mpb.ContainerOption{mpb.WithWidth(70)}
		if presentation.progressOutput != nil {
			progressOptions = append(progressOptions, mpb.WithOutput(presentation.progressOutput))
		}
		p = mpb.New(progressOptions...)
	}
	var tracker *downloader.ProgressTracker
	if p != nil && cfg.ProgressTracking.Enabled {
		tracker = downloader.NewProgressTracker(len(playlists), countChunks(playlists, cfg.Views), p)
	}

	outputPaths := make([]string, 0, len(playlists))
	completedLectures := 0
	for _, playlist := range playlists {
		// Route through the shared DownloadAndJoinPlaylist (the same method the
		// server job runner uses) so per-lecture download+join logic has one home.
		joinResult, err := d.DownloadAndJoinPlaylist(ctx, playlist, p, tracker)
		if err != nil {
			return downloadResult{}, err
		}
		outputPaths = append(outputPaths, joinResult.OutputPaths()...)
		completedLectures++

		if tracker != nil {
			downloader.LectureCompleted(tracker)
		}
	}

	if tracker != nil {
		tracker.Stop()
	}

	if p != nil {
		p.Wait()
	}
	return downloadResult{Status: "completed", OutputPaths: outputPaths, LectureCount: completedLectures}, nil
}
