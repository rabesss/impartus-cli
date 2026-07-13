package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/config"
)

type fakeHealthClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeHealthClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeHealthClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func testHealthResponse(status string) healthResponse {
	return healthResponse{
		Status:   status,
		Config:   configCheckResult{Status: "ok"},
		Upstream: statusCheckResult{Status: "reachable"},
		FFmpeg:   statusCheckResult{Status: "available"},
	}
}

func serveHealth(t *testing.T, s *APIServer, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
	s.router.ServeHTTP(rec, req)
	return rec
}

func TestLivenessIsDependencyFreeAndDoesNotTouchReadinessCache(t *testing.T) {
	s := newAPIServer(nil)
	s.cfg = nil
	s.readinessProbe = func(context.Context) healthResponse {
		panic("liveness invoked readiness probe")
	}
	wantCached := testHealthResponse("degraded")
	wantExpiry := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	s.readinessCache = readinessCache{
		response:  wantCached,
		expiresAt: wantExpiry,
		valid:     true,
	}

	rec := serveHealth(t, s, "/api/v1/health/live")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var envelope struct {
		Data livenessResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode liveness response: %v", err)
	}
	if envelope.Data.Status != "ok" {
		t.Fatalf("expected liveness status ok, got %q", envelope.Data.Status)
	}

	s.readinessCache.mu.Lock()
	defer s.readinessCache.mu.Unlock()
	if !s.readinessCache.valid || s.readinessCache.response != wantCached || !s.readinessCache.expiresAt.Equal(wantExpiry) {
		t.Fatal("liveness mutated the readiness cache")
	}
}

func TestHealthReadyAndCompatibilityAliasReturnSameResponse(t *testing.T) {
	s := newAPIServer(validServerConfig())
	var probes atomic.Int32
	s.readinessProbe = func(context.Context) healthResponse {
		probes.Add(1)
		return testHealthResponse("ok")
	}

	ready := serveHealth(t, s, "/api/v1/health/ready")
	compat := serveHealth(t, s, "/api/v1/health")
	if ready.Code != http.StatusOK || compat.Code != http.StatusOK {
		t.Fatalf("expected both routes to return 200, got ready=%d compatibility=%d", ready.Code, compat.Code)
	}
	if ready.Body.String() != compat.Body.String() {
		t.Fatalf("ready and compatibility responses differ:\nready: %s\ncompat: %s", ready.Body.String(), compat.Body.String())
	}
	if got := probes.Load(); got != 1 {
		t.Fatalf("expected compatibility alias to share readiness cache, got %d probes", got)
	}
}

func TestHealthCacheExpiryAndDegradedResult(t *testing.T) {
	clock := &fakeHealthClock{now: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)}
	s := newAPIServer(validServerConfig())
	s.healthNow = clock.Now
	var probes atomic.Int32
	s.readinessProbe = func(context.Context) healthResponse {
		probes.Add(1)
		return testHealthResponse("degraded")
	}

	first := serveHealth(t, s, "/api/v1/health/ready")
	if first.Code != http.StatusOK {
		t.Fatalf("degraded readiness must retain HTTP 200, got %d", first.Code)
	}
	clock.Advance(readinessCacheTTL - time.Nanosecond)
	second := serveHealth(t, s, "/api/v1/health/ready")
	if first.Body.String() != second.Body.String() {
		t.Fatal("cached degraded response changed before expiry")
	}
	if got := probes.Load(); got != 1 {
		t.Fatalf("expected one probe before expiry, got %d", got)
	}

	clock.Advance(time.Nanosecond)
	serveHealth(t, s, "/api/v1/health/ready")
	if got := probes.Load(); got != 2 {
		t.Fatalf("expected refresh at expiry, got %d probes", got)
	}
}

func TestReadinessConcurrentMissCollapsesToOneProbe(t *testing.T) {
	var upstreamProbes atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamProbes.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	s := newAPIServer(&config.Config{
		Username:         "user",
		Password:         "pass",
		BaseURL:          upstream.URL,
		DownloadLocation: "./downloads",
	})
	s.upstreamCacheMu.Lock()
	s.upstreamCache = &upstreamCacheEntry{token: "test-token", expiresAt: time.Now().Add(time.Hour)}
	s.upstreamCacheMu.Unlock()

	const requests = 100
	start := make(chan struct{})
	bodies := make([]string, requests)
	codes := make([]int, requests)
	var wg sync.WaitGroup
	wg.Add(requests)
	for i := range requests {
		go func() {
			defer wg.Done()
			<-start
			rec := serveHealth(t, s, "/api/v1/health/ready")
			codes[i] = rec.Code
			bodies[i] = rec.Body.String()
		}()
	}
	close(start)
	wg.Wait()

	if got := upstreamProbes.Load(); got != 1 {
		t.Fatalf("expected exactly one upstream probe, got %d", got)
	}
	for i := range requests {
		if codes[i] != http.StatusOK {
			t.Fatalf("request %d returned %d", i, codes[i])
		}
		if bodies[i] != bodies[0] {
			t.Fatalf("request %d returned a different readiness response", i)
		}
	}
}

func TestHealthCacheCanceledWaiterDoesNotCancelRefresh(t *testing.T) {
	s := newAPIServer(validServerConfig())
	started := make(chan struct{})
	release := make(chan struct{})
	var probes atomic.Int32
	s.readinessProbe = func(ctx context.Context) healthResponse {
		probes.Add(1)
		close(started)
		select {
		case <-release:
			return testHealthResponse("ok")
		case <-ctx.Done():
			return testHealthResponse("degraded")
		}
	}

	ownerDone := make(chan bool, 1)
	go func() {
		_, ok := s.cachedReadiness(context.Background())
		ownerDone <- ok
	}()
	<-started

	waiterCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ok := s.cachedReadiness(waiterCtx); ok {
		t.Fatal("expected canceled waiter to stop waiting")
	}
	if got := probes.Load(); got != 1 {
		t.Fatalf("canceled waiter started another probe; got %d", got)
	}
	close(release)
	select {
	case ok := <-ownerDone:
		if !ok {
			t.Fatal("active refresher was canceled by a waiter")
		}
	case <-time.After(time.Second):
		t.Fatal("active refresher did not finish")
	}
}

func TestProbeUpstreamHTTP_Cancel(t *testing.T) {
	started := make(chan struct{})
	requestCanceled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(requestCanceled)
	}))
	defer upstream.Close()

	s := newAPIServer(&config.Config{
		Username:         "user",
		Password:         "pass",
		BaseURL:          upstream.URL,
		DownloadLocation: "./downloads",
	})
	s.upstreamCacheMu.Lock()
	s.upstreamCache = &upstreamCacheEntry{token: "test-token", expiresAt: time.Now().Add(time.Hour)}
	s.upstreamCacheMu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	probeDone := make(chan struct{})
	go func() {
		s.probeUpstreamHTTP(ctx)
		close(probeDone)
	}()
	<-started
	cancel()

	select {
	case <-probeDone:
	case <-time.After(time.Second):
		t.Fatal("HTTP probe did not stop after context cancellation")
	}
	select {
	case <-requestCanceled:
	case <-time.After(time.Second):
		t.Fatal("upstream request did not observe context cancellation")
	}
}
