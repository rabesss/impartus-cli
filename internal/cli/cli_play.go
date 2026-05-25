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
	"syscall"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/downloader"
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

	cfg, err = applyAndValidateFlags(cfg, f.quality, f.views, false, "", "", f.skipNoAudio)
	if err != nil {
		return err
	}

	if f.includeNoAudio {
		cfg.SkipNoAudio = false
	}

	if f.subject <= 0 || f.session <= 0 {
		return runPlayInteractive(ctx, cfg, apiClient)
	}

	lectures, err := apiClient.GetLectures(ctx, cfg, client.Course{SubjectID: f.subject, SessionID: f.session})
	if err != nil {
		return err
	}

	selected, err := lectures.SelectRange(f.start, f.end)
	if err != nil {
		return err
	}

	warnNoAudioLectures(selected, cfg.SkipNoAudio)

	if cfg.SkipNoAudio {
		selected = selected.FilterNoAudio()
	}

	if len(selected) == 0 {
		return fmt.Errorf("no lectures available after filtering")
	}

	return playLectures(ctx, cfg, apiClient, selected)
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
	return nil
}

func runPlayInteractive(ctx context.Context, cfg *config.Config, apiClient *client.Client) error {
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

	return playLectures(ctx, cfg, apiClient, selected)
}

func playLectures(ctx context.Context, cfg *config.Config, apiClient *client.Client, lectures client.Lectures) error {
	d := downloader.New(cfg, apiClient)
	playlists, err := d.FetchLecturePlaylists(ctx, lectures)
	if err != nil {
		return err
	}
	if len(playlists) == 0 {
		return errors.New("no playlists available for selected lectures")
	}

	for _, playlist := range playlists {
		if err := playOnePlaylist(ctx, cfg, d, playlist); err != nil {
			return err
		}
	}

	return nil
}

func playOnePlaylist(ctx context.Context, cfg *config.Config, d *downloader.Downloader, playlist client.ParsedPlaylist) error {
	fmt.Printf("[INFO] Playing Lec %03d: %s\n", playlist.SeqNo, playlist.Title)
	fmt.Printf("[INFO] Views: %s (Press '_' in mpv to cycle views, 'q' to exit/next)\n", cfg.Views)

	playURL, cleanup, err := d.StartPlayServer(ctx, playlist)
	if err != nil {
		return fmt.Errorf("failed to start local playback server: %w", err)
	}
	defer cleanup()

	cmd := exec.CommandContext(ctx, "mpv", playURL) // #nosec G204
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if runErr := cmd.Run(); runErr != nil {
		return fmt.Errorf("mpv execution failed: %w", runErr)
	}
	return nil
}

func ensureMpv() error {
	if _, err := exec.LookPath("mpv"); err != nil {
		return errors.New("please add mpv to your path")
	}
	return nil
}
