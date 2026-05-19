package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
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

func warnNoAudioLectures(lectures client.Lectures, skipNoAudio bool) {
	noaudioCount := countNoAudioLectures(lectures)
	if noaudioCount > 0 && !skipNoAudio {
		fmt.Printf("[WARNING] %d lecture(s) in selection have no audio track (noaudio=1)\n", noaudioCount)
		fmt.Printf("[INFO] Use --skip-no-audio to filter these out, or --include-noaudio to include anyway\n")
	}
}

func countChunks(playlists []client.ParsedPlaylist, views string) int {
	total := 0
	for _, playlist := range playlists {
		if views != "right" {
			total += len(playlist.FirstViewURLs)
		}
		if views != "left" {
			total += len(playlist.SecondViewURLs)
		}
	}
	return total
}

// validateFlagOverrides validates config values after CLI flag overrides are applied.
// This ensures invalid flag values fail early, before any remote API calls.
func validateFlagOverrides(cfg *config.Config) error {
	if cfg.Quality != "" && !config.OneOf(cfg.Quality, "144", "450", "720") {
		return fmt.Errorf("invalid quality value %q: must be one of: 144, 450, 720", cfg.Quality)
	}
	if cfg.Views != "" && !config.OneOf(cfg.Views, "first", "second", "both", "left", "right") {
		return fmt.Errorf("invalid views value %q: must be one of: first, second, both, left, right", cfg.Views)
	}
	if cfg.AudioOnly && cfg.AudioFormat != "" && !config.OneOf(cfg.AudioFormat, "mp3", "m4a", "aac", "opus") {
		return fmt.Errorf("invalid audioFormat value %q: must be one of: mp3, m4a, aac, opus", cfg.AudioFormat)
	}
	return nil
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	return enc.Encode(v)
}

func showVersion(version, date string) {
	fmt.Printf("Impartus Video Downloader\nVersion: %s\nBuild Date: %s\n", version, date)
}

func showHelp(version, date string) {
	showVersion(version, date)
	fmt.Println("\nUsage:")
	fmt.Println("  impartus [command] [flags]")
	fmt.Println("\nCommands:")
	fmt.Println("  courses                              List courses (JSON)")
	fmt.Println("  lectures -s <subject> -S <session>   List lectures (JSON)")
	fmt.Println("  download [flags]                     Download lectures")
	fmt.Println("  play [flags]                         Play lectures in mpv")
	fmt.Println("  serve [--port <port>]                Start HTTP API server")
	fmt.Println("  version                              Show version")
	fmt.Println("  help                                 Show help")
	fmt.Println("\nDownload / Play Flags:")
	fmt.Println("  --subject,-s        Subject ID")
	fmt.Println("  --session,-S        Session ID")
	fmt.Println("  --start             Start lecture index (1-based)")
	fmt.Println("  --end               End lecture index (1-based)")
	fmt.Println("  --lecture,-l        Specific lecture index (1-based, play only)")
	fmt.Println("  --quality           Quality override")
	fmt.Println("  --views             Views override")
	fmt.Println("  --audio-only        Audio-only mode (download only)")
	fmt.Println("  --format            Audio format override (download only)")
	fmt.Println("  --output,-o         Output directory (download only)")
	fmt.Println("  --skip-no-audio     Skip lectures with no audio track")
	fmt.Println("  --include-noaudio   Include noaudio lectures (overrides --skip-no-audio)")
	fmt.Println("\nNo command starts interactive download mode (or interactive play mode if 'play' is used without flags).")
}

func ensureFFmpeg() error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return errors.New("please add ffmpeg to your path")
	}
	return nil
}
