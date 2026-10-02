package secrets

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"
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

func TestRedactURL_RedactsAuthorizationQueryAlias(t *testing.T) {
	for _, rawURL := range []string{
		"https://host/path?authorization=Bearer+secret-token",
		"https://host/path?Authorization=secret-token",
		"https://host/%zz?%61uthorization=Bearer%20secret-token&keep=a%2Fb",
		"https://host/path?x-api-key=secret-token",
		"https://host/path?refresh_token=secret-token",
		"https://host/path?client_secret=secret-token",
		"https://host/path?password=secret-token",
	} {
		got := RedactURL(rawURL)
		if strings.Contains(got, "secret-token") {
			t.Errorf("RedactURL(%q) leaked authorization query credential: %s", rawURL, got)
		}
		if !strings.Contains(got, "REDACTED") {
			t.Errorf("RedactURL(%q) should contain REDACTED, got %q", rawURL, got)
		}
	}
}

func TestRedactURL_RedactsAssignmentAliasesInQuery(t *testing.T) {
	for _, key := range []string{
		"api-key", "apikey", "xapikey", "proxy-authorization", "set_cookie",
		"access-token", "refresh-token", "client-secret",
	} {
		rawURL := "https://host/path?" + key + "=alias-secret&keep=1"
		got := RedactURL(rawURL)
		if strings.Contains(got, "alias-secret") {
			t.Fatalf("RedactURL(%q) leaked alias credential: %q", rawURL, got)
		}
		if !strings.Contains(got, "REDACTED") {
			t.Fatalf("RedactURL(%q) = %q, want redaction marker", rawURL, got)
		}
	}
}

func TestRedactURL_CanonicalizesObfuscatedSensitiveQueryKeys(t *testing.T) {
	t.Parallel()

	for _, rawURL := range []string{
		`https://host/path?to\u006ben=json-key-secret&keep=1`,
		"https://host/path?to\u200bken=format-key-secret&keep=1",
		`https://host/path?to%5Cu006ben=encoded-json-key-secret&keep=1`,
	} {
		got := RedactURL(rawURL)
		for _, secret := range []string{"json-key-secret", "format-key-secret", "encoded-json-key-secret"} {
			if strings.Contains(got, secret) {
				t.Fatalf("RedactURL(%q) leaked obfuscated query credential %q: %q", rawURL, secret, got)
			}
		}
		if !strings.Contains(got, "REDACTED") || !strings.Contains(got, "keep=1") {
			t.Fatalf("RedactURL(%q) = %q, want redaction marker and safe context", rawURL, got)
		}
	}
}

func TestIsSensitiveQueryKeyUsesBoundedCanonicalization(t *testing.T) {
	t.Parallel()

	for _, key := range []string{
		"token",
		`to\u006ben`,
		"to\u200bken",
		fullyPercentEncode("token"),
	} {
		if !IsSensitiveQueryKey(key) {
			t.Fatalf("IsSensitiveQueryKey(%q) = false, want sensitive", key)
		}
	}

	deepTokenKey := "token"
	for index := 0; index < 5; index++ {
		deepTokenKey = fullyPercentEncode(deepTokenKey)
	}
	if !IsSensitiveQueryKey(deepTokenKey) {
		t.Fatalf("IsSensitiveQueryKey() = false for deeply encoded token key")
	}
	rawQueryKey := "token"
	for index := 0; index < 4; index++ {
		rawQueryKey = fullyPercentEncode(rawQueryKey)
	}
	parsedQuery, err := url.ParseQuery(rawQueryKey + "=query-secret&course=1")
	if err != nil {
		t.Fatalf("url.ParseQuery() error = %v", err)
	}
	for key := range parsedQuery {
		if key == "course" && IsSensitiveQueryKey(key) {
			t.Fatal("IsSensitiveQueryKey() classified safe policy query key as sensitive")
		}
		if key != "course" && !IsSensitiveQueryKey(key) {
			t.Fatalf("IsSensitiveQueryKey(%q) = false after raw query parsing", key)
		}
	}

	for _, key := range []string{"course", `co\u0075rse`, "co\u200b urse"} {
		if IsSensitiveQueryKey(key) {
			t.Fatalf("IsSensitiveQueryKey(%q) = true, want ordinary key preserved", key)
		}
	}
	deepSafeKey := "course"
	for index := 0; index < 4; index++ {
		deepSafeKey = fullyPercentEncode(deepSafeKey)
	}
	if IsSensitiveQueryKey(deepSafeKey) {
		t.Fatalf("IsSensitiveQueryKey() = true for deeply encoded ordinary key")
	}
}

func TestRedactURLWithToken_RedactsDirectUnknownCredential(t *testing.T) {
	const token = "direct-url-token"
	rawURL := "https://host/path?unknown=" + token + "&keep=1"
	got := RedactURLWithToken(rawURL, token)
	if strings.Contains(got, token) {
		t.Fatalf("RedactURLWithToken(%q) leaked token: %q", rawURL, got)
	}
	if !strings.Contains(got, "REDACTED") || !strings.Contains(got, "keep=1") {
		t.Fatalf("RedactURLWithToken(%q) = %q, want marker and safe context", rawURL, got)
	}
}

func TestRedactURL_ScrubsDecodedNestedAssignmentInNonSensitiveValue(t *testing.T) {
	rawURL := "https://host/path?next=token%3DSECRET%26done%3D1"
	got := RedactURL(rawURL)
	if strings.Contains(got, "SECRET") {
		t.Fatalf("RedactURL(%q) leaked decoded nested credential: %q", rawURL, got)
	}
	if !strings.Contains(got, "REDACTED") || !strings.Contains(got, "done%3D1") {
		t.Fatalf("RedactURL(%q) = %q, want nested marker and safe context", rawURL, got)
	}
}

func TestRedactURL_MalformedFallbackStripsAllUserinfoForms(t *testing.T) {
	for _, rawURL := range []string{
		"https://username@host/%zz",
		"https://:password@host/%zz",
		"https://username:@host/%zz",
		"https://username%3Apassword@host/%zz",
		"https://%3Apassword@host/%zz",
	} {
		got := RedactURL(rawURL)
		for _, secret := range []string{"username", "password", "%3A", "@"} {
			if strings.Contains(strings.ToLower(got), strings.ToLower(secret)) {
				t.Fatalf("RedactURL(%q) leaked userinfo %q: %q", rawURL, secret, got)
			}
		}
		if !strings.Contains(got, "https://host/") {
			t.Fatalf("RedactURL(%q) lost safe URL context: %q", rawURL, got)
		}
	}
}

func TestRedactURL_MalformedFallbackRedactsNestedPercentEncodedCredential(t *testing.T) {
	const secret = "malformed-nested-secret"
	nested := url.QueryEscape("https://inner.example.test/cb?next=%61ccess_token%3D" + secret)
	rawURL := "https://host/%zz?next=" + nested
	got := RedactURL(rawURL)
	if strings.Contains(got, secret) {
		t.Fatalf("RedactURL(%q) leaked nested credential: %q", rawURL, got)
	}
	if !strings.Contains(got, "REDACTED") {
		t.Fatalf("RedactURL(%q) = %q, want redaction marker", rawURL, got)
	}
}

func TestRedactURL_MalformedFallbackFailsClosedAfterPercentDecodeBudget(t *testing.T) {
	const secret = "too-deep-malformed-secret"
	nested := "https://inner.example.test/cb?token=" + secret
	for index := 0; index <= maxRawPercentDecodeDepth; index++ {
		nested = url.QueryEscape(nested)
	}
	rawURL := "https://host/%zz?next=" + nested
	if _, err := url.Parse(rawURL); err == nil {
		t.Skip("precondition failed: url.Parse unexpectedly accepted malformed URL")
	}
	if got := RedactURL(rawURL); got != "REDACTED" {
		t.Fatalf("RedactURL() = %q, want opaque marker after decode budget", got)
	}
}

func TestScrubRedactsQuotedValuesForUnquotedSensitiveKeys(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		input  string
		secret string
		keep   string
	}{
		{name: "double quoted", input: `password = "p@ss w0rd"`, secret: `p@ss w0rd`},
		{name: "single quoted", input: "password='single quoted value'", secret: "single quoted value"},
		{name: "quoted newline", input: "password = \"line one\nline two\"", secret: "line one\nline two"},
		{name: "escaped quote", input: `password = "p\"ss w0rd"`, secret: `p\"ss w0rd`},
		{name: "colon with context", input: `token: "tok-one tok-two" failed`, secret: "tok-one tok-two", keep: "failed"},
		{name: "unterminated", input: `password = "open-one open-two`, secret: "open-one open-two"},
		{name: "authorization delimiter", input: `authorization = "custom-one & custom-two"`, secret: "custom-one custom-two"},
		{name: "nested assignment", input: `password = "pw-one token=pw-two" next=1`, secret: "pw-one pw-two", keep: "next=1"},
		{
			name:   "two assignments",
			input:  `password="pw-one pw-two" client_secret='cs-one cs-two' next=1`,
			secret: "pw-one pw-two cs-one cs-two",
			keep:   "next=1",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, got := range []string{
				Scrub(test.input),
				SanitizeError(errors.New("login failed: " + test.input)).Error(),
			} {
				// Check each word: a pass that redacts only the first word of a
				// quoted value still hides the complete value.
				for _, fragment := range strings.Fields(test.secret) {
					if strings.Contains(got, fragment) {
						t.Fatalf("Scrub(%q) = %q, leaked quoted value fragment %q", test.input, got, fragment)
					}
				}
				if !strings.Contains(got, "REDACTED") || !strings.Contains(got, test.keep) {
					t.Fatalf("Scrub(%q) = %q, want redaction that keeps %q", test.input, got, test.keep)
				}
			}
		})
	}
}

func TestScrubRedactsAdjacentQuotedAssignmentsWithoutSeparator(t *testing.T) {
	t.Parallel()

	// The closing quote of the first value is the only boundary before the
	// second key, and replacing the first value consumes it.
	for _, test := range []struct {
		name   string
		input  string
		secret string
	}{
		{name: "double quoted", input: `password="pw-one pw-two"sig="sig-one sig-two" next=1`, secret: "pw-one pw-two sig-one sig-two"},
		{name: "single quoted", input: `password='pw-one pw-two'sig='sig-one sig-two' next=1`, secret: "pw-one pw-two sig-one sig-two"},
		{name: "colon", input: `password: "pw-one pw-two"sig: "sig-one sig-two" next=1`, secret: "pw-one pw-two sig-one sig-two"},
		{
			name:   "three values",
			input:  `password="pw-one pw-two"sig="sig-one sig-two"token='tok-one tok-two' next=1`,
			secret: "pw-one pw-two sig-one sig-two tok-one tok-two",
		},
		{name: "authorization", input: `authorization="Custom au-one au-two"cookie="ck-one ck-two"&next=1`, secret: "au-one au-two ck-one ck-two"},
		{name: "unquoted second", input: `password="pw-one pw-two"sig=sig-one next=1`, secret: "pw-one pw-two sig-one"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := test.input
			for _, view := range []struct{ name, got string }{
				{"Scrub", Scrub(input)},
				{"SanitizeError", SanitizeError(errors.New("login failed: " + input)).Error()},
				{"SanitizeErrorWithCredentials", SanitizeErrorWithCredentials(errors.New("login failed: "+input), "unrelated-credential").Error()},
				{"query encoded", Scrub("next=" + url.QueryEscape(input))},
				{"path encoded", Scrub("next=" + url.PathEscape(input))},
			} {
				for _, fragment := range strings.Fields(test.secret) {
					if strings.Contains(view.got, fragment) {
						t.Fatalf("%s(%q) = %q, leaked quoted value fragment %q", view.name, input, view.got, fragment)
					}
				}
				if !strings.Contains(view.got, "REDACTED") || !strings.Contains(view.got, "next") {
					t.Fatalf("%s(%q) = %q, want redaction that keeps the context", view.name, input, view.got)
				}
			}
		})
	}

	// A key inside a quoted value is part of that value, not a new assignment.
	const nested = `password="pw-one sig=pw-two" next=1`
	if got, want := Scrub(nested), "password=REDACTED next=1"; got != want {
		t.Fatalf("Scrub(%q) = %q, want %q", nested, got, want)
	}
}

func TestRedactURL_ScrubsAllEmbeddedURLCandidatesIncludingFirst(t *testing.T) {
	t.Parallel()

	first := "https://first.example.test/cb?token=first-embedded-secret"
	second := "https://second.example.test/cb?token=second-embedded-secret"
	// The malformed outer URL forces the raw decoder path. Both nested URLs
	// become adjacent candidates after one decode, which previously skipped the
	// first candidate and returned after sanitizing only the later one.
	raw := "https://outer.example.test/%zz?next=" + url.QueryEscape(first+" "+second)
	got := RedactURL(raw)
	for _, secret := range []string{"first-embedded-secret", "second-embedded-secret"} {
		if strings.Contains(got, secret) {
			t.Fatalf("RedactURL(%q) leaked embedded URL credential %q: %q", raw, secret, got)
		}
	}
	if !strings.Contains(got, "REDACTED") {
		t.Fatalf("RedactURL(%q) = %q, want marker", raw, got)
	}
}

func TestScrub_ScrubsUppercaseHTTPSCredentials(t *testing.T) {
	t.Parallel()

	input := "upstream HTTPS://user:upper-user-secret@host/path?token=upper-query-secret"
	got := Scrub(input)
	for _, secret := range []string{"upper-user-secret", "upper-query-secret", "user:"} {
		if strings.Contains(got, secret) {
			t.Fatalf("Scrub(%q) leaked %q: %q", input, secret, got)
		}
	}
	if !strings.Contains(got, "REDACTED") {
		t.Fatalf("Scrub(%q) = %q, want marker", input, got)
	}
}

func TestRedactURL_StripsRepeatedUserinfo(t *testing.T) {
	const rawURL = "https://first-user@second-user@host/path"
	got := RedactURL(rawURL)
	if got != "https://host/path" {
		t.Fatalf("RedactURL(%q) = %q, want final host without userinfo", rawURL, got)
	}
}

func TestRedactURL_RedactsMixedAndDoubleEncodedDelimiters(t *testing.T) {
	const secret = "encoded-delimiter-secret"
	for _, rawURL := range []string{
		"https://host/%zz?%74oken%253D" + secret,
		"https://host/path#%74oken%3D" + secret,
		"https://host/path#%3F%61%62ccess_token%3D" + secret,
	} {
		got := RedactURL(rawURL)
		if strings.Contains(got, secret) {
			t.Fatalf("RedactURL(%q) leaked encoded credential: %q", rawURL, got)
		}
		if !strings.Contains(got, "REDACTED") {
			t.Fatalf("RedactURL(%q) = %q, want redaction marker", rawURL, got)
		}
	}
}

func TestRedactWithTokenRedactsMixedPercentEncodedViews(t *testing.T) {
	const token = "abc"
	for _, input := range []string{
		"https://host/a%62c/path",
		"https://host/path#unknown=a%62c",
		"https://host/%zz?unknown=a%62c",
		"upstream unknown=a%62c",
	} {
		got := RedactWithToken(input, token)
		if strings.Contains(got, token) || strings.Contains(got, "a%62c") {
			t.Fatalf("RedactWithToken(%q) leaked mixed-encoded token: %q", input, got)
		}
		if !strings.Contains(got, "REDACTED") {
			t.Fatalf("RedactWithToken(%q) = %q, want redaction marker", input, got)
		}
	}
}

func TestRedactWithTokenTreatsCredentialBytesAsCaseSensitive(t *testing.T) {
	t.Parallel()

	const token = "İ-token"
	for _, test := range []struct {
		name  string
		input string
		want  string
	}{
		{name: "exact unicode token", input: "prefix " + token + " suffix", want: "prefix REDACTED suffix"},
		{name: "different case remains distinct", input: "prefix i-token suffix", want: "prefix i-token suffix"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := RedactWithToken(test.input, token)
			if got != test.want {
				t.Fatalf("RedactWithToken(%q) = %q, want %q", test.input, got, test.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("RedactWithToken(%q) returned invalid UTF-8: %q", test.input, got)
			}
		})
	}
}

func TestRedactWithTokenDecodesArbitraryJSONEscapeSpellings(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		token string
		input string
	}{
		{
			name:  "mixed unicode and slash escapes",
			token: "secret/&part",
			input: `upstream \u0073ecret\/\u0026part`,
		},
		{
			name:  "surrogate pair and escaped slash",
			token: "café/🔐",
			input: `upstream c\u0061f\u00e9\/\ud83d\udd10`,
		},
		{
			name:  "escaped quote and backslash",
			token: "quote\"slash\\tail",
			input: `upstream quote\u0022slash\\tail`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := RedactWithToken(test.input, test.token)
			if strings.Contains(got, test.token) || strings.Contains(got, test.input[len("upstream "):]) {
				t.Fatalf("RedactWithToken(%q) leaked JSON-spelled token: %q", test.input, got)
			}
			if !strings.Contains(got, "REDACTED") {
				t.Fatalf("RedactWithToken(%q) = %q, want redaction marker", test.input, got)
			}
		})
	}
}

func TestRedactWithTokenFailsClosedAfterPercentDecodeBudget(t *testing.T) {
	t.Parallel()

	const token = "deep-percent-secret"
	encoded := fullyPercentEncode(token)
	for index := 0; index < maxRawPercentDecodeDepth+4; index++ {
		encoded = strings.ReplaceAll(encoded, "%", "%25")
	}
	got := RedactWithToken("upstream payload="+encoded, token)
	if got != "REDACTED" {
		t.Fatalf("RedactWithToken() = %q, want fixed fail-safe marker", got)
	}
}

func TestRedactWithTokenFailsClosedBeforeLargeVariantScan(t *testing.T) {
	t.Parallel()

	value := strings.Repeat("safe-diagnostic ", maxEncodedTokenViewSize/len("safe-diagnostic ")+1)
	got := RedactWithToken(value, "small-secret")
	if got != "REDACTED" {
		t.Fatalf("RedactWithToken() = %q, want fixed fail-safe marker", got)
	}
}

func TestRedactWithTokenRedactsMixedDuplicateRepresentations(t *testing.T) {
	t.Parallel()

	const token = `mixed+"token`
	encoded := url.QueryEscape(token)
	jsonEncoded := jsonEscapeToken(token)
	input := "first=" + token + " second=" + encoded + " third=" + jsonEncoded
	got := RedactWithToken(input, token)
	for _, secret := range []string{token, encoded, jsonEncoded} {
		if strings.Contains(got, secret) {
			t.Fatalf("RedactWithToken() leaked representation %q: %q", secret, got)
		}
	}
	if strings.Count(got, "REDACTED") < 3 {
		t.Fatalf("RedactWithToken() = %q, want all mixed occurrences redacted", got)
	}
}

func TestRedactURL_RedactsCredentialFragments(t *testing.T) {
	for _, rawURL := range []string{
		"https://host/path#token=fragsecret",
		"https://host/path#access_token=fragsecret",
		"https://host/path#%74%6f%6b%65%6e=fragsecret",
		"https://host/path#token%3Dfragsecret",
		"https://host/path#token%253Dfragsecret",
		"https://host/%zz#token=fragsecret",
		"https://host/%zz#token%3Dfragsecret",
	} {
		got := RedactURL(rawURL)
		if strings.Contains(got, "fragsecret") {
			t.Fatalf("RedactURL(%q) leaked fragment credential: %q", rawURL, got)
		}
		if !strings.Contains(got, "REDACTED") {
			t.Fatalf("RedactURL(%q) = %q, want redaction marker", rawURL, got)
		}
	}
	if got := RedactURL("https://host/path#token=fragsecret&keep=1"); got != "https://host/path#token=REDACTED&keep=1" {
		t.Fatalf("RedactURL() lost safe fragment context: %q", got)
	}
}

func TestSanitizeError_RedactsCredentialFragment(t *testing.T) {
	rawURL := "https://host/%zz#token=fragment-error-secret"
	raw := &url.Error{Op: "Get", URL: rawURL, Err: errors.New("redirect to " + rawURL)}
	got := SanitizeError(raw).Error()
	if strings.Contains(got, "fragment-error-secret") {
		t.Fatalf("SanitizeError leaked fragment credential: %q", got)
	}
	if !strings.Contains(got, "REDACTED") {
		t.Fatalf("SanitizeError(%q) = %q, want redaction marker", rawURL, got)
	}
}

func TestRedactURLWithTokenRedactsUnknownEncodedQueryCredential(t *testing.T) {
	const token = "Bearer secret+a&b=c?/"
	encodedQueryToken := url.QueryEscape(token)
	encodedPathToken := url.PathEscape(token)
	for _, rawURL := range []string{
		"https://host/path?unknown=" + encodedQueryToken + "&keep=1",
		"https://host/%zz?unknown=" + encodedPathToken + "&keep=1",
	} {
		t.Run(rawURL, func(t *testing.T) {
			got := RedactURLWithToken(rawURL, token)
			for _, secret := range []string{token, strings.TrimPrefix(token, "Bearer "), encodedQueryToken, encodedPathToken} {
				if strings.Contains(got, secret) {
					t.Fatalf("RedactURLWithToken(%q) leaked %q: %q", rawURL, secret, got)
				}
			}
			if !strings.Contains(got, "REDACTED") {
				t.Fatalf("RedactURLWithToken(%q) = %q, want redaction marker", rawURL, got)
			}
		})
	}
}

func TestRedactURLWithTokenRedactsFullyAndDoublyEncodedUnknownCredential(t *testing.T) {
	const token = "secret+a&b=c?/"
	encodedToken := fullyPercentEncode(token)
	doubleEncodedToken := fullyPercentEncode(encodedToken)
	doubleQueryEncodedToken := url.QueryEscape(url.QueryEscape(token))
	cases := []struct {
		name  string
		query string
	}{
		{name: "fully percent encoded", query: encodedToken},
		{name: "double fully percent encoded", query: doubleEncodedToken},
		{name: "double query encoded", query: doubleQueryEncodedToken},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, rawURL := range []string{
				"https://host/path?unknown=" + tc.query + "&keep=1",
				"https://host/%zz?unknown=" + tc.query + "&keep=1",
				"https://host/path?unknown=" + tc.query + "&bad=x%zz",
			} {
				got := RedactURLWithToken(rawURL, token)
				for _, secret := range []string{token, encodedToken, doubleEncodedToken, doubleQueryEncodedToken} {
					if strings.Contains(got, secret) {
						t.Fatalf("RedactURLWithToken(%q) leaked %q: %q", rawURL, secret, got)
					}
				}
				if !strings.Contains(got, "REDACTED") {
					t.Fatalf("RedactURLWithToken(%q) = %q, want redaction marker", rawURL, got)
				}
			}
		})
	}
}

func TestSanitizeErrorWithTokenRedactsFullyAndDoublyEncodedMalformedCredential(t *testing.T) {
	const token = "secret+a&b=c?/"
	encodedToken := fullyPercentEncode(token)
	doubleEncodedToken := fullyPercentEncode(encodedToken)
	for _, encoded := range []string{encodedToken, doubleEncodedToken} {
		rawURL := "https://host/%zz?unknown=" + encoded + "&bad=x%zz"
		raw := &url.Error{Op: "Get", URL: rawURL, Err: errors.New("redirect to " + rawURL)}
		got := SanitizeErrorWithToken(raw, token)
		if got == nil {
			t.Fatal("SanitizeErrorWithToken() = nil, want sanitized error")
		}
		for _, secret := range []string{token, encodedToken, doubleEncodedToken} {
			if strings.Contains(got.Error(), secret) {
				t.Fatalf("SanitizeErrorWithToken(%q) leaked %q: %q", rawURL, secret, got)
			}
		}
		if !strings.Contains(got.Error(), "REDACTED") {
			t.Fatalf("SanitizeErrorWithToken(%q) = %q, want redaction marker", rawURL, got)
		}
	}
}

func fullyPercentEncode(value string) string {
	const hex = "0123456789ABCDEF"
	var encoded strings.Builder
	encoded.Grow(len(value) * 3)
	for index := 0; index < len(value); index++ {
		encoded.WriteByte('%')
		encoded.WriteByte(hex[value[index]>>4])
		encoded.WriteByte(hex[value[index]&0x0f])
	}
	return encoded.String()
}

func TestSanitizeErrorWithTokenRedactsUnknownRedirectCredential(t *testing.T) {
	const token = "Bearer redirect-secret+a&b"
	rawURL := "https://host/callback?unknown=" + url.QueryEscape(token)
	raw := &url.Error{Op: "Get", URL: rawURL, Err: errors.New("redirect to " + rawURL)}
	got := SanitizeErrorWithToken(raw, token)
	if got == nil {
		t.Fatal("SanitizeErrorWithToken() = nil, want sanitized error")
	}
	for _, secret := range []string{token, strings.TrimPrefix(token, "Bearer "), url.QueryEscape(token)} {
		if strings.Contains(got.Error(), secret) {
			t.Fatalf("SanitizeErrorWithToken() leaked %q: %q", secret, got)
		}
	}
	if !strings.Contains(got.Error(), "REDACTED") {
		t.Fatalf("SanitizeErrorWithToken() = %q, want redaction marker", got)
	}
}

func TestSanitizeErrorWithTokenRedactsJSONEscapedCredential(t *testing.T) {
	const token = "json-token&part"
	raw := errors.New(`upstream response: {"detail":"json-token\u0026part"}`)
	got := SanitizeErrorWithToken(raw, token)
	if got == nil {
		t.Fatal("SanitizeErrorWithToken() = nil, want sanitized error")
	}
	if strings.Contains(got.Error(), token) || strings.Contains(got.Error(), `json-token\u0026part`) {
		t.Fatalf("SanitizeErrorWithToken() leaked JSON-escaped token: %q", got)
	}
	if !strings.Contains(got.Error(), "REDACTED") {
		t.Fatalf("SanitizeErrorWithToken() = %q, want redaction marker", got)
	}
}

func TestSanitizeErrorWithCredentialsRemovesLongerValueFirst(t *testing.T) {
	const username = "fixture-user"
	const password = "fixture-user-pw99"
	sentinel := errors.New("sentinel")
	raw := fmt.Errorf("upstream echoed %s: %w", password, sentinel)
	got := SanitizeErrorWithCredentials(raw, username, password)
	if strings.Contains(got.Error(), "fixture") || strings.Contains(got.Error(), "pw99") {
		t.Fatalf("SanitizeErrorWithCredentials() leaked a credential fragment: %q", got)
	}
	if !strings.Contains(got.Error(), "upstream echoed REDACTED") {
		t.Fatalf("SanitizeErrorWithCredentials() = %q, want redacted context", got)
	}
	if !errors.Is(got, sentinel) {
		t.Fatal("SanitizeErrorWithCredentials() lost the error classification")
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

func TestRedactionEvidenceRequiresBoundariesForShortValues(t *testing.T) {
	normalize := func(value string) string { return value }
	evidence := RedactionEvidence{values: []string{"0", "ab"}}
	if evidence.HasVisibleValueIn("failed 404 near cabinet", normalize) {
		t.Fatal("short credential substrings inside unrelated words were treated as visible")
	}
	if !evidence.HasVisibleValueIn("token=0", normalize) {
		t.Fatal("boundary-delimited one-character credential was not detected")
	}
	if !evidence.HasVisibleValueIn("sig=ab", normalize) {
		t.Fatal("boundary-delimited two-character credential was not detected")
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
			if test.name == "encoded authorization" {
				view := decodeEncodedViewLayer(rawEncodedView(test.input), true)
				ranges, _ := credentialValueRangesForView(view)
				t.Logf("view=%q ranges=%+v", view.text, ranges)
				for _, step := range credentialAssignmentSteps() {
					for _, index := range step.expression.FindAllStringSubmatchIndex(view.text, -1) {
						prefixEnd := index[step.prefixGroup*2+1]
						t.Logf("step=%p match=%q value=%q", step.expression, view.text[index[0]:index[1]], view.text[prefixEnd:index[1]])
					}
				}
				t.Logf("canonical=%+v", canonicalCredentialValueRanges(view.text))
			}
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

func TestScrub_RedactsEncodedFreeFormAssignmentsWithoutKnownToken(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		input  string
		secret string
		want   string
	}{
		{name: "encoded equals", input: "prefix token%3Dunknown-secret suffix", secret: "unknown-secret", want: "prefix token%3DREDACTED suffix"},
		{name: "lowercase delimiter hex", input: "prefix TOKEN%3dcase-secret suffix", secret: "case-secret", want: "prefix TOKEN%3dREDACTED suffix"},
		{name: "encoded authorization", input: "prefix authorization%3A%20Bearer%20authorization-secret suffix", secret: "authorization-secret", want: "prefix authorization%3A%20Bearer%20REDACTED suffix"},
		{name: "mixed encoded key", input: "prefix a%62ccess_token%3dmixed-secret suffix", secret: "mixed-secret", want: "prefix a%62ccess_token%3dREDACTED suffix"},
		{name: "double encoded delimiter", input: "prefix token%253Ddouble-secret suffix", secret: "double-secret", want: "prefix token%253DREDACTED suffix"},
		{name: "triple encoded delimiter", input: "prefix token%25253Dtriple-secret suffix", secret: "triple-secret", want: "prefix token%25253DREDACTED suffix"},
		{name: "json escaped key", input: `body {"access\u005ftoken":"json-key-secret"}`, secret: "json-key-secret", want: `body {"access\u005ftoken":"REDACTED"}`},
		{name: "json escaped value", input: `body {"token":"json\u002dvalue-secret"}`, secret: "json-value-secret", want: `body {"token":"REDACTED"}`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := Scrub(test.input)
			if strings.Contains(got, test.secret) {
				t.Fatalf("Scrub(%q) leaked %q: %q", test.input, test.secret, got)
			}
			if !strings.Contains(got, "REDACTED") {
				t.Fatalf("Scrub(%q) = %q, want redaction marker", test.input, got)
			}
			if test.want != "" && got != test.want {
				t.Fatalf("Scrub(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}

	const deepSecret = "too-deep-secret"
	deep := "token%2525253D" + deepSecret
	if got := Scrub("prefix " + deep + " suffix"); strings.Contains(got, deepSecret) {
		t.Fatalf("Scrub() leaked credential after decode budget: %q", got)
	}
	mixed := "token%3Dknown-secret token%2525253D" + deepSecret
	if got := Scrub(mixed); strings.Contains(got, deepSecret) {
		t.Fatalf("Scrub() leaked a deeper credential beside a redacted one: %q", got)
	}
	deeperDelimiter := "%3D"
	for index := 0; index < maxCredentialDecodeDepth+2; index++ {
		deeperDelimiter = strings.ReplaceAll(deeperDelimiter, "%", "%25")
	}
	if got := Scrub("prefix token" + deeperDelimiter + deepSecret + " suffix"); strings.Contains(got, deepSecret) {
		t.Fatalf("Scrub() leaked credential beyond the decode budget: %q", got)
	}
}

func TestScrub_RedactsPercentEncodedNestedJSONAssignment(t *testing.T) {
	t.Parallel()

	const secret = "nested-json-secret"
	nested := url.QueryEscape(`{"token":"` + secret + `"}`)
	input := "redirect next=" + nested
	got := Scrub(input)
	if strings.Contains(got, secret) {
		t.Fatalf("Scrub(%q) leaked nested JSON credential: %q", input, got)
	}
	if !strings.Contains(got, "REDACTED") {
		t.Fatalf("Scrub(%q) = %q, want redaction marker", input, got)
	}
	want := "redirect next=" + url.QueryEscape(`{"token":"REDACTED"}`)
	if got != want {
		t.Fatalf("Scrub(%q) = %q, want %q", input, got, want)
	}
}

func TestSanitizeError_RedactsEncodedUnknownAssignment(t *testing.T) {
	t.Parallel()

	const secret = "encoded-error-secret"
	raw := errors.New("upstream response authorization%3A%20Bearer%20" + secret)
	got := SanitizeError(raw)
	if got == nil {
		t.Fatal("SanitizeError() = nil")
	}
	if strings.Contains(got.Error(), secret) {
		t.Fatalf("SanitizeError() leaked encoded credential: %q", got)
	}
	if !strings.Contains(got.Error(), "REDACTED") {
		t.Fatalf("SanitizeError() = %q, want redaction marker", got)
	}
}

func TestScrub_RedactsFormatObfuscatedCredentialKey(t *testing.T) {
	t.Parallel()

	for _, input := range []string{
		"to\u200bken=zero-width-secret",
		"access\u2060_token: word-joiner-secret",
		`{"to\u200bken":"json-zero-width-secret"}`,
	} {
		got := Scrub(input)
		for _, secret := range []string{"zero-width-secret", "word-joiner-secret", "json-zero-width-secret"} {
			if strings.Contains(got, secret) {
				t.Fatalf("Scrub(%q) leaked %q: %q", input, secret, got)
			}
		}
		if !strings.Contains(got, "REDACTED") {
			t.Fatalf("Scrub(%q) = %q, want redaction marker", input, got)
		}
	}
}

func TestScrubRejectsOversizedDiagnosticsBeforeScanning(t *testing.T) {
	t.Parallel()

	input := strings.Repeat("safe diagnostic ", maxScrubInputSize/len("safe diagnostic ")+1)
	started := time.Now()
	got := Scrub(input)
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("Scrub() took %v on oversized diagnostic", elapsed)
	}
	if got != "REDACTED" {
		t.Fatalf("Scrub() = %q, want fixed fail-safe marker", got)
	}
}

func TestCanonicalCredentialScanStaysLinearOnOverlappingValues(t *testing.T) {
	// Adjacent obfuscated assignments can share one unbroken value, and each
	// delimiter used to rescan it to the end of the input. Compare each hostile
	// input with a benign one of similar size and delimiter count: the quadratic
	// scan was a hundred to a few thousand times slower. The linear scan stays
	// within a few times, except escaped quoted keys, which look back for their
	// opening quote as far as the key budget at every delimiter and so run a few
	// tens of times slower. The bound sits between the two, with room for
	// scheduler noise on a loaded machine and for -race.
	for _, test := range overlappingAssignmentInputs(8000) {
		t.Run(test.name, func(t *testing.T) {
			hostile := fastestCanonicalScan(test.hostile)
			benign := fastestCanonicalScan(test.benign)
			if limit := 20*benign + 150*time.Millisecond; hostile > limit {
				t.Fatalf("canonical scan took %v on hostile input, want at most %v (benign input took %v)", hostile, limit, benign)
			}
		})
	}
	// Bounding the scan must not change what is redacted. Shorter inputs of the
	// same shape keep the full Scrub quick under -race.
	for _, test := range overlappingAssignmentInputs(400) {
		if got := Scrub(test.hostile); strings.Contains(got, "X") {
			t.Errorf("%s: Scrub() left the trailing credential visible: %q", test.name, got[max(0, len(got)-32):])
		}
	}
}

type overlappingAssignmentInput struct {
	name    string
	hostile string
	benign  string
}

func overlappingAssignmentInputs(repeats int) []overlappingAssignmentInput {
	return []overlappingAssignmentInput{
		{"unbroken values", strings.Repeat("s\u200big=", repeats) + "X", strings.Repeat("s\u200big=X ", repeats)},
		{"authorization values", strings.Repeat("a\u200buth=", repeats) + "X", strings.Repeat("a\u200buth=X&", repeats)},
		{"authorization credentials", strings.Repeat("a\u200buth=Basic\u00a0", repeats/2) + "X", strings.Repeat("a\u200buth=Basic X&", repeats/2)},
		{"quoted keys", `"` + strings.Repeat(`x\"=`, repeats/2) + "X", strings.Repeat(`"x"=X `, repeats/2)},
	}
}

func fastestCanonicalScan(value string) time.Duration {
	fastest := time.Duration(1<<63 - 1)
	for range 5 {
		started := time.Now()
		canonicalCredentialValueRanges(value)
		fastest = min(fastest, time.Since(started))
	}
	return fastest
}

func TestURLRedactionRejectsOversizedInputBeforeScanning(t *testing.T) {
	input := "https://host/path?token=oversized-secret&padding=" +
		strings.Repeat("x", maxScrubInputSize)
	started := time.Now()
	if got := RedactURL(input); got != "REDACTED" {
		t.Fatalf("RedactURL() = %q, want fixed fail-safe marker", got)
	}
	if got, evidence := ScrubCredentialURLsWithEvidence(input); got != "REDACTED" || evidence.Count() != 0 {
		t.Fatalf("ScrubCredentialURLsWithEvidence() = (%q, %d), want fixed marker and no evidence", got, evidence.Count())
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("direct URL redaction took %v on oversized input", elapsed)
	}
	allocs := testing.AllocsPerRun(20, func() {
		_ = RedactURL(input)
		_, _ = ScrubCredentialURLsWithEvidence(input)
	})
	if allocs > 0 {
		t.Fatalf("oversized URL redaction allocated %.0f times, want no scanning allocations", allocs)
	}
}

func TestReplaceJSONEscapedTokenAvoidsPerByteAllocations(t *testing.T) {
	const tokenLength = 4096
	token := strings.Repeat("x", tokenLength)
	input := strings.Repeat(`\u0078`, tokenLength)
	started := time.Now()
	got := replaceJSONEscapedToken(input, token)
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("replaceJSONEscapedToken() took %v on a bounded escaped value", elapsed)
	}
	if got != "REDACTED" {
		t.Fatalf("replaceJSONEscapedToken() = %q, want fixed marker", got)
	}
	allocs := testing.AllocsPerRun(5, func() {
		_ = replaceJSONEscapedToken(input, token)
	})
	if allocs > 100 {
		t.Fatalf("replaceJSONEscapedToken() allocated %.0f times, want bounded allocations", allocs)
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

func TestRedactURL_MalformedQueryStillPreservesOtherParameters(t *testing.T) {
	const secret = "malformed-query-secret"
	raw := "https://host/path?authorization=Bearer%20" + secret + "&keep=1%2F2%zz"
	got := RedactURL(raw)
	if strings.Contains(got, secret) {
		t.Errorf("RedactURL leaked malformed-query credential: %q", got)
	}
	if !strings.Contains(got, "authorization=REDACTED") {
		t.Errorf("RedactURL should mark malformed-query credential, got %q", got)
	}
	if !strings.Contains(got, "keep=1%2F2%zz") {
		t.Errorf("RedactURL should preserve unrelated malformed query data, got %q", got)
	}
}

func TestRedactURLRedactsSemicolonCredentialDelimiter(t *testing.T) {
	for _, rawURL := range []string{
		"https://host/path?keep=1;token=semicolon-secret",
		"https://host/%zz?keep=1;authorization=Bearer%20semicolon-secret",
	} {
		got := RedactURL(rawURL)
		if strings.Contains(got, "semicolon-secret") {
			t.Fatalf("RedactURL(%q) leaked semicolon-delimited credential: %q", rawURL, got)
		}
		if !strings.Contains(got, "REDACTED") {
			t.Fatalf("RedactURL(%q) = %q, want redaction marker", rawURL, got)
		}
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

type hiddenCause struct {
	inner error
}

func (cause hiddenCause) Error() string { return "opaque transport failure" }

func (cause hiddenCause) Unwrap() error { return cause.inner }

func TestSanitizeErrorWithTokenSeversHiddenCredentialChain(t *testing.T) {
	const secret = "hidden-chain-secret"
	classification := errors.New("safe classification")
	raw := hiddenCause{inner: fmt.Errorf("request token=%s: %w", secret, classification)}
	sanitized := SanitizeErrorWithToken(raw, secret)
	if sanitized == nil {
		t.Fatal("SanitizeErrorWithToken() = nil, want sanitized error")
	}
	if strings.Contains(sanitized.Error(), secret) {
		t.Fatalf("SanitizeErrorWithToken() leaked hidden child credential: %q", sanitized)
	}
	if errors.Unwrap(sanitized) != nil {
		t.Fatalf("SanitizeErrorWithToken() exposed hidden child through Unwrap: %v", errors.Unwrap(sanitized))
	}
	if !errors.Is(sanitized, classification) {
		t.Fatal("SanitizeErrorWithToken() lost safe error classification")
	}
	var recovered hiddenCause
	if errors.As(sanitized, &recovered) {
		t.Fatalf("SanitizeErrorWithToken() exposed hidden cause through As: %v", recovered)
	}
}

func TestSanitizeErrorWithTokenPreservesCancellationClassification(t *testing.T) {
	const token = "cancel-token"
	rawURL := "https://host/path?unknown=" + url.QueryEscape(token)
	raw := fmt.Errorf("request failed: %w", &url.Error{Op: "Get", URL: rawURL, Err: context.Canceled})
	sanitized := SanitizeErrorWithToken(raw, token)
	if sanitized == nil {
		t.Fatal("SanitizeErrorWithToken() = nil, want sanitized error")
	}
	if !errors.Is(sanitized, context.Canceled) {
		t.Fatalf("SanitizeErrorWithToken() lost context.Canceled classification: %v", sanitized)
	}
	if errors.Unwrap(sanitized) != nil {
		t.Fatalf("SanitizeErrorWithToken() exposed raw cancellation chain: %v", errors.Unwrap(sanitized))
	}
	if strings.Contains(sanitized.Error(), token) {
		t.Fatalf("SanitizeErrorWithToken() leaked token: %q", sanitized)
	}
}

func TestSanitizeErrorSeversHiddenCredentialChain(t *testing.T) {
	const secret = "hidden-sanitize-secret"
	classification := errors.New("safe classification")
	raw := hiddenCause{inner: fmt.Errorf("request token=%s: %w", secret, classification)}
	sanitized := SanitizeError(raw)
	if sanitized == nil {
		t.Fatal("SanitizeError() = nil, want sanitized error")
	}
	if strings.Contains(sanitized.Error(), secret) {
		t.Fatalf("SanitizeError() leaked hidden child credential: %q", sanitized)
	}
	if errors.Unwrap(sanitized) != nil {
		t.Fatalf("SanitizeError() exposed hidden child through Unwrap: %v", errors.Unwrap(sanitized))
	}
	if !errors.Is(sanitized, classification) {
		t.Fatal("SanitizeError() lost safe error classification")
	}
}

type customAsSecretError struct {
	secret string
}

func (cause customAsSecretError) Error() string { return "opaque transport failure" }

func (cause customAsSecretError) As(target any) bool {
	recovered, ok := target.(*customAsSecretError)
	if !ok {
		return false
	}
	*recovered = cause
	return true
}

func TestSanitizeErrorTreatsCustomAsAsOpaque(t *testing.T) {
	const secret = "custom-as-secret"
	raw := &url.Error{
		Op:  "Get",
		URL: "https://host/path?token=" + secret,
		Err: customAsSecretError{secret: secret},
	}
	sanitized := SanitizeError(raw)
	if sanitized == nil {
		t.Fatal("SanitizeError() = nil, want sanitized error")
	}
	var recovered customAsSecretError
	if errors.As(sanitized, &recovered) {
		t.Fatalf("SanitizeError() exposed custom As error: %#v", recovered)
	}
	if errors.Unwrap(sanitized) != nil {
		t.Fatalf("SanitizeError() exposed raw chain: %v", errors.Unwrap(sanitized))
	}
}

type cyclicSecretError struct{}

func (cyclicSecretError) Error() string { return "cyclic transport failure" }

func (cause *cyclicSecretError) Unwrap() error { return cause }

func TestSanitizeErrorBoundsCyclicClassificationTraversal(t *testing.T) {
	raw := &cyclicSecretError{}
	started := time.Now()
	sanitized := SanitizeErrorWithToken(raw, "cycle-token")
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("SanitizeErrorWithToken() took %v on cyclic error", elapsed)
	}
	if sanitized == nil || errors.Unwrap(sanitized) != nil {
		t.Fatalf("SanitizeErrorWithToken() = %v, want opaque non-unwrapping error", sanitized)
	}
	if !errors.Is(sanitized, raw) {
		t.Fatal("SanitizeErrorWithToken() lost exact cyclic classification")
	}
}

func TestRedactURLBoundsNestedFragmentWork(t *testing.T) {
	fragment := "token=fragment-secret"
	for index := 0; index < 200; index++ {
		fragment = "next=https://host/path#" + url.QueryEscape(fragment)
	}
	rawURL := "https://host/path#" + fragment
	started := time.Now()
	got := RedactURL(rawURL)
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("RedactURL() took %v on nested fragment", elapsed)
	}
	if strings.Contains(got, "fragment-secret") {
		t.Fatalf("RedactURL() leaked nested fragment secret: %q", got)
	}
}

func TestScrubEmbeddedURLsFailsClosedAfterCandidateBudget(t *testing.T) {
	t.Parallel()

	parts := make([]string, maxEmbeddedURLCandidates+1)
	for index := range parts {
		parts[index] = fmt.Sprintf("https://host-%d.test/path?token=embedded-secret-%d", index, index)
	}
	input := strings.Join(parts, " ")
	started := time.Now()
	got, _ := scrubEmbeddedURLsWithEvidence(input, 0)
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("scrubEmbeddedURLsWithEvidence() took %v on many candidates", elapsed)
	}
	if got != "REDACTED" {
		t.Fatalf("scrubEmbeddedURLsWithEvidence() = %q, want fixed fail-safe marker", got)
	}
}

func TestTokenVariantsBoundsOversizedToken(t *testing.T) {
	token := strings.Repeat("x", 1<<20)
	variants := tokenVariants(token)
	if len(variants) != 1 || variants[0] != token {
		t.Fatalf("tokenVariants(1MiB token) returned %d expanded variants", len(variants))
	}
}

func TestRedactWithTokenOmitsOversizedJSONEncodedToken(t *testing.T) {
	token := strings.Repeat("x", maxTokenVariantInputSize+1) + "\n"
	encoded := jsonEscapeToken(token)
	got := RedactWithToken("upstream payload="+encoded, token)
	if got != "REDACTED" {
		t.Fatalf("RedactWithToken() = %q, want opaque marker for oversized credential", got)
	}
}

type pointerClassificationError struct {
	message string
}

func (cause *pointerClassificationError) Error() string { return cause.message }

func TestSanitizedErrorMatchesOnlyExactPointerClassification(t *testing.T) {
	first := &pointerClassificationError{message: "same classification"}
	second := &pointerClassificationError{message: "same classification"}
	sanitized := SanitizeError(first)
	if !errors.Is(sanitized, first) {
		t.Fatal("SanitizeError() lost exact pointer classification")
	}
	if errors.Is(sanitized, second) {
		t.Fatal("SanitizeError() matched a distinct pointer classification")
	}
	runtime.GC()
	if errors.Is(sanitized, second) {
		t.Fatal("SanitizeError() matched a distinct pointer after GC")
	}
}

type interfaceFieldClassification struct {
	value any
}

func (cause interfaceFieldClassification) Error() string { return "interface classification" }

func TestSanitizedErrorContainsUncomparableInterfaceClassification(t *testing.T) {
	t.Parallel()

	raw := interfaceFieldClassification{value: []byte("credential")}
	sanitized := SanitizeError(raw)
	if sanitized == nil {
		t.Fatal("SanitizeError() = nil")
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("classification comparison panicked: %v", recovered)
		}
	}()
	if errors.Is(sanitized, raw) {
		t.Fatal("SanitizeError() matched an uncomparable dynamic classification")
	}
}

func TestSanitizedErrorPreservesComparableErrnoClassification(t *testing.T) {
	const first syscall.Errno = 13
	const second syscall.Errno = 14
	sanitized := SanitizeError(first)
	if !errors.Is(sanitized, first) {
		t.Fatal("SanitizeError() lost comparable errno classification")
	}
	if errors.Is(sanitized, second) {
		t.Fatal("SanitizeError() matched a distinct errno classification")
	}
}

func TestSanitizedErrorExposesOnlySafeNetworkMetadata(t *testing.T) {
	dns := &net.DNSError{Err: "no such host", Name: "token=credential.example"}
	sanitized := SanitizeError(fmt.Errorf("lookup failed: %w", dns))
	var recoveredDNS *net.DNSError
	if !errors.As(sanitized, &recoveredDNS) {
		t.Fatal("SanitizeError() did not preserve DNS classification")
	}
	if recoveredDNS.Err != "upstream DNS failure" || recoveredDNS.Name != "" || recoveredDNS.Server != "" {
		t.Fatalf("sanitized DNS metadata = %#v, want fixed fields", recoveredDNS)
	}
	var networkErr net.Error
	if !errors.As(sanitized, &networkErr) {
		t.Fatal("SanitizeError() did not preserve net.Error classification")
	}
	if networkErr.Timeout() {
		t.Fatal("non-timeout DNS error was classified as a timeout")
	}
	if strings.Contains(sanitized.Error(), "credential.example") {
		t.Fatalf("SanitizeError() exposed DNS credential marker: %v", sanitized)
	}
}

type timeoutNetworkError struct{}

func (timeoutNetworkError) Error() string   { return "network timeout" }
func (timeoutNetworkError) Timeout() bool   { return true }
func (timeoutNetworkError) Temporary() bool { return true }

func TestSanitizedErrorPreservesTimeoutNetworkMetadata(t *testing.T) {
	sanitized := SanitizeError(&timeoutNetworkError{})
	var networkErr net.Error
	if !errors.As(sanitized, &networkErr) || !networkErr.Timeout() {
		t.Fatalf("SanitizeError() network metadata = %#v, want timeout net.Error", sanitized)
	}
}

type panicErrorMessage struct{}

func (panicErrorMessage) Error() string { panic("hostile Error must not escape") }

type panicUnwrapError struct{}

func (panicUnwrapError) Error() string { return "panic unwrap" }
func (panicUnwrapError) Unwrap() error { panic("hostile Unwrap must not escape") }

type panicClassificationError struct{}

func (panicClassificationError) Error() string { return "panic classifications" }
func (panicClassificationError) classifications() []error {
	panic("hostile classifications must not escape")
}

type panicNetworkMetadataError struct{}

func (panicNetworkMetadataError) Error() string { return "panic metadata" }
func (panicNetworkMetadataError) networkMetadata() networkMetadata {
	panic("hostile network metadata must not escape")
}

type panicTimeoutError struct{}

func (panicTimeoutError) Error() string   { return "panic timeout" }
func (panicTimeoutError) Timeout() bool   { panic("hostile Timeout must not escape") }
func (panicTimeoutError) Temporary() bool { return false }

type wideUnwrapError struct {
	children []error
}

func (wideUnwrapError) Error() string       { return "wide unwrap" }
func (err wideUnwrapError) Unwrap() []error { return err.children }

func TestSanitizeErrorPanicSafeAcrossHostileInterfaces(t *testing.T) {
	t.Parallel()

	for _, raw := range []error{
		panicErrorMessage{}, panicUnwrapError{}, panicClassificationError{}, panicNetworkMetadataError{}, panicTimeoutError{},
	} {
		t.Run(fmt.Sprintf("%T", raw), func(t *testing.T) {
			sanitized := SanitizeError(raw)
			if sanitized == nil {
				t.Fatal("SanitizeError() = nil")
			}
			if got := sanitized.Error(); got == "" {
				t.Fatal("SanitizeError() returned an empty safe message")
			}
			if errors.Unwrap(sanitized) != nil {
				t.Fatalf("SanitizeError() exposed a hostile chain: %v", errors.Unwrap(sanitized))
			}
		})
	}
}

func TestSanitizeErrorBoundsTotalHostileUnwrapTree(t *testing.T) {
	t.Parallel()

	children := make([]error, maxSanitizeErrorChildren*maxSanitizeErrorChildren)
	for index := range children {
		children[index] = fmt.Errorf("child-%d", index)
	}
	collector := classificationCollector{}
	collector.collect(wideUnwrapError{children: children}, 0)
	if collector.nodes > maxSanitizeErrorNodes {
		t.Fatalf("classification traversal visited %d nodes, want <= %d", collector.nodes, maxSanitizeErrorNodes)
	}
	if len(collector.classifications) > maxSanitizeErrorDepth*2 {
		t.Fatalf("classification collection kept %d values, want <= %d", len(collector.classifications), maxSanitizeErrorDepth*2)
	}
}

func TestSanitizeErrorIgnoresTypedNilNetworkErrors(t *testing.T) {
	t.Parallel()

	var dns *net.DNSError
	var timeout *panicTimeoutError
	for _, raw := range []error{dns, timeout} {
		sanitized := SanitizeError(raw)
		if sanitized == nil {
			t.Fatal("SanitizeError(typed nil) = nil")
		}
		var dnsResult *net.DNSError
		if errors.As(sanitized, &dnsResult) {
			t.Fatalf("typed nil DNS error was exposed: %#v", dnsResult)
		}
		var networkResult net.Error
		if errors.As(sanitized, &networkResult) {
			t.Fatalf("typed nil net.Error was exposed: %#v", networkResult)
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
