package downloader

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

type finalOutputGate struct {
	token chan struct{}
	refs  int
}

var finalOutputGates = struct {
	sync.Mutex
	items map[string]*finalOutputGate
}{items: make(map[string]*finalOutputGate)}

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

	output, err := d.runFFmpegToFinal(ctx, outfile, "-y", "-hide_banner", "-i", leftFile, "-i", rightFile, "-map", "0", "-map", "1", "-c", "copy")
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
	output, err := d.runFFmpegToFinal(ctx, outfile, "-y", "-hide_banner", "-i", m3u8File, "-c", "copy")
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

	output, err := d.runFFmpegToFinal(ctx, outfile,
		"-y",
		"-hide_banner",
		"-loglevel", "error",
		"-i", m3u8File,
		"-vn",
		"-acodec", getAudioCodec(format),
		"-ab", "192k",
	)
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

	output, err := d.runFFmpegToFinal(ctx, outfile,
		"-y",
		"-hide_banner",
		"-loglevel", "error",
		"-i", sourceFile,
		"-vn",
		"-acodec", getAudioCodec(format),
		"-ab", "192k",
	)
	if err != nil {
		return "", fmt.Errorf("ffmpeg combined audio output failed: %w: %s", err, string(output))
	}

	return outfile, nil
}

func (d *Downloader) runFFmpegToFinal(ctx context.Context, outfile string, arguments ...string) ([]byte, error) {
	release, lockErr := acquireFinalOutput(ctx, outfile)
	if lockErr != nil {
		return nil, lockErr
	}
	defer release()
	muxer, err := ffmpegMuxer(outfile)
	if err != nil {
		return nil, err
	}
	partial := outfile + ".part"
	if validationErr := d.validateOutputPath(partial); validationErr != nil {
		return nil, validationErr
	}
	if removeErr := os.Remove(partial); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return nil, fmt.Errorf("remove stale partial output: %w", removeErr)
	}
	commandArguments := append([]string(nil), arguments...)
	commandArguments = append(commandArguments, "-f", muxer, partial)
	command := exec.CommandContext(ctx, d.ffmpegPath, commandArguments...) // #nosec G204 -- executable is the configured local ffmpeg and all paths are validated
	output, commandErr := command.CombinedOutput()
	if commandErr != nil {
		removeErr := os.Remove(partial)
		if errors.Is(removeErr, os.ErrNotExist) {
			removeErr = nil
		}
		return output, errors.Join(commandErr, removeErr)
	}
	publication, err := syncAndReplaceOutput(partial, outfile)
	if err != nil {
		return output, errors.Join(err, removePartialOutput(partial))
	}
	if publication.Warning != nil {
		d.logDiagnostic("warning: %v", publication.Warning)
	}
	return output, nil
}

func acquireFinalOutput(ctx context.Context, path string) (func(), error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve final output lock path: %w", err)
	}
	key := filepath.Clean(absolute)
	finalOutputGates.Lock()
	gate := finalOutputGates.items[key]
	if gate == nil {
		gate = &finalOutputGate{token: make(chan struct{}, 1)}
		gate.token <- struct{}{}
		finalOutputGates.items[key] = gate
	}
	gate.refs++
	finalOutputGates.Unlock()

	select {
	case <-gate.token:
		var once sync.Once
		return func() {
			once.Do(func() {
				finalOutputGates.Lock()
				gate.token <- struct{}{}
				gate.refs--
				if gate.refs == 0 {
					delete(finalOutputGates.items, key)
				}
				finalOutputGates.Unlock()
			})
		}, nil
	case <-ctx.Done():
		finalOutputGates.Lock()
		gate.refs--
		if gate.refs == 0 {
			delete(finalOutputGates.items, key)
		}
		finalOutputGates.Unlock()
		return nil, ctx.Err()
	}
}

func removePartialOutput(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func ffmpegMuxer(path string) (string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp4":
		return "mp4", nil
	case ".mkv":
		return "matroska", nil
	case ".mp3":
		return "mp3", nil
	case ".m4a":
		return "ipod", nil
	case ".opus":
		return "opus", nil
	default:
		return "", fmt.Errorf("cannot determine ffmpeg output format for %q", path)
	}
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
