package downloader

import (
	"testing"
	"time"
)

func TestClampIntToInt32(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		expected int32
	}{
		{
			name:     "positive value within range",
			input:    100,
			expected: 100,
		},
		{
			name:     "zero value",
			input:    0,
			expected: 0,
		},
		{
			name:     "negative value within range",
			input:    -100,
			expected: -100,
		},
		{
			name:     "max int32 value",
			input:    int(mathMaxInt32),
			expected: mathMaxInt32,
		},
		{
			name:     "min int32 value",
			input:    int(mathMinInt32),
			expected: mathMinInt32,
		},
		{
			name:     "value above max int32",
			input:    int(mathMaxInt32) + 1,
			expected: mathMaxInt32,
		},
		{
			name:     "value below min int32",
			input:    int(mathMinInt32) - 1,
			expected: mathMinInt32,
		},
		{
			name:     "large positive value",
			input:    1000000,
			expected: 1000000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := clampIntToInt32(tt.input)
			if result != tt.expected {
				t.Errorf("clampIntToInt32(%d) = %d; want %d", tt.input, result, tt.expected)
			}
		})
	}
}

const mathMaxInt32 int32 = 2147483647
const mathMinInt32 int32 = -2147483648

func TestNewProgressTracker(t *testing.T) {
	tests := []struct {
		name               string
		totalLectures      int
		totalChunks        int
		progress           any // nil or some value
		expectNotNil       bool
		expectZeroLectures int32
		expectZeroChunks   int32
	}{
		{
			name:          "nil progress with valid inputs",
			totalLectures: 10,
			totalChunks:   5,
			progress:      nil,
			expectNotNil:  true,
		},
		{
			name:          "zero inputs with nil progress",
			totalLectures: 0,
			totalChunks:   0,
			progress:      nil,
			expectNotNil:  true,
		},
		{
			name:          "negative inputs with nil progress",
			totalLectures: -1,
			totalChunks:   -1,
			progress:      nil,
			expectNotNil:  true,
		},
		{
			name:          "large inputs with nil progress",
			totalLectures: 1000000,
			totalChunks:   500000,
			progress:      nil,
			expectNotNil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call with nil progress to avoid mpb dependency
			pt := NewProgressTracker(tt.totalLectures, tt.totalChunks, nil)

			if tt.expectNotNil && pt == nil {
				t.Fatal("NewProgressTracker returned nil")
			}

			if !tt.expectNotNil && pt != nil {
				t.Error("NewProgressTracker returned non-nil")
			}

			if pt != nil {
				// Verify the tracker is initialized
				if pt.startTime.IsZero() {
					t.Error("startTime should be initialized")
				}
				if pt.stopChan == nil {
					t.Error("stopChan should be initialized")
				}
				if pt.speedSamples == nil {
					t.Error("speedSamples should be initialized")
				}
				// statsBar should be nil when progress is nil
				if tt.progress == nil && pt.statsBar != nil {
					t.Error("statsBar should be nil when progress is nil")
				}
			}
		})
	}
}

func TestProgressTrackerGetStats(t *testing.T) {
	pt := NewProgressTracker(10, 5, nil)
	defer pt.Stop()

	stats := pt.GetStats()

	if stats.TotalLectures != 10 {
		t.Errorf("TotalLectures = %d; want 10", stats.TotalLectures)
	}
	if stats.TotalChunks != 5 {
		t.Errorf("TotalChunks = %d; want 5", stats.TotalChunks)
	}
	if stats.Elapsed == 0 {
		t.Error("Elapsed should be non-zero")
	}

	if stats.Speed < 0 {
		t.Errorf("speed should be non-negative, got %f", stats.Speed)
	}

	if stats.ETA < 0 {
		t.Errorf("eta should be non-negative, got %v", stats.ETA)
	}

	if stats.Elapsed <= 0 {
		t.Errorf("elapsed should be positive, got %v", stats.Elapsed)
	}
}

func TestProgressTrackerGetStatsNilReceiver(t *testing.T) {
	var pt *ProgressTracker
	stats := pt.GetStats()
	// Zero-value struct should have all fields at defaults
	if stats.TotalLectures != 0 || stats.TotalChunks != 0 {
		t.Error("GetStats() should return zero-value for nil receiver")
	}
}

func TestChunkCompleted(t *testing.T) {
	pt := NewProgressTracker(10, 5, nil)
	defer pt.Stop()

	stats := pt.GetStats()
	if stats.CompletedChunks != 0 {
		t.Errorf("initial completedChunks = %d; want 0", stats.CompletedChunks)
	}

	ChunkCompleted(pt, 1024*1024)

	stats = pt.GetStats()
	if stats.CompletedChunks != 1 {
		t.Errorf("completedChunks after one call = %d; want 1", stats.CompletedChunks)
	}
	if stats.DownloadedBytes != 1024*1024 {
		t.Errorf("downloadedBytes = %d; want 1048576", stats.DownloadedBytes)
	}
}

func TestChunkCompletedNilReceiver(t *testing.T) {
	// Should not panic with nil receiver
	ChunkCompleted(nil, 1024)
}

func TestLectureCompleted(t *testing.T) {
	pt := NewProgressTracker(10, 5, nil)
	defer pt.Stop()

	stats := pt.GetStats()
	if stats.CompletedLectures != 0 {
		t.Errorf("initial completedLectures = %d; want 0", stats.CompletedLectures)
	}

	LectureCompleted(pt)

	stats = pt.GetStats()
	if stats.CompletedLectures != 1 {
		t.Errorf("completedLectures after one call = %d; want 1", stats.CompletedLectures)
	}
}

func TestLectureCompletedNilReceiver(t *testing.T) {
	// Should not panic with nil receiver
	LectureCompleted(nil)
}

func TestProgressTrackerGetOverallProgress(t *testing.T) {
	tests := []struct {
		name          string
		totalLectures int
		totalChunks   int
		setupFunc     func(*ProgressTracker)
		expectedMin   float64
		expectedMax   float64
	}{
		{
			name:          "zero chunks returns 0",
			totalLectures: 10,
			totalChunks:   0,
			setupFunc:     func(pt *ProgressTracker) {},
			expectedMin:   0,
			expectedMax:   0,
		},
		{
			name:          "no downloads returns 0",
			totalLectures: 10,
			totalChunks:   5,
			setupFunc:     func(pt *ProgressTracker) {},
			expectedMin:   0,
			expectedMax:   0,
		},
		{
			name:          "after chunk download",
			totalLectures: 10,
			totalChunks:   5,
			setupFunc: func(pt *ProgressTracker) {
				ChunkCompleted(pt, 1000)
			},
			expectedMin: 0,
			expectedMax: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pt := NewProgressTracker(tt.totalLectures, tt.totalChunks, nil)
			defer pt.Stop()

			tt.setupFunc(pt)

			progress := pt.GetOverallProgress()
			if progress < tt.expectedMin || progress > tt.expectedMax {
				t.Errorf("GetOverallProgress() = %f; want between %f and %f", progress, tt.expectedMin, tt.expectedMax)
			}
		})
	}
}

func TestProgressTrackerGetETA(t *testing.T) {
	pt := NewProgressTracker(10, 5, nil)
	defer pt.Stop()

	// Without any downloads, ETA should be 0
	eta := pt.GetETA()
	if eta != 0 {
		t.Errorf("GetETA() with no downloads = %v; want 0", eta)
	}

	// With some progress but no speed samples, ETA should be 0
	ChunkCompleted(pt, 1000)
	eta = pt.GetETA()
	if eta != 0 {
		t.Errorf("GetETA() with no speed samples = %v; want 0", eta)
	}
}

func TestProgressTrackerGetCurrentSpeed(t *testing.T) {
	pt := NewProgressTracker(10, 5, nil)
	defer pt.Stop()

	// Without speed samples, speed should be 0
	speed := pt.GetCurrentSpeed()
	if speed != 0 {
		t.Errorf("GetCurrentSpeed() with no samples = %f; want 0", speed)
	}

	// Add some progress and wait a bit to get speed samples
	ChunkCompleted(pt, 1024*1024) // 1MB
	time.Sleep(100 * time.Millisecond)
	ChunkCompleted(pt, 1024*1024) // Another 1MB

	// Get speed after some time has passed to allow sample update
	// The update loop runs every 2 seconds by default, so speed may still be 0
	speed = pt.GetCurrentSpeed()
	if speed < 0 {
		t.Errorf("GetCurrentSpeed() should not be negative, got %f", speed)
	}
}

func TestProgressTrackerStop(t *testing.T) {
	pt := NewProgressTracker(10, 5, nil)

	// Should not panic when stopping
	pt.Stop()

	// Stop() is not idempotent - calling twice would panic
	// so we intentionally do not call it again
}

func TestProgressTrackerStopNilReceiver(t *testing.T) {
	var pt *ProgressTracker

	// Should not panic with nil receiver
	pt.Stop()
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Duration
		expected string
	}{
		{
			name:     "zero duration",
			input:    0,
			expected: "0s",
		},
		{
			name:     "negative duration",
			input:    -1 * time.Second,
			expected: "0s",
		},
		{
			name:     "seconds only",
			input:    30 * time.Second,
			expected: "30s",
		},
		{
			name:     "minutes and seconds",
			input:    2*time.Minute + 30*time.Second,
			expected: "2m 30s",
		},
		{
			name:     "hours minutes and seconds",
			input:    1*time.Hour + 30*time.Minute + 45*time.Second,
			expected: "1h 30m",
		},
		{
			name:     "one minute exactly",
			input:    1 * time.Minute,
			expected: "1m 0s",
		},
		{
			name:     "one hour exactly",
			input:    1 * time.Hour,
			expected: "1h 0m",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatDuration(tt.input)
			if result != tt.expected {
				t.Errorf("FormatDuration(%v) = %q; want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestProgressTrackerGetStatusString(t *testing.T) {
	pt := NewProgressTracker(5, 10, nil)
	defer pt.Stop()

	// getStatusString should not panic
	status := pt.getStatusString()
	if status == "" {
		t.Error("getStatusString() returned empty string")
	}

	// The status string should contain certain substrings
	// Check for progress percentage format
	if len(status) < 5 {
		t.Errorf("getStatusString() too short: %q", status)
	}
}

func TestProgressTrackerGetStatusStringWithProgress(t *testing.T) {
	pt := NewProgressTracker(5, 10, nil)
	defer pt.Stop()

	// Add some chunks to have progress
	ChunkCompleted(pt, 1024*1024) // 1MB
	ChunkCompleted(pt, 1024*1024) // 1MB
	LectureCompleted(pt)

	// getStatusString should not panic and should return a non-empty string
	status := pt.getStatusString()
	if status == "" {
		t.Error("getStatusString() returned empty string after progress")
	}
}

func TestProgressTrackerUpdateSpeedSample(t *testing.T) {
	pt := NewProgressTracker(5, 10, nil)
	defer pt.Stop()

	// updateSpeedSample should not panic
	pt.updateSpeedSample()

	// Add some data and call updateSpeedSample again
	ChunkCompleted(pt, 1024*1024) // 1MB
	pt.updateSpeedSample()

	// The speed samples should now have data
	pt.speedMutex.RLock()
	sampleCount := len(pt.speedSamples)
	pt.speedMutex.RUnlock()

	if sampleCount < 1 {
		t.Errorf("expected at least 1 speed sample after update, got %d", sampleCount)
	}
}

func TestProgressTrackerUpdateSpeedSampleMultiple(t *testing.T) {
	pt := NewProgressTracker(5, 10, nil)
	defer pt.Stop()

	// Add multiple samples
	for i := 0; i < 15; i++ {
		ChunkCompleted(pt, 100*1024) // 100KB per chunk
		pt.updateSpeedSample()
	}

	// maxSamples is 10, so we should have at most 10 samples
	pt.speedMutex.RLock()
	sampleCount := len(pt.speedSamples)
	pt.speedMutex.RUnlock()

	if sampleCount > pt.maxSamples {
		t.Errorf("expected at most %d samples, got %d", pt.maxSamples, sampleCount)
	}
}

func TestProgressTrackerUpdateDisplay(t *testing.T) {
	pt := NewProgressTracker(5, 10, nil)

	// updateDisplay should not panic even with nil statsBar
	// (it checks for nil statsBar internally)
	pt.updateDisplay()

	// Stop the tracker
	pt.Stop()
}

func TestProgressTrackerGetStatusStringSpeed(t *testing.T) {
	pt := NewProgressTracker(5, 10, nil)
	defer pt.Stop()

	// Add some chunks to get non-zero speed
	ChunkCompleted(pt, 1024*1024) // 1MB
	pt.updateSpeedSample()
	time.Sleep(10 * time.Millisecond)
	ChunkCompleted(pt, 1024*1024) // 1MB more
	pt.updateSpeedSample()

	// Get status string - should show speed information
	status := pt.getStatusString()
	if status == "" {
		t.Error("getStatusString() returned empty")
	}

	// Status should contain speed info (format: "X.X MB/s" or "-- MB/s")
	// We can't predict exact value but it should be formatted
	if len(status) < 5 {
		t.Errorf("getStatusString() result too short: %q", status)
	}
}

func TestProgressTrackerFormatDuration(t *testing.T) {
	pt := NewProgressTracker(1, 1, nil)
	defer pt.Stop()

	// formatDuration is a thin wrapper around FormatDuration
	// Test that it delegates correctly
	result := pt.formatDuration(30 * time.Second)
	if result != "30s" {
		t.Errorf("formatDuration(30s) = %q, want %q", result, "30s")
	}

	result = pt.formatDuration(2*time.Minute + 30*time.Second)
	if result != "2m 30s" {
		t.Errorf("formatDuration(2m30s) = %q, want %q", result, "2m 30s")
	}

	result = pt.formatDuration(1*time.Hour + 15*time.Minute)
	if result != "1h 15m" {
		t.Errorf("formatDuration(1h15m) = %q, want %q", result, "1h 15m")
	}
}

// Test ProgressTracker with various state combinations
func TestProgressTrackerStateCombinations(t *testing.T) {
	tests := []struct {
		name        string
		lectures    int
		chunks      int
		downloaded  int64
		completedCh int32
	}{
		{
			name:        "normal download in progress",
			lectures:    10,
			chunks:      20,
			downloaded:  5 * 1024 * 1024,
			completedCh: 5,
		},
		{
			name:        "no chunks",
			lectures:    10,
			chunks:      0,
			downloaded:  0,
			completedCh: 0,
		},
		{
			name:        "all chunks completed",
			lectures:    5,
			chunks:      10,
			downloaded:  100 * 1024 * 1024,
			completedCh: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pt := NewProgressTracker(tt.lectures, tt.chunks, nil)
			defer pt.Stop()

			// Simulate downloading
			for i := int32(0); i < tt.completedCh; i++ {
				ChunkCompleted(pt, tt.downloaded/int64(tt.completedCh))
			}

			// Verify GetStats works
			stats := pt.GetStats()
			_ = stats

			// Verify GetOverallProgress doesn't panic
			progress := pt.GetOverallProgress()
			if progress < 0 || progress > 100 {
				t.Errorf("GetOverallProgress() out of range: %f", progress)
			}

			// Verify GetETA doesn't panic
			pt.GetETA()

			// Verify GetCurrentSpeed doesn't panic
			pt.GetCurrentSpeed()
		})
	}
}
