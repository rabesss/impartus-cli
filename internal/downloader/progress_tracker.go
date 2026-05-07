package downloader

import (
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

// SpeedSample records bytes downloaded at a specific point in time for speed estimation.
type SpeedSample struct {
	timestamp time.Time
	bytes     int64
}

// ProgressStats holds a snapshot of download progress including speed, ETA, and byte counts.
type ProgressStats struct {
	TotalLectures     int32
	CompletedLectures int32
	TotalChunks       int32
	CompletedChunks   int32
	TotalBytes        int64
	DownloadedBytes   int64
	Progress          float64
	Speed             float64
	ETA               time.Duration
	Elapsed           time.Duration
}

// ProgressTracker tracks download progress across lectures and chunks, estimating speed and ETA.
type ProgressTracker struct {
	totalLectures     int32
	completedLectures int32

	totalChunks     int32
	completedChunks int32

	totalBytes      int64
	downloadedBytes int64
	startTime       time.Time

	speedSamples   []SpeedSample
	speedMutex     sync.RWMutex
	sampleInterval time.Duration
	maxSamples     int

	statsBar     *mpb.Bar
	updateTicker *time.Ticker
	stopChan     chan struct{}
	stopOnce     sync.Once

	mu sync.RWMutex
}

// NewProgressTracker creates a new progress tracker with the given lecture and chunk totals.
func NewProgressTracker(totalLectures, totalChunks int, p *mpb.Progress) *ProgressTracker {
	lectures := clampIntToInt32(totalLectures)
	chunks := clampIntToInt32(totalChunks)

	pt := &ProgressTracker{
		totalLectures:  lectures,
		totalChunks:    chunks,
		startTime:      time.Now(),
		sampleInterval: 2 * time.Second,
		maxSamples:     10,
		speedSamples:   make([]SpeedSample, 0, 10),
		stopChan:       make(chan struct{}),
	}

	if p != nil {
		pt.statsBar = p.AddBar(100,
			mpb.PrependDecorators(decor.Name("Overall Progress ", decor.WCSyncWidth)),
			mpb.AppendDecorators(
				decor.Any(func(decor.Statistics) string { return pt.getStatusString() }, decor.WCSyncWidth),
			),
			mpb.BarPriority(0),
		)
	}

	pt.updateTicker = time.NewTicker(pt.sampleInterval)
	go pt.updateLoop()
	return pt
}

func clampIntToInt32(v int) int32 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	if v < math.MinInt32 {
		return math.MinInt32
	}
	return int32(v)
}

// ChunkCompleted records a completed chunk download and updates byte counters.
func ChunkCompleted(pt *ProgressTracker, bytesDownloaded int64) {
	if pt == nil {
		return
	}
	atomic.AddInt32(&pt.completedChunks, 1)
	atomic.AddInt64(&pt.downloadedBytes, bytesDownloaded)
	pt.updateTotalBytesEstimate()
}

// LectureCompleted increments the completed lecture counter.
func LectureCompleted(pt *ProgressTracker) {
	if pt == nil {
		return
	}
	atomic.AddInt32(&pt.completedLectures, 1)
}

func (pt *ProgressTracker) updateTotalBytesEstimate() {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	completed := atomic.LoadInt32(&pt.completedChunks)
	downloaded := atomic.LoadInt64(&pt.downloadedBytes)
	if completed > 0 {
		avgChunkSize := downloaded / int64(completed)
		pt.totalBytes = avgChunkSize * int64(pt.totalChunks)
	}
}

// GetCurrentSpeed returns the current download speed in MB/s based on recent samples.
func (pt *ProgressTracker) GetCurrentSpeed() float64 {
	pt.speedMutex.RLock()
	defer pt.speedMutex.RUnlock()

	if len(pt.speedSamples) < 2 {
		return 0
	}

	firstSample := pt.speedSamples[0]
	lastSample := pt.speedSamples[len(pt.speedSamples)-1]
	timeDiff := lastSample.timestamp.Sub(firstSample.timestamp).Seconds()
	if timeDiff <= 0 {
		return 0
	}

	bytesDiff := lastSample.bytes - firstSample.bytes
	if bytesDiff < 0 {
		return 0
	}

	bytesPerSecond := float64(bytesDiff) / timeDiff
	return bytesPerSecond / (1024 * 1024)
}

// GetETA returns the estimated time remaining for the download to complete.
func (pt *ProgressTracker) GetETA() time.Duration {
	pt.mu.RLock()
	totalBytes := pt.totalBytes
	pt.mu.RUnlock()

	downloadedBytes := atomic.LoadInt64(&pt.downloadedBytes)
	if totalBytes <= 0 || downloadedBytes >= totalBytes {
		return 0
	}

	speed := pt.GetCurrentSpeed()
	if speed <= 0 {
		return 0
	}

	remainingBytes := totalBytes - downloadedBytes
	remainingMB := float64(remainingBytes) / (1024 * 1024)
	secondsRemaining := remainingMB / speed
	eta := time.Duration(secondsRemaining * float64(time.Second))

	maxETA := 99*time.Hour + 59*time.Minute
	if eta > maxETA {
		eta = maxETA
	}
	return eta
}

// GetOverallProgress returns the overall download progress as a percentage (0–100).
func (pt *ProgressTracker) GetOverallProgress() float64 {
	pt.mu.RLock()
	totalBytes := pt.totalBytes
	pt.mu.RUnlock()

	downloadedBytes := atomic.LoadInt64(&pt.downloadedBytes)
	if totalBytes <= 0 {
		completed := atomic.LoadInt32(&pt.completedChunks)
		if pt.totalChunks <= 0 {
			return 0
		}
		return (float64(completed) / float64(pt.totalChunks)) * 100
	}

	if downloadedBytes >= totalBytes {
		return 100
	}

	progress := (float64(downloadedBytes) / float64(totalBytes)) * 100
	if progress > 100 {
		return 100
	}
	if progress < 0 {
		return 0
	}
	return progress
}

func (pt *ProgressTracker) getStatusString() string {
	progress := pt.GetOverallProgress()
	speed := pt.GetCurrentSpeed()
	eta := pt.GetETA()
	completedLectures := atomic.LoadInt32(&pt.completedLectures)

	speedStr := "-- MB/s"
	if speed > 0 {
		speedStr = fmt.Sprintf("%.1f MB/s", speed)
	}

	etaStr := "ETA --"
	if eta > 0 {
		etaStr = fmt.Sprintf("ETA %s", pt.formatDuration(eta))
	}

	return fmt.Sprintf("%.1f%% | %s | %s | Lectures: %d/%d", progress, speedStr, etaStr, completedLectures, pt.totalLectures)
}

func (pt *ProgressTracker) formatDuration(d time.Duration) string {
	return formatDurationValue(d)
}

func formatDurationValue(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

func (pt *ProgressTracker) updateLoop() {
	for {
		select {
		case <-pt.updateTicker.C:
			pt.updateSpeedSample()
			pt.updateDisplay()
		case <-pt.stopChan:
			return
		}
	}
}

func (pt *ProgressTracker) updateSpeedSample() {
	currentBytes := atomic.LoadInt64(&pt.downloadedBytes)
	currentTime := time.Now()

	pt.speedMutex.Lock()
	defer pt.speedMutex.Unlock()

	pt.speedSamples = append(pt.speedSamples, SpeedSample{timestamp: currentTime, bytes: currentBytes})
	if len(pt.speedSamples) > pt.maxSamples {
		pt.speedSamples = pt.speedSamples[1:]
	}
}

func (pt *ProgressTracker) updateDisplay() {
	if pt.statsBar == nil {
		return
	}
	pt.statsBar.SetCurrent(int64(pt.GetOverallProgress()))
}

// Stop halts the progress tracker's update loop and finalizes the display.
func (pt *ProgressTracker) Stop() {
	if pt == nil {
		return
	}
	pt.stopOnce.Do(func() {
		if pt.updateTicker != nil {
			pt.updateTicker.Stop()
		}
		close(pt.stopChan)
		if pt.statsBar != nil {
			pt.statsBar.SetCurrent(100)
			pt.statsBar.Abort(true)
		}
	})
}

// GetStats returns a snapshot of the current download progress statistics.
func (pt *ProgressTracker) GetStats() ProgressStats {
	if pt == nil {
		return ProgressStats{}
	}

	pt.mu.RLock()
	totalBytes := pt.totalBytes
	pt.mu.RUnlock()

	return ProgressStats{
		TotalLectures:     pt.totalLectures,
		CompletedLectures: atomic.LoadInt32(&pt.completedLectures),
		TotalChunks:       pt.totalChunks,
		CompletedChunks:   atomic.LoadInt32(&pt.completedChunks),
		TotalBytes:        totalBytes,
		DownloadedBytes:   atomic.LoadInt64(&pt.downloadedBytes),
		Progress:          pt.GetOverallProgress(),
		Speed:             pt.GetCurrentSpeed(),
		ETA:               pt.GetETA(),
		Elapsed:           time.Since(pt.startTime),
	}
}
