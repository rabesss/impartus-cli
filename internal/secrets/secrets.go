// Package secrets provides redaction helpers that keep sensitive data
// (notably auth tokens embedded in upstream URLs) out of logs and errors.
//
// It has no internal dependencies, so it can be imported by both
// internal/client and internal/downloader without creating an import cycle.
package secrets

import (
	"errors"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// sensitiveParams is the single source of truth for query-parameter keys whose
// values may carry credentials or signed tokens. Values are replaced with
// "REDACTED" before logging. The malformed-URL fallback regex is derived from
// these keys (see sensitiveQueryRe) so the two redaction paths cannot drift.
var sensitiveParams = map[string]bool{
	"access_token": true,
	"token":        true,
	"sig":          true,
	"signature":    true,
	"secret":       true,
	"key":          true,
	"api_key":      true,
	"auth":         true,
}

// urlTokenRe matches absolute http(s) URLs embedded in free-form text so they
// can be scrubbed even when an error string was built without a structured URL.
var urlTokenRe = regexp.MustCompile(`https?://[^\s"'<>]+`)

// sensitiveQueryRe matches sensitive query parameters (key=value) at a URL "?"
// or "&" boundary. It is built from sensitiveParams so there is one source of
// truth, and tolerates malformed URLs that url.Parse refuses.
var sensitiveQueryRe = buildSensitiveQueryRe()

// userinfoRe strips embedded HTTP basic-auth credentials (user:pass@) from raw
// URL strings, including those url.Parse cannot interpret.
var userinfoRe = regexp.MustCompile(`(?i)(https?://)[^/\s:@]+:[^/\s@]+@`)

func buildSensitiveQueryRe() *regexp.Regexp {
	keys := make([]string, 0, len(sensitiveParams))
	for k := range sensitiveParams {
		keys = append(keys, regexp.QuoteMeta(k))
	}
	sort.Strings(keys)
	return regexp.MustCompile(`(?i)([?&])(` + strings.Join(keys, "|") + `)=[^&#\s]*`)
}

func isSensitiveParam(key string) bool {
	return sensitiveParams[strings.ToLower(key)]
}

// scrubRawQuery redacts sensitive query-parameter values in a raw string. It
// operates on literal "?"/"&" boundaries so it works on decoded parameter
// values and on malformed URLs that url.Parse refuses.
func scrubRawQuery(s string) string {
	return sensitiveQueryRe.ReplaceAllString(s, "${1}${2}=REDACTED")
}

// scrubRaw strips embedded HTTP basic-auth userinfo and sensitive query
// parameters from a raw string. It is the fallback for URLs url.Parse rejects,
// and is also used to scrub decoded parameter values.
func scrubRaw(rawURL string) string {
	return scrubRawQuery(userinfoRe.ReplaceAllString(rawURL, "$1"))
}

// RedactURL returns rawURL with sensitive data scrubbed: embedded HTTP
// basic-auth userinfo is removed, and sensitive query parameters are replaced
// with "REDACTED". Tokens nested inside the value of a non-sensitive parameter
// (e.g. ?next=...?token=SECRET, including percent-encoded forms) are scrubbed
// too, since values are decoded before inspection. If rawURL cannot be parsed,
// the raw string is scrubbed directly.
func RedactURL(rawURL string) string {
	if rawURL == "" {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return scrubRaw(rawURL)
	}
	u.User = nil // strip embedded HTTP basic-auth credentials
	// Scrub sensitive keys, and scrub any sensitive URL embedded in the decoded
	// value of a non-sensitive parameter (covers percent-encoded nested tokens).
	params := u.Query()
	for key, vals := range params {
		if isSensitiveParam(key) {
			params[key] = []string{"REDACTED"}
			continue
		}
		for i, v := range vals {
			if scrubbed := scrubRaw(v); scrubbed != v {
				vals[i] = scrubbed
			}
		}
	}
	u.RawQuery = params.Encode()
	return u.String()
}

// SanitizeError scrubs sensitive URL data from HTTP errors. http.Client.Do and
// http.NewRequest return a *url.Error whose Error() embeds the full request URL
// (including query tokens); this rebuilds it with a redacted URL so the value is
// safe to wrap with %w or log with %v.
//
// A direct type assertion (not errors.As) is used deliberately: when a
// *url.Error is buried inside a wrapped error, the Scrub fallback rebuilds the
// whole message (preserving the outer context) rather than discarding it to
// return only the inner *url.Error.
func SanitizeError(err error) error {
	if err == nil {
		return nil
	}
	if ue, ok := err.(*url.Error); ok {
		return &url.Error{Op: ue.Op, URL: RedactURL(ue.URL), Err: SanitizeError(ue.Err)}
	}
	// Defense-in-depth for wrapped/non-*url.Error types whose message embeds a
	// tokenized URL: scrub any embedded URLs. An unchanged message passes through
	// so the original error identity is kept; a changed message yields an opaque
	// error with no chain back to the secret (a true redaction boundary).
	scrubbed := Scrub(err.Error())
	if scrubbed == err.Error() {
		return err
	}
	return errors.New(scrubbed)
}

// Scrub redacts sensitive data from every http(s) URL embedded in free-form
// text (e.g. an arbitrary error string). It is a defense-in-depth layer for log
// paths that receive pre-formatted error messages.
func Scrub(s string) string {
	if s == "" {
		return s
	}
	return urlTokenRe.ReplaceAllStringFunc(s, RedactURL)
}

// ScrubError returns the error's message with any embedded URLs scrubbed. It
// returns "" for a nil error.
func ScrubError(err error) string {
	if err == nil {
		return ""
	}
	return Scrub(err.Error())
}
