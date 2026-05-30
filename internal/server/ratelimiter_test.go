package server

import (
	"testing"
	"time"
)

func TestLoginRateLimiterAllowWithinAndOverLimit(t *testing.T) {
	rl := newLoginRateLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !rl.allow("1.2.3.4") {
			t.Fatalf("attempt %d within limit should be allowed", i+1)
		}
	}
	if rl.allow("1.2.3.4") {
		t.Fatal("attempt over the limit should be denied")
	}
}

func TestLoginRateLimiterTracksIPsIndependently(t *testing.T) {
	rl := newLoginRateLimiter(1, time.Minute)
	if !rl.allow("a") || !rl.allow("b") {
		t.Fatal("first attempt for each IP should be allowed")
	}
	if rl.allow("a") {
		t.Fatal("second attempt for IP a should be denied")
	}
}

func TestLoginRateLimiterWindowExpiry(t *testing.T) {
	rl := newLoginRateLimiter(1, 10*time.Millisecond)
	if !rl.allow("ip") {
		t.Fatal("first attempt should be allowed")
	}
	if rl.allow("ip") {
		t.Fatal("second attempt within window should be denied")
	}
	time.Sleep(20 * time.Millisecond)
	if !rl.allow("ip") {
		t.Fatal("attempt after window expiry should be allowed")
	}
}

func TestLoginRateLimiterReset(t *testing.T) {
	rl := newLoginRateLimiter(1, time.Minute)
	rl.allow("ip")
	if rl.allow("ip") {
		t.Fatal("expected denial before reset")
	}
	rl.reset("ip")
	if !rl.allow("ip") {
		t.Fatal("expected allowance after reset")
	}
}

func TestLoginRateLimiterEvictOldest(t *testing.T) {
	rl := newLoginRateLimiter(5, time.Minute)
	rl.allow("old")
	rl.allow("new")

	rl.mu.Lock()
	rl.evictOldestUnsafe()
	_, hasOld := rl.entries["old"]
	_, hasNew := rl.entries["new"]
	rl.mu.Unlock()

	if hasOld {
		t.Error("least-recently-used entry should have been evicted")
	}
	if !hasNew {
		t.Error("most-recent entry should remain")
	}
}

func TestLoginRateLimiterCleanupRemovesStaleEntries(t *testing.T) {
	rl := newLoginRateLimiter(5, 10*time.Millisecond)
	rl.allow("stale")
	time.Sleep(30 * time.Millisecond) // older than 2x window

	rl.mu.Lock()
	rl.cleanup()
	_, exists := rl.entries["stale"]
	rl.mu.Unlock()

	if exists {
		t.Error("entry older than 2x window should be cleaned up")
	}
}

func TestLoginRateLimiterStartCleanupStopIsIdempotent(t *testing.T) {
	rl := newLoginRateLimiter(5, 5*time.Millisecond)
	stop := rl.startCleanup()
	time.Sleep(12 * time.Millisecond)
	stop()
	stop()
}
