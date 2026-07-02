package downloader

import (
	"context"
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
