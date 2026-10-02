package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
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

func TestDirectLoginEntryPointsRejectUnsafeBaseURL(t *testing.T) {
	const leakedSecret = "direct-login-base-url-secret"
	for _, test := range []struct {
		name string
		raw  string
		want error
	}{
		{name: "userinfo", raw: "https://user:" + leakedSecret + "@api.example.test", want: ErrMediaURLUserinfo},
		{name: "query", raw: "https://api.example.test/auth?token=" + leakedSecret, want: ErrInvalidMediaURL},
		{name: "force query", raw: "https://api.example.test/auth?", want: ErrInvalidMediaURL},
		{name: "fragment", raw: "https://api.example.test/auth#" + leakedSecret, want: ErrInvalidMediaURL},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, entryPoint := range []struct {
				name string
				run  func(*config.Config) error
			}{
				{
					name: "LoginAndSetToken",
					run: func(cfg *config.Config) error {
						return New(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
							t.Fatal("unsafe BaseURL reached the login transport")
							return nil, nil
						})}, nil).LoginAndSetToken(context.Background(), cfg)
					},
				},
				{
					name: "NewLoggedIn",
					run: func(cfg *config.Config) error {
						_, err := NewLoggedIn(context.Background(), cfg)
						return err
					},
				},
			} {
				t.Run(entryPoint.name, func(t *testing.T) {
					err := entryPoint.run(&config.Config{
						Username: "user",
						Password: "password",
						BaseURL:  test.raw,
					})
					if err == nil || !errors.Is(err, test.want) {
						t.Fatalf("%s() error = %v, want errors.Is(..., %v)", entryPoint.name, err, test.want)
					}
					if strings.Contains(err.Error(), leakedSecret) {
						t.Fatalf("%s() error leaked BaseURL credential: %v", entryPoint.name, err)
					}
				})
			}
		})
	}
}

func TestNewLoginRequestRejectsCredentialBearingBaseURL(t *testing.T) {
	const secret = "base-url-secret"
	for _, rawBaseURL := range []string{
		"https://user:" + secret + "@example.com",
		"https://example.com/api?token=" + secret,
		"https://example.com/api#token=" + secret,
		"https://example.com/api/../private",
	} {
		t.Run(rawBaseURL, func(t *testing.T) {
			_, err := New(nil, nil).newLoginRequest(context.Background(), &config.Config{
				Username: "user",
				Password: "pass",
			}, rawBaseURL)
			if err == nil {
				t.Fatal("newLoginRequest() error = nil, want unsafe base URL rejection")
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("newLoginRequest() leaked base URL credential: %v", err)
			}
		})
	}
}

func TestLoginCanonicalizesMixedCaseBaseURLScheme(t *testing.T) {
	var requestScheme string
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestScheme = req.URL.Scheme
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"token":"mixed-case-token"}`)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})
	cfg := &config.Config{
		Username:       "user",
		Password:       "password",
		BaseURL:        "HtTpS://api.example.test",
		TokenCachePath: filepath.Join(t.TempDir(), "token-cache"),
	}
	if err := New(&http.Client{Transport: transport}, nil).LoginAndSetToken(context.Background(), cfg); err != nil {
		t.Fatalf("LoginAndSetToken() error = %v", err)
	}
	if requestScheme != "https" {
		t.Fatalf("login request scheme = %q, want lower-case https", requestScheme)
	}
}

func TestNewLoginRequestCanonicalizesBaseURLPath(t *testing.T) {
	request, err := New(nil, nil).newLoginRequest(context.Background(), &config.Config{
		Username: "user",
		Password: "pass",
	}, "HTTPS://EXAMPLE.COM/api///")
	if err != nil {
		t.Fatalf("newLoginRequest() error = %v", err)
	}
	if got, want := request.URL.String(), "https://example.com/api/auth/signin"; got != want {
		t.Fatalf("newLoginRequest() URL = %q, want %q", got, want)
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

func TestLoginErrorRedactsEchoedSubmittedCredentials(t *testing.T) {
	const username = "fixture-student-42"
	// The password contains a credential-shaped assignment, so a generic scrub
	// that runs first would split it and leave fragments behind.
	const password = "fixture-secret:pass word@99"
	requestBody, err := json.Marshal(map[string]string{"username": username, "password": password})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	for _, test := range []struct {
		name string
		echo string
	}{
		{name: "raw password", echo: password},
		{name: "query escaped password", echo: url.QueryEscape(password)},
		{name: "raw username", echo: username},
		{name: "request body", echo: string(requestBody)},
	} {
		t.Run(test.name, func(t *testing.T) {
			// A broken upstream can echo the submitted body into a malformed status
			// line, which the transport reports verbatim.
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, fmt.Errorf("malformed HTTP status code %q", test.echo)
			})
			_, err := New(&http.Client{Transport: transport}, nil).login(context.Background(), &config.Config{
				Username: username,
				Password: password,
			}, "https://api.example.test")
			if err == nil {
				t.Fatal("login() error = nil, want transport failure")
			}
			for _, leaked := range []string{username, password, url.QueryEscape(password), "fixture", "word@99"} {
				if strings.Contains(err.Error(), leaked) {
					t.Fatalf("login() error leaked submitted credential %q: %v", leaked, err)
				}
			}
			if !strings.Contains(err.Error(), "malformed HTTP status code") {
				t.Fatalf("login() error = %v, want transport diagnostic context", err)
			}
		})
	}
}

func TestLoginDoesNotUseHTTPClientCookieJar(t *testing.T) {
	receivedCookie := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedCookie <- r.Header.Get("Cookie")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"token":"login-token"}`) //nolint:errcheck
	}))
	defer server.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New() error = %v", err)
	}
	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	jar.SetCookies(serverURL, []*http.Cookie{{Name: "session", Value: "jar-secret"}})
	httpClient := server.Client()
	httpClient.Jar = jar

	token, err := New(httpClient, nil).login(context.Background(), &config.Config{
		Username: "user",
		Password: "password",
	}, server.URL)
	if err != nil {
		t.Fatalf("login() error = %v", err)
	}
	if token != "login-token" {
		t.Fatalf("login() token = %q, want login-token", token)
	}
	if got := <-receivedCookie; got != "" {
		t.Fatalf("login request Cookie = %q, want no cookie-jar credentials", got)
	}
}

func TestValidateStoredTokenNeverFollowsRedirect(t *testing.T) {
	t.Parallel()

	var targetRequests int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetRequests++
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/profile" {
			t.Fatalf("profile request path = %q", r.URL.Path)
		}
		w.Header().Set("Location", target.URL+"/user/profile")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	valid, err := New(source.Client(), nil).validateStoredToken(context.Background(), source.URL, "stored-token")
	if err != nil {
		t.Fatalf("validateStoredToken() error = %v, want a false result for redirect", err)
	}
	if valid {
		t.Fatal("validateStoredToken() = true for redirect response")
	}
	if targetRequests != 0 {
		t.Fatalf("redirect target received %d requests, want zero", targetRequests)
	}
}
