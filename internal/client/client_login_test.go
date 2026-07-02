package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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
			// storeToken writes ".token" to the CWD; isolate it in a temp dir.
			prev, err := os.Getwd()
			if err != nil {
				t.Fatalf("getwd: %v", err)
			}
			if chErr := os.Chdir(t.TempDir()); chErr != nil {
				t.Fatalf("chdir: %v", chErr)
			}
			t.Cleanup(func() {
				chErr := os.Chdir(prev)
				_ = chErr
			})

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

			cfg := &config.Config{Username: "u", Password: "p", BaseURL: srv.URL}
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
