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
