package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/config"
)

// TestDoRequestWithToken_NeverLeaksToken is the regression guard for the P0
// secret-leak fix: http.Client.Do returns a *url.Error whose Error() embeds the
// full request URL (including a query token). The token must not survive into
// the returned error, neither in the wrapping message nor via %w unwrapping.
func TestDoRequestWithToken_NeverLeaksToken(t *testing.T) {
	const secret = "do-leak-secret-token"
	c := New(nil, nil) // safe zero-value client
	// A closed port on a routable loopback host triggers a *url.Error from Do.
	resp, err := c.doRequestWithToken(context.TODO(), http.MethodGet,
		"https://127.0.0.1:1/fetchvideo?ttid=1&token="+secret, nil, secret)
	if resp != nil {
		closeErr := resp.Body.Close()
		_ = closeErr
	}
	if err == nil {
		t.Skip("no error produced; cannot assert redaction")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("doRequestWithToken error leaked token: %v", err)
	}
}

// TestDoRequestWithToken_MalformedURLNoLeak exercises the parse-failure branch:
// an invalid percent escape makes http.NewRequest fail with a *url.Error whose
// URL field is the raw tokenized URL. Neither the explicit %s nor the wrapped
// error must leak the token.
func TestDoRequestWithToken_MalformedURLNoLeak(t *testing.T) {
	const secret = "malformed-secret"
	// "%zz" is an invalid percent-escape that url.Parse rejects.
	malformed := "https://127.0.0.1:1/fetchvideo/%zz?token=" + secret
	c := New(nil, nil)
	resp, err := c.doRequestWithToken(context.TODO(), http.MethodGet, malformed, nil, "")
	if resp != nil {
		closeErr := resp.Body.Close()
		_ = closeErr
	}
	if err == nil {
		t.Skip("no error produced; cannot assert redaction")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("doRequestWithToken leaked token via malformed URL: %v", err)
	}
}

// TestNewHTTPClient covers the timeout-defaulting branch: a non-positive
// timeout falls back to defaultHTTPTimeout, and a positive one is honored.
func TestNewHTTPClient(t *testing.T) {
	cases := []struct {
		name    string
		timeout time.Duration
		want    time.Duration
	}{
		{"zero uses default", 0, defaultHTTPTimeout},
		{"negative uses default", -time.Second, defaultHTTPTimeout},
		{"positive honored", 42 * time.Second, 42 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if c := NewHTTPClient(tc.timeout); c.Timeout != tc.want {
				t.Errorf("NewHTTPClient(%v) timeout = %v, want %v", tc.timeout, c.Timeout, tc.want)
			}
		})
	}
}

func TestNewClientFromConfigTimeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout string
		want    time.Duration
	}{
		{name: "minimum", timeout: "30s", want: 30 * time.Second},
		{name: "custom", timeout: "42s", want: 42 * time.Second},
		{name: "maximum", timeout: "60m", want: 60 * time.Minute},
		{name: "empty uses default", want: defaultHTTPTimeout},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := newClientFromConfig(&config.Config{HTTPTimeout: tt.timeout})
			if err != nil {
				t.Fatalf("newClientFromConfig() error = %v", err)
			}
			if c.httpClient.Timeout != tt.want {
				t.Fatalf("http timeout = %v, want %v", c.httpClient.Timeout, tt.want)
			}
		})
	}
}

func TestNewClientFromConfigRejectsInvalidTimeout(t *testing.T) {
	if _, err := newClientFromConfig(&config.Config{HTTPTimeout: "not-a-duration"}); err == nil || !strings.Contains(err.Error(), "invalid httpTimeout") {
		t.Fatalf("newClientFromConfig() error = %v, want contextual timeout error", err)
	}
}

func TestNewLoggedInRejectsInvalidTimeoutBeforeLogin(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()

	_, err := NewLoggedIn(context.Background(), &config.Config{
		Username:    "user",
		Password:    "password",
		BaseURL:     server.URL,
		HTTPTimeout: "invalid",
	})
	if err == nil {
		t.Fatal("NewLoggedIn() error = nil, want invalid timeout error")
	}
	if requests.Load() != 0 {
		t.Fatalf("login requests = %d, want 0", requests.Load())
	}
}

func TestNewPreservesInjectedHTTPClientTimeout(t *testing.T) {
	injected := &http.Client{Timeout: 17 * time.Second}
	c := New(injected, nil)

	if c.httpClient != injected {
		t.Fatal("New() replaced the injected HTTP client")
	}
	if c.httpClient.Timeout != 17*time.Second {
		t.Fatalf("injected timeout = %v, want 17s", c.httpClient.Timeout)
	}
}

func TestNewClientFromConfigRequiresConfig(t *testing.T) {
	_, err := newClientFromConfig(nil)
	if err == nil || err.Error() != "config is required" {
		t.Fatalf("newClientFromConfig(nil) error = %v, want config is required", err)
	}
}
