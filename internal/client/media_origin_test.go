package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/config"
)

type mediaOriginCaptureTransport struct {
	request  *http.Request
	response *http.Response
}

func (t *mediaOriginCaptureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.request = req.Clone(req.Context())
	if t.response == nil {
		t.response = &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("ok")),
			Header:     make(http.Header),
			Request:    req,
		}
	}
	return t.response, nil
}

func TestGetAuthorizedWithTokenRejectsRemoteHTTPMediaOrigin(t *testing.T) {
	transport := &mediaOriginCaptureTransport{}
	c := New(&http.Client{Transport: transport}, nil)

	resp, err := c.GetAuthorizedWithToken(context.Background(), "http://media.example.test/segment.ts", "bearer-secret")
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("GetAuthorizedWithToken() error = %v, want remote HTTP rejection", err)
	}
	if transport.request != nil {
		t.Fatal("remote HTTP media request was sent")
	}
}

func TestGetAuthorizedWithTokenRejectsURLUserinfo(t *testing.T) {
	transport := &mediaOriginCaptureTransport{}
	c := New(&http.Client{Transport: transport}, nil)

	resp, err := c.GetAuthorizedWithToken(context.Background(), "https://user:password@media.example.test/segment.ts", "bearer-secret")
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if err == nil || !errors.Is(err, ErrMediaURLUserinfo) {
		t.Fatalf("GetAuthorizedWithToken() error = %v, want ErrMediaURLUserinfo", err)
	}
	if transport.request != nil {
		t.Fatal("userinfo-bearing media request was sent")
	}
}

func TestGetAuthorizedWithTokenDoesNotAttachTokenToUnconfiguredLoopbackOrigin(t *testing.T) {
	transport := &mediaOriginCaptureTransport{}
	c := New(&http.Client{Transport: transport}, nil)

	resp, err := c.GetAuthorizedWithTokenForOrigins(context.Background(), "http://127.0.0.1:43123/segment.ts", "bearer-secret")
	if err != nil {
		t.Fatalf("GetAuthorizedWithToken() error = %v", err)
	}
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if transport.request == nil {
		t.Fatal("request was not sent")
	}
	if got := transport.request.Header.Get("Authorization"); got != "" {
		t.Fatalf("loopback Authorization = %q, want no bearer token without explicit policy", got)
	}
}

func TestGetAuthorizedWithTokenAttachesTokenToExplicitlyConfiguredLoopbackOrigin(t *testing.T) {
	transport := &mediaOriginCaptureTransport{}
	c := New(&http.Client{Transport: transport}, nil)
	resp, err := c.GetAuthorizedWithTokenForOrigins(
		context.Background(),
		"http://127.0.0.1:43123/segment.ts",
		"bearer-secret",
		"http://127.0.0.1:43123",
	)
	if err != nil {
		t.Fatalf("GetAuthorizedWithTokenForOrigins() error = %v", err)
	}
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if transport.request == nil {
		t.Fatal("request was not sent")
	}
	if got := transport.request.Header.Get("Authorization"); got != "Bearer bearer-secret" {
		t.Fatalf("loopback Authorization = %q, want explicitly configured bearer token", got)
	}
}

func TestGetAuthorizedWithTokenRejectsMalformedUnsupportedOrRelativeURL(t *testing.T) {
	transport := &mediaOriginCaptureTransport{}
	c := New(&http.Client{Transport: transport}, nil)
	for _, rawURL := range []string{
		"segment.ts",
		"//media.example.test/segment.ts",
		"file:///tmp/segment.ts",
		"https://media.example.test/%zz?token=bearer-secret",
		"https://media.example.test/segment.ts?token=bearer-secret%zz",
		"https://media.example.test/segment.ts?token=bearer-secret;keep=1",
	} {
		t.Run(rawURL, func(t *testing.T) {
			resp, err := c.GetAuthorizedWithTokenForOrigins(context.Background(), rawURL, "bearer-secret")
			if resp != nil {
				_ = resp.Body.Close() //nolint:errcheck
			}
			if err == nil || !errors.Is(err, ErrMediaOrigin) {
				t.Fatalf("GetAuthorizedWithToken(%q) error = %v, want ErrMediaOrigin", rawURL, err)
			}
		})
	}
	if transport.request != nil {
		t.Fatalf("invalid URL request was sent: %s", transport.request.URL)
	}
}

func TestGetAuthorizedWithTokenDoesNotAttachTokenToUnconfiguredRemoteOrigin(t *testing.T) {
	transport := &mediaOriginCaptureTransport{}
	c := New(&http.Client{Transport: transport}, nil)

	resp, err := c.GetAuthorizedWithTokenForOrigins(context.Background(), "https://media.example.test/segment.ts", "bearer-secret")
	if err != nil {
		t.Fatalf("GetAuthorizedWithToken() error = %v", err)
	}
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if transport.request == nil {
		t.Fatal("request was not sent")
	}
	if got := transport.request.Header.Get("Authorization"); got != "" {
		t.Fatalf("Authorization = %q, want no bearer token for unconfigured origin", got)
	}
}

func TestGetAuthorizedWithTokenRejectsBearerInUnconfiguredInitialURLFields(t *testing.T) {
	const token = "initial-url-secret"
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{name: "path", raw: "https://media.example.test/" + token + "/segment.ts"},
		{name: "encoded path", raw: "https://media.example.test/" + percentEncodeASCII(token) + "/segment.ts"},
		{name: "host", raw: "https://" + token + ".media.example.test/segment.ts"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := &mediaOriginCaptureTransport{}
			c := New(&http.Client{Transport: transport}, nil)
			resp, err := c.GetAuthorizedWithTokenForOrigins(context.Background(), tc.raw, token)
			if resp != nil {
				_ = resp.Body.Close() //nolint:errcheck
			}
			if err == nil || !errors.Is(err, ErrMediaOrigin) {
				t.Fatalf("GetAuthorizedWithTokenForOrigins(%q) error = %v, want ErrMediaOrigin", tc.raw, err)
			}
			if transport.request != nil {
				t.Fatal("unconfigured URL carrying the bearer was sent")
			}
		})
	}
}

func TestGetAuthorizedWithTokenStripsTokenlessInitialCredentialAliases(t *testing.T) {
	for _, key := range []string{
		"authorization", "AUTHORIZATION", "token", "x-api-key", "Cookie",
		"api-key", "apikey", "xapikey", "proxy-authorization", "proxy_authorization",
	} {
		t.Run(key, func(t *testing.T) {
			transport := &mediaOriginCaptureTransport{}
			c := New(&http.Client{Transport: transport}, nil)
			rawURL := "https://media.example.test/segment.ts?" + key + "=Bearer+query-only-secret&keep=1"
			resp, err := c.GetAuthorizedWithTokenForOrigins(context.Background(), rawURL, "")
			if err != nil {
				t.Fatalf("GetAuthorizedWithTokenForOrigins() error = %v", err)
			}
			if resp != nil {
				_ = resp.Body.Close() //nolint:errcheck
			}
			if transport.request == nil {
				t.Fatal("request was not sent")
			}
			if got := transport.request.URL.RawQuery; got != "keep=1" {
				t.Fatalf("request query = %q, want credential alias removed", got)
			}
		})
	}
}

func TestGetAuthorizedWithTokenCanonicalizesInitialURLScheme(t *testing.T) {
	transport := &mediaOriginCaptureTransport{}
	c := New(&http.Client{Transport: transport}, nil)
	resp, err := c.GetAuthorizedWithTokenForOrigins(context.Background(), "HtTp://media.example.test/segment.ts", "")
	if err != nil {
		t.Fatalf("GetAuthorizedWithTokenForOrigins() error = %v", err)
	}
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if transport.request == nil || transport.request.URL == nil {
		t.Fatal("request was not sent")
	}
	if transport.request.URL.Scheme != "http" {
		t.Fatalf("request scheme = %q, want lower-case http", transport.request.URL.Scheme)
	}
}

func TestGetAuthorizedWithTokenWrapsTokenlessHTTPMediaRequests(t *testing.T) {
	var startCookie string
	var finalCookie string
	var finalReferer string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			startCookie = r.Header.Get("Cookie")
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		finalCookie = r.Header.Get("Cookie")
		finalReferer = r.Header.Get("Referer")
		w.WriteHeader(http.StatusOK)
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
	jar.SetCookies(serverURL, []*http.Cookie{{Name: "session", Value: "must-not-cross"}})
	httpClient := server.Client()
	httpClient.Jar = jar

	resp, err := New(httpClient, nil).GetAuthorizedWithToken(context.Background(), server.URL+"/start", "")
	if err != nil {
		t.Fatalf("GetAuthorizedWithToken() error = %v", err)
	}
	if resp == nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GetAuthorizedWithToken() response = %#v, want 200 response", resp)
	}
	_ = resp.Body.Close() //nolint:errcheck
	if startCookie != "" {
		t.Fatalf("tokenless request carried cookie %q, want cookie jar isolation", startCookie)
	}
	if finalCookie != "" {
		t.Fatalf("tokenless redirect carried cookie %q, want cookie jar isolation", finalCookie)
	}
	if finalReferer != "" {
		t.Fatalf("tokenless redirect carried Referer %q, want redirect referer stripped", finalReferer)
	}
}

func TestGetAuthorizedWithTokenTokenlessRedirectRemainsSameOrigin(t *testing.T) {
	var targetRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetRequests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/final", http.StatusFound)
	}))
	defer source.Close()

	resp, err := New(source.Client(), nil).GetAuthorizedWithToken(context.Background(), source.URL+"/start", "")
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if err == nil || !errors.Is(err, ErrMediaOrigin) {
		t.Fatalf("tokenless cross-origin redirect error = %v, want ErrMediaOrigin", err)
	}
	if got := targetRequests.Load(); got != 0 {
		t.Fatalf("tokenless cross-origin redirect requests = %d, want 0", got)
	}
}

func TestGetAuthorizedWithTokenPreservesTokenlessHTTPRedirectCompatibility(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/final", http.StatusTemporaryRedirect)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	resp, err := New(server.Client(), nil).GetAuthorizedWithToken(context.Background(), server.URL+"/start", "")
	if err != nil {
		t.Fatalf("tokenless HTTP redirect error = %v, want compatibility success", err)
	}
	if resp == nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("tokenless HTTP redirect response = %#v, want 200 response", resp)
	}
	_ = resp.Body.Close() //nolint:errcheck
}

func TestGetAuthorizedWithTokenAttachesTokenToExactCallerOrigin(t *testing.T) {
	transport := &mediaOriginCaptureTransport{}
	c := New(&http.Client{Transport: transport}, nil)

	resp, err := c.GetAuthorizedWithToken(context.Background(), "https://api.example.test/subjects?keep=1", "bearer-secret")
	if err != nil {
		t.Fatalf("GetAuthorizedWithToken() error = %v", err)
	}
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if transport.request == nil {
		t.Fatal("request was not sent")
	}
	if got := transport.request.Header.Get("Authorization"); got != "Bearer bearer-secret" {
		t.Fatalf("Authorization = %q, want public API bearer contract", got)
	}
}

func TestGetAuthorizedWithTokenStripsBearerTokenQueryFromUnconfiguredOrigin(t *testing.T) {
	transport := &mediaOriginCaptureTransport{}
	c := New(&http.Client{Transport: transport}, nil)

	resp, err := c.GetAuthorizedWithTokenForOrigins(context.Background(), "https://media.example.test/segment.ts?token=bearer-secret&keep=1", "bearer-secret")
	if err != nil {
		t.Fatalf("GetAuthorizedWithToken() error = %v", err)
	}
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if transport.request == nil {
		t.Fatal("request was not sent")
	}
	if got := transport.request.URL.RawQuery; got != "keep=1" {
		t.Fatalf("request query = %q, want bearer token removed while preserving unrelated query", got)
	}
}

func TestGetAuthorizedWithTokenStripsBearerAcrossCredentialQueryAliases(t *testing.T) {
	for _, key := range []string{"auth", "authorization", "token", "access_token", "sig", "signature", "secret", "key", "api_key", "x-api-key", "x_api_key", "refresh_token", "client_secret", "password"} {
		t.Run(key, func(t *testing.T) {
			transport := &mediaOriginCaptureTransport{}
			c := New(&http.Client{Transport: transport}, nil)
			rawURL := "https://media.example.test/segment.ts?" + key + "=Bearer+bearer-secret&keep=1"
			resp, err := c.GetAuthorizedWithTokenForOrigins(context.Background(), rawURL, "bearer-secret")
			if err != nil {
				t.Fatalf("GetAuthorizedWithToken() error = %v", err)
			}
			if resp != nil {
				_ = resp.Body.Close() //nolint:errcheck
			}
			if transport.request == nil {
				t.Fatal("request was not sent")
			}
			if got := transport.request.Header.Get("Authorization"); got != "" {
				t.Fatalf("Authorization = %q, want no bearer token", got)
			}
			if got := transport.request.URL.RawQuery; got != "keep=1" {
				t.Fatalf("query = %q, want credential removed", got)
			}
		})
	}
}

func TestGetAuthorizedWithTokenFailsClosedForAmbiguousBearerQueryCredential(t *testing.T) {
	transport := &mediaOriginCaptureTransport{}
	c := New(&http.Client{Transport: transport}, nil)
	resp, err := c.GetAuthorizedWithTokenForOrigins(context.Background(), "https://media.example.test/segment.ts?sig=prefix-bearer-secret-suffix", "bearer-secret")
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if err != nil {
		t.Fatalf("GetAuthorizedWithToken() error = %v", err)
	}
	if transport.request == nil {
		t.Fatal("request was not sent")
	}
	if got := transport.request.URL.RawQuery; got != "" {
		t.Fatalf("query = %q, want ambiguous credential removed", got)
	}
}

func TestGetAuthorizedWithTokenAttachesTokenToConfiguredOrigin(t *testing.T) {
	transport := &mediaOriginCaptureTransport{}
	c := New(&http.Client{Transport: transport}, nil)
	resp, err := c.GetAuthorizedWithTokenForOrigins(context.Background(), "https://cdn.example.test/segment.ts", "bearer-secret", "https://cdn.example.test")
	if err != nil {
		t.Fatalf("GetAuthorizedWithToken() error = %v", err)
	}
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if transport.request == nil {
		t.Fatal("request was not sent")
	}
	if got := transport.request.Header.Get("Authorization"); got != "Bearer bearer-secret" {
		t.Fatalf("Authorization = %q, want configured bearer token", got)
	}
}

func TestGetAuthorizedWithTokenMatchesEquivalentIPv6Origins(t *testing.T) {
	transport := &mediaOriginCaptureTransport{}
	c := New(&http.Client{Transport: transport}, nil)
	resp, err := c.GetAuthorizedWithTokenForOrigins(
		context.Background(),
		"https://[0:0:0:0:0:0:0:1]/segment.ts",
		"bearer-secret",
		"https://[::1]:443",
	)
	if err != nil {
		t.Fatalf("GetAuthorizedWithTokenForOrigins() error = %v", err)
	}
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if transport.request == nil {
		t.Fatal("request was not sent")
	}
	if got := transport.request.Header.Get("Authorization"); got != "Bearer bearer-secret" {
		t.Fatalf("Authorization = %q, want configured bearer token", got)
	}
}

func TestMediaOriginKeyCanonicalizesIPv6AndDefaultPorts(t *testing.T) {
	tests := []struct {
		rawURL string
		want   string
	}{
		{rawURL: "https://[0:0:0:0:0:0:0:1]/segment.ts", want: "https://[::1]"},
		{rawURL: "https://[::1]:443/segment.ts", want: "https://[::1]"},
		{rawURL: "https://[::1]:0443/segment.ts", want: "https://[::1]"},
		{rawURL: "https://[2001:0db8:0:0:0:0:0:1]:443/segment.ts", want: "https://[2001:db8::1]"},
	}
	for _, tt := range tests {
		t.Run(tt.rawURL, func(t *testing.T) {
			parsed, err := parseRequestURL(tt.rawURL)
			if err != nil {
				t.Fatalf("parseRequestURL() error = %v", err)
			}
			got, err := mediaOriginKey(parsed)
			if err != nil {
				t.Fatalf("mediaOriginKey() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("mediaOriginKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetAuthorizedWithTokenStripsReservedEncodedBearerCredential(t *testing.T) {
	const token = "secret+a&b=c?d"
	transport := &mediaOriginCaptureTransport{}
	c := New(&http.Client{Transport: transport}, nil)
	rawURL := "https://media.example.test/segment.ts?%61uthorization=" + url.QueryEscape("Bearer "+token) + "&keep=" + url.QueryEscape("a/b")
	resp, err := c.GetAuthorizedWithTokenForOrigins(context.Background(), rawURL, token)
	if err != nil {
		t.Fatalf("GetAuthorizedWithToken() error = %v", err)
	}
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if transport.request == nil {
		t.Fatal("request was not sent")
	}
	if got := transport.request.URL.RawQuery; got != "keep=a%2Fb" {
		t.Fatalf("request query = %q, want encoded unrelated query only", got)
	}
}

func TestGetAuthorizedWithTokenOnlyFollowsConfiguredCrossOriginRedirect(t *testing.T) {
	const token = "redirect-bearer-secret"
	var sameOriginAuthorization string
	sameOrigin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/playlist.m3u8" {
			http.Redirect(w, r, "/final.m3u8", http.StatusFound)
			return
		}
		sameOriginAuthorization = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer sameOrigin.Close()

	c := New(sameOrigin.Client(), nil)
	resp, err := c.GetAuthorizedWithTokenForOrigins(context.Background(), sameOrigin.URL+"/playlist.m3u8", token, sameOrigin.URL)
	if err != nil {
		t.Fatalf("same-origin GetAuthorizedWithToken() error = %v", err)
	}
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if sameOriginAuthorization != "Bearer "+token {
		t.Fatalf("same-origin redirect Authorization = %q, want bearer token", sameOriginAuthorization)
	}

	var destinationAuthorization string
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destinationAuthorization = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer destination.Close()

	source := httptest.NewServer(http.RedirectHandler(destination.URL+"/segment.ts", http.StatusFound))
	defer source.Close()

	c = New(source.Client(), nil)
	resp, err = c.GetAuthorizedWithTokenForOrigins(context.Background(), source.URL+"/playlist.m3u8", token, source.URL, destination.URL)
	if err != nil {
		t.Fatalf("GetAuthorizedWithToken() error = %v", err)
	}
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if destinationAuthorization != "" {
		t.Fatalf("redirect Authorization = %q, want no bearer token across origins", destinationAuthorization)
	}

	var unauthorizedRequests int
	unauthorized := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		unauthorizedRequests++
		w.WriteHeader(http.StatusOK)
	}))
	defer unauthorized.Close()
	unauthorizedSource := httptest.NewServer(http.RedirectHandler(unauthorized.URL+"/segment.ts", http.StatusFound))
	defer unauthorizedSource.Close()

	c = New(unauthorizedSource.Client(), nil)
	resp, err = c.GetAuthorizedWithTokenForOrigins(context.Background(), unauthorizedSource.URL+"/playlist.m3u8", token, unauthorizedSource.URL)
	if err == nil || !errors.Is(err, ErrMediaOrigin) {
		t.Fatalf("unauthorized redirect error = %v, want ErrMediaOrigin", err)
	}
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if unauthorizedRequests != 0 {
		t.Fatalf("unauthorized redirect requests = %d, want 0", unauthorizedRequests)
	}
}

func TestGetAuthorizedWithTokenCannotReAddBearerOnCrossOriginRedirect(t *testing.T) {
	const token = "redirect-hook-secret"
	var receivedAuthorization string
	var receivedQuery string
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuthorization = r.Header.Get("Authorization")
		receivedQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer destination.Close()

	source := httptest.NewServer(http.RedirectHandler(destination.URL+"/segment.ts?authorization=Bearer+"+token, http.StatusFound))
	defer source.Close()
	httpClient := source.Client()
	httpClient.CheckRedirect = func(next *http.Request, _ []*http.Request) error {
		next.Header.Set("Authorization", "Bearer "+token)
		return nil
	}

	c := New(httpClient, nil)
	resp, err := c.GetAuthorizedWithTokenForOrigins(context.Background(), source.URL+"/playlist.m3u8", token, source.URL, destination.URL)
	if err != nil {
		t.Fatalf("GetAuthorizedWithTokenForOrigins() error = %v", err)
	}
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if receivedAuthorization != "" {
		t.Fatalf("cross-origin Authorization = %q, want empty", receivedAuthorization)
	}
	if receivedQuery != "" {
		t.Fatalf("cross-origin query = %q, want bearer credential removed", receivedQuery)
	}
}

func TestGetAuthorizedWithTokenRemovesCaseInsensitiveRedirectCredentialsAndReferer(t *testing.T) {
	const token = "case-insensitive-redirect-secret"
	var targetHeaders http.Header
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	source := httptest.NewServer(http.RedirectHandler(target.URL+"/segment.ts", http.StatusFound))
	defer source.Close()
	httpClient := source.Client()
	httpClient.CheckRedirect = func(next *http.Request, _ []*http.Request) error {
		next.Header["authorization"] = []string{"Bearer " + token}
		next.Header["pRoXy-AuThOrIzAtIoN"] = []string{"Bearer " + token}
		next.Header["rEfErEr"] = []string{"https://source.example.test/?token=" + token}
		next.Header["cookie"] = []string{"session=" + token}
		next.Header["CoOkIe2"] = []string{"session=" + token}
		return nil
	}

	c := New(httpClient, nil)
	resp, err := c.GetAuthorizedWithTokenForOrigins(context.Background(), source.URL+"/playlist.m3u8", token, source.URL, target.URL)
	if err != nil {
		t.Fatalf("GetAuthorizedWithTokenForOrigins() error = %v", err)
	}
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	for key, values := range targetHeaders {
		if strings.EqualFold(key, "Authorization") || strings.EqualFold(key, "Proxy-Authorization") || strings.EqualFold(key, "Referer") || strings.EqualFold(key, "Cookie") || strings.EqualFold(key, "Cookie2") {
			t.Fatalf("target received sensitive header %q: %v", key, values)
		}
		for _, value := range values {
			if strings.Contains(value, token) {
				t.Fatalf("target header %q leaked bearer token: %q", key, value)
			}
		}
	}
}

func TestGetAuthorizedWithTokenDoesNotCarryJarCookiesAcrossMediaRedirect(t *testing.T) {
	const token = "jar-cookie-secret"
	var targetCookie string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCookie = r.Header.Get("Cookie")
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	source := httptest.NewServer(http.RedirectHandler(target.URL+"/segment.ts", http.StatusFound))
	defer source.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New() error = %v", err)
	}
	targetURL, err := url.Parse(target.URL)
	if err != nil {
		t.Fatalf("url.Parse(target.URL) error = %v", err)
	}
	jar.SetCookies(targetURL, []*http.Cookie{{Name: "session", Value: token}})
	httpClient := source.Client()
	httpClient.Jar = jar
	c := New(httpClient, nil)
	resp, err := c.GetAuthorizedWithTokenForOrigins(context.Background(), source.URL+"/playlist.m3u8", token, source.URL, target.URL)
	if err != nil {
		t.Fatalf("GetAuthorizedWithTokenForOrigins() error = %v", err)
	}
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if targetCookie != "" {
		t.Fatalf("target Cookie = %q, want no jar cookie on media redirect", targetCookie)
	}
}

func TestGetAuthorizedWithTokenSanitizesRedirectHookViews(t *testing.T) {
	const token = "hook-via-secret"
	var targetRequests int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetRequests++
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	source := httptest.NewServer(http.RedirectHandler(target.URL+"/final?unknown="+token, http.StatusFound))
	defer source.Close()
	httpClient := source.Client()
	var hookSawCredential bool
	httpClient.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		hookSawCredential = requestViewContainsCredential(next, token)
		for _, request := range via {
			hookSawCredential = hookSawCredential || requestViewContainsCredential(request, token)
		}
		return nil
	}

	c := New(httpClient, nil)
	resp, err := c.GetAuthorizedWithTokenForOrigins(
		context.Background(),
		source.URL+"/start?unknown="+token,
		token,
		source.URL,
		target.URL,
	)
	if err != nil {
		t.Fatalf("GetAuthorizedWithTokenForOrigins() error = %v", err)
	}
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if hookSawCredential {
		t.Fatal("redirect hook observed bearer URL/header material through next or via")
	}
	if targetRequests != 1 {
		t.Fatalf("target requests = %d, want one allowed redirect", targetRequests)
	}
}

func TestSanitizedRedirectRequestHidesHostAndRequestURI(t *testing.T) {
	const token = "sanitized-request-secret"
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "https://source.example.test/start?unknown="+token, nil)
	request.Host = "Bearer " + token
	request.RequestURI = "/start?unknown=" + token

	safe := sanitizedRedirectRequest(request, token, false)
	if requestViewContainsCredential(safe, token) {
		t.Fatalf("sanitized redirect request leaked bearer material: host=%q requestURI=%q url=%q", safe.Host, safe.RequestURI, safe.URL)
	}
}

func TestGetAuthorizedWithTokenRejectsCredentialReintroducedInRedirectRequestFields(t *testing.T) {
	const token = "request-field-secret"
	encodedToken := percentEncodeASCII(token)
	for _, tc := range []struct {
		name string
		set  func(*http.Request)
	}{
		{name: "host", set: func(next *http.Request) { next.Host = "bEaReR " + token }},
		{name: "encoded host", set: func(next *http.Request) { next.Host = "vhost-" + encodedToken }},
		{name: "request URI", set: func(next *http.Request) { next.RequestURI = "/segment/" + encodedToken }},
		{name: "request URI query", set: func(next *http.Request) {
			next.RequestURI = "/segment.ts?authorization=Bearer+" + token
		}},
		{name: "header name", set: func(next *http.Request) {
			next.Header["X-"+token] = []string{"safe-value"}
		}},
		{name: "clean host mismatch", set: func(next *http.Request) {
			next.Host = "vhost.example.test"
		}},
		{name: "method", set: func(next *http.Request) { next.Method = http.MethodPost }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var targetRequests int
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				targetRequests++
				w.WriteHeader(http.StatusOK)
			}))
			defer target.Close()
			source := httptest.NewServer(http.RedirectHandler(target.URL+"/segment.ts", http.StatusFound))
			defer source.Close()
			httpClient := source.Client()
			httpClient.CheckRedirect = func(next *http.Request, _ []*http.Request) error {
				tc.set(next)
				return nil
			}

			c := New(httpClient, nil)
			resp, err := c.GetAuthorizedWithTokenForOrigins(context.Background(), source.URL+"/playlist.m3u8", token, source.URL, target.URL)
			if resp != nil {
				_ = resp.Body.Close() //nolint:errcheck
			}
			if err == nil || !errors.Is(err, ErrMediaOrigin) {
				t.Fatalf("redirect %s error = %v, want ErrMediaOrigin", tc.name, err)
			}
			if targetRequests != 0 {
				t.Fatalf("redirect %s target requests = %d, want 0", tc.name, targetRequests)
			}
		})
	}
}

func TestGetAuthorizedWithTokenCanonicalizesRedirectSchemeAfterHook(t *testing.T) {
	var targetRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetRequests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	source := httptest.NewServer(http.RedirectHandler(target.URL+"/segment.ts", http.StatusFound))
	defer source.Close()

	httpClient := source.Client()
	httpClient.CheckRedirect = func(next *http.Request, _ []*http.Request) error {
		next.URL.Scheme = strings.ToUpper(next.URL.Scheme)
		return nil
	}
	resp, err := New(httpClient, nil).GetAuthorizedWithTokenForOrigins(
		context.Background(), source.URL+"/start", "redirect-scheme-secret", source.URL, target.URL,
	)
	if err != nil {
		t.Fatalf("GetAuthorizedWithTokenForOrigins() error = %v, want canonicalized redirect success", err)
	}
	if resp == nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("response = %#v, want 200 response", resp)
	}
	_ = resp.Body.Close() //nolint:errcheck
	if got := targetRequests.Load(); got != 1 {
		t.Fatalf("target requests = %d, want one request", got)
	}
}

func TestGetAuthorizedWithTokenRejectsDoubleEncodedCredentialInRedirectHeader(t *testing.T) {
	const token = "double-header-secret"
	doubleEncoded := url.PathEscape(url.PathEscape(token))
	var targetRequests int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetRequests++
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	source := httptest.NewServer(http.RedirectHandler(target.URL+"/segment.ts", http.StatusFound))
	defer source.Close()
	httpClient := source.Client()
	httpClient.CheckRedirect = func(next *http.Request, _ []*http.Request) error {
		next.Header["x-tRaCe"] = []string{"Bearer " + doubleEncoded}
		return nil
	}

	c := New(httpClient, nil)
	resp, err := c.GetAuthorizedWithTokenForOrigins(context.Background(), source.URL+"/playlist.m3u8", token, source.URL, target.URL)
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if err == nil || !errors.Is(err, ErrMediaOrigin) {
		t.Fatalf("double-encoded redirect header error = %v, want ErrMediaOrigin", err)
	}
	if targetRequests != 0 {
		t.Fatalf("double-encoded redirect header target requests = %d, want 0", targetRequests)
	}
}

func TestGetAuthorizedWithTokenRejectsCredentialReintroducedInRedirectURLFields(t *testing.T) {
	const token = "url-field-secret"
	encodedToken := percentEncodeASCII(token)
	for _, field := range []string{"path", "raw path", "fragment", "raw fragment"} {
		t.Run(field, func(t *testing.T) {
			var targetRequests int
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				targetRequests++
				w.WriteHeader(http.StatusOK)
			}))
			defer target.Close()
			source := httptest.NewServer(http.RedirectHandler(target.URL+"/segment.ts", http.StatusFound))
			defer source.Close()
			httpClient := source.Client()
			httpClient.CheckRedirect = func(next *http.Request, _ []*http.Request) error {
				switch field {
				case "path":
					next.URL.Path = "/" + token
				case "raw path":
					next.URL.Path = "/" + token
					next.URL.RawPath = "/" + encodedToken
				case "fragment":
					next.URL.Fragment = token
				case "raw fragment":
					next.URL.Fragment = token
					next.URL.RawFragment = encodedToken
				}
				return nil
			}

			c := New(httpClient, nil)
			resp, err := c.GetAuthorizedWithTokenForOrigins(context.Background(), source.URL+"/playlist.m3u8", token, source.URL, target.URL)
			if resp != nil {
				_ = resp.Body.Close() //nolint:errcheck
			}
			if err == nil || !errors.Is(err, ErrMediaOrigin) {
				t.Fatalf("redirect with %s credential error = %v, want media-origin rejection", field, err)
			}
			if targetRequests != 0 {
				t.Fatalf("redirect with %s credential target requests = %d, want 0", field, targetRequests)
			}
		})
	}
}

func TestTokenlessCredentialQueryIsStrippedAcrossExplicitRedirect(t *testing.T) {
	const credential = "query-only-secret"
	var targetQuery string
	var targetAuthorization string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetQuery = r.URL.RawQuery
		targetAuthorization = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	source := httptest.NewServer(http.RedirectHandler(target.URL+"/segment.ts?token="+credential+"&keep=1", http.StatusFound))
	defer source.Close()

	c := New(source.Client(), nil)
	resp, err := c.GetAuthorizedWithTokenForOrigins(
		context.Background(),
		source.URL+"/start?token="+credential,
		"",
		source.URL,
		target.URL,
	)
	if err != nil {
		t.Fatalf("GetAuthorizedWithTokenForOrigins() error = %v", err)
	}
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if targetQuery != "keep=1" || targetAuthorization != "" {
		t.Fatalf("tokenless redirect target query=%q Authorization=%q, want keep=1 and no auth", targetQuery, targetAuthorization)
	}
}

func TestGetAuthorizedWithTokenStripsDoubleEncodedUnknownCredential(t *testing.T) {
	const token = "double encoded+secret"
	transport := &mediaOriginCaptureTransport{}
	c := New(&http.Client{Transport: transport}, nil)
	doubleEncoded := url.QueryEscape(url.QueryEscape(token))
	rawURL := "https://media.example.test/segment.ts?unknown=" + doubleEncoded + "&keep=1"
	resp, err := c.GetAuthorizedWithTokenForOrigins(context.Background(), rawURL, token)
	if err != nil {
		t.Fatalf("GetAuthorizedWithTokenForOrigins() error = %v", err)
	}
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if transport.request == nil {
		t.Fatal("request was not sent")
	}
	if got := transport.request.URL.RawQuery; got != "keep=1" {
		t.Fatalf("request query = %q, want double-encoded credential removed", got)
	}
}

func percentEncodeASCII(value string) string {
	var encoded strings.Builder
	for index := 0; index < len(value); index++ {
		const hex = "0123456789ABCDEF"
		encoded.WriteByte('%')
		encoded.WriteByte(hex[value[index]>>4])
		encoded.WriteByte(hex[value[index]&0x0f])
	}
	return encoded.String()
}

func TestSanitizedRedirectResponseRedactsStatusAndCookie2(t *testing.T) {
	const token = "redirect-response-secret"
	referer := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "https://source.example.test/start", nil)
	referer.Header["Cookie2"] = []string{"session=" + token}
	response := &http.Response{
		StatusCode: http.StatusFound,
		Status:     "302 token=" + token,
		Header: http.Header{
			"cOoKiE2":        []string{"session=" + token},
			"X-Trace":        []string{"Bearer " + token},
			"Content-Length": []string{"0"},
		},
		Request: referer,
		Body:    io.NopCloser(strings.NewReader("response body")),
	}

	safe := sanitizedRedirectResponse(response, token) //nolint:bodyclose // sanitized clone owns an inert replacement body
	if strings.Contains(safe.Status, token) {
		t.Fatalf("sanitized response status leaked token: %q", safe.Status)
	}
	if safe.Status != "302" {
		t.Fatalf("sanitized response status = %q, want numeric status", safe.Status)
	}
	for key, values := range safe.Header {
		if strings.EqualFold(key, "Cookie2") {
			t.Fatalf("sanitized response retained Cookie2: %v", values)
		}
		for _, value := range values {
			if strings.Contains(value, token) {
				t.Fatalf("sanitized response header %q leaked token: %q", key, value)
			}
		}
	}
	if safe.Request == nil || safe.Request.URL == nil {
		t.Fatal("sanitized response lost request metadata")
	}
	if strings.Contains(safe.Request.URL.String(), token) {
		t.Fatalf("sanitized response request URL leaked token: %q", safe.Request.URL)
	}
}

func requestViewContainsCredential(request *http.Request, token string) bool {
	if request == nil {
		return false
	}
	for _, value := range []string{request.Host, request.RequestURI} {
		if strings.Contains(value, token) {
			return true
		}
	}
	if request.URL != nil {
		for _, part := range []string{
			request.URL.String(),
			request.URL.Scheme,
			request.URL.Host,
			request.URL.Path,
			request.URL.RawPath,
			request.URL.RawQuery,
			request.URL.Fragment,
			request.URL.RawFragment,
			request.URL.Opaque,
		} {
			if strings.Contains(part, token) {
				return true
			}
		}
		if request.URL.User != nil && strings.Contains(request.URL.User.String(), token) {
			return true
		}
	}
	for _, values := range request.Header {
		for _, value := range values {
			if strings.Contains(value, token) {
				return true
			}
		}
	}
	if request.Response != nil {
		if strings.Contains(request.Response.Status, token) {
			return true
		}
		if request.Response.Request != nil && request.Response.Request != request {
			return requestViewContainsCredential(request.Response.Request, token)
		}
		for _, values := range request.Response.Header {
			for _, value := range values {
				if strings.Contains(value, token) {
					return true
				}
			}
		}
	}
	return false
}

func TestGetAuthorizedWithTokenRedactsUnknownQueryTokenFromRedirectError(t *testing.T) {
	const token = "redirect-error-secret+a&b"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/loop?unknown="+url.QueryEscape(token), http.StatusFound)
	}))
	defer server.Close()

	c := New(server.Client(), nil)
	resp, err := c.GetAuthorizedWithToken(context.Background(), server.URL+"/start", token)
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if err == nil {
		t.Fatal("GetAuthorizedWithToken() error = nil, want redirect loop failure")
	}
	for _, secret := range []string{token, strings.TrimPrefix(token, "Bearer "), url.QueryEscape(token)} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("redirect error leaked %q: %v", secret, err)
		}
	}
	if !strings.Contains(err.Error(), "REDACTED") {
		t.Fatalf("redirect error = %v, want redaction marker", err)
	}
}

func TestGetAuthorizedWithTokenRedactsOriginalTokenFromUnconfiguredHookError(t *testing.T) {
	const token = "unconfigured-hook-secret"
	server := httptest.NewServer(http.RedirectHandler("/final", http.StatusFound))
	defer server.Close()
	httpClient := server.Client()
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("custom redirect hook rejected unknown=" + token)
	}

	c := New(httpClient, nil)
	resp, err := c.GetAuthorizedWithTokenForOrigins(context.Background(), server.URL+"/start", token)
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if err == nil {
		t.Fatal("GetAuthorizedWithTokenForOrigins() error = nil, want hook failure")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("unconfigured hook error leaked original token: %v", err)
	}
	if !strings.Contains(err.Error(), "REDACTED") {
		t.Fatalf("unconfigured hook error = %v, want redaction marker", err)
	}
}

func TestGetAuthorizedWithTokenRedactsOriginalTokenFromUnconfiguredRedirectLoopLocation(t *testing.T) {
	const token = "unconfigured-location-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/loop?unknown="+url.QueryEscape(token), http.StatusFound)
	}))
	defer server.Close()

	c := New(server.Client(), nil)
	resp, err := c.GetAuthorizedWithTokenForOrigins(context.Background(), server.URL+"/start", token)
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if err == nil {
		t.Fatal("GetAuthorizedWithTokenForOrigins() error = nil, want redirect-loop failure")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("unconfigured redirect error leaked original token: %v", err)
	}
	if !strings.Contains(err.Error(), "REDACTED") {
		t.Fatalf("unconfigured redirect error = %v, want redaction marker", err)
	}
}

func TestGetAuthorizedWithTokenCannotReAddBearerOnSameOriginUnconfiguredRedirect(t *testing.T) {
	const token = "same-origin-hook-secret"
	var finalAuthorization string
	var finalQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/final?authorization=Bearer+"+token, http.StatusFound)
			return
		}
		finalAuthorization = r.Header.Get("Authorization")
		finalQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	httpClient := server.Client()
	httpClient.CheckRedirect = func(next *http.Request, _ []*http.Request) error {
		next.Header.Set("Authorization", "Bearer "+token)
		return nil
	}

	c := New(httpClient, nil)
	resp, err := c.GetAuthorizedWithTokenForOrigins(context.Background(), server.URL+"/start", token)
	if err != nil {
		t.Fatalf("GetAuthorizedWithTokenForOrigins() error = %v", err)
	}
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if finalAuthorization != "" {
		t.Fatalf("same-origin unconfigured Authorization = %q, want empty", finalAuthorization)
	}
	if finalQuery != "" {
		t.Fatalf("same-origin unconfigured query = %q, want bearer credential removed", finalQuery)
	}
}

func TestGetAuthorizedWithTokenPreservesDefaultRedirectLimit(t *testing.T) {
	var redirects atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirects.Add(1)
		http.Redirect(w, r, r.URL.Path, http.StatusFound)
	}))
	defer server.Close()

	c := New(server.Client(), nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := c.GetAuthorizedWithTokenForOrigins(ctx, server.URL+"/loop", "bearer-secret", server.URL)
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if err == nil || !strings.Contains(err.Error(), "stopped after 10 redirects") {
		t.Fatalf("redirect-loop error = %v, want Go's 10-redirect limit", err)
	}
	if got := redirects.Load(); got != 10 {
		t.Fatalf("redirect requests = %d, want 10", got)
	}
}

func TestGetAuthorizedWithTokenPolicyIsRequestScoped(t *testing.T) {
	transport := newConcurrentMediaOriginTransport()
	c := New(&http.Client{Transport: transport}, nil)
	origins := []string{"https://api-one.example.test", "https://api-two.example.test"}
	var wait sync.WaitGroup
	errs := make(chan error, len(origins))
	for _, origin := range origins {
		wait.Add(1)
		go func() {
			defer wait.Done()
			resp, err := c.GetAuthorizedWithTokenForOrigins(context.Background(), origin+"/subjects", "bearer-secret", origin)
			if resp != nil {
				_ = resp.Body.Close() //nolint:errcheck
			}
			errs <- err
		}()
	}
	<-transport.bothStarted
	close(transport.release)
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("request error = %v", err)
		}
	}
	if len(transport.requests) != len(origins) {
		t.Fatalf("requests = %d, want %d", len(transport.requests), len(origins))
	}
	for _, request := range transport.requests {
		if got := request.Header.Get("Authorization"); got != "Bearer bearer-secret" {
			t.Fatalf("Authorization for %s = %q, want bearer token", request.URL, got)
		}
	}
}

func TestGetPlaylistResolvesReferencesAgainstFinalRedirectURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start/playlist.m3u8" {
			http.Redirect(w, r, "/final/playlist.m3u8", http.StatusFound)
			return
		}
		if r.URL.Path != "/final/playlist.m3u8" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"keys/key.bin\"\n#EXTINF:1,\nsegments/segment.ts\n") //nolint:errcheck
	}))
	defer server.Close()

	c := New(server.Client(), nil)
	playlist, err := c.getPlaylist(context.Background(), server.URL+"/start/playlist.m3u8", "bearer-secret", Lecture{TTID: 42, Topic: "redirected"})
	if err != nil {
		t.Fatalf("getPlaylist() error = %v", err)
	}
	if playlist.KeyURL != server.URL+"/final/keys/key.bin" {
		t.Fatalf("KeyURL = %q, want final redirect base", playlist.KeyURL)
	}
	if len(playlist.FirstViewURLs) != 1 || playlist.FirstViewURLs[0] != server.URL+"/final/segments/segment.ts" {
		t.Fatalf("FirstViewURLs = %#v, want final redirect base", playlist.FirstViewURLs)
	}
}

type concurrentMediaOriginTransport struct {
	mu          sync.Mutex
	requests    []*http.Request
	bothStarted chan struct{}
	release     chan struct{}
	started     atomic.Int32
}

func newConcurrentMediaOriginTransport() *concurrentMediaOriginTransport {
	return &concurrentMediaOriginTransport{
		bothStarted: make(chan struct{}),
		release:     make(chan struct{}),
	}
}

func (t *concurrentMediaOriginTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.requests = append(t.requests, req.Clone(req.Context()))
	t.mu.Unlock()
	if t.started.Add(1) == 2 {
		close(t.bothStarted)
	}
	<-t.release
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header), Request: req}, nil
}

func TestGetPlaylistsAllowsConfiguredCDNOrigin(t *testing.T) {
	const token = "configured-cdn-secret"
	transport := &mediaOriginRoutingTransport{responses: map[string]string{
		"https://api.example.test/fetchvideo":           "https://cdn.example.test/1280x720/master.m3u8\n",
		"https://cdn.example.test/1280x720/master.m3u8": "#EXTM3U\n#EXTINF:1,\nsegment.ts\n",
	}}
	c := New(&http.Client{Transport: transport}, nil)
	playlists, err := c.GetPlaylists(context.Background(), &config.Config{
		BaseURL:      "https://api.example.test",
		MediaOrigins: []string{"https://cdn.example.test"},
		Quality:      "720",
		Token:        token,
	}, Lectures{{TTID: 42, Topic: "Configured CDN"}})
	if err != nil {
		t.Fatalf("GetPlaylists() error = %v", err)
	}
	if len(playlists) != 1 || playlists[0].FirstViewURLs[0] != "https://cdn.example.test/1280x720/segment.ts" {
		t.Fatalf("GetPlaylists() = %+v, want configured CDN playlist", playlists)
	}
	for _, request := range transport.requests {
		if got := request.Header.Get("Authorization"); got != "Bearer "+token {
			t.Fatalf("Authorization for %s = %q, want configured bearer token", request.URL, got)
		}
	}
}

type mediaOriginRoutingTransport struct {
	responses map[string]string
	requests  []*http.Request
}

func (t *mediaOriginRoutingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.requests = append(t.requests, req.Clone(req.Context()))
	responseBody, ok := t.responses[fmt.Sprintf("%s://%s%s", req.URL.Scheme, req.URL.Host, req.URL.Path)]
	if !ok {
		return nil, fmt.Errorf("unexpected request %s", req.URL)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(responseBody)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}
