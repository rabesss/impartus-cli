package downloader

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/retry"
	"github.com/rabesss/impartus-cli/internal/retry/retrytest"
)

// retryScript answers each path with its queued statuses in order, then with
// the sticky status (default 200). Retry-After is sent with every failure when
// set.
type retryScript struct {
	mu         sync.Mutex
	queued     map[string][]int
	sticky     map[string]int
	retryAfter string
	requests   map[string]int
}

func newRetryScript() *retryScript {
	return &retryScript{queued: map[string][]int{}, sticky: map[string]int{}, requests: map[string]int{}}
}

func (s *retryScript) handler(key []byte) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.requests[r.URL.Path]++
		status := http.StatusOK
		if queue := s.queued[r.URL.Path]; len(queue) > 0 {
			status, s.queued[r.URL.Path] = queue[0], queue[1:]
		} else if code, ok := s.sticky[r.URL.Path]; ok {
			status = code
		}
		retryAfter := s.retryAfter
		s.mu.Unlock()

		if status != http.StatusOK {
			if retryAfter != "" {
				w.Header().Set("Retry-After", retryAfter)
			}
			http.Error(w, "upstream busy", status)
			return
		}
		if r.URL.Path == "/key" {
			writeDownloadSpeedResponse(w, fakeKeyResponse(key))
			return
		}
		writeDownloadSpeedResponse(w, make([]byte, 16))
	})
}

func (s *retryScript) count(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests[path]
}

type retryHarness struct {
	script *retryScript
	server *httptest.Server
	clock  *retrytest.Clock
	d      *Downloader
}

func newRetryHarness(t *testing.T, pipeline bool, budget time.Duration) *retryHarness {
	t.Helper()
	script := newRetryScript()
	server := httptest.NewServer(script.handler([]byte("0123456789abcdef")))
	t.Cleanup(server.Close)
	cfg := &config.Config{
		BaseURL:                   server.URL,
		Token:                     "test-token",
		TempDirLocation:           t.TempDir(),
		Views:                     "left",
		EnablePipeline:            pipeline,
		DownloadWorkersPerLecture: 1,
		DecryptWorkersPerLecture:  1,
		RateLimit:                 100,
		APIRateLimit:              20,
	}
	d := New(cfg, client.New(server.Client(), nil))
	clock := retrytest.NewClock()
	d.retry = retry.Default(budget)
	d.retry.Clock = clock
	d.retry.Jitter = retrytest.MaxJitter
	return &retryHarness{script: script, server: server, clock: clock, d: d}
}

func (h *retryHarness) download(t *testing.T) error {
	t.Helper()
	_, err := h.d.DownloadPlaylist(t.Context(), client.ParsedPlaylist{
		ID:            42,
		SeqNo:         1,
		KeyURL:        h.server.URL + "/key",
		FirstViewURLs: []string{h.server.URL + "/chunk"},
	}, nil, nil)
	return err
}

func forEachDownloadPath(t *testing.T, run func(t *testing.T, pipeline bool)) {
	t.Helper()
	for _, tc := range []struct {
		name     string
		pipeline bool
	}{{"serial", false}, {"pipeline", true}} {
		t.Run(tc.name, func(t *testing.T) { run(t, tc.pipeline) })
	}
}

func TestChunkDownloadRetriesTransient516(t *testing.T) {
	forEachDownloadPath(t, func(t *testing.T, pipeline bool) {
		h := newRetryHarness(t, pipeline, 3*time.Minute)
		h.script.queued["/chunk"] = []int{516, 516, 516, 516}

		if err := h.download(t); err != nil {
			t.Fatalf("DownloadPlaylist() error = %v, want success after transient 516s", err)
		}
		if got := h.script.count("/chunk"); got != 5 {
			t.Fatalf("chunk requests = %d, want 5", got)
		}
		want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}
		if got := h.clock.Sleeps(); !slices.Equal(got, want) {
			t.Fatalf("backoff sleeps = %v, want %v", got, want)
		}
	})
}

func TestChunkDownloadDoesNotRetryUnauthorized(t *testing.T) {
	forEachDownloadPath(t, func(t *testing.T, pipeline bool) {
		h := newRetryHarness(t, pipeline, 3*time.Minute)
		h.script.sticky["/chunk"] = http.StatusUnauthorized

		err := h.download(t)
		if !errors.Is(err, client.ErrAuthentication) {
			t.Fatalf("DownloadPlaylist() error = %v, want ErrAuthentication", err)
		}
		if got := h.script.count("/chunk"); got != 1 {
			t.Fatalf("chunk requests = %d, want 1", got)
		}
		if got := h.clock.Sleeps(); len(got) != 0 {
			t.Fatalf("backoff sleeps = %v, want none", got)
		}
	})
}

func TestChunkDownloadStopsAtRetryBudget(t *testing.T) {
	forEachDownloadPath(t, func(t *testing.T, pipeline bool) {
		h := newRetryHarness(t, pipeline, time.Minute)
		h.script.sticky["/chunk"] = http.StatusServiceUnavailable

		err := h.download(t)
		if err == nil {
			t.Fatal("DownloadPlaylist() error = nil, want budget exhaustion")
		}
		// Only the pipeline path reports the per-chunk root cause.
		if pipeline && (!strings.Contains(err.Error(), "status 503") || !strings.Contains(err.Error(), "failed after 6 attempts")) {
			t.Fatalf("DownloadPlaylist() error = %v, want status 503 after 6 attempts", err)
		}
		// 1s+2s+4s+8s+16s = 31s; the next 30s step would pass the 1m budget.
		if got := h.script.count("/chunk"); got != 6 {
			t.Fatalf("chunk requests = %d, want 6", got)
		}
		if elapsed := h.clock.Elapsed(); elapsed > time.Minute {
			t.Fatalf("elapsed backoff = %v, want <= 1m", elapsed)
		}
	})
}

func TestChunkDownloadHonorsRetryAfter(t *testing.T) {
	forEachDownloadPath(t, func(t *testing.T, pipeline bool) {
		h := newRetryHarness(t, pipeline, 3*time.Minute)
		h.script.queued["/chunk"] = []int{http.StatusServiceUnavailable}
		h.script.retryAfter = "5"

		if err := h.download(t); err != nil {
			t.Fatalf("DownloadPlaylist() error = %v", err)
		}
		if got := h.clock.Sleeps(); !slices.Equal(got, []time.Duration{5 * time.Second}) {
			t.Fatalf("backoff sleeps = %v, want [5s] from Retry-After", got)
		}
	})
}

func TestDecryptionKeyRetriesTransientFailure(t *testing.T) {
	forEachDownloadPath(t, func(t *testing.T, pipeline bool) {
		h := newRetryHarness(t, pipeline, 3*time.Minute)
		h.script.queued["/key"] = []int{http.StatusBadGateway}

		if err := h.download(t); err != nil {
			t.Fatalf("DownloadPlaylist() error = %v, want success after a transient key 502", err)
		}
		if got := h.script.count("/key"); got != 2 {
			t.Fatalf("key requests = %d, want 2", got)
		}
	})
}

func TestDecryptionKeyDoesNotRetryUnauthorized(t *testing.T) {
	h := newRetryHarness(t, false, 3*time.Minute)
	h.script.sticky["/key"] = http.StatusUnauthorized

	err := h.download(t)
	if !errors.Is(err, client.ErrAuthentication) {
		t.Fatalf("DownloadPlaylist() error = %v, want ErrAuthentication", err)
	}
	if got := h.script.count("/key"); got != 1 {
		t.Fatalf("key requests = %d, want 1", got)
	}
	if got := h.script.count("/chunk"); got != 0 {
		t.Fatalf("chunk requests = %d, want none after key auth failure", got)
	}
}

func TestPlaybackKeyRetryBudgetIsCapped(t *testing.T) {
	h := newRetryHarness(t, false, 3*time.Minute)
	h.script.sticky["/key"] = http.StatusServiceUnavailable

	_, err := h.d.fetchPlaybackDecryptionKey(t.Context(), h.server.URL+"/key")
	if err == nil || !strings.Contains(err.Error(), "status 503") {
		t.Fatalf("fetchPlaybackDecryptionKey() error = %v, want status 503", err)
	}
	if elapsed := h.clock.Elapsed(); elapsed > playbackKeyRetryBudget {
		t.Fatalf("playback key backoff = %v, want <= %v", elapsed, playbackKeyRetryBudget)
	}
	// 1s+2s+4s+8s = 15s fits the cap; the next 16s step would pass it.
	if got := h.script.count("/key"); got != 5 {
		t.Fatalf("key requests = %d, want 5", got)
	}
}

func TestRetryDiagnosticsDoNotLeakToken(t *testing.T) {
	forEachDownloadPath(t, func(t *testing.T, pipeline bool) {
		h := newRetryHarness(t, pipeline, 3*time.Minute)
		var logs bytes.Buffer
		h.d.diagnostics = log.New(&logs, "", 0) // a Logger serializes writes
		h.script.queued["/key"] = []int{http.StatusServiceUnavailable}
		h.script.queued["/chunk"] = []int{http.StatusServiceUnavailable}
		token := h.d.config.Token

		_, err := h.d.DownloadPlaylist(t.Context(), client.ParsedPlaylist{
			ID:            42,
			SeqNo:         1,
			KeyURL:        h.server.URL + "/key?token=" + token,
			FirstViewURLs: []string{h.server.URL + "/chunk?token=" + token},
		}, nil, nil)
		if err != nil {
			t.Fatalf("DownloadPlaylist() error = %v", err)
		}
		output := logs.String()
		if !strings.Contains(output, "retrying decryption key") || !strings.Contains(output, "retrying left view chunk") && !strings.Contains(output, "retrying first view chunk") {
			t.Fatalf("diagnostics = %q, want key and chunk retry lines", output)
		}
		if strings.Contains(output, token) {
			t.Fatalf("retry diagnostics leaked the token: %q", output)
		}
	})
}

func TestPlaybackKeyFetchHasWallClockDeadline(t *testing.T) {
	saved := playbackKeyDeadline
	playbackKeyDeadline = 50 * time.Millisecond
	t.Cleanup(func() { playbackKeyDeadline = saved })

	h := newRetryHarness(t, false, 3*time.Minute)
	// A key endpoint that hangs until the client gives up.
	h.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})

	_, err := h.d.fetchPlaybackDecryptionKey(t.Context(), h.server.URL+"/key")
	if err == nil || !strings.Contains(err.Error(), "did not finish within 50ms") {
		t.Fatalf("fetchPlaybackDecryptionKey() error = %v, want the playback deadline", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("fetchPlaybackDecryptionKey() error = %v, want context.DeadlineExceeded", err)
	}
}
