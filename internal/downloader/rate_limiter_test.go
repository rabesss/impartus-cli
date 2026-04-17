package downloader

import (
	"context"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/config"
)

func TestNewRateLimiter(t *testing.T) {
	tests := []struct {
		name         string
		downloadRPS  float64
		apiRPS       float64
		enableJitter bool
		wantBurst    int
	}{
		{"normal rates", 10.0, 5.0, false, 20},
		{"zero download rate", 0.0, 5.0, false, 1},
		{"negative download rate", -1.0, 5.0, false, 1},
		{"zero api rate", 10.0, 0.0, false, 20},
		{"with jitter", 10.0, 5.0, true, 20},
		{"very high rate", 100.0, 50.0, false, 200},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rl := NewRateLimiter(tt.downloadRPS, tt.apiRPS, tt.enableJitter)
			if rl == nil {
				t.Fatal("expected non-nil RateLimiter")
			}
			if rl.jitterEnabled != tt.enableJitter {
				t.Errorf("jitterEnabled = %v, want %v", rl.jitterEnabled, tt.enableJitter)
			}
			if rl.downloadLimiter == nil {
				t.Error("downloadLimiter should not be nil")
			}
			if rl.apiLimiter == nil {
				t.Error("apiLimiter should not be nil")
			}
		})
	}
}

func TestNewRateLimiterFromConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
	}{
		{"nil config", nil},
		{"empty config", &config.Config{}},
		{"full config", &config.Config{
			RateLimit:    10.0,
			APIRateLimit: 5.0,
			EnableJitter: true,
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rl := NewRateLimiterFromConfig(tt.cfg)
			if rl == nil {
				t.Fatal("expected non-nil RateLimiter")
			}
		})
	}
}

func TestRateLimiterWaitForDownload(t *testing.T) {
	rl := NewRateLimiter(100.0, 50.0, false)
	ctx := context.Background()

	err := rl.WaitForDownload(ctx)
	if err != nil {
		t.Errorf("WaitForDownload() error = %v", err)
	}
}

func TestRateLimiterWaitForAPI(t *testing.T) {
	rl := NewRateLimiter(100.0, 50.0, false)
	ctx := context.Background()

	err := rl.WaitForAPI(ctx)
	if err != nil {
		t.Errorf("WaitForAPI() error = %v", err)
	}
}

func TestRateLimiterCancel(t *testing.T) {
	rl := NewRateLimiter(0.001, 0.001, false) // Very slow rate - 1 per 1000 seconds
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// Due to burst allowance, this may not always timeout
	// Just verify it doesn't hang indefinitely
	err := rl.WaitForDownload(ctx)
	// Context deadline exceeded is expected for very slow limiters with small timeout
	if err != nil && err != context.DeadlineExceeded {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSecureJitterDuration(t *testing.T) {
	// Run multiple times to check distribution
	for i := 0; i < 100; i++ {
		jitter, err := secureJitterDuration()
		if err != nil {
			t.Errorf("secureJitterDuration() error = %v", err)
			continue
		}
		// Jitter should be between -200ms and +200ms
		if jitter < -200*time.Millisecond || jitter > 200*time.Millisecond {
			t.Errorf("secureJitterDuration() = %v, expected between -200ms and 200ms", jitter)
		}
	}
}

func TestGetAudioCodec(t *testing.T) {
	tests := []struct {
		format string
		want   string
	}{
		{"mp3", "libmp3lame"},
		{"m4a", "aac"},
		{"aac", "aac"},
		{"opus", "libopus"},
		{"unknown", "libmp3lame"}, // default case
		{"", "libmp3lame"},        // empty defaults to mp3
	}

	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			got := getAudioCodec(tt.format)
			if got != tt.want {
				t.Errorf("getAudioCodec(%q) = %v, want %v", tt.format, got, tt.want)
			}
		})
	}
}

func TestRateLimiterJitterEnabled(t *testing.T) {
	// Test with jitter enabled - the WaitFor functions should still work
	// even if jitter adds some delay
	rl := NewRateLimiter(100.0, 50.0, true)
	ctx := context.Background()

	// These should complete without error
	err := rl.WaitForDownload(ctx)
	if err != nil {
		t.Errorf("WaitForDownload() with jitter error = %v", err)
	}

	err = rl.WaitForAPI(ctx)
	if err != nil {
		t.Errorf("WaitForAPI() with jitter error = %v", err)
	}
}

func TestRateLimiterJitterWithCancel(t *testing.T) {
	// Test that context cancellation works even with jitter enabled
	rl := NewRateLimiter(0.001, 0.001, true) // Very slow rate with jitter
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	err := rl.WaitForDownload(ctx)
	// Context deadline exceeded is expected for very slow limiters
	if err != nil && err != context.DeadlineExceeded && err != context.Canceled {
		t.Errorf("unexpected error: %v", err)
	}
}
