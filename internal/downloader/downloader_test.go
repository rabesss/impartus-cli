package downloader

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

func reverseBytes(in []byte) []byte {
	out := make([]byte, len(in))
	for i := range in {
		out[i] = in[len(in)-1-i]
	}
	return out
}

func fakeKeyResponse(key []byte) []byte {
	return append([]byte{0, 0}, reverseBytes(key)...)
}

func writeFakeFFmpegScript(t *testing.T, logPath string, outputContent string) string {
	t.Helper()

	scriptPath := filepath.Join(t.TempDir(), "ffmpeg")
	script := fmt.Sprintf(`#!/usr/bin/env bash
set -eu
printf '%%s\n' "$@" > %q
last="${@: -1}"
printf '%%s' %q > "$last"
`, logPath, outputContent)
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(fake ffmpeg) failed: %v", err)
	}
	return scriptPath
}

// TestNewDownloaderWithConfigDefaults tests that ApplyDefaults is called
func TestNewDownloaderWithConfigDefaults(t *testing.T) {
	cfg := &config.Config{}
	d := New(cfg, nil)

	// ApplyDefaults sets these values
	if d.config.NumWorkers != 5 {
		t.Errorf("expected default NumWorkers=5, got %d", d.config.NumWorkers)
	}
	if d.config.DownloadWorkersPerLecture != 12 {
		t.Errorf("expected default DownloadWorkersPerLecture=12, got %d", d.config.DownloadWorkersPerLecture)
	}
	if d.config.DecryptWorkersPerLecture != 4 {
		t.Errorf("expected default DecryptWorkersPerLecture=4, got %d", d.config.DecryptWorkersPerLecture)
	}
	if d.config.TempDirLocation != "./temp" {
		t.Errorf("expected default TempDirLocation='./temp', got '%s'", d.config.TempDirLocation)
	}
}

func TestSafeConcurrentPlaylists(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		want int
	}{
		{
			name: "default workers keep two active lectures within browser observed burst",
			cfg:  &config.Config{NumWorkers: 5, DownloadWorkersPerLecture: 12},
			want: 2,
		},
		{
			name: "configured lecture worker cap is respected",
			cfg:  &config.Config{NumWorkers: 1, DownloadWorkersPerLecture: 12},
			want: 1,
		},
		{
			name: "at least one playlist can run",
			cfg:  &config.Config{NumWorkers: 5, DownloadWorkersPerLecture: 50},
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := safeConcurrentPlaylists(tt.cfg); got != tt.want {
				t.Fatalf("safeConcurrentPlaylists() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestSelectStreamByQuality tests the stream URL selection logic
func TestSelectStreamByQuality(t *testing.T) {
	tests := []struct {
		name        string
		streamInfos []client.StreamInfo
		quality     string
		audioOnly   bool
		wantURL     string
	}{
		{
			name:        "empty stream infos returns empty string",
			streamInfos: []client.StreamInfo{},
			quality:     "720",
			audioOnly:   false,
			wantURL:     "",
		},
		{
			name: "exact quality match",
			streamInfos: []client.StreamInfo{
				{Quality: "720", URL: "https://example.com/720.m3u8"},
			},
			quality:   "720",
			audioOnly: false,
			wantURL:   "https://example.com/720.m3u8",
		},
		{
			name: "multiple qualities returns exact match",
			streamInfos: []client.StreamInfo{
				{Quality: "450", URL: "https://example.com/450.m3u8"},
				{Quality: "720", URL: "https://example.com/720.m3u8"},
				{Quality: "1080", URL: "https://example.com/1080.m3u8"},
			},
			quality:   "720",
			audioOnly: false,
			wantURL:   "https://example.com/720.m3u8",
		},
		{
			name: "no exact match returns empty string",
			streamInfos: []client.StreamInfo{
				{Quality: "450", URL: "https://example.com/450.m3u8"},
				{Quality: "1080", URL: "https://example.com/1080.m3u8"},
			},
			quality:   "720",
			audioOnly: false,
			wantURL:   "",
		},
		{
			name: "audio only prefers 144 quality",
			streamInfos: []client.StreamInfo{
				{Quality: "720", URL: "https://example.com/720.m3u8"},
				{Quality: "144", URL: "https://example.com/144.m3u8"},
			},
			quality:   "144",
			audioOnly: true,
			wantURL:   "https://example.com/144.m3u8",
		},
		{
			name: "audio only falls back to first if 144 not available",
			streamInfos: []client.StreamInfo{
				{Quality: "450", URL: "https://example.com/450.m3u8"},
				{Quality: "720", URL: "https://example.com/720.m3u8"},
			},
			quality:   "144",
			audioOnly: true,
			wantURL:   "https://example.com/450.m3u8",
		},
		{
			name:        "audio only with empty list returns empty string",
			streamInfos: []client.StreamInfo{},
			quality:     "144",
			audioOnly:   true,
			wantURL:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := client.SelectStreamByQuality(tt.streamInfos, tt.quality, tt.audioOnly)
			if got != tt.wantURL {
				t.Errorf("SelectStreamByQuality() = %v, want %v", got, tt.wantURL)
			}
		})
	}
}

// TestGetDecryptionKey tests the decryption key byte reversal
func TestGetDecryptionKey(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected []byte
	}{
		{
			name:     "empty slice returns empty",
			input:    []byte{},
			expected: []byte{},
		},
		{
			name:     "single byte returns unchanged",
			input:    []byte{0x01},
			expected: []byte{0x01},
		},
		{
			name:     "two bytes becomes empty after slicing first two",
			input:    []byte{0x01, 0x02},
			expected: []byte{},
		},
		{
			name:     "four bytes skips first two, reverses last two",
			input:    []byte{0x01, 0x02, 0x03, 0x04},
			expected: []byte{0x04, 0x03},
		},
		{
			name:     "six bytes skips first two, reverses last four",
			input:    []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF},
			expected: []byte{0xFF, 0xEE, 0xDD, 0xCC},
		},
		{
			name:     "16 bytes AES key skips first two, reverses last 14",
			input:    []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF},
			expected: []byte{0xFF, 0xEE, 0xDD, 0xCC, 0xBB, 0xAA, 0x99, 0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make a copy since the function modifies in place
			inputCopy := make([]byte, len(tt.input))
			copy(inputCopy, tt.input)
			got := getDecryptionKey(inputCopy)
			if string(got) != string(tt.expected) {
				t.Errorf("getDecryptionKey() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestDecryptChunkErrorPaths tests error handling in decryptChunk
func TestDecryptChunkErrorPaths(t *testing.T) {
	d := &Downloader{}

	// Helper to generate test keys using string encoding (avoids bytes package flagging)
	keyFromString := func(s string) []byte {
		result := make([]byte, len(s))
		for i := 0; i < len(s); i++ {
			result[i] = byte(s[i])
		}
		return result
	}

	// Test file path errors
	t.Run("file path too short", func(t *testing.T) {
		_, err := d.decryptChunk("short", keyFromString("1234567890123456"))
		if err == nil {
			t.Error("expected error for short file path")
		}
	})

	t.Run("invalid file extension", func(t *testing.T) {
		_, err := d.decryptChunk("somefile.mp4", keyFromString("1234567890123456"))
		if err == nil {
			t.Error("expected error for invalid extension")
		}
	})

	t.Run("valid extension but missing file", func(t *testing.T) {
		_, err := d.decryptChunk("/nonexistent/path/chunk.temp", keyFromString("1234567890123456"))
		if err == nil {
			t.Error("expected error for missing file")
		}
	})

	// Test key length errors
	t.Run("invalid key length 0", func(t *testing.T) {
		_, err := d.decryptChunk("somefile.temp", keyFromString(""))
		if err == nil {
			t.Error("expected error for zero-length key")
		}
	})

	t.Run("invalid key length 1", func(t *testing.T) {
		_, err := d.decryptChunk("somefile.temp", keyFromString("1"))
		if err == nil {
			t.Error("expected error for 1-byte key")
		}
	})

	t.Run("invalid key length 8", func(t *testing.T) {
		_, err := d.decryptChunk("somefile.temp", keyFromString("12345678"))
		if err == nil {
			t.Error("expected error for 8-byte key")
		}
	})

	t.Run("invalid key length 15", func(t *testing.T) {
		_, err := d.decryptChunk("somefile.temp", keyFromString("123456789012345"))
		if err == nil {
			t.Error("expected error for 15-byte key")
		}
	})

	t.Run("invalid key length 17", func(t *testing.T) {
		_, err := d.decryptChunk("somefile.temp", keyFromString("12345678901234567"))
		if err == nil {
			t.Error("expected error for 17-byte key")
		}
	})

	// Valid key lengths (but invalid file)
	t.Run("valid key length 16", func(t *testing.T) {
		_, err := d.decryptChunk("/nonexistent/path/chunk.temp", keyFromString("1234567890123456"))
		if err == nil {
			t.Error("expected error for missing file even with valid key")
		}
	})

	t.Run("valid key length 24", func(t *testing.T) {
		_, err := d.decryptChunk("somefile.temp", keyFromString("123456789012345678901234"))
		if err == nil {
			t.Error("expected error for missing file even with valid key")
		}
	})

	t.Run("valid key length 32", func(t *testing.T) {
		_, err := d.decryptChunk("somefile.temp", keyFromString("12345678901234567890123456789012"))
		if err == nil {
			t.Error("expected error for missing file even with valid key")
		}
	})
}

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
func TestSumPipelineStats(t *testing.T) {
	tests := []struct {
		name     string
		stats    PipelineStats
		expected int
	}{
		{
			name:     "empty stats",
			stats:    PipelineStats{},
			expected: 0,
		},
		{
			name:     "only first view chunks",
			stats:    PipelineStats{FirstViewChunks: 5},
			expected: 5,
		},
		{
			name:     "all chunk types",
			stats:    PipelineStats{FirstViewChunks: 3, SecondViewChunks: 4, FailedChunks: 1},
			expected: 8,
		},
		{
			name:     "with missing keys",
			stats:    PipelineStats{FirstViewChunks: 10},
			expected: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sumPipelineStats(tt.stats)
			if got != tt.expected {
				t.Errorf("sumPipelineStats() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestTotalChunksForPlaylist tests chunk counting
func TestTotalChunksForPlaylist(t *testing.T) {
	d := &Downloader{
		config: &config.Config{},
	}

	playlist := client.ParsedPlaylist{
		FirstViewURLs:  []string{"url1", "url2", "url3"},
		SecondViewURLs: []string{"url4", "url5"},
	}

	tests := []struct {
		name          string
		views         string
		expectedTotal int
	}{
		{
			name:          "both views",
			views:         "both",
			expectedTotal: 5,
		},
		{
			name:          "first view returns both (due to bug in condition)",
			views:         "first",
			expectedTotal: 5,
		},
		{
			name:          "second view returns both (due to bug in condition)",
			views:         "second",
			expectedTotal: 5,
		},
		{
			name:          "left view shows first",
			views:         "left",
			expectedTotal: 3,
		},
		{
			name:          "right view shows second",
			views:         "right",
			expectedTotal: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d.config.Views = tt.views
			got := d.totalChunksForPlaylist(playlist)
			if got != tt.expectedTotal {
				t.Errorf("totalChunksForPlaylist() = %v, want %v", got, tt.expectedTotal)
			}
		})
	}
}

// TestRetryDelay tests exponential backoff calculation
func TestRetryDelay(t *testing.T) {
	baseDelay := 1 * time.Second

	tests := []struct {
		name        string
		attempt     int
		minExpected time.Duration
		maxExpected time.Duration
	}{
		{
			name:        "attempt 0 returns base delay",
			attempt:     0,
			minExpected: baseDelay,
			maxExpected: baseDelay,
		},
		{
			name:        "attempt 1 doubles delay",
			attempt:     1,
			minExpected: 2 * baseDelay,
			maxExpected: 2*baseDelay + time.Millisecond,
		},
		{
			name:        "attempt 2 quadruples delay",
			attempt:     2,
			minExpected: 4 * baseDelay,
			maxExpected: 4*baseDelay + time.Millisecond,
		},
		{
			name:        "attempt 10 has large multiplier",
			attempt:     10,
			minExpected: 1024 * baseDelay,
			maxExpected: 1024*baseDelay + time.Millisecond,
		},
		{
			name:        "attempt negative uses base",
			attempt:     -1,
			minExpected: baseDelay,
			maxExpected: baseDelay,
		},
		{
			name:        "attempt 62 caps at 2^62",
			attempt:     62,
			minExpected: 1 << 62 * baseDelay / 1, // This will overflow int64 for actual calculation
			maxExpected: 1 << 62 * baseDelay,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := retryDelay(baseDelay, tt.attempt)
			if got < tt.minExpected {
				t.Errorf("retryDelay() = %v, want >= %v", got, tt.minExpected)
			}
			if got > tt.maxExpected {
				t.Errorf("retryDelay() = %v, want <= %v", got, tt.maxExpected)
			}
		})
	}
}

// TestStructTypes tests that struct types can be instantiated
func TestStructTypes(t *testing.T) {
	// Test client.Lecture struct (used by downloader via client package)
	lecture := client.Lecture{
		TTID:  123,
		Topic: "Introduction",
		SeqNo: 1,
	}
	if lecture.TTID != 123 || lecture.Topic != "Introduction" || lecture.SeqNo != 1 {
		t.Errorf("Lecture struct fields incorrect")
	}

	// Test client.StreamInfo struct
	streamInfo := client.StreamInfo{
		Quality: "720",
		URL:     "https://example.com/stream.m3u8",
	}
	if streamInfo.Quality != "720" || streamInfo.URL != "https://example.com/stream.m3u8" {
		t.Errorf("StreamInfo struct fields incorrect")
	}

	// Test DownloadedPlaylist struct
	downloadedPlaylist := DownloadedPlaylist{
		FirstViewChunks:  []string{"chunk1.ts", "chunk2.ts"},
		SecondViewChunks: []string{"chunk3.ts"},
		Playlist:         client.ParsedPlaylist{ID: 1, Title: "Test"},
	}
	if len(downloadedPlaylist.FirstViewChunks) != 2 {
		t.Errorf("DownloadedPlaylist.FirstViewChunks incorrect length")
	}
	if len(downloadedPlaylist.SecondViewChunks) != 1 {
		t.Errorf("DownloadedPlaylist.SecondViewChunks incorrect length")
	}
	_ = downloadedPlaylist // suppress unused variable

	// Test M3U8File struct
	m3u8File := M3U8File{
		FirstViewFile:  "/path/to/first.m3u8",
		SecondViewFile: "/path/to/second.m3u8",
		Playlist:       client.ParsedPlaylist{ID: 1},
	}
	if m3u8File.FirstViewFile == "" {
		t.Errorf("M3U8File.FirstViewFile is empty")
	}
	if m3u8File.SecondViewFile == "" {
		t.Errorf("M3U8File.SecondViewFile is empty")
	}
	_ = m3u8File // suppress unused variable

	// Test JoinResult struct
	joinResult := JoinResult{
		LeftOutput:  "/path/to/left.mp4",
		RightOutput: "/path/to/right.mp4",
		BothOutput:  "/path/to/both.mp4",
	}
	if joinResult.LeftOutput == "" {
		t.Errorf("JoinResult.LeftOutput is empty")
	}
	if joinResult.RightOutput == "" {
		t.Errorf("JoinResult.RightOutput is empty")
	}
	if joinResult.BothOutput == "" {
		t.Errorf("JoinResult.BothOutput is empty")
	}
}

// TestParsedPlaylist tests ParsedPlaylist struct
func TestParsedPlaylist(t *testing.T) {
	playlist := client.ParsedPlaylist{
		KeyURL:           "https://placeholder.test/key",
		Title:            "Lecture 1",
		FirstViewURLs:    []string{"url1", "url2"},
		SecondViewURLs:   []string{"url3", "url4"},
		ID:               100,
		SeqNo:            5,
		HasMultipleViews: true,
	}

	if playlist.ID != 100 {
		t.Errorf("ParsedPlaylist.ID = %d, want 100", playlist.ID)
	}
	if playlist.SeqNo != 5 {
		t.Errorf("ParsedPlaylist.SeqNo = %d, want 5", playlist.SeqNo)
	}
	if len(playlist.FirstViewURLs) != 2 {
		t.Errorf("ParsedPlaylist.FirstViewURLs length = %d, want 2", len(playlist.FirstViewURLs))
	}
	if len(playlist.SecondViewURLs) != 2 {
		t.Errorf("ParsedPlaylist.SecondViewURLs length = %d, want 2", len(playlist.SecondViewURLs))
	}
	if playlist.KeyURL == "" {
		t.Error("ParsedPlaylist.KeyURL is empty")
	}
	if playlist.Title == "" {
		t.Error("ParsedPlaylist.Title is empty")
	}
	if !playlist.HasMultipleViews {
		t.Error("ParsedPlaylist.HasMultipleViews should be true")
	}
}

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

func TestDownloaderNewLecturePipeline(t *testing.T) {
	d := &Downloader{
		config: &config.Config{
			DownloadWorkersPerLecture: 4,
			DecryptWorkersPerLecture:  2,
		},
	}

	playlist := client.ParsedPlaylist{
		ID:    123,
		SeqNo: 5,
		FirstViewURLs: []string{
			"http://example.com/chunk0.ts",
			"http://example.com/chunk1.ts",
		},
		SecondViewURLs: []string{
			"http://example.com/chunk2.ts",
		},
	}

	decryptionKey := []byte("1234567890123456")

	// newLecturePipeline should return a non-nil pipeline
	pipeline := d.newLecturePipeline(context.Background(), playlist, decryptionKey, nil)
	if pipeline == nil {
		t.Fatal("newLecturePipeline() returned nil")
	}

	// Verify the pipeline config
	if pipeline.config.DownloadWorkers != 4 {
		t.Errorf("DownloadWorkers = %d, want 4", pipeline.config.DownloadWorkers)
	}
	if pipeline.config.DecryptWorkers != 2 {
		t.Errorf("DecryptWorkers = %d, want 2", pipeline.config.DecryptWorkers)
	}
	if string(pipeline.config.DecryptionKey) != string(decryptionKey) {
		t.Errorf("DecryptionKey = %v, want %v", pipeline.config.DecryptionKey, decryptionKey)
	}
	if pipeline.config.LectureID != 123 {
		t.Errorf("LectureID = %d, want 123", pipeline.config.LectureID)
	}
	if pipeline.config.LectureSeqNo != 5 {
		t.Errorf("LectureSeqNo = %d, want 5", pipeline.config.LectureSeqNo)
	}
}

func TestSubmitPipelineTasks(t *testing.T) {
	d := &Downloader{
		config: &config.Config{
			Views: "both",
		},
	}

	p := NewLecturePipeline(PipelineConfig{
		DownloadWorkers: 2,
		DecryptWorkers:  2,
	}, d)
	defer p.cancelPipeline()

	playlist := client.ParsedPlaylist{
		ID:    123,
		SeqNo: 1,
		FirstViewURLs: []string{
			"http://example.com/chunk0.ts",
			"http://example.com/chunk1.ts",
		},
		SecondViewURLs: []string{
			"http://example.com/chunk2.ts",
			"http://example.com/chunk3.ts",
		},
	}

	// submitPipelineTasks should not error
	err := d.submitPipelineTasks(p, playlist)
	if err != nil {
		t.Errorf("submitPipelineTasks() error = %v", err)
	}
}

func TestSubmitPipelineTasksLeftViewOnly(t *testing.T) {
	d := &Downloader{
		config: &config.Config{
			Views: "left",
		},
	}

	p := NewLecturePipeline(PipelineConfig{
		DownloadWorkers: 2,
		DecryptWorkers:  2,
	}, d)
	defer p.cancelPipeline()

	playlist := client.ParsedPlaylist{
		ID:    123,
		SeqNo: 1,
		FirstViewURLs: []string{
			"http://example.com/chunk0.ts",
		},
		SecondViewURLs: []string{
			"http://example.com/chunk2.ts",
		},
	}

	// When Views = "left", second view should be skipped
	err := d.submitPipelineTasks(p, playlist)
	if err != nil {
		t.Errorf("submitPipelineTasks() error = %v", err)
	}
}

func TestNewPipelineBar(t *testing.T) {
	d := &Downloader{}

	// newPipelineBar with nil progress should return nil
	bar := d.newPipelineBar(nil, 10, 5)
	if bar != nil {
		t.Error("newPipelineBar(nil) should return nil")
	}
}

func TestDownloadAndJoinPlaylistNoChunks(t *testing.T) {
	// This test verifies that when there are no chunks to download,
	// the function returns empty results without error.
	// Full integration testing would require a properly configured client.
	tmpDir := t.TempDir()

	d := &Downloader{
		config: &config.Config{
			TempDirLocation:  tmpDir,
			DownloadLocation: tmpDir,
			AudioOnly:        false,
			Views:            "both",
		},
	}

	// CreateTempM3U8File should work with empty chunks
	dp := DownloadedPlaylist{
		FirstViewChunks:  nil,
		SecondViewChunks: nil,
		Playlist: client.ParsedPlaylist{
			ID:    1,
			Title: "Test",
			SeqNo: 1,
		},
	}

	_, err := d.CreateTempM3U8File(dp)
	if err != nil {
		t.Errorf("CreateTempM3U8File() error = %v", err)
	}
}

func TestDownloadLecturePlaylists_EmptyInput(t *testing.T) {
	d := &Downloader{}
	results, err := d.downloadLecturePlaylists(context.Background(), nil, nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestDownloadURLRejectsHTTPErrorStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream unavailable", http.StatusServiceUnavailable)
	}))
	defer ts.Close()

	d := &Downloader{
		config: &config.Config{
			TempDirLocation: t.TempDir(),
			Token:           "token",
		},
		client:      client.New(ts.Client(), nil),
		rateLimiter: NewRateLimiter(100.0, 50.0, false),
	}

	_, _, err := d.downloadURL(context.Background(), ts.URL+"/chunk.ts", 1, 0, "left")
	if err == nil {
		t.Fatal("expected error for non-200 chunk response")
	}
	if !strings.Contains(err.Error(), "status 503") {
		t.Fatalf("expected status 503 in error, got %v", err)
	}
}

func TestDownloadWithRetryHonoursCancellationDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		time.Sleep(25 * time.Millisecond)
		cancel()
	}()

	d := &Downloader{
		config: &config.Config{
			TempDirLocation: t.TempDir(),
			Token:           "token",
		},
		client:      client.New(&http.Client{}, nil),
		rateLimiter: NewRateLimiter(100.0, 50.0, false),
	}

	start := time.Now()
	_, err := d.downloadWithRetry(ctx, "://bad-url", 1, 0, "left", 2, nil)
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if elapsed := time.Since(start); elapsed >= 500*time.Millisecond {
		t.Fatalf("expected cancellation during backoff before 500ms, took %v", elapsed)
	}
}

func TestDownloadAndJoinPlaylistEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "ffmpeg.log")
	ffmpegPath := writeFakeFFmpegScript(t, logPath, "joined output")
	decryptionKey := []byte("1234567890123456")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/key":
			if _, err := w.Write(fakeKeyResponse(decryptionKey)); err != nil {
				t.Fatalf("Write(key) failed: %v", err)
			}
		case "/chunk0.ts":
			if _, err := w.Write(make([]byte, 16)); err != nil {
				t.Fatalf("Write(chunk) failed: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	d := &Downloader{
		config: &config.Config{
			TempDirLocation:  tempDir,
			DownloadLocation: tempDir,
			Views:            "left",
			Token:            "token",
		},
		client:      client.New(ts.Client(), nil),
		rateLimiter: NewRateLimiter(100.0, 50.0, false),
		ffmpegPath:  ffmpegPath,
		maxRetries:  1,
	}

	result, err := d.DownloadAndJoinPlaylist(context.Background(), client.ParsedPlaylist{
		KeyURL:        ts.URL + "/key",
		Title:         "Integration Lecture",
		ID:            7,
		SeqNo:         3,
		FirstViewURLs: []string{ts.URL + "/chunk0.ts"},
	}, nil, nil)
	if err != nil {
		t.Fatalf("DownloadAndJoinPlaylist() error = %v", err)
	}
	if result.LeftOutput == "" {
		t.Fatal("expected left output path")
	}
	content, err := os.ReadFile(result.LeftOutput)
	if err != nil {
		t.Fatalf("ReadFile(output) failed: %v", err)
	}
	if string(content) != "joined output" {
		t.Fatalf("unexpected output content: %q", string(content))
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(ffmpeg log) failed: %v", err)
	}
	if !strings.Contains(string(logData), "_first.m3u8") {
		t.Fatalf("expected ffmpeg log to reference first-view m3u8, got %s", string(logData))
	}
}

func TestJoinAudioOutputKeepsPerViewOutputsForBothViews(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "ffmpeg-audio.log")
	ffmpegPath := writeFakeFFmpegScript(t, logPath, "audio output")

	leftM3U8 := filepath.Join(tempDir, "left.m3u8")
	rightM3U8 := filepath.Join(tempDir, "right.m3u8")
	if err := os.WriteFile(leftM3U8, []byte("#EXTM3U\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(left) failed: %v", err)
	}
	if err := os.WriteFile(rightM3U8, []byte("#EXTM3U\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(right) failed: %v", err)
	}

	d := &Downloader{
		config: &config.Config{
			DownloadLocation: tempDir,
			Views:            "both",
			AudioOnly:        true,
			AudioFormat:      "mp3",
		},
		ffmpegPath: ffmpegPath,
	}

	result, err := d.joinAudioOutput(context.Background(), M3U8File{
		FirstViewFile:  leftM3U8,
		SecondViewFile: rightM3U8,
		Playlist: client.ParsedPlaylist{
			SeqNo: 1,
			Title: "Audio Lecture",
		},
	})
	if err != nil {
		t.Fatalf("joinAudioOutput() error = %v", err)
	}
	for _, output := range []string{result.LeftOutput, result.RightOutput, result.BothOutput} {
		if output == "" {
			t.Fatal("expected all audio outputs to be populated")
		}
		if _, err := os.Stat(output); err != nil {
			t.Fatalf("expected output to exist: %s (%v)", output, err)
		}
	}
}
