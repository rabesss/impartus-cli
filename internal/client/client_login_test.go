package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/config"
)

// TestNewLoggedIn exercises the shared login constructor end to end against a
// stub auth server: a 200 with a token stores it on the returned client, while
// a 401 (or an empty token) propagates the login error.
func TestNewLoggedIn(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		token   string
		wantErr bool
	}{
		{"success stores token", http.StatusOK, "abc-123", false},
		{"unauthorized returns error", http.StatusUnauthorized, "", true},
		{"empty token returns error", http.StatusOK, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/auth/signin" || r.Method != http.MethodPost {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				if tc.status == http.StatusOK {
					encErr := json.NewEncoder(w).Encode(map[string]string{"token": tc.token})
					_ = encErr
				}
			}))
			defer srv.Close()

			cfg := &config.Config{
				Username:       "u",
				Password:       "p",
				BaseURL:        srv.URL,
				TokenCachePath: filepath.Join(t.TempDir(), "auth-cache"),
			}
			c, err := NewLoggedIn(context.Background(), cfg)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				if c != nil {
					t.Errorf("expected nil client on error, got %v", c)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewLoggedIn unexpected error: %v", err)
			}
			if c == nil {
				t.Fatal("expected non-nil client on success")
			}
			if cfg.Token != tc.token {
				t.Errorf("cfg.Token = %q, want %q", cfg.Token, tc.token)
			}
			if got := c.tokenValue(); got != tc.token {
				t.Errorf("client token = %q, want %q", got, tc.token)
			}
		})
	}
}

func TestLoginNeverFollowsCredentialBearingRedirect(t *testing.T) {
	for _, test := range []struct {
		name       string
		statusCode int
		downgrade  bool
	}{
		{name: "https to http 307", statusCode: http.StatusTemporaryRedirect, downgrade: true},
		{name: "https to http 308", statusCode: http.StatusPermanentRedirect, downgrade: true},
		{name: "cross origin 307", statusCode: http.StatusTemporaryRedirect},
		{name: "cross origin 308", statusCode: http.StatusPermanentRedirect},
	} {
		t.Run(test.name, func(t *testing.T) {
			var targetRequests int
			var targetBody string
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				targetRequests++
				body, _ := io.ReadAll(r.Body) //nolint:errcheck
				targetBody = string(body)
				w.WriteHeader(http.StatusOK)
			}))
			defer target.Close()

			redirectHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Location", target.URL+"/auth/signin")
				w.WriteHeader(test.statusCode)
			})
			var source *httptest.Server
			var httpClient *http.Client
			if test.downgrade {
				source = httptest.NewTLSServer(redirectHandler)
				httpClient = source.Client()
			} else {
				source = httptest.NewServer(redirectHandler)
				httpClient = source.Client()
			}
			defer source.Close()

			apiClient := New(httpClient, nil)
			token, err := apiClient.login(context.Background(), &config.Config{
				Username: "login-user",
				Password: "login-password",
			}, source.URL)
			if token != "" {
				t.Fatalf("login token = %q, want empty after redirect rejection", token)
			}
			if err == nil || !strings.Contains(err.Error(), "login failed with status "+strconv.Itoa(test.statusCode)) {
				t.Fatalf("login error = %v, want status-only redirect rejection", err)
			}
			if strings.Contains(err.Error(), "login-user") || strings.Contains(err.Error(), "login-password") {
				t.Fatalf("login error leaked credentials: %v", err)
			}
			if targetRequests != 0 || targetBody != "" {
				t.Fatalf("redirect target received requests=%d body=%q, want zero requests/body", targetRequests, targetBody)
			}
		})
	}
}
