// Package secrets provides redaction helpers that keep sensitive data
// (notably auth tokens embedded in upstream URLs) out of logs and errors.
//
// It has no internal dependencies, so it can be imported by both
// internal/client and internal/downloader without creating an import cycle.
package secrets

import (
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// sensitiveParams is the single source of truth for URL parameter keys whose
// values may carry credentials or signed tokens. Values are replaced with
// "REDACTED" before logging. The malformed-URL fallback regex is derived from
// these keys (see sensitiveQueryRe) so the two redaction paths cannot drift.
var sensitiveParams = map[string]bool{
	"access_token":        true,
	"token":               true,
	"sig":                 true,
	"signature":           true,
	"secret":              true,
	"key":                 true,
	"api_key":             true,
	"auth":                true,
	"authorization":       true,
	"cookie":              true,
	"set-cookie":          true,
	"x-api-key":           true,
	"x_api_key":           true,
	"api-key":             true,
	"apikey":              true,
	"xapikey":             true,
	"proxy-authorization": true,
	"proxy_authorization": true,
	"proxyauthorization":  true,
	"set_cookie":          true,
	"setcookie":           true,
	"access-token":        true,
	"accesstoken":         true,
	"refresh_token":       true,
	"refresh-token":       true,
	"refreshtoken":        true,
	"client_secret":       true,
	"client-secret":       true,
	"clientsecret":        true,
	"password":            true,
}

// urlTokenRe matches absolute http(s) URLs embedded in free-form text so they
// can be scrubbed even when an error string was built without a structured URL.
var urlTokenRe = regexp.MustCompile(`https?://[^\s"'<>]+`)

// sensitiveQueryRe matches sensitive query or fragment parameters (key=value)
// at a URL delimiter boundary. It is built from sensitiveParams so there is
// one source of truth, and tolerates malformed URLs that url.Parse refuses.
var sensitiveQueryRe = buildSensitiveQueryRe()

// userinfoRe strips any HTTP userinfo (including username-only and
// percent-encoded forms) from raw URL strings, including those url.Parse
// cannot interpret. Query and fragment delimiters stop the authority scan so
// an @ in ordinary diagnostic data is not mistaken for userinfo.
var userinfoRe = regexp.MustCompile(`(?i)(https?://)[^/?#\s@]*@`)

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
	`(?i)(^|[^/\\a-z0-9_-])((?:authorization|proxy[-_]?authorization|auth|(?:x[-_])?api[-_]?key|cookie|set[-_]?cookie)\s*[:=]\s*)[^\r\n]+`,
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
		keys = append(keys, encodedQueryKeyPattern(k))
	}
	sort.Strings(keys)
	return regexp.MustCompile(`(?i)([?&;#])(` + strings.Join(keys, "|") + `)(?:=|%3d)[^&#;\s]*`)
}

// encodedQueryKeyPattern matches a credential key whether its bytes are
// written literally or percent-encoded. URL parsing handles this distinction
// for well-formed URLs; the raw fallback uses this pattern when an unrelated
// component (for example, a malformed path escape) prevents parsing.
func encodedQueryKeyPattern(key string) string {
	var pattern strings.Builder
	pattern.WriteString("(?:")
	for i := 0; i < len(key); i++ {
		pattern.WriteString("(?:")
		pattern.WriteString(regexp.QuoteMeta(string(key[i])))
		pattern.WriteString("|%")
		const hex = "0123456789ABCDEF"
		pattern.WriteByte(hex[key[i]>>4])
		pattern.WriteByte(hex[key[i]&0x0f])
		pattern.WriteString(")")
	}
	pattern.WriteString(")")
	return pattern.String()
}

func buildSensitiveAssignmentKey() string {
	keys := make([]string, 0, len(sensitiveParams)+5)
	for key := range sensitiveParams {
		keys = append(keys, regexp.QuoteMeta(key))
	}
	keys = append(keys, "authorization", "password", "cookie", `set[-_]?cookie`, `(?:x[_-])?api[_-]?key`, `[a-z0-9_-]+(?:token|password|secret|signature)`)
	sort.Strings(keys)
	return `(?:` + strings.Join(keys, "|") + `)`
}

func isSensitiveParam(key string) bool {
	for depth := 0; depth <= 2; depth++ {
		lowerKey := strings.ToLower(key)
		if sensitiveParams[lowerKey] || sensitiveParams[strings.NewReplacer("-", "_").Replace(lowerKey)] {
			return true
		}
		decoded, err := url.QueryUnescape(key)
		if err != nil || decoded == key {
			break
		}
		key = decoded
	}
	return false
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
		separator := strings.IndexByte(match, '=')
		separatorWidth := 1
		if separator < 0 {
			separator = strings.Index(strings.ToLower(match), "%3d")
			separatorWidth = len("%3d")
		}
		if separator >= 0 {
			value := match[separator+separatorWidth:]
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
// userinfo is removed, and sensitive query or fragment parameters are replaced
// with "REDACTED". Tokens nested inside the value of a non-sensitive parameter
// (e.g. ?next=...?token=SECRET, including percent-encoded forms) are scrubbed
// too, since values are decoded before inspection. If rawURL cannot be parsed,
// the raw string is scrubbed directly.
func RedactURL(rawURL string) string {
	redacted, _, _ := redactURLWithEvidence(rawURL)
	return redacted
}

// RedactURLWithToken redacts URL credentials and the caller-provided token,
// even when the token appears under an unknown query key. It never returns a
// token-bearing representation to the caller.
func RedactURLWithToken(rawURL, token string) string {
	return replaceTokenVariants(RedactURL(rawURL), token)
}

// RedactWithToken scrubs free-form text and the caller-provided token. It is
// intended for errors whose URL may have been rewritten by a redirect.
func RedactWithToken(value, token string) string {
	return replaceTokenVariants(Scrub(value), token)
}

// SanitizeErrorWithToken scrubs an error chain and then removes every known
// representation of token from its presentation. If token material was found,
// the returned error is intentionally opaque and does not unwrap to the leak.
func SanitizeErrorWithToken(err error, token string) error {
	if err == nil {
		return nil
	}
	sanitized := SanitizeError(err)
	if sanitized == nil {
		return nil
	}
	redacted := RedactWithToken(sanitized.Error(), token)
	if redacted == sanitized.Error() && !errorChainContainsToken(err, token, 0) {
		return sanitized
	}
	return newSanitizedError(redacted, err)
}

// errorChainContainsToken checks every error that a caller could reach before
// sanitization. A wrapper may hide its child credential from Error(), so only
// checking the outer message would leave the original chain reachable.
func errorChainContainsToken(err error, token string, depth int) bool {
	if err == nil || depth >= maxSanitizeErrorDepth || token == "" {
		return false
	}
	if replaceTokenVariants(err.Error(), token) != err.Error() {
		return true
	}
	if multi, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range multi.Unwrap() {
			if errorChainContainsToken(child, token, depth+1) {
				return true
			}
		}
		return false
	}
	if single, ok := err.(interface{ Unwrap() error }); ok {
		return errorChainContainsToken(single.Unwrap(), token, depth+1)
	}
	return false
}

func replaceTokenVariants(value, token string) string {
	variants := tokenVariants(token)
	for _, variant := range variants {
		if variant == "" {
			continue
		}
		value = replaceCaseInsensitive(value, variant, "REDACTED")
	}
	return value
}

func tokenVariants(token string) []string {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return nil
	}
	values := []string{trimmed}
	fields := strings.Fields(trimmed)
	if len(fields) >= 2 && strings.EqualFold(fields[0], "bearer") {
		values = append(values, strings.TrimSpace(strings.TrimPrefix(trimmed, fields[0])))
	} else {
		values = append(values, "Bearer "+trimmed)
	}
	// A token can be copied through more than one URL parser before it reaches
	// an error string. Keep the expansion bounded while covering the raw,
	// standard query/path, fully percent-encoded, and twice-encoded forms.
	const maxEncodingDepth = 2
	variants := make([]string, 0, len(values)*20)
	seen := make(map[string]struct{}, len(values)*20)
	for _, value := range values {
		frontier := []string{value}
		for depth := 0; depth <= maxEncodingDepth; depth++ {
			next := make([]string, 0, len(frontier)*3)
			for _, candidate := range frontier {
				if candidate != "" {
					if _, ok := seen[candidate]; !ok {
						seen[candidate] = struct{}{}
						variants = append(variants, candidate)
					}
				}
				if depth == maxEncodingDepth {
					continue
				}
				next = append(next,
					url.QueryEscape(candidate),
					url.PathEscape(candidate),
					fullyPercentEncodeToken(candidate),
					jsonEscapeToken(candidate),
				)
			}
			frontier = next
		}
	}
	// Replace longer encodings first. This prevents a shorter representation
	// from consuming part of a longer, still-secret representation.
	sort.SliceStable(variants, func(left, right int) bool {
		return len(variants[left]) > len(variants[right])
	})
	return variants
}

func fullyPercentEncodeToken(value string) string {
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

func jsonEscapeToken(value string) string {
	escaped, err := json.Marshal(value)
	if err != nil || len(escaped) < 2 {
		return ""
	}
	return string(escaped[1 : len(escaped)-1])
}

func replaceCaseInsensitive(value, needle, replacement string) string {
	lowerValue := strings.ToLower(value)
	lowerNeedle := strings.ToLower(needle)
	if lowerNeedle == "" {
		return value
	}
	var scrubbed strings.Builder
	searchFrom := 0
	for searchFrom < len(lowerValue) {
		index := strings.Index(lowerValue[searchFrom:], lowerNeedle)
		if index < 0 {
			break
		}
		index += searchFrom
		scrubbed.WriteString(value[searchFrom:index])
		scrubbed.WriteString(replacement)
		searchFrom = index + len(needle)
	}
	if searchFrom == 0 {
		return value
	}
	scrubbed.WriteString(value[searchFrom:])
	return scrubbed.String()
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
	if u.Fragment != "" {
		fragment, fragmentEvidence := scrubFragmentWithEvidence(u.Fragment)
		if fragment != u.Fragment {
			u.Fragment = fragment
			// RawFragment is an optional spelling of Fragment. Clear it after
			// changing the decoded value so URL.String cannot restore the leak.
			u.RawFragment = ""
			changed = true
		}
		evidence.values = append(evidence.values, fragmentEvidence.values...)
	}
	// Scrub sensitive keys, and scrub any sensitive URL embedded in the decoded
	// value of a non-sensitive parameter (covers percent-encoded nested tokens).
	params, queryErr := url.ParseQuery(u.RawQuery)
	if queryErr != nil {
		// url.Parse accepts some malformed raw queries and URL.Query silently
		// drops their values. Fall back to delimiter-preserving scrubbing so a
		// malformed escape cannot either leak a credential or erase diagnostics.
		redacted, rawEvidence := scrubRawWithEvidence(rawURL)
		return redacted, redacted != rawURL, evidence.Combined(rawEvidence)
	}
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

// scrubFragmentWithEvidence checks a fragment after a small, fixed number of
// percent-decoding passes. Fragments sometimes carry redirect metadata that
// has already been encoded once or twice by the caller. Returning the first
// scrubbed decoded spelling is safe because it discards the original encoding
// only when a credential was found.
func scrubFragmentWithEvidence(fragment string) (string, RedactionEvidence) {
	candidate := fragment
	for depth := 0; depth <= 2; depth++ {
		// Treat query-shaped fragments as parameter lists so an &-delimited
		// safe field is retained when a preceding credential is removed.
		scrubbed, evidence := scrubRawQueryWithEvidence("#" + candidate)
		if scrubbed != "#"+candidate {
			return strings.TrimPrefix(scrubbed, "#"), evidence
		}
		scrubbed, evidence = ScrubWithEvidence(candidate)
		if scrubbed != candidate {
			return scrubbed, evidence
		}
		next, err := url.QueryUnescape(candidate)
		if err != nil || next == candidate {
			break
		}
		candidate = next
	}
	return fragment, RedactionEvidence{}
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
	_, sanitized := sanitizeErrorTree(err, 0)
	return sanitized
}

const maxSanitizeErrorDepth = 64

// sanitizeErrorTree sanitizes every reachable child before deciding whether
// the current error can be returned. Generic wrappers cannot be rebuilt with a
// sanitized child, so a changed child produces an opaque error that retains
// only errors.Is classification through sanitizedError.Is. Known URL errors
// can be rebuilt and keep their useful type and operation metadata.
func sanitizeErrorTree(err error, depth int) (bool, error) {
	if err == nil {
		return false, nil
	}
	if depth >= maxSanitizeErrorDepth {
		return true, newSanitizedError(Scrub(err.Error()), err)
	}
	if ue, ok := err.(*url.Error); ok {
		safeURL := RedactURL(ue.URL)
		childChanged, safeErr := sanitizeErrorTree(ue.Err, depth+1)
		if safeURL == ue.URL && !childChanged {
			return false, err
		}
		return true, &url.Error{Op: ue.Op, URL: safeURL, Err: safeErr}
	}

	scrubbed := Scrub(err.Error())
	childChanged := false
	if multi, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range multi.Unwrap() {
			changed, safeChild := sanitizeErrorTree(child, depth+1)
			_ = safeChild
			childChanged = childChanged || changed
		}
	} else if single, ok := err.(interface{ Unwrap() error }); ok {
		var safeChild error
		childChanged, safeChild = sanitizeErrorTree(single.Unwrap(), depth+1)
		_ = safeChild
	}
	if scrubbed == err.Error() && !childChanged {
		return false, err
	}
	if _, unwraps := err.(interface{ Unwrap() error }); unwraps {
		return true, newSanitizedError(scrubbed, err)
	}
	if _, unwraps := err.(interface{ Unwrap() []error }); unwraps {
		return true, newSanitizedError(scrubbed, err)
	}
	return true, errors.New(scrubbed)
}

// sanitizedError deliberately has no Unwrap or As method. Its private cause
// is used only to preserve errors.Is classification, never exposed as a
// reachable child that could carry a credential.
type sanitizedError struct {
	message string
	cause   error
}

func newSanitizedError(message string, cause error) error {
	return sanitizedError{message: message, cause: cause}
}

func (err sanitizedError) Error() string { return err.message }

func (err sanitizedError) Is(target error) bool {
	return err.cause != nil && errors.Is(err.cause, target)
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
