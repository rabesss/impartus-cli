package secrets

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestRedactURL_RedactsKnownSensitiveParams(t *testing.T) {
	cases := []string{
		"https://host/fetchvideo?ttid=1&token=secret&type=index.m3u8",
		"https://host/path?access_token=abc&keep=1",
		"https://host/path?signature=deadbeef",
		"https://host/path?api_key=k&KEY=K",
	}
	for _, in := range cases {
		got := RedactURL(in)
		// Multi-char secret values must never survive redaction.
		for _, secret := range []string{"secret", "abc", "deadbeef"} {
			if strings.Contains(got, secret) {
				t.Errorf("RedactURL(%q) leaked secret %q: %s", in, secret, got)
			}
		}
		if !strings.Contains(got, "REDACTED") {
			t.Errorf("RedactURL(%q) should contain REDACTED, got %q", in, got)
		}
	}
}

func TestRedactURL_Passthrough(t *testing.T) {
	// Empty input is returned unchanged; a URL with no sensitive params keeps
	// its (non-secret) query values intact.
	if got := RedactURL(""); got != "" {
		t.Errorf("RedactURL(\"\") = %q, want empty", got)
	}
	got := RedactURL("https://host/path?keep=1&other=2")
	if !strings.Contains(got, "keep=1") || !strings.Contains(got, "other=2") {
		t.Errorf("RedactURL should preserve non-sensitive params, got %q", got)
	}
}

// TestSanitizeError_ReplacesTokenInURLError is the core regression guard: an
// error from http.Client.Do embeds the full request URL (with query token) in a
// *url.Error; it must not survive sanitization.
func TestSanitizeError_ReplacesTokenInURLError(t *testing.T) {
	const secret = "supersecret-token"
	rawURL := "https://upstream/fetchvideo?ttid=1&token=" + secret
	raw := &url.Error{Op: "Get", URL: rawURL, Err: errors.New("connection refused")}

	if got := raw.Error(); !strings.Contains(got, secret) {
		t.Fatalf("precondition failed: raw url.Error must contain the token, got %q", got)
	}
	sanitized := SanitizeError(raw)
	got := sanitized.Error()
	if strings.Contains(got, secret) {
		t.Errorf("sanitized error leaked token: %q", got)
	}
	if !strings.Contains(got, "REDACTED") {
		t.Errorf("sanitized error should mark token REDACTED, got %q", got)
	}
}

func TestScrubError_StripsEmbeddedURLs(t *testing.T) {
	wrapped := &url.Error{Op: "Get", URL: "https://host/x?token=leak", Err: errors.New("dial: refused")}
	got := ScrubError(wrapped)
	if strings.Contains(got, "leak") {
		t.Errorf("ScrubError leaked embedded token: %q", got)
	}
	if !strings.Contains(got, "REDACTED") {
		t.Errorf("ScrubError should redact embedded token, got %q", got)
	}
}

func TestScrubPreservesCredentialFreeURLsByteForByte(t *testing.T) {
	t.Parallel()

	const input = "retry https://example.com/path?tokenquerysecret after refresh"
	if got := Scrub(input); got != input {
		t.Fatalf("Scrub(%q) = %q, want byte-for-byte preservation", input, got)
	}
}

func TestScrubURLHelpersStayEquivalent(t *testing.T) {
	t.Parallel()

	for _, input := range []string{
		"",
		"retry https://example.com/path?tokenquerysecret after refresh",
		"https://example.com/path?token=url-secret&keep=1",
		"https://alice:password@example.com/path",
		"nested https://example.com/?next=https%3A%2F%2Finner.example%2F%3Ftoken%3Dnested-secret",
	} {
		if got, want := ScrubURLs(input), ScrubCredentialURLs(input); got != want {
			t.Fatalf("URL scrub helpers disagree for %q: ScrubURLs = %q, ScrubCredentialURLs = %q", input, got, want)
		}
	}
}

func TestScrubWithEvidenceCountsCredentialWhoseValueContainsRedactionMarker(t *testing.T) {
	got, evidence := ScrubWithEvidence("token=hunter2REDACTEDc")
	if got != "token=REDACTED" {
		t.Fatalf("ScrubWithEvidence() = %q, want token=REDACTED", got)
	}
	if evidence.Count() != 1 {
		t.Fatalf("ScrubWithEvidence() evidence count = %d, want 1", evidence.Count())
	}
}

func TestScrubCredentialURLsWithEvidenceCountsUserinfoWithoutMarkerText(t *testing.T) {
	got, evidence := ScrubCredentialURLsWithEvidence("https://user:password@example.com/path")
	if got != "https://example.com/path" {
		t.Fatalf("ScrubCredentialURLsWithEvidence() = %q", got)
	}
	if evidence.Count() != 1 {
		t.Fatalf("ScrubCredentialURLsWithEvidence() evidence count = %d, want 1", evidence.Count())
	}
}

func TestRedactionEvidenceDetectsValuesThatRemainVisible(t *testing.T) {
	normalize := func(value string) string { return strings.TrimPrefix(value, "ansi:") }
	evidence := RedactionEvidence{values: []string{"ansi:secret"}}
	if !evidence.HasVisibleValueIn("prefix secret suffix", normalize) {
		t.Fatal("visible credential was not detected")
	}
	if evidence.HasVisibleValueIn("prefix REDACTED suffix", normalize) {
		t.Fatal("redacted credential was treated as visible")
	}
}

func TestScrub_RedactsFreeFormCredentialAssignments(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		input  string
		secret string
	}{
		{name: "authorization header", input: "Authorization: Bearer body-secret", secret: "body-secret"},
		{name: "prefixed authorization header", input: "proxy error: Authorization: Bearer body-secret", secret: "body-secret"},
		{name: "auth equals", input: "upstream auth=body-secret failed", secret: "body-secret"},
		{name: "json auth", input: `{"auth":"body-secret"}`, secret: "body-secret"},
		{name: "json authorization", input: `{"authorization":"Bearer body-secret"}`, secret: "body-secret"},
		{name: "equals token", input: "upstream token=body-secret failed", secret: "body-secret"},
		{name: "equals key", input: "upstream key=body-secret failed", secret: "body-secret"},
		{name: "json token", input: `{"refresh_token":"body-secret"}`, secret: "body-secret"},
		{name: "json key", input: `{"key":"body-secret"}`, secret: "body-secret"},
		{name: "password colon", input: "password:body-secret", secret: "body-secret"},
		{name: "inline tight auth colon", input: "upstream auth:body-secret failed", secret: "body-secret"},
		{name: "inline spaced auth colon", input: "upstream auth: body-secret failed", secret: "body-secret"},
		{name: "inline short auth colon", input: "upstream auth: abc123 failed", secret: "abc123"},
		{name: "inline short bare token colon", input: "upstream token: z failed", secret: "z"},
		{name: "inline short token colon", input: "upstream access_token: z failed", secret: "z"},
		{name: "inline short api key colon", input: "upstream api-key: q failed", secret: "q"},
		{name: "inline short secret colon", input: "upstream secret: v failed", secret: "v"},
		{name: "inline short key colon", input: "upstream key: q7 failed", secret: "q7"},
		{name: "inline short password colon", input: "upstream password: hunter2 failed", secret: "hunter2"},
		{name: "line token colon", input: "token: body-secret", secret: "body-secret"},
		{name: "auth bearer", input: "upstream auth: Bearer body-secret", secret: "body-secret"},
		{name: "token bearer", input: "upstream token=Bearer body-secret", secret: "body-secret"},
		{name: "authorization digest", input: `Authorization: Digest username="alice", realm="lecture", response="digest-secret"`, secret: "digest-secret"},
		{name: "authorization negotiate", input: "Authorization: Negotiate negotiate-secret", secret: "negotiate-secret"},
		{name: "authorization aws", input: "authorization=AWS4-HMAC-SHA256 Credential=aws-secret SignedHeaders=host", secret: "aws-secret"},
		{name: "authorization custom", input: "authorization: Custom id=public; proof=custom-secret", secret: "custom-secret"},
		{name: "auth digest", input: `auth: Digest username="alice", response="digest-secret"`, secret: "digest-secret"},
		{name: "proxy authorization", input: "Proxy-Authorization: Custom proof=proxy-secret", secret: "proxy-secret"},
		{name: "x api key", input: "X-Api-Key: api-secret", secret: "api-secret"},
		{name: "json x api key", input: `{"x-api-key":"api-secret"}`, secret: "api-secret"},
		{name: "signature equals", input: "upstream signature=body-secret failed", secret: "body-secret"},
		{name: "short sig colon", input: "upstream sig: q failed", secret: "q"},
		{name: "json signature", input: `{"signature":"body-secret"}`, secret: "body-secret"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := Scrub(test.input)
			if strings.Contains(got, test.secret) || !strings.Contains(got, "REDACTED") {
				t.Fatalf("Scrub(%q) = %q", test.input, got)
			}
		})
	}
}

func TestScrub_RedactsQuotedKeysWithNonJSONValues(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		input string
		want  string
	}{
		{input: `"token": bare-secret`, want: `"token": REDACTED`},
		{input: `"token"=equals-secret`, want: `"token"=REDACTED`},
		{input: `'token': single-secret`, want: `'token': REDACTED`},
		{input: `'token': 'quoted secret'`, want: `'token': 'REDACTED'`},
		{input: `"token ": "secret"`, want: `"token ": "REDACTED"`},
		{input: `'token ': 'secret'`, want: `'token ': 'REDACTED'`},
		{input: `"token": 'secret'`, want: `"token": 'REDACTED'`},
		{input: `'token': "secret"`, want: `'token': "REDACTED"`},
		{input: `"token"="secret"`, want: `"token"="REDACTED"`},
		{input: `"token": bearer s3cr3t`, want: `"token": REDACTED`},
	} {
		if got := Scrub(test.input); got != test.want {
			t.Fatalf("Scrub(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestScrub_FreeFormCoverageTracksEverySensitiveQueryKey(t *testing.T) {
	t.Parallel()

	for key := range sensitiveParams {
		for _, input := range []string{
			fmt.Sprintf("upstream %s=body-secret failed", key),
			fmt.Sprintf("upstream %s: body-secret failed", key),
			fmt.Sprintf(`{"%s":"body-secret"}`, key),
		} {
			if got := Scrub(input); strings.Contains(got, "body-secret") || !strings.Contains(got, "REDACTED") {
				t.Fatalf("Scrub(%q) = %q for sensitive key %q", input, got, key)
			}
		}
	}
}

func TestScrub_RedactsMultipleAssignmentsWithoutDiscardingTheirKeys(t *testing.T) {
	t.Parallel()

	input := "token=secret-value refresh_token=refresh-value signature=signed-value"
	got := Scrub(input)
	for _, secret := range []string{"secret-value", "refresh-value", "signed-value"} {
		if strings.Contains(got, secret) {
			t.Fatalf("Scrub(%q) leaked %q: %q", input, secret, got)
		}
	}
	for _, key := range []string{"token=REDACTED", "refresh_token=REDACTED", "signature=REDACTED"} {
		if !strings.Contains(got, key) {
			t.Fatalf("Scrub(%q) lost diagnostic key %q: %q", input, key, got)
		}
	}
}

func TestScrub_PreservesPathDiagnostics(t *testing.T) {
	t.Parallel()

	for _, diagnostic := range []string{
		"open /etc/auth: no such file or directory",
		"open /etc/key: permission denied",
		"open /etc/token: no such file or directory",
	} {
		if got := Scrub(diagnostic); got != diagnostic {
			t.Fatalf("Scrub(%q) = %q", diagnostic, got)
		}
	}
}

func TestScrub_RedactsAmbiguousPasswordValueButPreservesContext(t *testing.T) {
	t.Parallel()

	const diagnostic = "login failed because password: expired"
	if got := Scrub(diagnostic); got != "login failed because password: REDACTED" {
		t.Fatalf("Scrub(%q) = %q", diagnostic, got)
	}
}

func TestScrub_RedactsAmbiguousParserTokenValueButPreservesContext(t *testing.T) {
	t.Parallel()

	const diagnostic = "decode failed: unexpected token: EOF"
	if got := Scrub(diagnostic); got != "decode failed: unexpected token: REDACTED" {
		t.Fatalf("Scrub(%q) = %q", diagnostic, got)
	}
}

// TestSanitizeError_NilSafe ensures the helper tolerates nil.
func TestSanitizeError_NilSafe(t *testing.T) {
	if got := SanitizeError(nil); got != nil {
		t.Errorf("SanitizeError(nil) = %v, want nil", got)
	}
	if got := ScrubError(nil); got != "" {
		t.Errorf("ScrubError(nil) = %q, want empty", got)
	}
}

// TestRedactURL_MalformedURLStillRedacts closes the parse-failure gap: when
// url.Parse rejects a tokenized URL (e.g. an invalid percent escape), the raw
// string must still have its sensitive params scrubbed rather than returned
// verbatim.
func TestRedactURL_MalformedURLStillRedacts(t *testing.T) {
	const secret = "zz-secret"
	// "%zz" is an invalid percent-escape: url.Parse rejects this URL.
	raw := "https://host/%zz?token=" + secret + "&keep=1"
	if _, err := url.Parse(raw); err == nil { //nolint:staticcheck // SA1007: intentionally invalid URL to exercise the parse-failure redaction path
		t.Skip("precondition failed: url.Parse unexpectedly accepted the malformed URL")
	}
	got := RedactURL(raw)
	if strings.Contains(got, secret) {
		t.Errorf("RedactURL leaked token on unparseable URL: %q", got)
	}
	if !strings.Contains(got, "token=REDACTED") {
		t.Errorf("RedactURL should mark token REDACTED on unparseable URL, got %q", got)
	}
	if !strings.Contains(got, "keep=1") {
		t.Errorf("RedactURL should preserve non-sensitive params, got %q", got)
	}
}

// TestSanitizeError_MalformedURLErrorStillRedacts: http.NewRequest wraps a
// url.Parse failure in a *url.Error whose URL is the raw malformed tokenized
// URL. SanitizeError must scrub it, not rebuild the leak.
func TestSanitizeError_MalformedURLErrorStillRedacts(t *testing.T) {
	const secret = "parsefail-secret"
	malformed := "https://host/%zz?token=" + secret
	raw := &url.Error{Op: "Get", URL: malformed, Err: errors.New("invalid URL escape %zz")}
	if got := raw.Error(); !strings.Contains(got, secret) {
		t.Fatalf("precondition failed: raw error must contain token, got %q", got)
	}
	got := SanitizeError(raw).Error()
	if strings.Contains(got, secret) {
		t.Errorf("SanitizeError leaked token from malformed-URL error: %q", got)
	}
	if !strings.Contains(got, "REDACTED") {
		t.Errorf("SanitizeError should redact malformed-URL token, got %q", got)
	}
}

// TestSanitizeError_NonURLErrorScrubsEmbeddedURL is the defense-in-depth guard
// for arbitrary error types whose message embeds a tokenized URL. Such errors
// must not pass through unscrubbed.
func TestSanitizeError_NonURLErrorScrubsEmbeddedURL(t *testing.T) {
	const secret = "embedded-secret"
	raw := errors.New("upstream redirect to https://host/cb?token=" + secret)
	got := SanitizeError(raw).Error()
	if strings.Contains(got, secret) {
		t.Errorf("SanitizeError leaked embedded-URL token from non-url.Error: %q", got)
	}
	if !strings.Contains(got, "REDACTED") {
		t.Errorf("SanitizeError should redact embedded-URL token, got %q", got)
	}
	// Errors with no embedded URLs pass through unchanged (same string).
	plain := errors.New("connection refused")
	if SanitizeError(plain).Error() != plain.Error() {
		t.Errorf("SanitizeError should pass through clean errors unchanged")
	}
}

// TestSanitizeError_UnwrapDoesNotRecoverToken is the chain-severance guard.
// SanitizeError is a redaction boundary: a caller that walks the error chain
// via errors.Unwrap / errors.Is / errors.As must never recover the original
// secret-bearing error, even when it was wrapped with %w.
func TestSanitizeError_UnwrapDoesNotRecoverToken(t *testing.T) {
	const secret = "unwrap-secret"
	inner := errors.New("redirect to https://host/cb?token=" + secret)
	wrapped := fmt.Errorf("login failed: %w", inner) // not a *url.Error
	sanitized := SanitizeError(wrapped)

	// Walk every reachable error via Unwrap; none may carry the token.
	visited := map[error]bool{}
	for cur := sanitized; cur != nil && !visited[cur]; cur = errors.Unwrap(cur) {
		visited[cur] = true
		if strings.Contains(cur.Error(), secret) {
			t.Errorf("token recovered via error chain unwrapping: %q", cur.Error())
		}
	}

	// Same guarantee for a *url.Error whose inner Err embeds the token.
	nested := &url.Error{
		Op: "Get", URL: "https://host/p?token=" + secret,
		Err: errors.New("dial failed; see https://host/log?token=" + secret),
	}
	for cur := SanitizeError(nested); cur != nil && !visited[cur]; cur = errors.Unwrap(cur) {
		visited[cur] = true
		if strings.Contains(cur.Error(), secret) {
			t.Errorf("token recovered via url.Error chain unwrapping: %q", cur.Error())
		}
	}
}

// TestRedactURL_StripsBasicAuthUserinfo closes the userinfo leak: HTTP
// basic-auth credentials (user:pass@) must be stripped, not passed through.
func TestRedactURL_StripsBasicAuthUserinfo(t *testing.T) {
	const password = "basic-auth-password"
	in := "https://alice:" + password + "@host/path?keep=1"
	got := RedactURL(in)
	if strings.Contains(got, password) {
		t.Errorf("RedactURL leaked basic-auth password: %q", got)
	}
	if strings.Contains(got, "alice:") {
		t.Errorf("RedactURL should strip userinfo, got %q", got)
	}
	if !strings.Contains(got, "keep=1") {
		t.Errorf("RedactURL should preserve non-sensitive params, got %q", got)
	}
}

// TestRedactURL_NestedTokenInParamValue covers tokens nested inside a
// non-sensitive parameter value, both as a literal URL and percent-encoded.
func TestRedactURL_NestedTokenInParamValue(t *testing.T) {
	const secret = "nested-secret"
	cases := []struct {
		name string
		in   string
	}{
		{
			name: "literal nested url",
			in:   "https://host/cb?next=https://host/cb?token=" + secret,
		},
		{
			name: "percent-encoded nested url",
			in:   "https://host/cb?next=" + url.QueryEscape("https://host/cb?token="+secret),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactURL(tc.in)
			if strings.Contains(got, secret) {
				t.Errorf("RedactURL leaked nested token (%s): %q", tc.name, got)
			}
			if !strings.Contains(got, "REDACTED") {
				t.Errorf("RedactURL should redact nested token (%s): %q", tc.name, got)
			}
		})
	}
}
