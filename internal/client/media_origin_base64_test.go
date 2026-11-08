package client

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// An unconfigured media origin must never receive the bearer, including its
// base64 and base64url spellings (#247). The token is a fake placeholder.
func TestGetAuthorizedWithTokenWithholdsBase64BearerFromUnconfiguredOrigin(t *testing.T) {
	const token = "secret-bearer-abc123"
	const signing = "Expires=1700000000&Signature=fake-cdn-signature"
	for _, encoding := range []struct {
		name     string
		encoding *base64.Encoding
	}{
		{"std padded", base64.StdEncoding},
		{"std raw", base64.RawStdEncoding},
		{"url padded", base64.URLEncoding},
		{"url raw", base64.RawURLEncoding},
	} {
		for _, source := range []string{token, "Bearer " + token} {
			encoded := encoding.encoding.EncodeToString([]byte(source))
			t.Run(encoding.name+"/query/"+source[:6], func(t *testing.T) {
				transport := &mediaOriginCaptureTransport{}
				c := New(&http.Client{Transport: transport}, nil)
				rawURL := "https://media.example.test/seg.ts?" + signing + "&x=" + url.QueryEscape(encoded) + "&keep=1"
				resp, err := c.GetAuthorizedWithTokenForOrigins(context.Background(), rawURL, token, "https://api.example.test")
				if resp != nil {
					_ = resp.Body.Close() //nolint:errcheck
				}
				if err != nil {
					t.Fatalf("GetAuthorizedWithTokenForOrigins() error = %v", err)
				}
				if transport.request == nil {
					t.Fatal("request was not sent")
				}
				if got := transport.request.Header.Get("Authorization"); got != "" {
					t.Fatalf("Authorization = %q, want no bearer token", got)
				}
				if got, want := transport.request.URL.RawQuery, signing+"&keep=1"; got != want {
					t.Fatalf("request query = %q, want %q", got, want)
				}
			})
			t.Run(encoding.name+"/path/"+source[:6], func(t *testing.T) {
				transport := &mediaOriginCaptureTransport{}
				c := New(&http.Client{Transport: transport}, nil)
				rawURL := "https://media.example.test/" + url.PathEscape(encoded) + "/seg.ts"
				resp, err := c.GetAuthorizedWithTokenForOrigins(context.Background(), rawURL, token, "https://api.example.test")
				if resp != nil {
					_ = resp.Body.Close() //nolint:errcheck
				}
				if err == nil || !errors.Is(err, ErrMediaOrigin) {
					t.Fatalf("GetAuthorizedWithTokenForOrigins(%q) error = %v, want ErrMediaOrigin", rawURL, err)
				}
				if strings.Contains(err.Error(), encoded) {
					t.Fatalf("error kept the encoded bearer: %v", err)
				}
				if transport.request != nil {
					t.Fatal("unconfigured URL carrying the encoded bearer was sent")
				}
			})
		}
	}
}
