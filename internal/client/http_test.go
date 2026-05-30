package client

import (
	"net/http"
	"testing"
	"time"
)

func TestNewHTTPClient(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
		checkFn func(*http.Client) bool
	}{
		{
			name:    "default timeout",
			timeout: 0,
			checkFn: func(c *http.Client) bool {
				return c.Timeout == defaultHTTPTimeout
			},
		},
		{
			name:    "negative timeout",
			timeout: -1,
			checkFn: func(c *http.Client) bool {
				return c.Timeout == defaultHTTPTimeout
			},
		},
		{
			name:    "custom timeout",
			timeout: 5 * time.Second,
			checkFn: func(c *http.Client) bool {
				return c.Timeout == 5*time.Second
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewHTTPClient(tt.timeout)
			if client == nil {
				t.Fatal("expected non-nil http.Client")
			}
			if !tt.checkFn(client) {
				t.Errorf("NewHTTPClient(%v) timeout = %v, check failed", tt.timeout, client.Timeout)
			}
		})
	}
}

func TestClientTokenMethods(t *testing.T) {
	client := New(nil, nil)

	// Test tokenValue on nil token
	if token := client.tokenValue(); token != "" {
		t.Errorf("expected empty token, got %q", token)
	}

	// Test setToken and tokenValue
	client.setToken("test-token")
	if token := client.tokenValue(); token != "test-token" {
		t.Errorf("expected 'test-token', got %q", token)
	}
}

func TestClientEnsure(t *testing.T) {
	client := New(nil, nil)
	client.initialize()

	if client.httpClient == nil {
		t.Error("httpClient should be initialized after ensure()")
	}
	if client.UserAgentProvider == nil {
		t.Error("UserAgentProvider should be initialized after ensure()")
	}
}

func TestClientRandomUserAgent(t *testing.T) {
	client := New(nil, nil)
	ua := client.userAgent()

	if ua == "" {
		t.Error("expected non-empty user agent")
	}

	// Should be consistent when called multiple times with same provider
	ua2 := client.userAgent()
	if ua != ua2 {
		t.Errorf("expected consistent UA, got %q and %q", ua, ua2)
	}
}

func TestClientRandomUserAgentWithCustomProvider(t *testing.T) {
	customUA := "custom-test-agent/1.0"
	client := New(nil, func() string { return customUA })

	ua := client.userAgent()
	if ua != customUA {
		t.Errorf("expected %q, got %q", customUA, ua)
	}
}
