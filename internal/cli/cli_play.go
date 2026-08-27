package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/rabesss/impartus-cli/internal/app"
	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/downloader"
	"github.com/rabesss/impartus-cli/internal/player"
)

type playFlags struct {
	subject        int
	session        int
	start          int
	end            int
	lecture        int
	quality        string
	views          string
	skipNoAudio    bool
	includeNoAudio bool
	mpvMode        string
}

func runPlay(args []string) error {
	f, err := parsePlayFlags(args)
	if err != nil {
		return err
	}
	if validateErr := validatePlayFlags(f); validateErr != nil {
		return validateErr
	}
	if mpvErr := ensureMpv(); mpvErr != nil {
		return mpvErr
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, apiClient, err := initClient(ctx)
	if err != nil {
		return err
	}

	cfg, err = applyAndValidateFlags(cfg, f.quality, f.views, false, false, "", "", f.skipNoAudio)
	if err != nil {
		return err
	}

	if f.includeNoAudio {
		cfg.SkipNoAudio = false
	}

	if f.subject <= 0 || f.session <= 0 {
		return runPlayInteractive(ctx, cfg, apiClient, f.mpvMode)
	}

	lectures, err := apiClient.GetLectures(ctx, cfg, client.Course{SubjectID: f.subject, SessionID: f.session})
	if err != nil {
		return err
	}

	selected, _, err := lectures.SelectForDownload(f.start, f.end, cfg.SkipNoAudio)
	if err != nil {
		return err
	}

	warnNoAudioLectures(os.Stderr, selected, cfg.SkipNoAudio)

	return playLectures(ctx, cfg, apiClient, selected, f.mpvMode)
}

func parsePlayFlags(args []string) (playFlags, error) {
	fs := flag.NewFlagSet("play", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var f playFlags
	fs.IntVar(&f.subject, "subject", 0, "Subject ID")
	fs.IntVar(&f.subject, "s", 0, "Subject ID")
	fs.IntVar(&f.session, "session", 0, "Session ID")
	fs.IntVar(&f.session, "S", 0, "Session ID")
	fs.IntVar(&f.start, "start", 0, "Start lecture index (1-based)")
	fs.IntVar(&f.end, "end", 0, "End lecture index (1-based)")
	fs.IntVar(&f.lecture, "lecture", 0, "Lecture index (1-based, shortcut for start & end)")
	fs.IntVar(&f.lecture, "l", 0, "Lecture index (1-based, shortcut for start & end)")
	fs.StringVar(&f.quality, "quality", "", "Video quality override")
	fs.StringVar(&f.views, "views", "", "Views override: left/right/both or first/second/both")
	fs.BoolVar(&f.skipNoAudio, "skip-no-audio", false, "Skip lectures with no audio track")
	fs.BoolVar(&f.includeNoAudio, "include-noaudio", false, "Include lectures with no audio track (overrides --skip-no-audio)")
	fs.StringVar(&f.mpvMode, "mpv-mode", defaultMPVModeForOS(runtime.GOOS), "mpv control mode: ipc or legacy")

	if err := fs.Parse(args); err != nil {
		return playFlags{}, err
	}
	if fs.NArg() > 0 {
		return playFlags{}, errors.New("play does not accept positional arguments")
	}

	if f.lecture > 0 {
		f.start = f.lecture
		f.end = f.lecture
	}

	return f, nil
}

func defaultMPVModeForOS(goos string) string {
	if goos == "windows" {
		return "legacy"
	}
	return "ipc"
}

func validatePlayFlags(f playFlags) error {
	hasSubject := f.subject > 0
	hasSession := f.session > 0
	hasRangeSelection := f.start > 0 || f.end > 0

	if f.subject < 0 || f.session < 0 {
		return errors.New("play requires positive --subject/-s and --session/-S values")
	}
	if f.start < 0 || f.end < 0 || f.lecture < 0 {
		return errors.New("play lecture selection values must be positive")
	}
	if hasSubject != hasSession {
		return errors.New("play requires both --subject/-s and --session/-S for direct playback")
	}
	if hasRangeSelection && (!hasSubject || !hasSession) {
		return errors.New("play lecture range flags require --subject/-s and --session/-S")
	}
	if f.mpvMode != "" && f.mpvMode != "ipc" && f.mpvMode != "legacy" {
		return errors.New("play mpv mode must be ipc or legacy")
	}
	_, err := applyAndValidateFlags(&config.Config{}, f.quality, f.views, false, false, "", "", f.skipNoAudio)
	return err
}

func runPlayInteractive(ctx context.Context, cfg *config.Config, apiClient *client.Client, mpvMode string) error {
	fmt.Println("Impartus Video Player")
	fmt.Println()

	course, err := selectCourseInteractive(ctx, cfg, apiClient)
	if err != nil {
		return err
	}

	selected, err := filterLecturesInteractive(ctx, cfg, apiClient, course)
	if err != nil {
		return err
	}

	return playLectures(ctx, cfg, apiClient, selected, mpvMode)
}

func playLectures(ctx context.Context, cfg *config.Config, apiClient *client.Client, lectures client.Lectures, mpvMode string) error {
	return routePlayLectures(ctx, cfg, apiClient, lectures, mpvMode, app.New(cfg, apiClient), playLecturesLegacy)
}

type legacyPlayer func(context.Context, *config.Config, *client.Client, client.Lectures) error

func routePlayLectures(ctx context.Context, cfg *config.Config, apiClient *client.Client, lectures client.Lectures, mpvMode string, supervised sequentialPlayer, legacy legacyPlayer) error {
	if mpvMode == "legacy" {
		return legacy(ctx, cfg, apiClient, lectures)
	}
	return playLecturesWithService(ctx, cfg, lectures, supervised)
}

type sequentialPlayer interface {
	PlaySequential(context.Context, client.Lectures, func(client.ParsedPlaylist)) error
}

func playLecturesWithService(ctx context.Context, cfg *config.Config, lectures client.Lectures, service sequentialPlayer) error {
	return service.PlaySequential(ctx, lectures, func(playlist client.ParsedPlaylist) {
		fmt.Printf("[INFO] Playing Lec %03d: %s\n", playlist.SeqNo, playlist.Title)
		fmt.Printf("[INFO] Views: %s (mpv is controlled over private JSON IPC; q exits)\n", cfg.Views)
	})
}

func playLecturesLegacy(ctx context.Context, cfg *config.Config, apiClient *client.Client, lectures client.Lectures) error {
	d := downloader.New(cfg, apiClient)
	playlists, err := d.FetchLecturePlaylists(ctx, lectures)
	if err != nil {
		return err
	}
	if len(playlists) == 0 {
		return errors.New("no playlists available for selected lectures")
	}

	for _, playlist := range playlists {
		if err := playOnePlaylistLegacy(ctx, cfg, d, playlist); err != nil {
			return err
		}
	}

	return nil
}

func playOnePlaylistLegacy(ctx context.Context, cfg *config.Config, d *downloader.Downloader, playlist client.ParsedPlaylist) error {
	fmt.Printf("[INFO] Playing Lec %03d: %s\n", playlist.SeqNo, playlist.Title)
	fmt.Printf("[INFO] Views: %s (Press '_' in mpv to cycle views, 'q' to exit/next)\n", cfg.Views)

	stream, err := d.StartPlaybackStream(ctx, playlist)
	if err != nil {
		return fmt.Errorf("failed to start local playback server: %w", err)
	}
	defer stream.Cleanup()

	cmd := exec.CommandContext(ctx, "mpv", stream.URL) // #nosec G204
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if startErr := cmd.Start(); startErr != nil {
		return fmt.Errorf("start mpv: %w", startErr)
	}
	finished := make(chan error, 1)
	go func() { finished <- cmd.Wait() }()
	return waitLegacyPlayback(ctx, stream.Failures, finished, func() error {
		if cmd.Process == nil {
			return nil
		}
		return cmd.Process.Kill()
	})
}

func waitLegacyPlayback(ctx context.Context, failures <-chan error, finished <-chan error, kill func() error) error {
	select {
	case failure := <-failures:
		if kill != nil {
			_ = kill() //nolint:errcheck // failure is reported below; finished reaps the process
		}
		<-finished
		return fmt.Errorf("legacy playback failed: %w", failure)
	case runErr := <-finished:
		select {
		case failure := <-failures:
			return fmt.Errorf("legacy playback failed: %w", failure)
		default:
		}
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		if runErr == nil {
			return nil
		}
		return fmt.Errorf("mpv execution failed: %w", runErr)
	}
}

func ensureMpv() error {
	return player.CheckBinary("mpv")
}
