package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/config"
)

func TestValidateLoginResponseUnauthorizedReturnsTypedAuthenticationError(t *testing.T) {
	const secretMarker = "password=upstream-response-secret"
	response := &http.Response{
		StatusCode: http.StatusUnauthorized,
		Body:       io.NopCloser(strings.NewReader(secretMarker)),
	}

	err := validateLoginResponse(response)
	if err == nil {
		t.Fatal("validateLoginResponse() error = nil, want typed authentication error")
	}
	if !errors.Is(err, ErrAuthentication) {
		t.Fatalf("validateLoginResponse() error = %T %q, want ErrAuthentication identity", err, err)
	}
	var authErr *AuthenticationError
	if !errors.As(err, &authErr) {
		t.Fatalf("validateLoginResponse() error = %T %q, want *AuthenticationError", err, err)
	}
	if authErr.Operation != "login" || authErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("authentication metadata = (%q, %d), want (login, 401)", authErr.Operation, authErr.StatusCode)
	}
	if got, want := err.Error(), "wrong credentials please retry"; got != want {
		t.Fatalf("validateLoginResponse() error = %q, want %q", got, want)
	}
	if strings.Contains(err.Error(), secretMarker) {
		t.Fatalf("validateLoginResponse() error leaked response body: %q", err)
	}
}

func TestValidateLoginResponseNonUnauthorizedErrorsAreStatusOnly(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{
			name:       "secret-like response",
			statusCode: http.StatusForbidden,
			body:       "username=alice password=body-secret token=body-token",
		},
		{
			name:       "oversized response",
			statusCode: http.StatusInternalServerError,
			body:       strings.Repeat("oversized-secret-marker", 1<<16),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := &http.Response{
				StatusCode: test.statusCode,
				Body:       io.NopCloser(strings.NewReader(test.body)),
			}

			err := validateLoginResponse(response)
			want := "login failed with status " + strconv.Itoa(test.statusCode)
			if err == nil || err.Error() != want {
				t.Fatalf("validateLoginResponse() error = %v, want %q", err, want)
			}
			if strings.Contains(err.Error(), test.body) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("validateLoginResponse() error leaked response body: %q", err)
			}
		})
	}
}

func TestNewLoggedInTreatsCachedProfileUnauthorizedAsMissAndReplacesToken(t *testing.T) {
	const (
		staleToken = "stale-cache-token"
		freshToken = "fresh-login-token"
	)
	cachePath := filepath.Join(t.TempDir(), "auth-cache")
	if err := writeTokenCache(cachePath, []byte(staleToken)); err != nil {
		t.Fatalf("seed explicit token cache: %v", err)
	}
	var profileCalls, loginCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user/profile":
			profileCalls++
			if got := r.Header.Get("Authorization"); got != "Bearer "+staleToken {
				t.Errorf("profile authorization = %q, want stale cached token", got)
			}
			w.WriteHeader(http.StatusUnauthorized)
			writeAuthTestResponse(t, w, "profile-body-secret")
		case "/auth/signin":
			loginCalls++
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]string{"token": freshToken}); err != nil {
				t.Errorf("encode login response: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		Username:       "user",
		Password:       "pass",
		BaseURL:        server.URL,
		TokenCachePath: cachePath,
	}
	apiClient, err := NewLoggedIn(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewLoggedIn() error = %v, want cached 401 fallback success", err)
	}
	if profileCalls != 1 || loginCalls != 1 {
		t.Fatalf("request calls = profile:%d login:%d, want 1 each", profileCalls, loginCalls)
	}
	if cfg.Token != freshToken || apiClient.tokenValue() != freshToken {
		t.Fatalf("active token = config:%q client:%q, want fresh login token", cfg.Token, apiClient.tokenValue())
	}
	got, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read explicit token cache: %v", err)
	}
	if string(got) != freshToken {
		t.Fatalf("explicit token cache = %q, want fresh login token", got)
	}
}

func TestNewLoggedInReturnsTypedFreshLoginFailureWithoutPoisoningCachedToken(t *testing.T) {
	const (
		staleToken = "stale-cache-token"
		secretBody = "password=fresh-login-response-secret"
	)
	cachePath := filepath.Join(t.TempDir(), "auth-cache")
	if err := writeTokenCache(cachePath, []byte(staleToken)); err != nil {
		t.Fatalf("seed explicit token cache: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user/profile":
			w.WriteHeader(http.StatusUnauthorized)
		case "/auth/signin":
			w.WriteHeader(http.StatusUnauthorized)
			writeAuthTestResponse(t, w, secretBody)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		Username:       "user",
		Password:       "pass",
		BaseURL:        server.URL,
		TokenCachePath: cachePath,
	}
	apiClient, err := NewLoggedIn(context.Background(), cfg)
	if apiClient != nil {
		t.Fatalf("NewLoggedIn() client = %v, want nil after fresh-login 401", apiClient)
	}
	if !errors.Is(err, ErrAuthentication) {
		t.Fatalf("NewLoggedIn() error = %T %q, want ErrAuthentication identity", err, err)
	}
	var authErr *AuthenticationError
	if !errors.As(err, &authErr) || authErr.Operation != "login" || authErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("NewLoggedIn() authentication error = %#v, want login/401 metadata", authErr)
	}
	if got, want := err.Error(), "wrong credentials please retry"; got != want {
		t.Fatalf("NewLoggedIn() error = %q, want %q", got, want)
	}
	if strings.Contains(err.Error(), secretBody) {
		t.Fatalf("NewLoggedIn() error leaked response body: %q", err)
	}
	if cfg.Token != "" {
		t.Fatalf("config token = %q, want empty after failed fresh login", cfg.Token)
	}
	got, readErr := os.ReadFile(cachePath)
	if readErr != nil {
		t.Fatalf("read explicit token cache: %v", readErr)
	}
	if string(got) != staleToken {
		t.Fatalf("explicit token cache = %q, want unchanged stale token after failed fresh login", got)
	}
}
