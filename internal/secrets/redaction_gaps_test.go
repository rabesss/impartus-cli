package secrets

import (
	"encoding/base64"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Every credential below is a fake placeholder. The tables pin the gaps from
// issues #247, #249, #256, #257 and #260.

func TestTokenAwareHelpersRecognizeBase64Bearer(t *testing.T) {
	t.Parallel()

	const token = "secret-bearer-abc123"
	encodings := []struct {
		name     string
		encoding *base64.Encoding
	}{
		{"std padded", base64.StdEncoding},
		{"std raw", base64.RawStdEncoding},
		{"url padded", base64.URLEncoding},
		{"url raw", base64.RawURLEncoding},
	}
	for _, source := range []struct{ name, value string }{
		{"token", token},
		{"bearer prefixed", "Bearer " + token},
	} {
		for _, encoding := range encodings {
			encoded := encoding.encoding.EncodeToString([]byte(source.value))
			for _, position := range []struct{ name, url string }{
				{"query", "https://cdn.example.test/seg.ts?x=" + url.QueryEscape(encoded)},
				{"raw query", "https://cdn.example.test/seg.ts?x=" + encoded},
				{"path", "https://cdn.example.test/" + url.PathEscape(encoded) + "/seg.ts"},
			} {
				name := source.name + "/" + encoding.name + "/" + position.name
				t.Run(name, func(t *testing.T) {
					if !ContainsToken(position.url, token) {
						t.Fatalf("ContainsToken(%q) = false", position.url)
					}
					for _, view := range []struct{ name, got string }{
						{"RedactURLWithToken", RedactURLWithToken(position.url, token)},
						{"RedactWithToken", RedactWithToken("GET "+position.url+" failed", token)},
					} {
						if strings.Contains(view.got, encoded) || strings.Contains(view.got, url.QueryEscape(encoded)) {
							t.Fatalf("%s kept the encoded bearer: %q", view.name, view.got)
						}
						if !strings.Contains(view.got, "REDACTED") {
							t.Fatalf("%s = %q, want a REDACTED marker", view.name, view.got)
						}
					}
				})
			}
		}
	}
}

func TestTokenAwareHelpersKeepUnrelatedBase64(t *testing.T) {
	t.Parallel()

	const token = "secret-bearer-abc123"
	unrelated := base64.StdEncoding.EncodeToString([]byte("unrelated-value-xyz"))
	value := "https://cdn.example.test/seg.ts?x=" + unrelated
	if ContainsToken(value, token) {
		t.Fatalf("ContainsToken(%q) = true for an unrelated base64 value", value)
	}
	if got, want := RedactURLWithToken(value, token), RedactURL(value); got != want {
		t.Fatalf("RedactURLWithToken(%q) = %q, want %q", value, got, want)
	}
}

func TestScrubSeparatorlessAndCompoundKeys(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		input string
		want  string
	}{
		{"Bearer secret-abc123", "Bearer REDACTED"},
		{"request failed: bearer secret-abc123 rejected", "request failed: bearer REDACTED rejected"},
		{"Authorization Bearer secret-abc123", "Authorization REDACTED"},
		{"Proxy-Authorization Basic c2VjcmV0LWFiYzEyMw==", "Proxy-Authorization REDACTED"},
		{"x-api-key secret-abc123, status=401", "x-api-key REDACTED, status=401"},
		{"X-Api-Key secret-abc123", "X-Api-Key REDACTED"},
		{"headers: Cookie session=secret-abc123", "headers: Cookie REDACTED"},
		{"Cookie2: session=secret-abc123", "Cookie2: REDACTED"},
		{"Set-Cookie2: session=secret-abc123", "Set-Cookie2: REDACTED"},
		{"my_api_key=secret-abc123", "my_api_key=REDACTED"},
		{"private_key=secret-abc123", "private_key=REDACTED"},
		{"client-secret-key: secret-abc123", "client-secret-key: REDACTED"},
		{"Authorization: Bearer secret-abc123", "Authorization: REDACTED"},
		{"Cookie: session=secret-abc123", "Cookie: REDACTED"},
		// Non-credential *_key names and prose stay readable.
		{"cache_key=lecture-42 sort_key=title", "cache_key=lecture-42 sort_key=title"},
		{"bearer token missing", "bearer token missing"},
		{"api-key required", "api-key required"},
		// Short alphabetic credentials still fail closed.
		{"Bearer huntertwo", "Bearer REDACTED"},
		{"X-Api-Key huntertwo", "X-Api-Key REDACTED"},
		{"Bearer REDACTED", "Bearer REDACTED"},
		{"cookie consent banner dismissed", "cookie consent banner dismissed"},
		{"authorization failed for lecture 42", "authorization failed for lecture 42"},
		{"error: api-key missing", "error: api-key missing"},
		{"the doctor says it contains a bearer token", "the doctor says it contains a bearer token"},
	} {
		t.Run(test.input, func(t *testing.T) {
			if got := Scrub(test.input); got != test.want {
				t.Fatalf("Scrub(%q) = %q, want %q", test.input, got, test.want)
			}
			if test.want == test.input {
				return
			}
			secret := "secret-abc123"
			if !strings.Contains(test.input, secret) {
				secret = "huntertwo"
			}
			wrapped := SanitizeError(errors.New("upstream: " + test.input)).Error()
			if strings.Contains(wrapped, secret) {
				t.Fatalf("SanitizeError kept the credential: %q", wrapped)
			}
			// Wrapped, percent-encoded and JSON-encoded views redact the value
			// and record evidence for it.
			for _, view := range []string{
				"headers: " + test.input,
				"next=" + url.PathEscape(test.input),
				`{"error":` + strconv.Quote(test.input) + `}`,
			} {
				got, evidence := ScrubWithEvidence(view)
				if strings.Contains(got, secret) || evidence.Count() == 0 {
					t.Fatalf("ScrubWithEvidence(%q) = %q with %d evidence values", view, got, evidence.Count())
				}
			}
		})
	}
}

func TestScrubSensitiveParamsCoverCookie2(t *testing.T) {
	t.Parallel()

	for _, rawURL := range []string{
		"https://api.example.test/x?cookie2=secret-abc123&keep=1",
		"https://api.example.test/x?set-cookie2=secret-abc123&keep=1",
	} {
		got := RedactURL(rawURL)
		if strings.Contains(got, "secret-abc123") || !strings.Contains(got, "keep=1") {
			t.Fatalf("RedactURL(%q) = %q", rawURL, got)
		}
	}
}

func TestScrubPercentEncodedQuotedValueAfterNonASCIIPrefix(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		input string
		want  string
	}{
		{"%C3%A9token%3D%22secret-one%20secret-two%22", "%C3%A9token%3D%22REDACTED%22"},
		{"%C3%A9password%3D%22secret-one%20secret-two%22", "%C3%A9password%3D%22REDACTED%22"},
		{"%C3%A9token%3Dsecret-one", "%C3%A9token%3DREDACTED"},
		{"xtoken%3D%22secret-one%20secret-two%22", "xtoken%3D%22REDACTED%22"},
		{`étoken="secret-one secret-two"`, ""},
		{`étoken='secret-one secret-two' keep=1`, ""},
		// A nested assignment inside the quoted value does not stand in for
		// the outer value.
		{"%C3%A9token%3D%22password%3Dsecret-one%20secret-two%22", "%C3%A9token%3D%22REDACTED%22"},
	} {
		t.Run(test.input, func(t *testing.T) {
			got := Scrub(test.input)
			if strings.Contains(got, "secret-one") || strings.Contains(got, "secret-two") {
				t.Fatalf("Scrub(%q) = %q, kept a credential word", test.input, got)
			}
			if test.want != "" && got != test.want {
				t.Fatalf("Scrub(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestScrubEscapedAndNestedQuotedAssignments(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		input string
		want  string
	}{
		{`password=\"secret-one secret-two\"`, `password=\"REDACTED\"`},
		{`{"msg":"password=\"secret-one secret-two\" next"}`, `{"msg":"password=\"REDACTED\" next"}`},
		{`x-auth-token: \"secret-a secret-b\" tail-visible`, `x-auth-token: \"REDACTED\" tail-visible`},
		{`password=abc"sig="secret-x secret-y"`, ""},
		{`password=\'secret-one secret-two\' keep=1`, `password=\'REDACTED\' keep=1`},
		{`{"msg":"token: \"secret-a secret-b\", status=401"}`, `{"msg":"token: \"REDACTED\", status=401"}`},
	} {
		t.Run(test.input, func(t *testing.T) {
			got := Scrub(test.input)
			for _, secret := range []string{"secret-one", "secret-two", "secret-a", "secret-b", "secret-x", "secret-y"} {
				if strings.Contains(got, secret) {
					t.Fatalf("Scrub(%q) = %q, kept %q", test.input, got, secret)
				}
			}
			if test.want != "" && got != test.want {
				t.Fatalf("Scrub(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestScrubQuotedStrongValuesKeepTheRestOfTheLine(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		input string
		want  string
	}{
		{`cookie="secret-a secret-b" keep=1 visible`, `cookie="REDACTED" keep=1 visible`},
		{`authorization="secret-c secret-d" keep=1 visible`, `authorization="REDACTED" keep=1 visible`},
		{`Cookie: "secret-a secret-b" status=401`, `Cookie: "REDACTED" status=401`},
		{`set-cookie='secret-a secret-b' keep=1 visible`, `set-cookie='REDACTED' keep=1 visible`},
		{
			`password="secret-a secret-b"authorization="secret-c secret-d" keep=1 visible`,
			`password="REDACTED"authorization="REDACTED" keep=1 visible`,
		},
		{`authorization="Bearer secret-c" keep=1 visible`, `authorization="REDACTED" keep=1 visible`},
		{`password="secret-a secret-b" keep=1 visible`, `password="REDACTED" keep=1 visible`},
		// Controls: unquoted strong values keep running to the end of the line,
		// and text glued to a closing quote stays redacted.
		{`password="secret-a"secret-b keep=1`, `password=REDACTED keep=1`},
		{`cookie: session=secret-a; theme=dark`, `cookie: REDACTED`},
		{`authorization: Custom secret-c secret-d`, `authorization: REDACTED`},
	} {
		t.Run(test.input, func(t *testing.T) {
			got := Scrub(test.input)
			for _, secret := range []string{"secret-a", "secret-b", "secret-c", "secret-d"} {
				if strings.Contains(got, secret) {
					t.Fatalf("Scrub(%q) = %q, kept %q", test.input, got, secret)
				}
			}
			if got != test.want {
				t.Fatalf("Scrub(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestScrubRejectedSeparatorlessHeadersStayLinear(t *testing.T) {
	// Rejected separator-less rules resume inside their value. Many of them in
	// one input, each opening a quote that never closes, must not rescan the
	// rest of the input every time. Compare against a benign input of the same
	// size and shape whose keys do not match any rule.
	build := func(separator, key string) string {
		var builder strings.Builder
		for builder.Len() < maxScrubInputSize/8 {
			builder.WriteString(separator + key + ` "plain words`)
		}
		return builder.String()
	}
	fastest := func(input string) time.Duration {
		best := time.Duration(1<<63 - 1)
		for range 2 {
			start := time.Now()
			_ = Scrub(input)
			best = min(best, time.Since(start))
		}
		return best
	}
	for _, separator := range []string{"\n", ",", ";"} {
		for _, key := range []string{"authorization", "cookie", "x-api-key"} {
			t.Run(strconv.Quote(separator)+"/"+key, func(t *testing.T) {
				hostile := fastest(build(separator, key))
				benign := fastest(build(separator, "xyzzy"))
				if limit := 20*benign + 150*time.Millisecond; hostile > limit {
					t.Fatalf("Scrub took %v on hostile input, want at most %v (benign input took %v)", hostile, limit, benign)
				}
			})
		}
	}
}
