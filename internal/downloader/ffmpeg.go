package downloader

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

func (d *Downloader) JoinViews(leftFile, rightFile, name string) (string, error) {
	title := fmt.Sprintf("%s BOTH.mkv", name)
	outfile := filepath.Join(d.config.DownloadLocation, title)
	if err := validateFFmpegArgs(leftFile, rightFile, outfile); err != nil {
		return "", err
	}

	cmd := exec.Command(d.ffmpegPath, "-y", "-hide_banner", "-i", leftFile, "-i", rightFile, "-map", "0", "-map", "1", "-c", "copy", outfile) // #nosec G204 -- arguments are validated local file paths and fixed ffmpeg flags
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ffmpeg join views failed: %w: %s", err, string(output))
	}

	return outfile, nil
}

func (d *Downloader) JoinChunksFromM3U8(m3u8File, title string) (string, error) {
	outfile := filepath.Join(d.config.DownloadLocation, title)
	if err := validateFFmpegArgs(m3u8File, outfile); err != nil {
		return "", err
	}
	cmd := exec.Command(d.ffmpegPath, "-y", "-hide_banner", "-i", m3u8File, "-c", "copy", outfile) // #nosec G204 -- arguments are validated local file paths and fixed ffmpeg flags
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ffmpeg join chunks failed: %w: %s", err, string(output))
	}
	return outfile, nil
}

func (d *Downloader) JoinChunksFromM3U8AudioOnly(m3u8File, title, format string) (string, error) {
	ext := "." + format
	if format == "aac" {
		ext = ".m4a"
	}
	titleWithoutExt := strings.TrimSuffix(title, filepath.Ext(title))
	outfile := filepath.Join(d.config.DownloadLocation, titleWithoutExt+ext)
	if err := validateFFmpegArgs(m3u8File, outfile); err != nil {
		return "", err
	}

	cmd := exec.Command(d.ffmpegPath,
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

func (d *Downloader) CreateBothViewsAudioOutput(sourceFile, name, format string) (string, error) {
	ext := "." + format
	if format == "aac" {
		ext = ".m4a"
	}
	title := fmt.Sprintf("%s BOTH%s", name, ext)
	outfile := filepath.Join(d.config.DownloadLocation, title)
	if err := validateFFmpegArgs(sourceFile, outfile); err != nil {
		return "", err
	}

	cmd := exec.Command(d.ffmpegPath,
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
