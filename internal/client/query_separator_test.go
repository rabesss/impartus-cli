package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestStripBearerTokenQueryKeepsSeparatorsAndOrder(t *testing.T) {
	const token = "issue-303-placeholder-bearer"
	tests := []struct {
		name, raw, want string
	}{
		{"semicolons kept", "https://media.example.test/a.ts?z=1;token=x;b=2&a=3", "https://media.example.test/a.ts?z=1;b=2&a=3"},
		{"first removed", "https://media.example.test/a.ts?token=x;z=1&b=2", "https://media.example.test/a.ts?z=1&b=2"},
		{"every value of a bearer key removed", "https://media.example.test/a.ts?k=1;v=" + token + ";k=2;v=other", "https://media.example.test/a.ts?k=1;k=2"},
		{"escapes kept byte for byte", "https://media.example.test/a.ts?v=a%2Fb+c;auth=x;e=%7E", "https://media.example.test/a.ts?v=a%2Fb+c;e=%7E"},
		{"encoded credential key", "https://media.example.test/a.ts?keep=1;acc%65ss_token=x", "https://media.example.test/a.ts?keep=1"},
		{"all removed", "https://media.example.test/a.ts?token=x;auth=y", "https://media.example.test/a.ts"},
		{"nothing removed is unchanged", "https://media.example.test/a.ts?z=1;a=2", "https://media.example.test/a.ts?z=1;a=2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := stripBearerTokenQuery(tt.raw, token)
			if err != nil {
				t.Fatalf("stripBearerTokenQuery() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("stripBearerTokenQuery() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTokenlessConfiguredOriginKeepsSemicolonQuery(t *testing.T) {
	transport := &mediaOriginCaptureTransport{}
	c := New(&http.Client{Transport: transport}, nil)
	rawURL := "https://media.example.test/segment.ts?z=1;token=query-only-secret;b=2&a=3"
	resp, err := c.GetAuthorizedWithTokenForOrigins(context.Background(), rawURL, "", "https://media.example.test")
	if err != nil {
		t.Fatalf("GetAuthorizedWithTokenForOrigins() error = %v", err)
	}
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if transport.request == nil {
		t.Fatal("request was not sent")
	}
	if got := transport.request.URL.RawQuery; got != "z=1;b=2&a=3" {
		t.Fatalf("request query = %q, want the separators and order kept", got)
	}
}

func TestSanitizedRedirectURLKeepsSemicolonQuery(t *testing.T) {
	raw, err := url.Parse("https://media.example.test/next.ts?z=1;access_token=x;b=2")
	if err != nil {
		t.Fatal(err)
	}
	if got := sanitizedRedirectURL(raw, "issue-303-placeholder-bearer").String(); got != "https://media.example.test/next.ts?z=1;b=2" {
		t.Fatalf("sanitizedRedirectURL() = %q, want the separators kept", got)
	}
}

func TestMalformedMediaQueryHasSpecificError(t *testing.T) {
	const raw = "https://media.example.test/a.ts?v=1;x=%zz"
	check := func(name string, err error) {
		t.Helper()
		if !errors.Is(err, ErrInvalidMediaQuery) || !errors.Is(err, ErrInvalidMediaURL) || !errors.Is(err, ErrMediaOrigin) {
			t.Fatalf("%s error = %v, want ErrInvalidMediaQuery matching ErrInvalidMediaURL and ErrMediaOrigin", name, err)
		}
		if err.Error() != ErrInvalidMediaQuery.Error() || strings.Contains(err.Error(), "%zz") {
			t.Fatalf("%s error = %q, want the fixed query message", name, err)
		}
	}
	_, err := stripBearerTokenQuery(raw, "token")
	check("stripBearerTokenQuery", err)
	_, err = stripBearerQueryParams(raw, "token")
	check("stripBearerQueryParams", err)
	_, err = parseRequestURL(raw)
	check("parseRequestURL", err)
	check("ValidateMediaOrigins", ValidateMediaOrigins(raw))

	if _, err := parseRequestURL("relative/a.ts?x=%zz"); errors.Is(err, ErrInvalidMediaQuery) || !errors.Is(err, ErrInvalidMediaURL) {
		t.Fatalf("parseRequestURL(relative) error = %v, want only ErrInvalidMediaURL", err)
	}
}

// interleavedBearerSpelling percent-encodes a double JSON-escaped bearer: each
// byte becomes \\u00XX and then every byte of that is percent-escaped.
func interleavedBearerSpelling(token string) string {
	var escaped strings.Builder
	for i := 0; i < len(token); i++ {
		fmt.Fprintf(&escaped, "\\\\u%04x", token[i])
	}
	var out strings.Builder
	for i := 0; i < escaped.Len(); i++ {
		fmt.Fprintf(&out, "%%%02X", escaped.String()[i])
	}
	return out.String()
}

func TestInterleavedBearerSpellingIsFoundInPathAndRedirect(t *testing.T) {
	const token = "issue-303-interleaved-bearer"
	spelling := interleavedBearerSpelling(token)
	path := "/media/" + spelling + "/a.ts"
	if !containsTokenRepresentation(path, token) {
		t.Fatalf("containsTokenRepresentation(%q) = false, want true", path)
	}
	destination := &url.URL{Scheme: "https", Host: "media.example.test", Path: "/media/x/a.ts", RawPath: path}
	if err := validateRedirectCredentialBoundary(destination, token, true); !errors.Is(err, ErrMediaOrigin) {
		t.Fatalf("validateRedirectCredentialBoundary() error = %v, want ErrMediaOrigin", err)
	}
	if containsTokenRepresentation("/media/"+interleavedBearerSpelling("unrelated-path-value")+"/a.ts", token) {
		t.Fatal("containsTokenRepresentation() flagged an unrelated interleaved spelling")
	}
}
