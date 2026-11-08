package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/retry"
	"github.com/rabesss/impartus-cli/internal/retry/retrytest"
)

const (
	retryTestToken    = "retry-test-secret-token"
	retryTestStream   = "/1280x720/master.m3u8"
	retryTestPlaylist = "#EXTM3U\n#EXTINF:1,\nsegment.ts\n"
)

// scriptedRetryServer serves the stream index and playlist. Each path answers
// with the queued status codes in order, then 200 (or the sticky status).
type scriptedRetryServer struct {
	mu       sync.Mutex
	statuses map[string][]int
	sticky   map[string]int
	requests map[string]int
	server   *httptest.Server
}

func newScriptedRetryServer(t *testing.T) *scriptedRetryServer {
	t.Helper()
	s := &scriptedRetryServer{statuses: map[string][]int{}, sticky: map[string]int{}, requests: map[string]int{}}
	s.server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.server.Close)
	return s
}

func (s *scriptedRetryServer) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests[r.URL.Path]++
	status := http.StatusOK
	if queue := s.statuses[r.URL.Path]; len(queue) > 0 {
		status, s.statuses[r.URL.Path] = queue[0], queue[1:]
	} else if code, ok := s.sticky[r.URL.Path]; ok {
		status = code
	}
	s.mu.Unlock()

	if status != http.StatusOK {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, "upstream unavailable token="+retryTestToken) //nolint:errcheck
		return
	}
	switch r.URL.Path {
	case "/fetchvideo":
		_, _ = io.WriteString(w, s.server.URL+retryTestStream+"\n") //nolint:errcheck
	case retryTestStream:
		_, _ = io.WriteString(w, retryTestPlaylist) //nolint:errcheck
	default:
		http.NotFound(w, r)
	}
}

func (s *scriptedRetryServer) count(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests[path]
}

func newRetryTestClient(s *scriptedRetryServer, clock *retrytest.Clock) *Client {
	c := New(s.server.Client(), nil)
	c.retryClock = clock
	c.retryJitter = retrytest.MaxJitter
	return c
}

func retryTestConfig(s *scriptedRetryServer, budget string) *config.Config {
	return &config.Config{BaseURL: s.server.URL, Token: retryTestToken, Quality: "720", RetryBudget: budget}
}

func TestGetPlaylistsRetriesTransientFailures(t *testing.T) {
	for _, path := range []string{"/fetchvideo", retryTestStream} {
		t.Run(path, func(t *testing.T) {
			s := newScriptedRetryServer(t)
			s.statuses[path] = []int{http.StatusBadGateway}
			clock := retrytest.NewClock()

			playlists, err := newRetryTestClient(s, clock).GetPlaylists(context.Background(), retryTestConfig(s, "3m"), Lectures{{TTID: 42, Topic: "Retry"}})
			if err != nil {
				t.Fatalf("GetPlaylists() error = %v", err)
			}
			if len(playlists) != 1 || len(playlists[0].FirstViewURLs) != 1 {
				t.Fatalf("GetPlaylists() = %+v, want one parsed playlist", playlists)
			}
			if got := s.count(path); got != 2 {
				t.Fatalf("%s requests = %d, want 2", path, got)
			}
			if got := clock.Sleeps(); len(got) != 1 || got[0] != time.Second {
				t.Fatalf("backoff sleeps = %v, want [1s]", got)
			}
		})
	}
}

func TestGetPlaylistsDoesNotRetryUnauthorized(t *testing.T) {
	for _, path := range []string{"/fetchvideo", retryTestStream} {
		t.Run(path, func(t *testing.T) {
			s := newScriptedRetryServer(t)
			s.sticky[path] = http.StatusUnauthorized
			clock := retrytest.NewClock()

			_, err := newRetryTestClient(s, clock).GetPlaylists(context.Background(), retryTestConfig(s, "3m"), Lectures{{TTID: 42, Topic: "Retry"}})
			var authErr *AuthenticationError
			if !errors.As(err, &authErr) {
				t.Fatalf("GetPlaylists() error = %v, want *AuthenticationError", err)
			}
			if got := s.count(path); got != 1 {
				t.Fatalf("%s requests = %d, want 1", path, got)
			}
			if got := clock.Sleeps(); len(got) != 0 {
				t.Fatalf("backoff sleeps = %v, want none", got)
			}
		})
	}
}

func TestGetPlaylistsStopsAtRetryBudget(t *testing.T) {
	for _, path := range []string{"/fetchvideo", retryTestStream} {
		t.Run(path, func(t *testing.T) {
			s := newScriptedRetryServer(t)
			s.sticky[path] = http.StatusServiceUnavailable
			clock := retrytest.NewClock()

			_, err := newRetryTestClient(s, clock).GetPlaylists(context.Background(), retryTestConfig(s, "1m"), Lectures{{TTID: 42, Topic: "Retry"}})
			if err == nil {
				t.Fatal("GetPlaylists() error = nil, want budget exhaustion")
			}
			if !strings.Contains(err.Error(), "status 503") || !strings.Contains(err.Error(), "attempts") {
				t.Fatalf("GetPlaylists() error = %q, want status 503 after attempts", err)
			}
			if strings.Contains(err.Error(), retryTestToken) {
				t.Fatalf("GetPlaylists() error leaked token: %q", err)
			}
			var statusErr *retry.StatusError
			if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusServiceUnavailable {
				t.Fatalf("GetPlaylists() error = %v, want *retry.StatusError 503", err)
			}
			// 1s+2s+4s+8s+16s = 31s; the next 30s step would pass 1m.
			if got := s.count(path); got != 6 {
				t.Fatalf("%s requests = %d, want 6 within a 1m budget", path, got)
			}
			if elapsed := clock.Elapsed(); elapsed > time.Minute {
				t.Fatalf("elapsed backoff = %v, want <= 1m", elapsed)
			}
		})
	}
}

func TestGetPlaylistsZeroRetryBudgetMakesOneAttempt(t *testing.T) {
	s := newScriptedRetryServer(t)
	s.sticky["/fetchvideo"] = http.StatusServiceUnavailable
	clock := retrytest.NewClock()

	_, err := newRetryTestClient(s, clock).GetPlaylists(context.Background(), retryTestConfig(s, "0s"), Lectures{{TTID: 42, Topic: "Retry"}})
	if err == nil || !strings.Contains(err.Error(), "status 503") {
		t.Fatalf("GetPlaylists() error = %v, want status 503", err)
	}
	if strings.Contains(err.Error(), "attempts") {
		t.Fatalf("GetPlaylists() error = %q, want no attempt count for a single attempt", err)
	}
	if got := s.count("/fetchvideo"); got != 1 {
		t.Fatalf("stream info requests = %d, want 1", got)
	}
}
