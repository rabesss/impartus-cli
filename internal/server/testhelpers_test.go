package server

import (
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/config"
)

// Shared test helpers for server package tests.

func strPtr(v string) *string { return &v }

func boolPtr(v bool) *bool { return &v }

func intPtr(v int) *int { return &v }

// assertMapField asserts that the given key exists in the map and is a map[string]any.

// assertMapField asserts that the given key exists in the map and is a map[string]any.
func assertMapField(t *testing.T, m map[string]any, key string) map[string]any {
	t.Helper()
	v, ok := m[key].(map[string]any)
	if !ok {
		t.Fatalf("expected %q field to be map[string]any, got %T", key, m[key])
	}
	return v
}

func validServerConfig() *config.Config {
	return &config.Config{
		Username:         "user",
		Password:         "pass",
		BaseURL:          "https://example.com",
		Quality:          "450",
		Views:            "both",
		DownloadLocation: "./downloads",
		NumWorkers:       5,
		RateLimit:        1,
		APIRateLimit:     1,
		AudioFormat:      "mp3",
		HTTPTimeout:      "1m",
	}
}

// setupAuth creates an auth token for the given server and returns it.
func setupAuth(t *testing.T, s *APIServer) string {
	t.Helper()
	token, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}
	s.tokenStore.Store(token, TokenInfo{
		Username:  "user",
		Expiry:    time.Now().Add(1 * time.Hour),
		CreatedAt: time.Now(),
	})
	return token
}

// waitForBackgroundJobWork is a test-only barrier. It waits for jobs created by
// the test to become terminal, then occupies every runner slot so any runner
// publishing that terminal state must finish its deferred work before cleanup.
func waitForBackgroundJobWork(t *testing.T, s *APIServer) {
	t.Helper()
	if s == nil || s.jobStore == nil || cap(s.jobSem) == 0 {
		t.Fatal("server has no background job runner slots")
	}

	timer := time.NewTimer(2 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer timer.Stop()
	defer ticker.Stop()

	for {
		allTerminal := true
		for _, job := range s.jobStore.ListJobCopies() {
			if !isTerminalStatus(job.Status) {
				allTerminal = false
				break
			}
		}
		if allTerminal {
			break
		}
		select {
		case <-ticker.C:
		case <-timer.C:
			t.Fatal("background jobs did not reach a terminal state before test cleanup")
		}
	}

	slots := cap(s.jobSem)
	acquired := 0
	defer func() {
		for acquired > 0 {
			<-s.jobSem
			acquired--
		}
	}()
	for acquired < slots {
		select {
		case s.jobSem <- struct{}{}:
			acquired++
		case <-timer.C:
			t.Fatal("background job runners did not release their work slots before test cleanup")
		}
	}
}
