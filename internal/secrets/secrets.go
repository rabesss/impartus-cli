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
	"strings"
)

// sensitiveParams are query-string keys whose values may carry credentials or
// signed tokens. Values are replaced with "REDACTED" before logging.
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
// or "&" boundary. It is deliberately tolerant of malformed URLs where
// url.Parse refuses, redacting only the value after the "=".
var sensitiveQueryRe = regexp.MustCompile(`(?i)([?&])(access_token|token|sig|signature|secret|key|api_key|auth)=[^&#\s]*`)

// scrubRawQuery redacts sensitive query-parameter values in a raw string. It is
// the fallback path for URLs that url.Parse cannot interpret.
func scrubRawQuery(s string) string {
	return sensitiveQueryRe.ReplaceAllString(s, "${1}${2}=REDACTED")
}

// RedactURL returns rawURL with any sensitive query parameters replaced by
// "REDACTED". If rawURL cannot be parsed, sensitive params are scrubbed from
// the raw string so a malformed tokenized URL cannot leak.
func RedactURL(rawURL string) string {
	if rawURL == "" {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		// Unparseable URL: scrub sensitive params from the raw string so a
		// malformed-but-tokenized URL cannot leak when url.Parse rejects it.
		return scrubRawQuery(rawURL)
	}
	params := u.Query()
	for key := range params {
		if isSensitiveParam(key) {
			params.Set(key, "REDACTED")
		}
	}
	u.RawQuery = params.Encode()
	return u.String()
}

func isSensitiveParam(key string) bool {
	for s := range sensitiveParams {
		if strings.EqualFold(key, s) {
			return true
		}
	}
	return false
}

// SanitizeError scrubs sensitive URL data from HTTP errors. http.Client.Do
// returns a *url.Error whose Error() string embeds the full request URL
// (including query tokens); this rebuilds it with a redacted URL so the value
// is safe to wrap with %w or log with %v.
func SanitizeError(err error) error {
	if err == nil {
		return nil
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		return &url.Error{Op: ue.Op, URL: RedactURL(ue.URL), Err: SanitizeError(ue.Err)}
	}
	// Defense-in-depth for non-*url.Error types (e.g. a plain error whose
	// message embeds a tokenized redirect URL): scrub any embedded URLs.
	// Unchanged messages pass through so the original error identity is kept.
	scrubbed := Scrub(err.Error())
	if scrubbed == err.Error() {
		return err
	}
	return errors.New(scrubbed)
}

// Scrub redacts sensitive query parameters from every http(s) URL embedded in
// free-form text (e.g. an arbitrary error string). It is a defense-in-depth
// layer for log paths that receive pre-formatted error messages.
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
