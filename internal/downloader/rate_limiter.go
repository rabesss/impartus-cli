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

type RateLimiter struct {
	downloadLimiter *rate.Limiter
	apiLimiter      *rate.Limiter
	jitterEnabled   bool
	rngMutex        sync.Mutex
}

func NewRateLimiter(downloadRPS, apiRPS float64, enableJitter bool) *RateLimiter {
	downloadBurst := int(downloadRPS * 2)
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

func NewRateLimiterFromConfig(cfg *config.Config) *RateLimiter {
	if cfg == nil {
		cfg = &config.Config{}
	}
	cfg.ApplyDefaults()
	return NewRateLimiter(cfg.RateLimit, cfg.APIRateLimit, cfg.EnableJitter)
}

func (rl *RateLimiter) WaitForDownload(ctx context.Context) error {
	if err := rl.downloadLimiter.Wait(ctx); err != nil {
		return err
	}
	if rl.jitterEnabled {
		rl.addJitter()
	}
	return nil
}

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
	n, err := rand.Int(rand.Reader, big.NewInt(400))
	if err != nil {
		return 0, err
	}
	return time.Duration(n.Int64()-200) * time.Millisecond, nil
}
