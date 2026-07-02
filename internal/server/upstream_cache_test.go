package server

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

// ============================================================================
// Token Cache Tests
// ============================================================================

// mockLoginCallCounter tracks how many times the mock login function is called.
type mockLoginCallCounter struct {
	mu    sync.Mutex
	calls int
}

func (m *mockLoginCallCounter) increment() {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()
}

func (m *mockLoginCallCounter) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// mockUpstreamLogin creates a mock login function that returns a fake client
// and token without contacting any real upstream server.
func mockUpstreamLogin(counter *mockLoginCallCounter) UpstreamLoginFunc {
	return func(ctx context.Context, cfg *config.Config) (*client.Client, *config.Config, error) {
		if counter != nil {
			counter.increment()
		}
		apiClient := client.New(nil, nil)
		cfg.Token = "mock-token-12345"
		return apiClient, cfg, nil
	}
}

// mockFailingLogin creates a mock login function that always returns an error.

// mockFailingLogin creates a mock login function that always returns an error.
func mockFailingLogin() UpstreamLoginFunc {
	return func(ctx context.Context, cfg *config.Config) (*client.Client, *config.Config, error) {
		return nil, nil, fmt.Errorf("mock: upstream login failed")
	}
}

// TestUpstreamCachePopulatedAfterFirstCall verifies that after the first call
// to getOrRefreshUpstreamClient, the cache is populated with a valid token,
// client, and expiry time. (VAL-CACHE-001)

// TestUpstreamCachePopulatedAfterFirstCall verifies that after the first call
// to getOrRefreshUpstreamClient, the cache is populated with a valid token,
// client, and expiry time. (VAL-CACHE-001)
func TestUpstreamCachePopulatedAfterFirstCall(t *testing.T) {
	counter := &mockLoginCallCounter{}
	s := NewAPIServerWithLogin("8080", validServerConfig(), mockUpstreamLogin(counter))

	// Before any call, cache should be nil
	s.upstreamCacheMu.RLock()
	beforeCache := s.upstreamCache
	s.upstreamCacheMu.RUnlock()
	if beforeCache != nil {
		t.Error("expected nil cache before first call")
	}

	// First call should populate cache
	apiClient, cfg, err := s.getOrRefreshUpstreamClient(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if apiClient == nil {
		t.Fatal("expected non-nil client")
	}
	if cfg.Token != "mock-token-12345" {
		t.Errorf("expected mock token, got %q", cfg.Token)
	}
	if counter.count() != 1 {
		t.Errorf("expected 1 login call, got %d", counter.count())
	}

	// After call, cache should be populated
	s.upstreamCacheMu.RLock()
	afterCache := s.upstreamCache
	s.upstreamCacheMu.RUnlock()
	if afterCache == nil {
		t.Fatal("expected non-nil cache after first call")
	}
	if afterCache.token != "mock-token-12345" {
		t.Errorf("expected non-empty token in cache, got %q", afterCache.token)
	}
	if afterCache.client == nil {
		t.Error("expected non-nil client in cache")
	}
	if afterCache.expiresAt.IsZero() {
		t.Error("expected non-zero expiresAt in cache")
	}
}

// TestUpstreamCacheReuseOnSubsequentCalls verifies that subsequent calls
// return the same cached client without calling the login function again. (VAL-CACHE-002)

// TestUpstreamCacheReuseOnSubsequentCalls verifies that subsequent calls
// return the same cached client without calling the login function again. (VAL-CACHE-002)
func TestUpstreamCacheReuseOnSubsequentCalls(t *testing.T) {
	counter := &mockLoginCallCounter{}
	s := NewAPIServerWithLogin("8080", validServerConfig(), mockUpstreamLogin(counter))

	// First call
	client1, cfg1, err1 := s.getOrRefreshUpstreamClient(context.Background())
	if err1 != nil {
		t.Fatalf("first call failed: %v", err1)
	}

	// Second call should return the same cached client (no new login)
	client2, cfg2, err2 := s.getOrRefreshUpstreamClient(context.Background())
	if err2 != nil {
		t.Fatalf("second call failed: %v", err2)
	}

	// Should be the same client instance (cached)
	if client1 != client2 {
		t.Error("expected same client instance on subsequent calls")
	}

	// Token should be the same
	if cfg1.Token != cfg2.Token {
		t.Errorf("expected same token, got %q and %q", cfg1.Token, cfg2.Token)
	}

	// Login should only have been called once (cache reused on second call)
	if counter.count() != 1 {
		t.Errorf("expected 1 login call (cache reused), got %d", counter.count())
	}
}

// TestUpstreamCacheExpiredTokenRefreshes verifies that when the cached token
// is expired, a new login is triggered and cache is updated. (VAL-CACHE-003)

// TestUpstreamCacheExpiredTokenRefreshes verifies that when the cached token
// is expired, a new login is triggered and cache is updated. (VAL-CACHE-003)
func TestUpstreamCacheExpiredTokenRefreshes(t *testing.T) {
	counter := &mockLoginCallCounter{}
	s := NewAPIServerWithLogin("8080", validServerConfig(), mockUpstreamLogin(counter))

	// First call to populate cache
	_, cfg1, err := s.getOrRefreshUpstreamClient(context.Background())
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	// Verify cfg1 has a non-empty token
	if cfg1.Token == "" {
		t.Error("expected non-empty token after first call")
	}

	// Verify login was called once
	if counter.count() != 1 {
		t.Errorf("expected 1 login call after first get, got %d", counter.count())
	}

	// Manually expire the cached token
	s.upstreamCacheMu.Lock()
	if s.upstreamCache != nil {
		s.upstreamCache.expiresAt = time.Now().Add(-1 * time.Hour) // Expired
	}
	s.upstreamCacheMu.Unlock()

	// Next call should trigger a refresh (new login)
	_, cfg2, err := s.getOrRefreshUpstreamClient(context.Background())
	if err != nil {
		t.Fatalf("call after expiry failed: %v", err)
	}

	// Token should still be valid (mock always returns same token, but login was re-invoked)
	if cfg2.Token == "" {
		t.Error("expected non-empty token after refresh")
	}

	// Login should have been called twice now (initial + refresh)
	if counter.count() != 2 {
		t.Errorf("expected 2 login calls (initial + refresh after expiry), got %d", counter.count())
	}

	// Verify cache was updated with new expiry
	s.upstreamCacheMu.RLock()
	if s.upstreamCache == nil {
		t.Fatal("expected non-nil cache after refresh")
	}
	if s.upstreamCache.expiresAt.Before(time.Now()) {
		t.Error("expected cache expiry to be in the future after refresh")
	}
	s.upstreamCacheMu.RUnlock()
}

// TestUpstreamCacheLoginFailureDoesNotPoisonCache verifies that if upstream login
// fails, the cache is not populated with a bad entry. (VAL-CACHE-004)

// TestUpstreamCacheLoginFailureDoesNotPoisonCache verifies that if upstream login
// fails, the cache is not populated with a bad entry. (VAL-CACHE-004)
func TestUpstreamCacheLoginFailureDoesNotPoisonCache(t *testing.T) {
	s := NewAPIServerWithLogin("8080", validServerConfig(), mockFailingLogin())

	// First call - will fail login
	_, _, err := s.getOrRefreshUpstreamClient(context.Background())
	if err == nil {
		t.Fatal("expected error from failing mock login")
	}
	if !strings.Contains(err.Error(), "mock: upstream login failed") {
		t.Errorf("expected mock login error, got: %v", err)
	}

	// Cache should not be populated after failed login
	s.upstreamCacheMu.RLock()
	cached := s.upstreamCache
	s.upstreamCacheMu.RUnlock()

	if cached != nil {
		t.Error("cache should be nil after failed login (no poisoned entry), got a non-nil cache entry")
	}
}

// TestUpstreamCacheConcurrentAccess verifies that multiple goroutines
// accessing the cache simultaneously get the same cached client. (VAL-CACHE-005)

// TestUpstreamCacheConcurrentAccess verifies that multiple goroutines
// accessing the cache simultaneously get the same cached client. (VAL-CACHE-005)
func TestUpstreamCacheConcurrentAccess(t *testing.T) {
	counter := &mockLoginCallCounter{}
	s := NewAPIServerWithLogin("8080", validServerConfig(), mockUpstreamLogin(counter))

	// First call to populate cache
	_, _, err := s.getOrRefreshUpstreamClient(context.Background())
	if err != nil {
		t.Fatalf("initial call failed: %v", err)
	}

	// Get the cached client
	s.upstreamCacheMu.RLock()
	expectedClient := s.upstreamCache.client
	expectedToken := s.upstreamCache.token
	s.upstreamCacheMu.RUnlock()

	var wg sync.WaitGroup
	numGoroutines := 10
	errChan := make(chan error, numGoroutines)

	// Launch multiple goroutines that all try to get the client
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client, cfg, err := s.getOrRefreshUpstreamClient(context.Background())
			if err != nil {
				errChan <- err
				return
			}
			// All should get the same client instance
			if client != expectedClient {
				errChan <- fmt.Errorf("got different client instance")
				return
			}
			// All should get the same token
			if cfg.Token != expectedToken {
				errChan <- fmt.Errorf("got different token: %q vs %q", cfg.Token, expectedToken)
				return
			}
		}()
	}

	wg.Wait()
	close(errChan)

	// Check for errors
	for err := range errChan {
		t.Errorf("concurrent access error: %v", err)
	}

	// Login should still only have been called once (all concurrent reads hit cache)
	if counter.count() != 1 {
		t.Errorf("expected 1 login call (all reads from cache), got %d", counter.count())
	}
}
