package downloader

import (
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/config"
)

func BenchmarkRateLimiterWait(b *testing.B) {
	rl := NewRateLimiter(100, 1, true)
	ctx := b.Context()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := rl.WaitForDownload(ctx); err != nil {
			b.Fatalf("WaitForDownload() failed: %v", err)
		}
	}
}

func BenchmarkRateLimiterFromConfig(b *testing.B) {
	cfg := &config.Config{RateLimit: 50, APIRateLimit: 2, EnableJitter: true}
	for i := 0; i < b.N; i++ {
		_ = NewRateLimiterFromConfig(cfg)
	}
}

func BenchmarkRateLimiterConcurrentWait(b *testing.B) {
	rl := NewRateLimiter(1000, 10, true)
	ctx := b.Context()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := rl.WaitForDownload(ctx); err != nil {
				b.Fatalf("WaitForDownload() failed: %v", err)
			}
		}
	})
}

func TestRateLimiterConcurrency(t *testing.T) {
	rl := NewRateLimiter(100, 5, false)
	ctx := t.Context()

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			if err := rl.WaitForDownload(ctx); err != nil {
				t.Errorf("WaitForDownload() failed: %v", err)
			}
			done <- true
		}()
	}

	timeout := time.After(5 * time.Second)
	for i := 0; i < 10; i++ {
		select {
		case <-done:
		case <-timeout:
			t.Fatal("timeout waiting for concurrent rate limiter calls")
		}
	}
}

func TestRateLimiterFromConfig(t *testing.T) {
	cfg := &config.Config{RateLimit: 10, APIRateLimit: 2, EnableJitter: true}
	rl := NewRateLimiterFromConfig(cfg)
	if rl == nil {
		t.Fatal("expected non-nil rate limiter")
	}

	cfgNil := (*config.Config)(nil)
	rl2 := NewRateLimiterFromConfig(cfgNil)
	if rl2 == nil {
		t.Fatal("expected non-nil rate limiter with nil config")
	}
}
