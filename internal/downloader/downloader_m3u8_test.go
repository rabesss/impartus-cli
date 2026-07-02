package downloader

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

// TestWriteM3U8File tests m3u8 file creation
func TestWriteM3U8File(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name        string
		filename    string
		chunks      []string
		wantErr     bool
		checkHeader bool
	}{
		{
			name:        "empty chunks",
			filename:    "empty.m3u8",
			chunks:      []string{},
			wantErr:     false,
			checkHeader: true,
		},
		{
			name:        "single chunk",
			filename:    "single.m3u8",
			chunks:      []string{"chunk1.ts"},
			wantErr:     false,
			checkHeader: true,
		},
		{
			name:        "multiple chunks",
			filename:    "multi.m3u8",
			chunks:      []string{"chunk1.ts", "chunk2.ts", "chunk3.ts"},
			wantErr:     false,
			checkHeader: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(tmpDir, tt.filename)
			err := writeM3U8File(path, tt.chunks)
			if (err != nil) != tt.wantErr {
				t.Errorf("writeM3U8File() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				content, err := os.ReadFile(path)
				if err != nil {
					t.Errorf("failed to read created file: %v", err)
				}
				if tt.checkHeader && len(content) == 0 {
					t.Error("written file is empty")
				}
			}
		})
	}
}

// TestSumPipelineStats tests pipeline statistics summation

// TestJoinIfPresent tests joinIfPresent function
func TestJoinIfPresent(t *testing.T) {
	d := &Downloader{}

	tests := []struct {
		name    string
		path    string
		enabled bool
		title   string
		wantErr bool
		wantOut string
	}{
		{
			name:    "empty path returns empty",
			path:    "",
			enabled: true,
			title:   "test",
			wantErr: false,
			wantOut: "",
		},
		{
			name:    "disabled returns empty",
			path:    "/path/to/file.m3u8",
			enabled: false,
			title:   "test",
			wantErr: false,
			wantOut: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockJoin := func(_ context.Context, path, title string) (string, error) {
				return path, nil
			}
			got, err := d.joinIfPresent(context.Background(), tt.path, tt.enabled, tt.title, mockJoin)
			if (err != nil) != tt.wantErr {
				t.Errorf("joinIfPresent() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.wantOut {
				t.Errorf("joinIfPresent() = %v, want %v", got, tt.wantOut)
			}
		})
	}
}

// TestCreateTempM3U8File tests temporary M3U8 file creation

// TestCreateTempM3U8File tests temporary M3U8 file creation
func TestCreateTempM3U8File(t *testing.T) {
	tmpDir := t.TempDir()

	d := &Downloader{
		config: &config.Config{
			TempDirLocation: tmpDir,
		},
	}

	tests := []struct {
		name               string
		downloadedPlaylist DownloadedPlaylist
		wantFirstViewFile  bool
		wantSecondViewFile bool
	}{
		{
			name: "both views",
			downloadedPlaylist: DownloadedPlaylist{
				FirstViewChunks:  []string{"chunk1.ts", "chunk2.ts"},
				SecondViewChunks: []string{"chunk3.ts", "chunk4.ts"},
				Playlist: client.ParsedPlaylist{
					ID:    123,
					Title: "Test Lecture",
					SeqNo: 1,
				},
			},
			wantFirstViewFile:  true,
			wantSecondViewFile: true,
		},
		{
			name: "first view only",
			downloadedPlaylist: DownloadedPlaylist{
				FirstViewChunks: []string{"chunk1.ts"},
				Playlist: client.ParsedPlaylist{
					ID:    456,
					Title: "First Only",
					SeqNo: 2,
				},
			},
			wantFirstViewFile:  true,
			wantSecondViewFile: false,
		},
		{
			name: "second view only",
			downloadedPlaylist: DownloadedPlaylist{
				SecondViewChunks: []string{"chunk1.ts"},
				Playlist: client.ParsedPlaylist{
					ID:    789,
					Title: "Second Only",
					SeqNo: 3,
				},
			},
			wantFirstViewFile:  false,
			wantSecondViewFile: true,
		},
		{
			name: "no chunks",
			downloadedPlaylist: DownloadedPlaylist{
				Playlist: client.ParsedPlaylist{
					ID:    100,
					Title: "No Chunks",
					SeqNo: 4,
				},
			},
			wantFirstViewFile:  false,
			wantSecondViewFile: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m3u8File, err := d.CreateTempM3U8File(tt.downloadedPlaylist)
			if err != nil {
				t.Errorf("CreateTempM3U8File() error = %v", err)
				return
			}

			if (m3u8File.FirstViewFile != "") != tt.wantFirstViewFile {
				t.Errorf("FirstViewFile present = %v, want %v", m3u8File.FirstViewFile != "", tt.wantFirstViewFile)
			}

			if (m3u8File.SecondViewFile != "") != tt.wantSecondViewFile {
				t.Errorf("SecondViewFile present = %v, want %v", m3u8File.SecondViewFile != "", tt.wantSecondViewFile)
			}

			// Verify the files exist if they should be created
			if tt.wantFirstViewFile && m3u8File.FirstViewFile != "" {
				if _, err := os.Stat(m3u8File.FirstViewFile); os.IsNotExist(err) {
					t.Errorf("FirstViewFile does not exist: %s", m3u8File.FirstViewFile)
				}
			}

			if tt.wantSecondViewFile && m3u8File.SecondViewFile != "" {
				if _, err := os.Stat(m3u8File.SecondViewFile); os.IsNotExist(err) {
					t.Errorf("SecondViewFile does not exist: %s", m3u8File.SecondViewFile)
				}
			}
		})
	}
}

func TestCreateTempM3U8FileInvalidDir(t *testing.T) {
	d := &Downloader{
		config: &config.Config{
			TempDirLocation: "/nonexistent/path/that/cannot/be/created",
		},
	}

	dp := DownloadedPlaylist{
		FirstViewChunks: []string{"chunk1.ts"},
		Playlist: client.ParsedPlaylist{
			ID:    1,
			Title: "Test",
			SeqNo: 1,
		},
	}

	_, err := d.CreateTempM3U8File(dp)
	if err == nil {
		t.Error("CreateTempM3U8File() expected error for invalid directory")
	}
}
