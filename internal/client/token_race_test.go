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
	const token = "concurrent-fallback-token"
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("[]")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})
	apiClient := New(&http.Client{Transport: transport}, nil)
	apiClient.setToken(token)
	// Keep the config token empty so concurrent requests exercise the
	// synchronized Client cache. LoginAndSetToken's compatibility write to
	// cfg.Token is caller-owned state and is not performed concurrently with
	// direct config reads.
	cfg := &config.Config{BaseURL: "https://api.example.test"}
	ctx := context.Background()

	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		for index := 0; index < 200; index++ {
			apiClient.setToken(token)
		}
	}()
	go func() {
		defer wait.Done()
		for index := 0; index < 200; index++ {
			if _, err := apiClient.GetCourses(ctx, cfg); err != nil {
				t.Errorf("GetCourses() error = %v", err)
			}
		}
	}()
	wait.Wait()
	if got := apiClient.tokenValue(); got != token {
		t.Fatalf("cached token = %q, want %q", got, token)
	}
}

func TestClientLoginAndFallbackRequestsShareTokenSafely(t *testing.T) {
	const token = "concurrent-login-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/signin":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"token": token}) //nolint:errcheck
		case "/subjects":
			_, _ = io.WriteString(w, "[]") //nolint:errcheck
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
		for index := 0; index < 100; index++ {
			_, _ = apiClient.GetCourses(context.Background(), cfg) //nolint:errcheck
		}
	}()
	close(start)
	wait.Wait()
	if err := <-loginDone; err != nil {
		t.Fatalf("LoginAndSetToken() error = %v", err)
	}
	if got := apiClient.tokenValue(); got != token {
		t.Fatalf("cached token = %q, want %q", got, token)
	}
}
