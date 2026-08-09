package downloader

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rabesss/impartus-cli/internal/config"
)

func TestJoinChunksPublishesFinalOutputAtomically(t *testing.T) {
	outputDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "ffmpeg.log")
	downloader := New(&config.Config{DownloadLocation: outputDir}, nil)
	downloader.ffmpegPath = writeFakeFFmpegScript(t, logPath, "new media")
	manifestPath := filepath.Join(t.TempDir(), "input.m3u8")
	if err := os.WriteFile(manifestPath, []byte("#EXTM3U"), 0o600); err != nil {
		t.Fatal(err)
	}

	outputPath, err := downloader.JoinChunksFromM3U8(t.Context(), manifestPath, "lecture.mp4")
	if err != nil {
		t.Fatalf("JoinChunksFromM3U8() error = %v", err)
	}
	contents, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "new media" {
		t.Fatalf("final contents = %q", contents)
	}
	if _, statErr := os.Stat(outputPath + ".part"); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("partial output remains: %v", statErr)
	}
	arguments, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(arguments), outputPath+".part") {
		t.Fatalf("ffmpeg arguments did not target same-directory partial: %s", arguments)
	}
}

func TestJoinChunksSyncFailureRemovesPartialAndPreservesFinal(t *testing.T) {
	outputDir := t.TempDir()
	outputPath := filepath.Join(outputDir, "lecture.mp4")
	if err := os.WriteFile(outputPath, []byte("previous media"), 0o600); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(t.TempDir(), "ffmpeg")
	if err := os.WriteFile(scriptPath, []byte("#!/usr/bin/env bash\nset -eu\nlast=\"${@: -1}\"\nmkdir \"$last\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	d := New(&config.Config{DownloadLocation: outputDir}, nil)
	d.ffmpegPath = scriptPath
	manifestPath := filepath.Join(t.TempDir(), "input.m3u8")
	if err := os.WriteFile(manifestPath, []byte("#EXTM3U"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := d.JoinChunksFromM3U8(t.Context(), manifestPath, "lecture.mp4"); err == nil {
		t.Fatal("JoinChunksFromM3U8() error = nil, want partial sync failure")
	}
	contents, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "previous media" {
		t.Fatalf("sync failure replaced prior final with %q", contents)
	}
	if _, err := os.Lstat(outputPath + ".part"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("sync failure left partial: %v", err)
	}
}

func TestConcurrentJoinChunksSerializesCollidingOutput(t *testing.T) {
	outputDir := t.TempDir()
	lockPath := filepath.Join(t.TempDir(), "ffmpeg-active")
	scriptPath := filepath.Join(t.TempDir(), "ffmpeg")
	script := "#!/usr/bin/env bash\nset -eu\nlast=\"${@: -1}\"\nmkdir \"" + lockPath + "\"\ntrap 'rmdir \"" + lockPath + "\"' EXIT\nsleep 0.1\nprintf 'media' > \"$last\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	d := New(&config.Config{DownloadLocation: outputDir}, nil)
	d.ffmpegPath = scriptPath
	manifestPath := filepath.Join(t.TempDir(), "input.m3u8")
	if err := os.WriteFile(manifestPath, []byte("#EXTM3U"), 0o600); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	errorsFound := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := d.JoinChunksFromM3U8(context.Background(), manifestPath, "lecture.mp4")
			errorsFound <- err
		}()
	}
	close(start)
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatalf("concurrent JoinChunksFromM3U8() error = %v", err)
		}
	}
	if _, err := os.Stat(filepath.Join(outputDir, "lecture.mp4")); err != nil {
		t.Fatalf("final output missing: %v", err)
	}
}

func TestJoinChunksFailurePreservesPreviousFinalAndRemovesPartial(t *testing.T) {
	outputDir := t.TempDir()
	outputPath := filepath.Join(outputDir, "lecture.mp4")
	if err := os.WriteFile(outputPath, []byte("previous media"), 0o600); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(t.TempDir(), "ffmpeg")
	if err := os.WriteFile(scriptPath, []byte("#!/usr/bin/env bash\nset -eu\nlast=\"${@: -1}\"\nprintf 'partial media' > \"$last\"\nexit 42\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	downloader := New(&config.Config{DownloadLocation: outputDir}, nil)
	downloader.ffmpegPath = scriptPath
	manifestPath := filepath.Join(t.TempDir(), "input.m3u8")
	if err := os.WriteFile(manifestPath, []byte("#EXTM3U"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := downloader.JoinChunksFromM3U8(t.Context(), manifestPath, "lecture.mp4"); err == nil {
		t.Fatal("JoinChunksFromM3U8() error = nil, want ffmpeg failure")
	}
	contents, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "previous media" {
		t.Fatalf("failed join replaced prior final with %q", contents)
	}
	if _, err := os.Stat(outputPath + ".part"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed join left partial: %v", err)
	}
}

func TestValidateFFmpegArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr error
	}{
		{
			name:    "valid arguments",
			args:    []string{"/path/to/input.m3u8", "/path/to/output.mp4"},
			wantErr: nil,
		},
		{
			name:    "single valid argument",
			args:    []string{"/path/to/file.mkv"},
			wantErr: nil,
		},
		{
			name:    "empty string argument",
			args:    []string{"", "/path/to/output.mp4"},
			wantErr: errors.New("ffmpeg arguments must not be empty"),
		},
		{
			name:    "whitespace-only argument",
			args:    []string{"   ", "/path/to/output.mp4"},
			wantErr: errors.New("ffmpeg arguments must not be empty"),
		},
		{
			name:    "multiple empty arguments",
			args:    []string{"", "", ""},
			wantErr: errors.New("ffmpeg arguments must not be empty"),
		},
		{
			name:    "argument with null byte",
			args:    []string{"/path/to/file\x00.mkv"},
			wantErr: errors.New("ffmpeg arguments must not contain null bytes"),
		},
		{
			name:    "mixed valid and invalid",
			args:    []string{"/path/to/file.mkv", "", "/path/to/output"},
			wantErr: errors.New("ffmpeg arguments must not be empty"),
		},
		{
			name:    "no arguments is valid",
			args:    []string{},
			wantErr: nil,
		},
		{
			name:    "tab character is valid",
			args:    []string{"/path/to/file\twith\ttabs.mkv"},
			wantErr: nil,
		},
		{
			name:    "newline in argument fails",
			args:    []string{"/path/to/file\n.mkv"},
			wantErr: nil, // newline is not a null byte, so it's allowed
		},
		{
			name:    "file path with spaces is valid",
			args:    []string{"/path/with spaces/file.mkv", "/path/with spaces/out.mkv"},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFFmpegArgs(tt.args...)
			if tt.wantErr != nil {
				if err == nil {
					t.Errorf("validateFFmpegArgs(%v) expected error %q, got nil", tt.args, tt.wantErr)
					return
				}
				if err.Error() != tt.wantErr.Error() {
					t.Errorf("validateFFmpegArgs(%v) error = %q, want %q", tt.args, err.Error(), tt.wantErr.Error())
				}
			} else {
				if err != nil {
					t.Errorf("validateFFmpegArgs(%v) unexpected error: %v", tt.args, err)
				}
			}
		})
	}
}

func TestNewDownloader(t *testing.T) {
	tests := []struct {
		name       string
		cfg        *config.Config
		wantNil    bool
		ffmpegPath string
	}{
		{
			name:       "nil config creates valid downloader",
			cfg:        nil,
			wantNil:    false,
			ffmpegPath: "ffmpeg",
		},
		{
			name:       "empty config creates valid downloader",
			cfg:        &config.Config{},
			wantNil:    false,
			ffmpegPath: "ffmpeg",
		},
		{
			name:       "config with values creates valid downloader",
			cfg:        &config.Config{DownloadLocation: "/tmp/downloads"},
			wantNil:    false,
			ffmpegPath: "ffmpeg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := New(tt.cfg, nil)

			if tt.wantNil {
				if d != nil {
					t.Errorf("New() = %v, want nil", d)
				}
				return
			}

			if d == nil {
				t.Fatal("New() returned nil")
			}

			if d.config == nil {
				t.Error("config should not be nil")
			}

			if d.rateLimiter == nil {
				t.Error("rateLimiter should not be nil")
			}

			if d.maxRetries != 3 {
				t.Errorf("maxRetries = %d, want 3", d.maxRetries)
			}

			if d.ffmpegPath != tt.ffmpegPath {
				t.Errorf("ffmpegPath = %q, want %q", d.ffmpegPath, tt.ffmpegPath)
			}
		})
	}
}

func TestNewDownloaderWithClient(t *testing.T) {
	cfg := &config.Config{}
	d := New(cfg, nil)

	if d == nil {
		t.Fatal("New() returned nil")
	}

	if d.client == nil {
		t.Error("client should not be nil even when nil is passed")
	}
}
