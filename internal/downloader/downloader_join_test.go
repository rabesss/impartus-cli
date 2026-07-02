package downloader

import (
	"context"
	"testing"

	"github.com/rabesss/impartus-cli/internal/config"
)

func TestJoinLectureOutput(t *testing.T) {
	tmpDir := t.TempDir()

	d := &Downloader{
		config: &config.Config{
			AudioOnly:        false,
			Views:            "both",
			TempDirLocation:  tmpDir,
			DownloadLocation: tmpDir,
			AudioFormat:      "mp3",
		},
	}

	// JoinLectureOutput without audio-only mode
	result, err := d.JoinLectureOutput(context.Background(), M3U8File{})
	if err != nil {
		t.Errorf("JoinLectureOutput() error = %v", err)
	}
	if result.LeftOutput != "" {
		t.Errorf("LeftOutput should be empty for empty M3U8File, got %s", result.LeftOutput)
	}
}

func TestJoinLectureOutputAudioOnly(t *testing.T) {
	tmpDir := t.TempDir()

	d := &Downloader{
		config: &config.Config{
			AudioOnly:        true,
			Views:            "both",
			TempDirLocation:  tmpDir,
			DownloadLocation: tmpDir,
			AudioFormat:      "m4a",
		},
	}

	// JoinLectureOutput with audio-only mode
	result, err := d.JoinLectureOutput(context.Background(), M3U8File{})
	if err != nil {
		t.Errorf("JoinLectureOutput() error = %v", err)
	}
	if result.LeftOutput != "" {
		t.Errorf("LeftOutput should be empty for empty M3U8File, got %s", result.LeftOutput)
	}
}
