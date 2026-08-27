package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/selection"
)

func filterEmptyLectures(lectures client.Lectures) client.Lectures {
	filtered := make(client.Lectures, 0, len(lectures))
	for _, lecture := range lectures {
		topic := strings.ToLower(strings.TrimSpace(lecture.Topic))
		if strings.Contains(topic, "no class") || strings.Contains(topic, "no lecture") {
			continue
		}
		filtered = append(filtered, lecture)
	}
	return filtered
}

func countNoAudioLectures(lectures client.Lectures) int {
	count := 0
	for _, lecture := range lectures {
		if lecture.NoAudio == 1 {
			count++
		}
	}
	return count
}

func warnNoAudioLectures(output io.Writer, lectures client.Lectures, skipNoAudio bool) {
	noaudioCount := countNoAudioLectures(lectures)
	if output != nil && noaudioCount > 0 && !skipNoAudio {
		if _, err := fmt.Fprintf(output, "[WARNING] %d lecture(s) in selection have no audio track (noaudio=1)\n", noaudioCount); err != nil {
			return
		}
		if _, err := fmt.Fprintln(output, "[INFO] Use --skip-no-audio to filter these out, or --include-noaudio to include anyway"); err != nil {
			return
		}
	}
}

func countChunks(playlists []client.ParsedPlaylist, views string) int {
	selected, ok := selection.ParseView(views)
	if !ok {
		return 0
	}
	total := 0
	for _, playlist := range playlists {
		if selected.Includes(selection.ViewLeft) {
			total += len(playlist.FirstViewURLs)
		}
		if selected.Includes(selection.ViewRight) {
			total += len(playlist.SecondViewURLs)
		}
	}
	return total
}

// validateFlagOverrides validates config values after CLI flag overrides are applied.
// This ensures invalid flag values fail early, before any remote API calls.
func validateFlagOverrides(cfg *config.Config) error {
	if cfg.Quality != "" && !selection.ValidQuality(cfg.Quality) {
		return fmt.Errorf("invalid quality value %q: must be one of: 144, 450, 720", cfg.Quality)
	}
	if cfg.Views != "" {
		if _, ok := selection.ParseView(cfg.Views); !ok {
			return fmt.Errorf("invalid views value %q: must be one of: first, second, both, left, right", cfg.Views)
		}
	}
	if cfg.AudioOnly && cfg.AudioFormat != "" && !selection.ValidAudioFormat(cfg.AudioFormat) {
		return fmt.Errorf("invalid audioFormat value %q: must be one of: mp3, m4a, aac, opus", cfg.AudioFormat)
	}
	return nil
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	return enc.Encode(v)
}

func showVersion(version, date string) error {
	return showVersionTo(os.Stdout, version, date)
}

func showHelp(version, date string) error {
	return showHelpTo(os.Stdout, version, date)
}

func showVersionTo(output io.Writer, version, date string) error {
	_, err := fmt.Fprintf(output, "Impartus Video Downloader\nVersion: %s\nBuild Date: %s\n", version, date)
	return err
}

func showHelpTo(output io.Writer, version, date string) error {
	if err := showVersionTo(output, version, date); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(output, "\nUsage:\n  impartus [command] [flags]\n\nCommands:"); err != nil {
		return err
	}
	for _, line := range []string{
		"  tui                                  Browse, play, download, and resume (TUI)",
		"  courses                              List courses (JSON)",
		"  lectures -s <subject> -S <session>   List lectures (JSON)",
		"  download [flags]                     Download lectures",
		"  play [flags]                         Play lectures in mpv",
		"  doctor                               Check local dependencies and private paths",
		"  library list|show|verify             Inspect and verify the local lecture library",
		"  watch [--once] [--dry-run]           Poll and durably download new lectures",
		"  serve [--port <port>]                Start HTTP API server",
		"  version                              Show version",
		"  help                                 Show help",
	} {
		if _, err := fmt.Fprintln(output, line); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(output, "\nGlobal Flags:"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(output, "  --json               Emit one machine-readable JSON envelope"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(output, "\nDownload / Play Flags:"); err != nil {
		return err
	}
	for _, line := range []string{
		"  --subject,-s        Subject ID", "  --session,-S        Session ID",
		"  --start             Start lecture index (1-based)", "  --end               End lecture index (1-based)",
		"  --ttid              Exact lecture TTID (download only; exclusive with --start/--end)",
		"  --lecture,-l        Specific lecture index (1-based, play only)", "  --quality           Quality override",
		"  --views             Views override", "  --audio-only        Audio-only mode (download only)",
		"  --format            Audio format override (download only)", "  --output,-o         Output directory (download only)",
		"  --skip-no-audio     Skip lectures with no audio track", "  --include-noaudio   Include noaudio lectures (overrides --skip-no-audio)",
		"  --mpv-mode          Playback mode: ipc by default, legacy on Windows (play only)",
		"  --events            NDJSON lifecycle stream (download/watch; exclusive with --json)",
	} {
		if _, err := fmt.Fprintln(output, line); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(output, "\nNo arguments launch the TUI only when both stdin and stdout are terminals; otherwise, help is printed to stderr and the command exits 2.")
	return err
}

func ensureFFmpeg() error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return errors.New("please add ffmpeg to your path")
	}
	return nil
}
