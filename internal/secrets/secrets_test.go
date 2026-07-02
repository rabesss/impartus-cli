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
