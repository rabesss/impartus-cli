package server

import (
	"container/list"
	"sync"
	"time"
)

// rateLimiterEntry tracks login attempts for a given IP using a sliding window.
type rateLimiterEntry struct {
	timestamps []time.Time
}

const maxRateLimiterEntries = 10000

// loginRateLimiter provides per-IP rate limiting for login attempts.
type loginRateLimiter struct {
	mu       sync.Mutex
	entries  map[string]*rateLimiterEntry
	order    *list.List               // LRU order: front = most recent, back = oldest
	elements map[string]*list.Element // ip -> list element for O(1) access
	limit    int
	window   time.Duration
}

func newLoginRateLimiter(limit int, window time.Duration) *loginRateLimiter {
	return &loginRateLimiter{
		entries:  make(map[string]*rateLimiterEntry),
		order:    list.New(),
		elements: make(map[string]*list.Element),
		limit:    limit,
		window:   window,
	}
}

func (l *loginRateLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	entry, exists := l.entries[ip]
	if !exists {
		// Evict oldest entries if at capacity
		if len(l.entries) >= maxRateLimiterEntries {
			l.evictOldestUnsafe()
		}
		l.entries[ip] = &rateLimiterEntry{timestamps: []time.Time{now}}
		elem := l.order.PushFront(ip)
		l.elements[ip] = elem
		return true
	}

	// Move to front (most recently accessed)
	if elem, ok := l.elements[ip]; ok {
		l.order.MoveToFront(elem)
	}

	// Prune timestamps outside the window
	cutoff := now.Add(-l.window)
	pruned := entry.timestamps[:0]
	for _, t := range entry.timestamps {
		if t.After(cutoff) {
			pruned = append(pruned, t)
		}
	}
	entry.timestamps = pruned

	// If still at or over limit, deny
	if len(entry.timestamps) >= l.limit {
		return false
	}

	entry.timestamps = append(entry.timestamps, now)
	return true
}

// evictOldestUnsafe removes the least-recently-used entry from the rate limiter.
// Must be called with l.mu held.
func (l *loginRateLimiter) evictOldestUnsafe() {
	back := l.order.Back()
	if back == nil {
		return
	}
	ip, ok := back.Value.(string)
	if !ok {
		l.order.Remove(back)
		return
	}
	l.order.Remove(back)
	delete(l.elements, ip)
	delete(l.entries, ip)
}

// cleanup removes entries whose most recent timestamp is older than 2x the window.
// Must be called with l.mu held.
func (l *loginRateLimiter) cleanup() {
	cutoff := time.Now().Add(-2 * l.window)
	for ip, entry := range l.entries {
		if len(entry.timestamps) == 0 {
			if elem, ok := l.elements[ip]; ok {
				l.order.Remove(elem)
				delete(l.elements, ip)
			}
			delete(l.entries, ip)
			continue
		}
		lastTS := entry.timestamps[len(entry.timestamps)-1]
		if lastTS.Before(cutoff) {
			if elem, ok := l.elements[ip]; ok {
				l.order.Remove(elem)
				delete(l.elements, ip)
			}
			delete(l.entries, ip)
		}
	}
}

// startCleanup starts a background goroutine that periodically removes stale rate limiter entries.
// It returns a stop function that should be called on shutdown.
func (l *loginRateLimiter) startCleanup() func() {
	ticker := time.NewTicker(l.window)
	stop := make(chan struct{})
	done := make(chan struct{})
	var stopOnce sync.Once

	go func() {
		defer ticker.Stop()
		defer close(done)
		for {
			select {
			case <-ticker.C:
				l.mu.Lock()
				l.cleanup()
				l.mu.Unlock()
			case <-stop:
				return
			}
		}
	}()

	return func() {
		stopOnce.Do(func() {
			close(stop)
		})
		<-done
	}
}

func (l *loginRateLimiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if elem, ok := l.elements[ip]; ok {
		l.order.Remove(elem)
		delete(l.elements, ip)
	}
	delete(l.entries, ip)
}
