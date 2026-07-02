package downloader

import (
	"context"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

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
			name:          "first normalizes to left (first view only)",
			views:         "first",
			expectedTotal: 3,
		},
		{
			name:          "second normalizes to right (second view only)",
			views:         "second",
			expectedTotal: 2,
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
			d.config.Views = config.NormalizeViews(tt.views)
			got := d.totalChunksForPlaylist(playlist)
			if got != tt.expectedTotal {
				t.Errorf("totalChunksForPlaylist() = %v, want %v", got, tt.expectedTotal)
			}
		})
	}
}

// TestRetryDelay tests exponential backoff calculation

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
