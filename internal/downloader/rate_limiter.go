package downloader

import (
	"context"
	"crypto/rand"
	"math/big"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/rabesss/impartus-cli/internal/config"
)

// RateLimiter provides token-bucket rate limiting for download and API requests with optional API jitter.
type RateLimiter struct {
	downloadLimiter *rate.Limiter
	apiLimiter      *rate.Limiter
	jitterEnabled   bool
	rngMutex        sync.Mutex
}

// NewRateLimiter creates a new RateLimiter with the specified requests-per-second for downloads and API calls.
func NewRateLimiter(downloadRPS, apiRPS float64, enableJitter bool) *RateLimiter {
	downloadBurst := int(downloadRPS * 10)
	if downloadBurst < 1 {
		downloadBurst = 1
	}

	apiBurst := int(apiRPS * 2)
	if apiBurst < 1 {
		apiBurst = 1
	}

	return &RateLimiter{
		downloadLimiter: rate.NewLimiter(rate.Limit(downloadRPS), downloadBurst),
		apiLimiter:      rate.NewLimiter(rate.Limit(apiRPS), apiBurst),
		jitterEnabled:   enableJitter,
	}
}

// NewRateLimiterFromConfig creates a RateLimiter using values from the application config.
func NewRateLimiterFromConfig(cfg *config.Config) *RateLimiter {
	if cfg == nil {
		cfg = &config.Config{}
	}
	cfg.ApplyDefaults()
	return NewRateLimiter(cfg.RateLimit, cfg.APIRateLimit, cfg.EnableJitter)
}

// WaitForDownload blocks until the download rate limiter allows the next request.
func (rl *RateLimiter) WaitForDownload(ctx context.Context) error {
	return rl.downloadLimiter.Wait(ctx)
}

// WaitForAPI blocks until the API rate limiter allows the next request.
func (rl *RateLimiter) WaitForAPI(ctx context.Context) error {
	if err := rl.apiLimiter.Wait(ctx); err != nil {
		return err
	}
	if rl.jitterEnabled {
		rl.addJitter()
	}
	return nil
}

func (rl *RateLimiter) addJitter() {
	rl.rngMutex.Lock()
	jitter, err := secureJitterDuration()
	rl.rngMutex.Unlock()
	if err != nil {
		return
	}
	if jitter > 0 {
		time.Sleep(jitter)
	}
}

func secureJitterDuration() (time.Duration, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(2))
	if err != nil {
		return 0, err
	}
	return time.Duration(n.Int64()) * time.Millisecond, nil
}
