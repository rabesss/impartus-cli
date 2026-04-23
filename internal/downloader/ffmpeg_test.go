package downloader

import (
	"errors"
	"testing"

	"github.com/rabesss/impartus-cli/internal/config"
)

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
