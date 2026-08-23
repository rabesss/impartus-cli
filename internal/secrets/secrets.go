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
	"unicode"
	"unicode/utf8"
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

// Free-form response bodies use the same exact credential keys as URL query
// redaction, plus common suffix forms such as refresh_token and client_secret.
// Building the regex from sensitiveParams keeps sig/signature and future keys
// from drifting between URL and body sanitization.
var sensitiveAssignmentKey = buildSensitiveAssignmentKey()
var credentialSchemes = []string{"bearer", "basic", "token", "apikey", "oauth"}
var quotedSecretValue = regexp.MustCompile(
	`(?i)(\b` + sensitiveAssignmentKey + `\s*["']\s*[:=]\s*")((?:\\.|[^"\\])*)`,
)
var singleQuotedSecretValue = regexp.MustCompile(
	`(?i)(\b` + sensitiveAssignmentKey + `\s*["']\s*[:=]\s*')((?:\\.|[^'\\])*)`,
)
var quotedKeySchemeSecretValue = regexp.MustCompile(
	`(?i)(\b` + sensitiveAssignmentKey + `\s*["']\s*[:=]\s*)(?:` + strings.Join(credentialSchemes, "|") + `)\s+[^\s,;}]+`,
)
var quotedKeyBareSecretValue = regexp.MustCompile(
	`(?i)(\b` + sensitiveAssignmentKey + `\s*["']\s*[:=]\s*)[^\s"',;}][^\s,;}]*`,
)
var strongCredentialAssignment = regexp.MustCompile(
	`(?i)(^|[^/\\a-z0-9_-])((?:authorization|proxy[-_]?authorization|auth|(?:x[-_])?api[-_]?key)\s*[:=]\s*)[^\r\n]+`,
)
var schemeSecretAssignment = regexp.MustCompile(
	`(?i)(^|[^/\\a-z0-9_-])(` + sensitiveAssignmentKey + `\s*[:=]\s*)(?:` + strings.Join(credentialSchemes, "|") + `)\s+[^\s,;}]+`,
)
var bareSecretEquals = regexp.MustCompile(
	`(?i)(^|[^/\\a-z0-9_-])(` + sensitiveAssignmentKey + `\s*=\s*)[^\s,;}]+`,
)
var bareSecretColon = regexp.MustCompile(
	`(?i)(^|[^/\\a-z0-9_-])(` + sensitiveAssignmentKey + `\s*:\s*)[^\s,;}]+`,
)

// RedactionEvidence records credential values recognized during a scrub. The
// values remain private so callers can compare detection views without making
// sensitive text available for logging or presentation.
type RedactionEvidence struct {
	values []string
}

// Count returns the number of credential values recognized during a scrub.
func (evidence RedactionEvidence) Count() int {
	return len(evidence.values)
}

// Combined returns the evidence from both scrub passes without exposing the
// credential values they contain.
func (evidence RedactionEvidence) Combined(other RedactionEvidence) RedactionEvidence {
	combined := RedactionEvidence{values: make([]string, 0, len(evidence.values)+len(other.values))}
	combined.values = append(combined.values, evidence.values...)
	combined.values = append(combined.values, other.values...)
	return combined
}

// HasVisibleValueIn reports whether any recognized credential value remains in
// candidate after both are transformed into the caller's presentation view.
// The values stay private: callers can ask the security question without
// receiving credential material that could accidentally be logged.
func (evidence RedactionEvidence) HasVisibleValueIn(candidate string, normalize func(string) string) bool {
	visible := normalize(candidate)
	for _, value := range evidence.values {
		credential := normalize(value)
		if credential != "" && containsVisibleCredential(visible, credential) {
			return true
		}
	}
	return false
}

func containsVisibleCredential(visible, credential string) bool {
	// Short values commonly occur inside unrelated words or status numbers.
	// Their real scrub sites are delimiter-bounded assignments or URL fields,
	// so require the same token boundaries when checking the sanitized output.
	if utf8.RuneCountInString(credential) > 2 {
		return strings.Contains(visible, credential)
	}
	for searchFrom := 0; searchFrom <= len(visible)-len(credential); {
		index := strings.Index(visible[searchFrom:], credential)
		if index < 0 {
			return false
		}
		index += searchFrom
		end := index + len(credential)
		beforeBoundary := index == 0
		if !beforeBoundary {
			character, _ := utf8.DecodeLastRuneInString(visible[:index])
			beforeBoundary = !unicode.IsLetter(character) && !unicode.IsNumber(character)
		}
		afterBoundary := end == len(visible)
		if !afterBoundary {
			character, _ := utf8.DecodeRuneInString(visible[end:])
			afterBoundary = !unicode.IsLetter(character) && !unicode.IsNumber(character)
		}
		if beforeBoundary && afterBoundary {
			return true
		}
		searchFrom = end
	}
	return false
}

func buildSensitiveQueryRe() *regexp.Regexp {
	keys := make([]string, 0, len(sensitiveParams))
	for k := range sensitiveParams {
		keys = append(keys, regexp.QuoteMeta(k))
	}
	sort.Strings(keys)
	return regexp.MustCompile(`(?i)([?&])(` + strings.Join(keys, "|") + `)=[^&#\s]*`)
}

func buildSensitiveAssignmentKey() string {
	keys := make([]string, 0, len(sensitiveParams)+3)
	for key := range sensitiveParams {
		keys = append(keys, regexp.QuoteMeta(key))
	}
	keys = append(keys, "authorization", "password", `(?:x[_-])?api[_-]?key`, `[a-z0-9_-]+(?:token|password|secret|signature)`)
	sort.Strings(keys)
	return `(?:` + strings.Join(keys, "|") + `)`
}

func isSensitiveParam(key string) bool {
	return sensitiveParams[strings.ToLower(key)]
}

// IsCredentialScheme reports whether value is an authentication scheme that
// gives the following word credential semantics in free-form diagnostics.
func IsCredentialScheme(value string) bool {
	for _, scheme := range credentialSchemes {
		if strings.EqualFold(value, scheme) {
			return true
		}
	}
	return false
}

func scrubRawQueryWithEvidence(s string) (string, RedactionEvidence) {
	var evidence RedactionEvidence
	indices := sensitiveQueryRe.FindAllStringSubmatchIndex(s, -1)
	if len(indices) == 0 {
		return s, evidence
	}
	var scrubbed strings.Builder
	last := 0
	for _, index := range indices {
		scrubbed.WriteString(s[last:index[0]])
		scrubbed.Write(sensitiveQueryRe.ExpandString(nil, "${1}${2}=REDACTED", s, index))
		match := s[index[0]:index[1]]
		if separator := strings.IndexByte(match, '='); separator >= 0 {
			value := match[separator+1:]
			if value != "REDACTED" {
				evidence.values = append(evidence.values, value)
			}
		}
		last = index[1]
	}
	scrubbed.WriteString(s[last:])
	return scrubbed.String(), evidence
}

func scrubRawWithEvidence(rawURL string) (string, RedactionEvidence) {
	var evidence RedactionEvidence
	indices := userinfoRe.FindAllStringSubmatchIndex(rawURL, -1)
	var withoutUserinfo strings.Builder
	last := 0
	for _, index := range indices {
		withoutUserinfo.WriteString(rawURL[last:index[0]])
		withoutUserinfo.Write(userinfoRe.ExpandString(nil, "$1", rawURL, index))
		credential := strings.TrimSuffix(rawURL[index[3]:index[1]], "@")
		if credential != "REDACTED" {
			evidence.values = append(evidence.values, credential)
		}
		last = index[1]
	}
	withoutUserinfo.WriteString(rawURL[last:])
	scrubbed, queryEvidence := scrubRawQueryWithEvidence(withoutUserinfo.String())
	evidence.values = append(evidence.values, queryEvidence.values...)
	return scrubbed, evidence
}

// ScrubURLs redacts credentials from absolute HTTP URLs embedded in text
// without applying the free-form assignment rules.
func ScrubURLs(s string) string {
	return ScrubCredentialURLs(s)
}

// ScrubCredentialURLs redacts only URL candidates that actually contain
// credentials. Candidates without credentials are returned byte-for-byte so a
// caller can use an internal separator marker without re-encoding prose.
func ScrubCredentialURLs(s string) string {
	scrubbed, _ := ScrubCredentialURLsWithEvidence(s)
	return scrubbed
}

// ScrubCredentialURLsWithEvidence applies ScrubCredentialURLs and returns
// opaque evidence for every credential value it recognized. The evidence is
// intended only for in-process comparison between differently decoded views.
func ScrubCredentialURLsWithEvidence(s string) (string, RedactionEvidence) {
	if s == "" {
		return s, RedactionEvidence{}
	}
	var evidence RedactionEvidence
	indices := urlTokenRe.FindAllStringIndex(s, -1)
	if len(indices) == 0 {
		return s, evidence
	}
	var scrubbed strings.Builder
	last := 0
	for _, index := range indices {
		scrubbed.WriteString(s[last:index[0]])
		rawURL := s[index[0]:index[1]]
		redacted, changed, urlEvidence := redactURLWithEvidence(rawURL)
		if changed {
			scrubbed.WriteString(redacted)
		} else {
			scrubbed.WriteString(rawURL)
		}
		evidence.values = append(evidence.values, urlEvidence.values...)
		last = index[1]
	}
	scrubbed.WriteString(s[last:])
	return scrubbed.String(), evidence
}

// RedactURL returns rawURL with sensitive data scrubbed: embedded HTTP
// basic-auth userinfo is removed, and sensitive query parameters are replaced
// with "REDACTED". Tokens nested inside the value of a non-sensitive parameter
// (e.g. ?next=...?token=SECRET, including percent-encoded forms) are scrubbed
// too, since values are decoded before inspection. If rawURL cannot be parsed,
// the raw string is scrubbed directly.
func RedactURL(rawURL string) string {
	redacted, _, _ := redactURLWithEvidence(rawURL)
	return redacted
}

func redactURLWithEvidence(rawURL string) (string, bool, RedactionEvidence) {
	var evidence RedactionEvidence
	if rawURL == "" {
		return rawURL, false, evidence
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		redacted, rawEvidence := scrubRawWithEvidence(rawURL)
		return redacted, redacted != rawURL, rawEvidence
	}
	changed := u.User != nil
	if u.User != nil {
		evidence.values = append(evidence.values, u.User.String())
	}
	u.User = nil // strip embedded HTTP basic-auth credentials
	// Scrub sensitive keys, and scrub any sensitive URL embedded in the decoded
	// value of a non-sensitive parameter (covers percent-encoded nested tokens).
	params := u.Query()
	for key, vals := range params {
		if isSensitiveParam(key) {
			for _, value := range vals {
				if value != "REDACTED" {
					changed = true
					evidence.values = append(evidence.values, value)
				}
			}
			params[key] = []string{"REDACTED"}
			continue
		}
		for i, v := range vals {
			if scrubbed, nestedEvidence := scrubRawWithEvidence(v); scrubbed != v {
				vals[i] = scrubbed
				changed = true
				evidence.values = append(evidence.values, nestedEvidence.values...)
			}
		}
	}
	u.RawQuery = params.Encode()
	return u.String(), changed, evidence
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

// Scrub redacts sensitive URLs and credential assignments from free-form text.
// It is the shared defense-in-depth boundary for logs, terminal output, and
// durable error summaries.
func Scrub(s string) string {
	scrubbed, _ := ScrubWithEvidence(s)
	return scrubbed
}

// ScrubWithEvidence applies Scrub and returns opaque evidence for every
// credential value it recognized. Callers must use the evidence only for
// in-process comparison and must never persist or present it.
func ScrubWithEvidence(s string) (string, RedactionEvidence) {
	if s == "" {
		return s, RedactionEvidence{}
	}
	scrubbed, evidence := ScrubCredentialURLsWithEvidence(s)
	for _, step := range []struct {
		expression  *regexp.Regexp
		prefixGroup int
		replacement string
	}{
		{quotedSecretValue, 1, "${1}REDACTED"},
		{singleQuotedSecretValue, 1, "${1}REDACTED"},
		{quotedKeySchemeSecretValue, 1, "${1}REDACTED"},
		{quotedKeyBareSecretValue, 1, "${1}REDACTED"},
		{strongCredentialAssignment, 2, "${1}${2}REDACTED"},
		{schemeSecretAssignment, 2, "${1}${2}REDACTED"},
		{bareSecretEquals, 2, "${1}${2}REDACTED"},
		{bareSecretColon, 2, "${1}${2}REDACTED"},
	} {
		var stepEvidence RedactionEvidence
		scrubbed, stepEvidence = replaceCredentialValues(
			scrubbed,
			step.expression,
			step.prefixGroup,
			step.replacement,
		)
		evidence.values = append(evidence.values, stepEvidence.values...)
	}
	return scrubbed, evidence
}

func replaceCredentialValues(
	value string,
	expression *regexp.Regexp,
	prefixGroup int,
	replacement string,
) (string, RedactionEvidence) {
	var evidence RedactionEvidence
	indices := expression.FindAllStringSubmatchIndex(value, -1)
	if len(indices) == 0 {
		return value, evidence
	}
	var scrubbed strings.Builder
	last := 0
	for _, index := range indices {
		scrubbed.WriteString(value[last:index[0]])
		scrubbed.Write(expression.ExpandString(nil, replacement, value, index))
		prefixEnd := index[prefixGroup*2+1]
		credential := value[prefixEnd:index[1]]
		if credential != "REDACTED" {
			evidence.values = append(evidence.values, credential)
		}
		last = index[1]
	}
	scrubbed.WriteString(value[last:])
	return scrubbed.String(), evidence
}

// ScrubError returns the error's message with embedded credentials scrubbed.
// It returns "" for a nil error.
func ScrubError(err error) string {
	if err == nil {
		return ""
	}
	return Scrub(err.Error())
}
