// Package retrytest provides a fake clock for retry policy tests.
package retrytest

import (
	"context"
	"sync"
	"time"
)

// Clock is a fake retry.Clock: Sleep advances Now instantly and records the
// requested delay. It is safe for concurrent use.
type Clock struct {
	mu     sync.Mutex
	now    time.Time
	sleeps []time.Duration
}

// NewClock returns a fake clock starting at a fixed instant.
func NewClock() *Clock {
	return &Clock{now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
}

// Now returns the fake current time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Sleep records d and advances the clock unless ctx is already done.
func (c *Clock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)
	return nil
}

// Advance moves the clock forward without recording a sleep.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// Sleeps returns a copy of the recorded delays.
func (c *Clock) Sleeps() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.sleeps...)
}

// Elapsed returns the total recorded sleep time.
func (c *Clock) Elapsed() time.Duration {
	var total time.Duration
	for _, d := range c.Sleeps() {
		total += d
	}
	return total
}

// MaxJitter is a deterministic jitter that always waits the full ceiling.
func MaxJitter(ceiling time.Duration) time.Duration { return ceiling }
