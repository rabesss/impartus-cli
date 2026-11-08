package client

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rabesss/impartus-cli/internal/config"
)

const strictTestConfiguredOrigin = "https://api.example.test"

func newCountingServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

func requireStrictRefusal(t *testing.T, err error, wantOrigin, token string) {
	t.Helper()
	if err == nil {
		t.Fatal("error = nil, want strict media-origin refusal")
	}
	if !errors.Is(err, ErrMediaOrigin) {
		t.Fatalf("errors.Is(err, ErrMediaOrigin) = false for %T %q", err, err)
	}
	if !errors.Is(err, ErrUnconfiguredMediaOrigin) {
		t.Fatalf("errors.Is(err, ErrUnconfiguredMediaOrigin) = false for %T %q", err, err)
	}
	if errors.Is(err, ErrAuthentication) {
		t.Fatalf("strict refusal %q must not be an authentication failure", err)
	}
	var originErr *UnconfiguredMediaOriginError
	if !errors.As(err, &originErr) {
		t.Fatalf("error = %T %q, want *UnconfiguredMediaOriginError", err, err)
	}
	if !originErr.Strict || originErr.StatusCode != 0 {
		t.Fatalf("refusal = %+v, want Strict with no status", originErr)
	}
	if originErr.Origin != wantOrigin {
		t.Fatalf("refusal origin = %q, want %q", originErr.Origin, wantOrigin)
	}
	message := err.Error()
	if !strings.Contains(message, wantOrigin) || !strings.Contains(message, "strictMediaOrigins") {
		t.Fatalf("message %q must name origin %q and strictMediaOrigins", message, wantOrigin)
	}
	if token != "" && containsTokenRepresentation(message, token) {
		t.Fatalf("message %q leaks the bearer", message)
	}
}

func TestStrictMediaOriginsRefusesUnconfiguredOriginBeforeDialling(t *testing.T) {
	const token = "secret-bearer-abc123"
	server, hits := newCountingServer(t)
	stdB64 := base64.StdEncoding.EncodeToString([]byte(token))
	urlB64 := base64.RawURLEncoding.EncodeToString([]byte(token))
	cases := []struct {
		name  string
		path  string
		token string
	}{
		{name: "plain", path: "/seg.ts", token: token},
		{name: "query token", path: "/seg.ts?x=" + token, token: token},
		{name: "query base64 token", path: "/seg.ts?x=" + url.QueryEscape(stdB64), token: token},
		{name: "path base64url token", path: "/" + urlB64 + "/seg.ts", token: token},
		{name: "tokenless", path: "/public.ts", token: ""},
	}
	c := New(server.Client(), nil)
	policy := MediaRequestPolicy{Origins: []string{strictTestConfiguredOrigin}, Strict: true}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := c.GetAuthorizedWithMediaPolicy(context.Background(), server.URL+tc.path, tc.token, policy)
			if resp != nil {
				_ = resp.Body.Close() //nolint:errcheck
			}
			requireStrictRefusal(t, err, server.URL, token)
		})
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("unconfigured origin received %d requests, want 0", got)
	}
}

func TestStrictMediaOriginsAllowsConfiguredOrigins(t *testing.T) {
	const token = "strict-configured-bearer"
	var authorization atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization.Store(r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := New(server.Client(), nil)
	policy := MediaRequestPolicy{Origins: []string{strictTestConfiguredOrigin, server.URL}, Strict: true}
	resp, err := c.GetAuthorizedWithMediaPolicy(context.Background(), server.URL+"/seg.ts", token, policy)
	if err != nil {
		t.Fatalf("configured origin error = %v", err)
	}
	_ = resp.Body.Close() //nolint:errcheck
	if got, _ := authorization.Load().(string); got != "Bearer "+token {
		t.Fatalf("Authorization = %q, want bearer on configured origin", got)
	}
}

func TestNonStrictMediaOriginsStillContactsUnconfiguredOriginWithoutBearer(t *testing.T) {
	const token = "non-strict-bearer"
	var authorization atomic.Value
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		authorization.Store(r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := New(server.Client(), nil)
	for _, call := range []func() (*http.Response, error){
		func() (*http.Response, error) {
			return c.GetAuthorizedWithTokenForOrigins(context.Background(), server.URL+"/seg.ts", token, strictTestConfiguredOrigin)
		},
		func() (*http.Response, error) {
			return c.GetAuthorizedWithMediaPolicy(context.Background(), server.URL+"/seg.ts", token, MediaRequestPolicy{Origins: []string{strictTestConfiguredOrigin}})
		},
	} {
		resp, err := call()
		if err != nil {
			t.Fatalf("non-strict error = %v", err)
		}
		_ = resp.Body.Close() //nolint:errcheck
		if got, _ := authorization.Load().(string); got != "" {
			t.Fatalf("Authorization = %q, want none on unconfigured origin", got)
		}
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("unconfigured origin hits = %d, want 2 (default stays permissive)", got)
	}
}

func TestStrictMediaOriginsRefusesRedirectToUnconfiguredOrigin(t *testing.T) {
	const token = "strict-redirect-bearer"
	unconfigured, hits := newCountingServer(t)
	source := httptest.NewServer(http.RedirectHandler(unconfigured.URL+"/segment.ts", http.StatusFound))
	defer source.Close()

	c := New(source.Client(), nil)
	for _, requestToken := range []string{token, ""} {
		resp, err := c.GetAuthorizedWithMediaPolicy(context.Background(), source.URL+"/playlist.m3u8", requestToken, MediaRequestPolicy{Origins: []string{source.URL}, Strict: true})
		if resp != nil {
			_ = resp.Body.Close() //nolint:errcheck
		}
		requireStrictRefusal(t, err, unconfigured.URL, token)
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("unconfigured redirect destination received %d requests, want 0", got)
	}
}

func TestStrictMediaOriginsRefusesRedirectRewrittenByCallerHook(t *testing.T) {
	const token = "strict-hook-bearer"
	unconfigured, hits := newCountingServer(t)
	configured, configuredHits := newCountingServer(t)
	source := httptest.NewServer(http.RedirectHandler(configured.URL+"/segment.ts", http.StatusFound))
	defer source.Close()

	httpClient := source.Client()
	httpClient.CheckRedirect = func(next *http.Request, _ []*http.Request) error {
		rewritten, err := url.Parse(unconfigured.URL + "/segment.ts")
		if err != nil {
			return err
		}
		next.URL = rewritten
		return nil
	}
	c := New(httpClient, nil)
	resp, err := c.GetAuthorizedWithMediaPolicy(context.Background(), source.URL+"/playlist.m3u8", token, MediaRequestPolicy{Origins: []string{source.URL, configured.URL}, Strict: true})
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	requireStrictRefusal(t, err, unconfigured.URL, token)
	if got := hits.Load() + configuredHits.Load(); got != 0 {
		t.Fatalf("redirect destinations received %d requests, want 0", got)
	}
}

func TestStrictMediaOriginsFollowsRedirectToConfiguredOrigin(t *testing.T) {
	const token = "strict-allowed-redirect"
	destination, hits := newCountingServer(t)
	source := httptest.NewServer(http.RedirectHandler(destination.URL+"/segment.ts", http.StatusFound))
	defer source.Close()

	c := New(source.Client(), nil)
	resp, err := c.GetAuthorizedWithMediaPolicy(context.Background(), source.URL+"/playlist.m3u8", token, MediaRequestPolicy{Origins: []string{source.URL, destination.URL}, Strict: true})
	if err != nil {
		t.Fatalf("configured redirect error = %v", err)
	}
	_ = resp.Body.Close() //nolint:errcheck
	if got := hits.Load(); got != 1 {
		t.Fatalf("configured redirect destination hits = %d, want 1", got)
	}
}

func TestStrictMediaOriginsDoesNotEchoBearerInOriginHost(t *testing.T) {
	const token = "hostbearer"
	transport := &mediaOriginCaptureTransport{}
	c := New(&http.Client{Transport: transport}, nil)
	resp, err := c.GetAuthorizedWithMediaPolicy(context.Background(), "https://"+token+".cdn.example.test/seg.ts", token, MediaRequestPolicy{Origins: []string{strictTestConfiguredOrigin}, Strict: true})
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if err == nil || !errors.Is(err, ErrMediaOrigin) {
		t.Fatalf("error = %v, want ErrMediaOrigin", err)
	}
	var originErr *UnconfiguredMediaOriginError
	if errors.As(err, &originErr) {
		t.Fatalf("error = %+v, must not carry an origin containing the bearer", originErr)
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("error %q leaks the bearer", err)
	}
	if transport.request != nil {
		t.Fatalf("request sent to %s, want none", transport.request.URL)
	}
}

func TestGetPlaylistsStrictMediaOriginsRefusesUnconfiguredPlaylistOrigin(t *testing.T) {
	const token = "strict-playlist-bearer"
	transport := &mediaOriginRoutingTransport{responses: map[string]string{
		"https://api.example.test/fetchvideo":           "https://cdn.example.test/1280x720/master.m3u8\n",
		"https://cdn.example.test/1280x720/master.m3u8": "#EXTM3U\n#EXTINF:1,\nsegment.ts\n",
	}}
	c := New(&http.Client{Transport: transport}, nil)
	_, err := c.GetPlaylists(context.Background(), &config.Config{
		BaseURL:            strictTestConfiguredOrigin,
		StrictMediaOrigins: true,
		Quality:            "720",
		Token:              token,
	}, Lectures{{TTID: 42, Topic: "Strict CDN"}})
	requireStrictRefusal(t, err, "https://cdn.example.test", token)
	for _, request := range transport.requests {
		if request.URL.Host != "api.example.test" {
			t.Fatalf("request sent to %s, want only the configured API origin", request.URL)
		}
	}
}

func TestUnconfiguredMediaOriginErrorStrictUnwrap(t *testing.T) {
	strict := &UnconfiguredMediaOriginError{Origin: "https://cdn.example.test", Strict: true}
	if !errors.Is(strict, ErrMediaOrigin) || !errors.Is(strict, ErrUnconfiguredMediaOrigin) {
		t.Fatalf("strict refusal must match ErrMediaOrigin and ErrUnconfiguredMediaOrigin")
	}
	want := "media origin https://cdn.example.test is not in mediaOrigins (IMPARTUS_MEDIA_ORIGINS); strictMediaOrigins refused the request before it was sent"
	if strict.Error() != want {
		t.Fatalf("Error() = %q, want %q", strict.Error(), want)
	}

	if got := errors.Unwrap(strict); got != ErrUnconfiguredMediaOrigin { //nolint:errorlint // checks the single-error Unwrap contract
		t.Fatalf("errors.Unwrap(strict) = %v, want ErrUnconfiguredMediaOrigin", got)
	}

	refused := &UnconfiguredMediaOriginError{Origin: "https://cdn.example.test", StatusCode: http.StatusForbidden}
	if got := errors.Unwrap(refused); got != ErrUnconfiguredMediaOrigin { //nolint:errorlint // checks the single-error Unwrap contract
		t.Fatalf("errors.Unwrap(refused) = %v, want ErrUnconfiguredMediaOrigin", got)
	}
	if errors.Is(refused, ErrMediaOrigin) {
		t.Fatal("a refusal after sending must not match ErrMediaOrigin")
	}
	if !errors.Is(refused, ErrUnconfiguredMediaOrigin) {
		t.Fatal("a refusal after sending must match ErrUnconfiguredMediaOrigin")
	}
}

// A caller-supplied CheckRedirect hook can return the exported error type with
// arbitrary fields. It must not be passed through as a trusted strict refusal.
func TestStrictMediaOriginsDoesNotTrustForgedHookRefusal(t *testing.T) {
	const token = "forged-hook-bearer"
	destination, hits := newCountingServer(t)
	source := httptest.NewServer(http.RedirectHandler(destination.URL+"/segment.ts", http.StatusFound))
	defer source.Close()

	forgedOrigin := "https://" + token + ".example.test/private/path?sig=signed-query-value"
	httpClient := source.Client()
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return &UnconfiguredMediaOriginError{Origin: forgedOrigin, RespondingOrigin: forgedOrigin, Strict: true}
	}
	c := New(httpClient, nil)
	resp, err := c.GetAuthorizedWithMediaPolicy(context.Background(), source.URL+"/playlist.m3u8", token, MediaRequestPolicy{Origins: []string{source.URL, destination.URL}, Strict: true})
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if err == nil {
		t.Fatal("forged hook refusal error = nil, want an error")
	}
	var originErr *UnconfiguredMediaOriginError
	if errors.As(err, &originErr) {
		t.Fatalf("forged hook refusal passed through as %#v", originErr)
	}
	// The hook's own message is sanitized like any hook error; it must not
	// carry the bearer.
	if strings.Contains(err.Error(), token) {
		t.Fatalf("error %q leaks the bearer", err.Error())
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("redirect destination hits = %d, want 0", got)
	}
}
