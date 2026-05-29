package downloader

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// JoinViews merges two video view files into a single combined output using FFmpeg.
func (d *Downloader) JoinViews(ctx context.Context, leftFile, rightFile, name string) (string, error) {
	title := fmt.Sprintf("%s BOTH.mkv", name)
	outfile := filepath.Join(d.config.DownloadLocation, title)
	if err := validateFFmpegArgs(leftFile, rightFile, outfile); err != nil {
		return "", err
	}
	if err := d.validateOutputPath(outfile); err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, d.ffmpegPath, "-y", "-hide_banner", "-i", leftFile, "-i", rightFile, "-map", "0", "-map", "1", "-c", "copy", outfile) // #nosec G204 -- arguments are validated local file paths and fixed ffmpeg flags
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ffmpeg join views failed: %w: %s", err, string(output))
	}

	return outfile, nil
}

// JoinChunksFromM3U8 concatenates video chunks listed in an M3U8 manifest into a single output file.
func (d *Downloader) JoinChunksFromM3U8(ctx context.Context, m3u8File, title string) (string, error) {
	outfile := filepath.Join(d.config.DownloadLocation, title)
	if err := validateFFmpegArgs(m3u8File, outfile); err != nil {
		return "", err
	}
	if err := d.validateOutputPath(outfile); err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, d.ffmpegPath, "-y", "-hide_banner", "-i", m3u8File, "-c", "copy", outfile) // #nosec G204 -- arguments are validated local file paths and fixed ffmpeg flags
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ffmpeg join chunks failed: %w: %s", err, string(output))
	}
	return outfile, nil
}

// JoinChunksFromM3U8AudioOnly extracts and joins audio from an M3U8 manifest into a single audio file.
func (d *Downloader) JoinChunksFromM3U8AudioOnly(ctx context.Context, m3u8File, title, format string) (string, error) {
	ext := "." + format
	if format == "aac" {
		ext = ".m4a"
	}
	titleWithoutExt := strings.TrimSuffix(title, filepath.Ext(title))
	outfile := filepath.Join(d.config.DownloadLocation, titleWithoutExt+ext)
	if err := validateFFmpegArgs(m3u8File, outfile); err != nil {
		return "", err
	}
	if err := d.validateOutputPath(outfile); err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, d.ffmpegPath,
		"-y",
		"-hide_banner",
		"-loglevel", "error",
		"-i", m3u8File,
		"-vn",
		"-acodec", getAudioCodec(format),
		"-ab", "192k",
		outfile,
	) // #nosec G204 -- arguments are validated local file paths and fixed ffmpeg flags

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ffmpeg audio extract failed: %w: %s", err, string(output))
	}

	return outfile, nil
}

// CreateBothViewsAudioOutput produces a combined audio-only file from a source video using FFmpeg.
func (d *Downloader) CreateBothViewsAudioOutput(ctx context.Context, sourceFile, name, format string) (string, error) {
	ext := "." + format
	if format == "aac" {
		ext = ".m4a"
	}
	title := fmt.Sprintf("%s BOTH%s", name, ext)
	outfile := filepath.Join(d.config.DownloadLocation, title)
	if err := validateFFmpegArgs(sourceFile, outfile); err != nil {
		return "", err
	}
	if err := d.validateOutputPath(outfile); err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, d.ffmpegPath,
		"-y",
		"-hide_banner",
		"-loglevel", "error",
		"-i", sourceFile,
		"-vn",
		"-acodec", getAudioCodec(format),
		"-ab", "192k",
		outfile,
	) // #nosec G204 -- arguments are validated local file paths and fixed ffmpeg flags

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ffmpeg combined audio output failed: %w: %s", err, string(output))
	}

	return outfile, nil
}

// validateOutputPath checks that outfile is under the configured DownloadLocation.
func (d *Downloader) validateOutputPath(outfile string) error {
	absOut, err := filepath.Abs(outfile)
	if err != nil {
		return fmt.Errorf("cannot resolve output path: %w", err)
	}
	absDownload, err := filepath.Abs(d.config.DownloadLocation)
	if err != nil {
		return fmt.Errorf("cannot resolve download location: %w", err)
	}
	if absOut != absDownload && !strings.HasPrefix(absOut, absDownload+string(filepath.Separator)) {
		return fmt.Errorf("output path %q escapes download location %q", outfile, d.config.DownloadLocation)
	}
	return nil
}

func getAudioCodec(format string) string {
	switch format {
	case "mp3":
		return "libmp3lame"
	case "m4a", "aac":
		return "aac"
	case "opus":
		return "libopus"
	default:
		return "libmp3lame"
	}
}

func validateFFmpegArgs(args ...string) error {
	for _, arg := range args {
		if strings.TrimSpace(arg) == "" {
			return errors.New("ffmpeg arguments must not be empty")
		}
		if strings.ContainsRune(arg, '\x00') {
			return errors.New("ffmpeg arguments must not contain null bytes")
		}
	}
	return nil
}
