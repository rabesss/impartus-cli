package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

const largeURLTestToken = "secret-bearer-abc123"

func TestContainsTokenRepresentationIgnoresSizeAlone(t *testing.T) {
	long := strings.Repeat("a", 70*1024)
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "large without token", value: "/" + long, want: false},
		{name: "large with token", value: "/" + long + "/" + largeURLTestToken, want: true},
		{name: "large with percent-encoded token", value: "/" + long + "/" + url.PathEscape("x/"+largeURLTestToken) + "%2F" + strings.ReplaceAll(largeURLTestToken, "b", "%62"), want: true},
		{name: "large with bearer prefix", value: long + "Bearer " + largeURLTestToken, want: true},
		{name: "small credential assignment", value: "/seg?access_token=unrelated-credential", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsTokenRepresentation(tt.value, largeURLTestToken); got != tt.want {
				t.Fatalf("containsTokenRepresentation() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestQueryCarriesBearerIgnoresSizeAlone(t *testing.T) {
	long := strings.Repeat("a", 70*1024)
	if queryCarriesBearer("p="+long, largeURLTestToken) {
		t.Fatal("queryCarriesBearer(70 KiB without token) = true, want false")
	}
	if !queryCarriesBearer("p="+long+url.QueryEscape(largeURLTestToken), largeURLTestToken) {
		t.Fatal("queryCarriesBearer(70 KiB with token) = false, want true")
	}
}

func TestGetAuthorizedForwardsLargeURLComponentsToUnconfiguredOrigin(t *testing.T) {
	long := strings.Repeat("a", 70*1024)
	seen := make(chan *http.Request, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case seen <- r.Clone(context.Background()):
		default:
		}
	}))
	server.Config.MaxHeaderBytes = 1 << 20
	server.Start()
	defer server.Close()

	tests := []struct {
		name      string
		rawURL    string
		wantPath  string
		wantQuery string
	}{
		{name: "long path", rawURL: server.URL + "/" + long + "/seg.ts", wantPath: "/" + long + "/seg.ts"},
		{name: "long query parameter", rawURL: server.URL + "/seg.ts?p=" + long, wantPath: "/seg.ts", wantQuery: "p=" + long},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := New(server.Client(), nil).GetAuthorizedWithTokenForOrigins(context.Background(), tt.rawURL, largeURLTestToken, "https://api.example.test")
			if err != nil {
				t.Fatalf("GetAuthorizedWithTokenForOrigins() error = %v", err)
			}
			_ = resp.Body.Close() //nolint:errcheck
			var got *http.Request
			select {
			case got = <-seen:
			default:
				t.Fatal("request was not sent")
			}
			if got.URL.Path != tt.wantPath || got.URL.RawQuery != tt.wantQuery {
				t.Fatalf("server saw path %d bytes, query %d bytes; want %d and %d", len(got.URL.Path), len(got.URL.RawQuery), len(tt.wantPath), len(tt.wantQuery))
			}
			if got.Header.Get("Authorization") != "" {
				t.Fatal("unconfigured origin received Authorization")
			}
		})
	}
}

func TestGetAuthorizedStillRefusesLargePathCarryingToken(t *testing.T) {
	transport := &mediaOriginCaptureTransport{}
	rawURL := "https://media.example.test/" + strings.Repeat("a", 70*1024) + "/" + largeURLTestToken + "/seg.ts"
	resp, err := New(&http.Client{Transport: transport}, nil).GetAuthorizedWithTokenForOrigins(context.Background(), rawURL, largeURLTestToken, "https://api.example.test")
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if err == nil {
		t.Fatal("GetAuthorizedWithTokenForOrigins() error = nil, want a refusal for a token-carrying path")
	}
	if transport.request != nil {
		t.Fatal("token-carrying path was sent")
	}
}

func TestGetAuthorizedRefusesLargePathCarryingMarkerSpelledToken(t *testing.T) {
	// A bearer spelled like the redaction marker must still be detected:
	// replacing it with the marker leaves the text unchanged.
	const token = "REDACTED"
	var sent atomic.Bool
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { sent.Store(true) }))
	server.Config.MaxHeaderBytes = 1 << 20
	server.Start()
	defer server.Close()

	rawURL := server.URL + "/" + strings.Repeat("a", 70*1024) + "/" + token + "/seg.ts"
	resp, err := New(server.Client(), nil).GetAuthorizedWithTokenForOrigins(context.Background(), rawURL, token, "https://api.example.test")
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if err == nil || sent.Load() {
		t.Fatalf("GetAuthorizedWithTokenForOrigins() error = %v, sent = %v; want a refusal before sending", err, sent.Load())
	}
	if !containsTokenRepresentation("/"+strings.Repeat("a", 70*1024)+"/"+token, token) {
		t.Fatal("containsTokenRepresentation(marker-spelled token) = false")
	}
}
