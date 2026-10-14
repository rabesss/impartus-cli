package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rabesss/impartus-cli/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestClientTokenCacheIsSafeDuringFallbackRequests(t *testing.T) {
	const (
		firstToken  = "fallback-token-short"
		secondToken = "fallback-token-with-a-different-length"
		iterations  = 200
	)
	tokens := [...]string{firstToken, secondToken}
	headers := make(chan string, iterations)
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		headers <- req.Header.Get("Authorization")
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("[]")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})
	apiClient := New(&http.Client{Transport: transport}, nil)
	apiClient.setToken(firstToken)
	// Keep the config token empty so concurrent requests exercise the
	// synchronized Client cache. LoginAndSetToken's compatibility write to
	// cfg.Token is caller-owned state and is not performed concurrently with
	// direct config reads.
	cfg := &config.Config{BaseURL: "https://api.example.test"}
	ctx := context.Background()

	start := make(chan struct{})
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		<-start
		for index := 0; index < iterations; index++ {
			apiClient.setToken(tokens[index%len(tokens)])
		}
	}()
	go func() {
		defer wait.Done()
		<-start
		for index := 0; index < iterations; index++ {
			if got := apiClient.tokenValue(); got != firstToken && got != secondToken {
				t.Errorf("cached token = %q, want one of the complete tokens", got)
			}
			if _, err := apiClient.GetCourses(ctx, cfg); err != nil {
				t.Errorf("GetCourses() error = %v", err)
			}
		}
	}()
	close(start)
	wait.Wait()
	close(headers)
	assertTokenRaceHeaders(t, headers, iterations, firstToken, secondToken)
	if got := apiClient.tokenValue(); got != secondToken {
		t.Fatalf("cached token = %q, want last written token %q", got, secondToken)
	}
}

func TestClientLoginAndFallbackRequestsShareTokenSafely(t *testing.T) {
	const (
		initialToken = "initial-token-short"
		loginToken   = "concurrent-login-token-with-a-different-length"
		iterations   = 100
	)
	headers := make(chan string, iterations)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/signin":
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]string{"token": loginToken}); err != nil {
				t.Errorf("write login response: %v", err)
			}
		case "/subjects":
			headers <- r.Header.Get("Authorization")
			if _, err := io.WriteString(w, "[]"); err != nil {
				t.Errorf("write courses response: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		Username:       "user",
		Password:       "password",
		BaseURL:        server.URL,
		TokenCachePath: filepath.Join(t.TempDir(), "token-cache"),
	}
	apiClient := New(server.Client(), nil)
	// Requests can legitimately run before login finishes. Seed a distinct
	// fallback token so every request must succeed, regardless of scheduling.
	apiClient.setToken(initialToken)
	start := make(chan struct{})
	loginDone := make(chan error, 1)
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		<-start
		loginDone <- apiClient.LoginAndSetToken(context.Background(), cfg)
	}()
	go func() {
		defer wait.Done()
		<-start
		for index := 0; index < iterations; index++ {
			if _, err := apiClient.GetCourses(context.Background(), cfg); err != nil {
				t.Errorf("GetCourses() error = %v", err)
			}
		}
	}()
	close(start)
	wait.Wait()
	close(headers)
	assertTokenRaceHeaders(t, headers, iterations, initialToken, loginToken)
	if err := <-loginDone; err != nil {
		t.Fatalf("LoginAndSetToken() error = %v", err)
	}
	if got := apiClient.tokenValue(); got != loginToken {
		t.Fatalf("cached token = %q, want %q", got, loginToken)
	}
	if cfg.Token != loginToken {
		t.Fatalf("config token = %q, want %q", cfg.Token, loginToken)
	}
}

// Either token may be observed, and scheduling need not expose both.
// Detecting unsynchronized access itself requires running with -race.
func assertTokenRaceHeaders(t *testing.T, headers <-chan string, wantCount int, firstToken, secondToken string) {
	t.Helper()
	count := 0
	for header := range headers {
		count++
		if header != "Bearer "+firstToken && header != "Bearer "+secondToken {
			t.Errorf("Authorization header = %q, want one of the complete bearer tokens", header)
		}
	}
	if count != wantCount {
		t.Errorf("Authorization header count = %d, want %d", count, wantCount)
	}
}
