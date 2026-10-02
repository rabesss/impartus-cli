// Package secrets provides redaction helpers that keep sensitive data
// (notably auth tokens embedded in upstream URLs) out of logs and errors.
//
// It has no internal dependencies, so it can be imported by both
// internal/client and internal/downloader without creating an import cycle.
package secrets

import (
	"encoding/json"
	"net"
	"net/url"
	"reflect"
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
var urlTokenRe = regexp.MustCompile(`(?i)https?://[^\s"'<>]+`)

// sensitiveQueryRe matches sensitive query or fragment parameters (key=value)
// at a URL delimiter boundary. It is built from sensitiveParams so there is
// one source of truth, and tolerates malformed URLs that url.Parse refuses.
var sensitiveQueryRe = buildSensitiveQueryRe()

// httpsSchemeRe finds URL authorities in raw diagnostic text. Userinfo is
// stripped with stripRawUserinfo rather than a single regexp match so
// repeated "@" delimiters cannot leave an earlier credential-bearing prefix.
var httpsSchemeRe = regexp.MustCompile(`(?i)https?://`)

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
	`(?i)(\b` + sensitiveAssignmentKey + `\s*["']\s*[:=]\s*)(?:` + strings.Join(credentialSchemes, "|") + `)\s+[^\s,;&}]+`,
)
var quotedKeyBareSecretValue = regexp.MustCompile(
	`(?i)(\b` + sensitiveAssignmentKey + `\s*["']\s*[:=]\s*)[^\s"',;&}][^\s,;&}]*`,
)
var strongCredentialAssignment = regexp.MustCompile(
	`(?i)(^|[^/\\a-z0-9_-])((?:authorization|proxy[-_]?authorization|auth|(?:x[-_])?api[-_]?key|cookie|set[-_]?cookie)\s*[:=]\s*)[^\r\n&]+`,
)
var schemeSecretAssignment = regexp.MustCompile(
	`(?i)(^|[^/\\a-z0-9_-])(` + sensitiveAssignmentKey + `\s*[:=]\s*)(?:` + strings.Join(credentialSchemes, "|") + `)\s+[^\s,;&}]+`,
)
var bareSecretEquals = regexp.MustCompile(
	`(?i)(^|[^/\\a-z0-9_-])(` + sensitiveAssignmentKey + `\s*=\s*)[^\s,;&}]+`,
)
var bareSecretColon = regexp.MustCompile(
	`(?i)(^|[^/\\a-z0-9_-])(` + sensitiveAssignmentKey + `\s*:\s*)[^\s,;&}]+`,
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
	if len(key) > maxCredentialKeyRunes*utf8.UTFMax {
		// A key this large cannot be a normal bounded alias. Treat it as
		// sensitive so callers that use this predicate for request policy do not
		// forward a credential hidden in an attacker-sized encoding.
		return true
	}
	for depth := 0; depth <= maxSensitiveParamDecodeDepth; depth++ {
		if isCanonicalSensitiveParamKey(key) {
			return true
		}
		decoded, err := url.QueryUnescape(key)
		if err != nil || decoded == key {
			return false
		}
		key = decoded
	}
	// A still-changing key has exceeded the bounded canonicalization budget.
	// Keep request policy fail-closed rather than forwarding a value whose
	// sensitive alias is hidden behind more decoding layers.
	return true
}

const maxSensitiveParamDecodeDepth = maxCredentialDecodeDepth*2 + 2

// isCanonicalSensitiveParamKey applies the same bounded JSON decoding and
// Unicode format-character removal used by free-form assignment scrubbing.
// URL query keys are attacker-controlled, so do not allocate decoded views for
// keys larger than the assignment-key budget.
func isCanonicalSensitiveParamKey(key string) bool {
	if len(key) > maxCredentialKeyRunes*utf8.UTFMax {
		return false
	}
	for depth := 0; depth <= maxCredentialDecodeDepth; depth++ {
		canonical := canonicalCredentialKey(key)
		if sensitiveParams[canonical] || sensitiveParams[strings.ReplaceAll(canonical, "-", "_")] {
			return true
		}
		if !strings.Contains(key, `\`) {
			return false
		}
		view := decodeEncodedViewLayer(rawEncodedView(key), false)
		if !view.changed || len(view.text) > maxCredentialKeyRunes*utf8.UTFMax {
			return false
		}
		key = view.text
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

// IsSensitiveQueryKey reports whether key is one of the credential-bearing
// query aliases covered by URL redaction. It is shared with request policy
// code so stripping and diagnostics cannot drift as aliases are added.
func IsSensitiveQueryKey(key string) bool {
	return isSensitiveParam(key)
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
	return scrubRawWithEvidenceDepth(rawURL, 0)
}

const maxRawPercentDecodeDepth = 3

func scrubRawWithEvidenceDepth(rawURL string, depth int) (string, RedactionEvidence) {
	var evidence RedactionEvidence
	withoutUserinfo, userinfoEvidence := stripRawUserinfo(rawURL)
	evidence.values = append(evidence.values, userinfoEvidence.values...)
	scrubbed, queryEvidence := scrubRawQueryWithEvidence(withoutUserinfo)
	evidence.values = append(evidence.values, queryEvidence.values...)
	// A decoded non-sensitive query value can still contain a bare assignment
	// such as token=SECRET. Apply the same bounded assignment scrubber used by
	// free-form diagnostics before returning a raw URL view.
	if assigned, assignmentEvidence := scrubEncodedAssignmentsWithEvidence(scrubbed); assigned != scrubbed {
		scrubbed = assigned
		evidence.values = append(evidence.values, assignmentEvidence.values...)
	}
	if depth >= maxRawPercentDecodeDepth {
		if _, stillEncoded := decodePercentEscapes(scrubbed); stillEncoded {
			// A credential may still be hidden behind another percent-encoded
			// layer. Do not return a partially decoded representation after the
			// bounded work budget is exhausted.
			return "REDACTED", evidence
		}
		return scrubbed, evidence
	}
	decoded, changed := decodePercentEscapes(scrubbed)
	if !changed {
		return scrubbed, evidence
	}
	if embedded, embeddedEvidence := scrubEmbeddedURLsWithEvidence(decoded, depth); embedded != decoded {
		evidence.values = append(evidence.values, embeddedEvidence.values...)
		return embedded, evidence
	}
	decodedScrubbed, decodedEvidence := scrubRawWithEvidenceDepth(decoded, depth+1)
	if decodedScrubbed == decoded {
		return scrubbed, evidence
	}
	evidence.values = append(evidence.values, decodedEvidence.values...)
	return decodedScrubbed, evidence
}

func scrubEmbeddedURLsWithEvidence(value string, depth int) (string, RedactionEvidence) {
	if depth >= maxURLRedactionDepth {
		return value, RedactionEvidence{}
	}
	indices := httpsSchemeRe.FindAllStringIndex(value, -1)
	if len(indices) == 0 {
		return value, RedactionEvidence{}
	}
	if len(indices) > maxEmbeddedURLCandidates {
		// A diagnostic with an unbounded number of nested URL candidates is not a
		// useful presentation. Do not leave candidates after a fixed work budget
		// where one of them could still carry a credential.
		return "REDACTED", RedactionEvidence{}
	}
	// When the decoded value is itself one complete URL, handing it back to
	// redactURLWithEvidenceDepth would recurse on the same candidate. Let the
	// raw decoder advance its depth in that case; genuinely embedded or
	// multiple URL candidates are still processed below.
	if len(indices) == 1 && indices[0][0] == 0 && indices[0][1] == len(value) {
		return value, RedactionEvidence{}
	}
	var evidence RedactionEvidence
	changed := false
	for occurrence := len(indices) - 1; occurrence >= 0; occurrence-- {
		start := indices[occurrence][0]
		end := start
		for end < len(value) {
			switch value[end] {
			case ' ', '\t', '\r', '\n', '"', '\'', '<', '>':
				goto embeddedURLDone
			default:
				end++
			}
		}
	embeddedURLDone:
		if len(indices) == 1 && start == 0 && end == len(value) {
			continue
		}
		redacted, redactedChanged, urlEvidence := redactURLWithEvidenceDepth(value[start:end], depth+1)
		if redactedChanged {
			value = value[:start] + redacted + value[end:]
			changed = true
		}
		evidence.values = append(evidence.values, urlEvidence.values...)
	}
	if !changed {
		return value, evidence
	}
	return value, evidence
}

func stripRawUserinfo(rawURL string) (string, RedactionEvidence) {
	var evidence RedactionEvidence
	indices := httpsSchemeRe.FindAllStringIndex(rawURL, -1)
	if len(indices) == 0 {
		return rawURL, evidence
	}
	var scrubbed strings.Builder
	last := 0
	changed := false
	for _, index := range indices {
		authorityStart := index[1]
		authorityEnd := authorityStart
		for authorityEnd < len(rawURL) {
			switch rawURL[authorityEnd] {
			case '/', '?', '#', ' ', '\t', '\r', '\n', '"', '\'', '<', '>':
				goto authorityDone
			default:
				authorityEnd++
			}
		}
	authorityDone:
		authority := rawURL[authorityStart:authorityEnd]
		at := strings.LastIndexByte(authority, '@')
		if at < 0 {
			continue
		}
		if !changed {
			scrubbed.Grow(len(rawURL))
		}
		scrubbed.WriteString(rawURL[last:index[0]])
		scrubbed.WriteString(rawURL[index[0]:index[1]])
		credential := authority[:at]
		if credential != "" && credential != "REDACTED" {
			evidence.values = append(evidence.values, credential)
		}
		scrubbed.WriteString(authority[at+1:])
		last = authorityEnd
		changed = true
	}
	if !changed {
		return rawURL, evidence
	}
	scrubbed.WriteString(rawURL[last:])
	return scrubbed.String(), evidence
}

func decodePercentEscapes(value string) (string, bool) {
	var decoded strings.Builder
	changed := false
	for index := 0; index < len(value); index++ {
		if value[index] != '%' || index+2 >= len(value) {
			decoded.WriteByte(value[index])
			continue
		}
		high, highOK := percentNibble(value[index+1])
		low, lowOK := percentNibble(value[index+2])
		if !highOK || !lowOK {
			decoded.WriteByte(value[index])
			continue
		}
		decoded.WriteByte(high<<4 | low)
		index += 2
		changed = true
	}
	if !changed {
		return value, false
	}
	return decoded.String(), true
}

func percentNibble(value byte) (byte, bool) {
	switch {
	case value >= '0' && value <= '9':
		return value - '0', true
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10, true
	case value >= 'A' && value <= 'F':
		return value - 'A' + 10, true
	default:
		return 0, false
	}
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
	if len(s) > maxScrubInputSize {
		return "REDACTED", RedactionEvidence{}
	}
	return scrubCredentialURLsWithEvidenceDepth(s, 0)
}

func scrubCredentialURLsWithEvidenceDepth(s string, depth int) (string, RedactionEvidence) {
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
		redacted, changed, urlEvidence := redactURLWithEvidenceDepth(rawURL, depth)
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
	if len(rawURL) > maxScrubInputSize {
		return "REDACTED"
	}
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

// ContainsToken reports whether value carries any representation of token
// that RedactWithToken would remove. Unlike RedactWithToken it ignores other
// credential-shaped text, so request policy can tell the caller's own token
// apart from unrelated parameters such as a CDN signature.
func ContainsToken(value, token string) bool {
	if value == "" || strings.TrimSpace(token) == "" {
		return false
	}
	return replaceTokenVariants(value, token) != value
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
	safe, ok := sanitized.(*sanitizedError)
	if !ok {
		return &sanitizedError{message: redacted}
	}
	return newSanitizedError(redacted, safe.classifications(), safe.network)
}

// SanitizeErrorWithCredentials scrubs an error chain after removing every
// known representation of the caller-provided credentials, for example a
// submitted username and password that a broken upstream echoed back. The
// values are removed before the generic scrub so a partial assignment match
// inside one of them cannot split it and leave a fragment visible.
func SanitizeErrorWithCredentials(err error, credentials ...string) error {
	if err == nil {
		return nil
	}
	ordered := append([]string(nil), credentials...)
	// Remove longer values first so a shorter credential that is a substring of
	// a longer one cannot leave the rest of the longer value behind.
	sort.SliceStable(ordered, func(left, right int) bool {
		return len(ordered[left]) > len(ordered[right])
	})
	message := safeErrorMessage(err)
	for _, credential := range ordered {
		message = replaceTokenVariants(message, credential)
	}
	collector := classificationCollector{}
	collector.collect(err, 0)
	return newSanitizedError(Scrub(message), collector.classifications, collector.network)
}

func replaceTokenVariants(value, token string) string {
	if len(strings.TrimSpace(token)) > maxTokenVariantInputSize {
		if value == "" {
			return value
		}
		// Do not attempt partial matching of an oversized credential. A JSON,
		// percent, or mixed-encoding spelling can be a prefix of the token, so
		// retain only a fixed marker for this hostile-size input.
		return "REDACTED"
	}
	if value == "" {
		return value
	}
	// Variant generation includes percent and JSON spellings. Do not spend a
	// bounded-but-expensive scan over an arbitrarily large diagnostic before
	// reaching the fail-safe: preserving a large unknown representation could
	// leak a credential that is hidden behind an encoding not in the catalog.
	if len(value) > maxEncodedTokenViewSize {
		return "REDACTED"
	}
	redacted := replaceTokenVariantsDirect(value, token)
	rawChanged := redacted != value
	decodedChanged := false
	// URL parsers and upstream diagnostics can percent-encode only selected
	// bytes (for example, "a%62c"). Inspect a small, bounded number of decoded
	// views so mixed encodings are covered without enumerating combinations.
	candidate := redacted
	for depth := 0; depth < maxRawPercentDecodeDepth; depth++ {
		decoded, changed := decodePercentEscapes(candidate)
		if !changed {
			break
		}
		decodedRedacted := replaceTokenVariantsDirect(decoded, token)
		if decodedRedacted != decoded {
			decodedChanged = true
		}
		candidate = decodedRedacted
	}
	// A representation that is still changing after the decode budget is
	// exhausted is intentionally opaque. Returning the original value here
	// would make a deep percent-encoding an escape hatch around token removal.
	if _, changed := decodePercentEscapes(candidate); changed {
		return "REDACTED"
	}
	if decodedChanged {
		return candidate
	}
	if rawChanged {
		return redacted
	}
	return value
}

const (
	maxTokenVariantInputSize = 4 * 1024
	maxTokenVariantCount     = 64
)

const maxEncodedTokenViewSize = 64 * 1024

func replaceTokenVariantsDirect(value, token string) string {
	variants := tokenVariants(token)
	for _, variant := range variants {
		if variant == "" {
			continue
		}
		// Credentials are case-sensitive. In particular, case-folding Unicode
		// before indexing the original byte string can splice at the wrong byte
		// offsets and leak or corrupt adjacent text. Authentication schemes are
		// handled by the free-form assignment rules; the credential itself is
		// always matched byte-for-byte.
		value = strings.ReplaceAll(value, variant, "REDACTED")
		value = replaceJSONEscapedToken(value, variant)
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
	} else if len(trimmed) <= maxTokenVariantInputSize {
		values = append(values, "Bearer "+trimmed)
	}
	if len(trimmed) > maxTokenVariantInputSize {
		// Keep the raw (and, for Bearer input, stripped) spellings only. URL and
		// JSON expansion of a multi-megabyte token would turn one attacker-sized
		// value into hundreds of megabytes of transient allocations. The raw
		// value is still enough to redact the ordinary presentation directly.
		return values
	}
	// A token can be copied through more than one URL parser before it reaches
	// an error string. Keep the expansion bounded while covering the raw,
	// standard query/path, fully percent-encoded, and twice-encoded forms.
	const maxEncodingDepth = 2
	variants := make([]string, 0, maxTokenVariantCount)
	seen := make(map[string]struct{}, maxTokenVariantCount)
	for _, value := range values {
		frontier := []string{value}
		for depth := 0; depth <= maxEncodingDepth; depth++ {
			next := expandTokenVariantFrontier(frontier, depth, &variants, seen)
			if len(variants) >= maxTokenVariantCount {
				break
			}
			frontier = next
		}
		if len(variants) >= maxTokenVariantCount {
			break
		}
	}
	// Replace longer encodings first. This prevents a shorter representation
	// from consuming part of a longer, still-secret representation.
	sort.SliceStable(variants, func(left, right int) bool {
		return len(variants[left]) > len(variants[right])
	})
	return variants
}

func expandTokenVariantFrontier(
	frontier []string,
	depth int,
	variants *[]string,
	seen map[string]struct{},
) []string {
	next := make([]string, 0, len(frontier)*4)
	for _, candidate := range frontier {
		if candidate != "" {
			if _, ok := seen[candidate]; !ok {
				seen[candidate] = struct{}{}
				*variants = append(*variants, candidate)
				if len(*variants) >= maxTokenVariantCount {
					break
				}
			}
		}
		if depth == 2 {
			continue
		}
		next = append(next,
			url.QueryEscape(candidate),
			url.PathEscape(candidate),
			fullyPercentEncodeToken(candidate),
			jsonEscapeToken(candidate),
		)
	}
	return next
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

// replaceJSONEscapedToken removes token occurrences represented by any valid
// JSON escape spelling. It decodes once into a contiguous view and keeps one
// source span per decoded byte, so matching is linear in the diagnostic and a
// replacement never splits a UTF-8 sequence or leaves the escaped spelling
// behind. The caller caps the diagnostic size before entering this helper.
func replaceJSONEscapedToken(value, token string) string {
	if value == "" || token == "" || !strings.Contains(value, "\\") {
		return value
	}
	view := decodeEncodedViewLayer(rawEncodedView(value), false)
	if !view.changed {
		return value
	}
	return replaceEncodedViewMatches(value, view, []string{token}, "REDACTED")
}

type rawSpan struct {
	start int
	end   int
}

type encodedView struct {
	text    string
	spans   []rawSpan
	changed bool
}

func rawEncodedView(value string) encodedView {
	spans := make([]rawSpan, len(value))
	for index := range spans {
		spans[index] = rawSpan{start: index, end: index + 1}
	}
	return encodedView{text: value, spans: spans}
}

// decodeEncodedViewLayer decodes one bounded layer of percent and JSON
// escapes. Percent decoding is intentionally byte-oriented, matching URL
// escaping even when the resulting free-form value is not valid UTF-8. JSON
// escapes can expand to several UTF-8 bytes; all of those bytes share the
// source span of the original escape.
func decodeEncodedViewLayer(source encodedView, decodePercent bool) encodedView {
	if source.text == "" || len(source.spans) != len(source.text) {
		return source
	}
	decoded := make([]byte, 0, len(source.text))
	spans := make([]rawSpan, 0, len(source.text))
	changed := false
	for index := 0; index < len(source.text); {
		if decodePercent && source.text[index] == '%' && index+2 < len(source.text) {
			high, highOK := percentNibble(source.text[index+1])
			low, lowOK := percentNibble(source.text[index+2])
			if highOK && lowOK {
				decoded = append(decoded, high<<4|low)
				spans = append(spans, combineRawSpans(source.spans[index], source.spans[index+2]))
				index += 3
				changed = true
				continue
			}
		}
		before := len(decoded)
		var width int
		var ok bool
		decoded, width, ok = appendJSONEscape(decoded, source.text, index)
		if ok {
			span := combineRawSpans(source.spans[index], source.spans[index+width-1])
			for range decoded[before:] {
				spans = append(spans, span)
			}
			index += width
			changed = true
			continue
		}
		decoded = append(decoded, source.text[index])
		spans = append(spans, source.spans[index])
		index++
	}
	if !changed {
		return encodedView{text: source.text, spans: source.spans, changed: false}
	}
	return encodedView{text: string(decoded), spans: spans, changed: true}
}

func combineRawSpans(first, last rawSpan) rawSpan {
	return rawSpan{start: first.start, end: last.end}
}

func replaceEncodedViewMatches(value string, view encodedView, needles []string, replacement string) string {
	if value == "" || view.text == "" || len(view.spans) != len(view.text) {
		return value
	}
	matches := findEncodedViewMatches(view.text, needles)
	if len(matches) == 0 {
		return value
	}
	sort.Slice(matches, func(left, right int) bool {
		if matches[left].start != matches[right].start {
			return matches[left].start < matches[right].start
		}
		return matches[left].end > matches[right].end
	})
	return replaceEncodedViewRanges(value, view, matches, replacement)
}

type encodedMatch struct{ start, end int }

func findEncodedViewMatches(value string, needles []string) []encodedMatch {
	matches := make([]encodedMatch, 0, len(needles))
	for _, needle := range needles {
		if needle == "" {
			continue
		}
		for searchFrom := 0; searchFrom <= len(value)-len(needle); {
			index := strings.Index(value[searchFrom:], needle)
			if index < 0 {
				break
			}
			index += searchFrom
			matches = append(matches, encodedMatch{start: index, end: index + len(needle)})
			searchFrom = index + len(needle)
		}
	}
	return matches
}

func replaceEncodedViewRanges(value string, view encodedView, matches []encodedMatch, replacement string) string {
	var scrubbed strings.Builder
	scrubbed.Grow(len(value))
	lastRaw := 0
	changed := false
	lastDecodedEnd := 0
	for _, candidate := range matches {
		if candidate.start < lastDecodedEnd || candidate.start < 0 || candidate.end > len(view.spans) {
			continue
		}
		rawStart, rawEnd, ok := rawRangeForDecoded(view, candidate.start, candidate.end)
		if !ok || rawStart < lastRaw || rawEnd <= rawStart || rawStart < 0 || rawEnd > len(value) {
			continue
		}
		scrubbed.WriteString(value[lastRaw:rawStart])
		scrubbed.WriteString(replacement)
		lastRaw = rawEnd
		lastDecodedEnd = candidate.end
		changed = true
	}
	if !changed {
		return value
	}
	scrubbed.WriteString(value[lastRaw:])
	return scrubbed.String()
}

func rawRangeForDecoded(view encodedView, start, end int) (int, int, bool) {
	if start < 0 || end <= start || end > len(view.spans) {
		return 0, 0, false
	}
	first := view.spans[start]
	last := view.spans[end-1]
	if first.start < 0 || last.end <= first.start {
		return 0, 0, false
	}
	return first.start, last.end, true
}

func appendJSONEscape(dst []byte, value string, index int) ([]byte, int, bool) {
	if index < 0 || index+1 >= len(value) || value[index] != '\\' {
		return dst, 0, false
	}
	escapeWidth := 2
	switch value[index+1] {
	case '"':
		dst = append(dst, '"')
	case '\\':
		dst = append(dst, '\\')
	case '/':
		dst = append(dst, '/')
	case 'b':
		dst = append(dst, '\b')
	case 'f':
		dst = append(dst, '\f')
	case 'n':
		dst = append(dst, '\n')
	case 'r':
		dst = append(dst, '\r')
	case 't':
		dst = append(dst, '\t')
	case 'u':
		code, width, ok := decodeJSONCodePoint(value, index)
		if !ok {
			return dst, 0, false
		}
		escapeWidth = width
		decodedRune := utf8.RuneError
		if code <= utf8.MaxRune {
			decodedRune = rune(code)
		}
		var encoded [utf8.UTFMax]byte
		count := utf8.EncodeRune(encoded[:], decodedRune)
		dst = append(dst, encoded[:count]...)
	default:
		return dst, 0, false
	}
	return dst, escapeWidth, true
}

func decodeJSONCodePoint(value string, index int) (uint32, int, bool) {
	if index+6 > len(value) {
		return 0, 0, false
	}
	code, ok := parseJSONHex4(value[index+2 : index+6])
	if !ok {
		return 0, 0, false
	}
	if isJSONHighSurrogate(code) && index+12 <= len(value) && value[index+6] == '\\' && value[index+7] == 'u' {
		low, lowOK := parseJSONHex4(value[index+8 : index+12])
		if lowOK && isJSONLowSurrogate(low) {
			combined := uint32(0x10000) + (uint32(code-0xd800)<<10 | uint32(low-0xdc00))
			return combined, 12, true
		}
	}
	if isJSONSurrogate(code) {
		// encoding/json replaces an unpaired surrogate with U+FFFD. Match
		// that behavior instead of emitting invalid UTF-8.
		code = 0xfffd
	}
	return uint32(code), 6, true
}

func parseJSONHex4(value string) (uint16, bool) {
	if len(value) != 4 {
		return 0, false
	}
	var result uint16
	for index := 0; index < len(value); index++ {
		nibble, ok := percentNibble(value[index])
		if !ok {
			return 0, false
		}
		result = result<<4 | uint16(nibble)
	}
	return result, true
}

func isJSONHighSurrogate(value uint16) bool { return value >= 0xd800 && value <= 0xdbff }
func isJSONLowSurrogate(value uint16) bool  { return value >= 0xdc00 && value <= 0xdfff }
func isJSONSurrogate(value uint16) bool     { return value >= 0xd800 && value <= 0xdfff }

func redactURLWithEvidence(rawURL string) (string, bool, RedactionEvidence) {
	return redactURLWithEvidenceDepth(rawURL, 0)
}

const (
	maxURLRedactionDepth     = 16
	maxFragmentRedactionSize = 64 * 1024
	maxEmbeddedURLCandidates = 64
)

func redactURLWithEvidenceDepth(rawURL string, depth int) (string, bool, RedactionEvidence) {
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
		fragment, fragmentEvidence := scrubFragmentWithEvidenceDepth(u.Fragment, depth+1)
		if fragment != u.Fragment {
			u.Fragment = fragment
			// RawFragment is an optional spelling of Fragment. Clear it after
			// changing the decoded value so URL.String cannot restore the leak.
			u.RawFragment = ""
			changed = true
		}
		evidence.values = append(evidence.values, fragmentEvidence.values...)
	}
	// ParseQuery treats an encoded '=' as part of the key (for example,
	// token%3Dsecret), and silently leaves double-encoded delimiters behind.
	// Scrub the raw spelling first; the bounded decoder in scrubRawWithEvidence
	// also catches mixed encodings such as a%62ccess_token.
	rawQuery, rawQueryEvidence := scrubRawWithEvidence("?" + u.RawQuery)
	if rawQuery != "?"+u.RawQuery {
		u.RawQuery = strings.TrimPrefix(rawQuery, "?")
		changed = true
	}
	evidence.values = append(evidence.values, rawQueryEvidence.values...)
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
			scrubInput := v
			prefix := ""
			if !strings.ContainsAny(scrubInput, "?&;#") {
				prefix = "?"
				scrubInput = prefix + scrubInput
			}
			if scrubbed, nestedEvidence := scrubRawWithEvidence(scrubInput); scrubbed != scrubInput {
				vals[i] = strings.TrimPrefix(scrubbed, prefix)
				changed = true
				evidence.values = append(evidence.values, nestedEvidence.values...)
			}
		}
	}
	u.RawQuery = params.Encode()
	return u.String(), changed, evidence
}

func scrubFragmentWithEvidenceDepth(fragment string, depth int) (string, RedactionEvidence) {
	if len(fragment) > maxFragmentRedactionSize || depth >= maxURLRedactionDepth {
		// Once the bounded work budget is exhausted, preserve no fragment text:
		// an unknown nested parameter may contain a credential that cannot be
		// enumerated safely without unbounded recursion or allocation.
		return "REDACTED", RedactionEvidence{}
	}
	candidate := fragment
	for decodeDepth := 0; decodeDepth <= 2; decodeDepth++ {
		// Treat query-shaped fragments as parameter lists so an &-delimited
		// safe field is retained when a preceding credential is removed.
		scrubbed, evidence := scrubRawWithEvidence("#" + candidate)
		if scrubbed != "#"+candidate {
			return strings.TrimPrefix(scrubbed, "#"), evidence
		}
		scrubbed, evidence = scrubWithEvidenceDepth(candidate, depth)
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

// SanitizeError scrubs sensitive URL data from HTTP errors. It always returns
// an opaque error boundary: arbitrary wrappers can expose secrets through
// Unwrap or a custom As method even when their top-level Error string is clean.
// The returned value keeps exact identity for bounded, comparable error
// classifications without exposing any raw error in its chain. Callers must
// rely on Error and errors.Is; only fixed network metadata is available via
// the narrowly controlled errors.As implementation.
func SanitizeError(err error) error {
	if err == nil {
		return nil
	}
	collector := classificationCollector{}
	collector.collect(err, 0)
	return newSanitizedError(Scrub(safeErrorMessage(err)), collector.classifications, collector.network)
}

func safeErrorMessage(err error) (message string) {
	if err == nil {
		return ""
	}
	completed := false
	func() {
		defer swallowPanic()
		message = err.Error()
		completed = true
	}()
	if !completed {
		return panicSafeErrorMessage
	}
	return message
}

const maxSanitizeErrorDepth = 64

const (
	// Error chains are application-controlled input. Keep both recursion and
	// total work bounded, including errors.Join trees and private metadata
	// slices returned by trusted in-package wrappers.
	maxSanitizeErrorNodes       = 256
	maxSanitizeErrorChildren    = 64
	maxSanitizeErrorClassifiers = 64
	panicSafeErrorMessage       = "upstream error"
)

type classificationCollector struct {
	classifications []error
	network         networkMetadata
	nodes           int
}

func (collector *classificationCollector) collect(err error, depth int) {
	if err == nil || depth >= maxSanitizeErrorDepth || collector.nodes >= maxSanitizeErrorNodes {
		return
	}
	collector.nodes++
	if safe, ok := err.(interface{ classifications() []error }); ok {
		for _, classification := range boundedClassifications(safe) {
			collector.addClassification(classification)
		}
	}
	collector.addClassification(err)
	collector.network.observe(err)
	if multi, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range boundedUnwrapErrors(multi) {
			collector.collect(child, depth+1)
		}
		return
	}
	if single, ok := err.(interface{ Unwrap() error }); ok {
		collector.collect(safeUnwrapError(single), depth+1)
	}
}

func boundedClassifications(source interface{ classifications() []error }) []error {
	var classifications []error
	completed := false
	func() {
		defer swallowPanic()
		classifications = source.classifications()
		completed = true
	}()
	if !completed || len(classifications) == 0 {
		return nil
	}
	if len(classifications) > maxSanitizeErrorClassifiers {
		classifications = classifications[:maxSanitizeErrorClassifiers]
	}
	return classifications
}

func boundedUnwrapErrors(source interface{ Unwrap() []error }) []error {
	var children []error
	completed := false
	func() {
		defer swallowPanic()
		children = source.Unwrap()
		completed = true
	}()
	if !completed || len(children) == 0 {
		return nil
	}
	if len(children) > maxSanitizeErrorChildren {
		children = children[:maxSanitizeErrorChildren]
	}
	return children
}

func safeUnwrapError(source interface{ Unwrap() error }) (child error) {
	defer swallowPanic()
	return source.Unwrap()
}

func (collector *classificationCollector) addClassification(candidate error) {
	if !isSafeClassification(candidate) {
		return
	}
	for _, existing := range collector.classifications {
		if sameClassification(existing, candidate) {
			return
		}
	}
	if len(collector.classifications) < maxSanitizeErrorDepth*2 {
		collector.classifications = append(collector.classifications, candidate)
	}
}

func isSafeClassification(err error) bool {
	value := reflect.ValueOf(err)
	if !value.IsValid() {
		return false
	}
	if value.Kind() == reflect.Pointer {
		return !value.IsNil()
	}
	return value.Type().Comparable()
}

// sameClassification compares only exact identities/values while containing
// the one remaining panic surface in interface equality: a struct can report
// Comparable when one of its interface fields holds a slice or map.
func sameClassification(left, right error) (equal bool) {
	if !isSafeClassification(left) || !isSafeClassification(right) {
		return false
	}
	defer func() {
		if recover() != nil {
			equal = false
		}
	}()
	return left == right
}

type networkMetadata struct {
	hasNetwork bool
	dns        bool
	timeout    bool
}

func (metadata *networkMetadata) observe(err error) {
	if safe, ok := err.(interface{ networkMetadata() networkMetadata }); ok {
		metadata.merge(safeNetworkMetadata(safe))
	}
	if dnsErr, ok := err.(*net.DNSError); ok && dnsErr != nil {
		metadata.hasNetwork = true
		metadata.dns = true
		metadata.timeout = metadata.timeout || dnsErr.IsTimeout
	}
	if netErr, ok := err.(net.Error); ok && !isNilInterface(netErr) {
		metadata.hasNetwork = true
		metadata.timeout = metadata.timeout || safeTimeout(netErr)
	}
}

func isNilInterface(value any) bool {
	reflected := reflect.ValueOf(value)
	if !reflected.IsValid() {
		return true
	}
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		return reflected.IsNil()
	case reflect.Invalid, reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128, reflect.Array, reflect.String,
		reflect.Struct:
		return false
	}
	return false
}

func safeNetworkMetadata(source interface{ networkMetadata() networkMetadata }) (metadata networkMetadata) {
	defer swallowPanic()
	return source.networkMetadata()
}

func safeTimeout(source net.Error) (timeout bool) {
	defer swallowPanic()
	return source.Timeout()
}

func swallowPanic() {
	if recover() == nil {
		return
	}
	// Hostile error implementations are treated as opaque. The recovered
	// value is intentionally discarded and never enters a diagnostic.
}

func (metadata *networkMetadata) merge(other networkMetadata) {
	metadata.hasNetwork = metadata.hasNetwork || other.hasNetwork
	metadata.dns = metadata.dns || other.dns
	metadata.timeout = metadata.timeout || other.timeout
}

// sanitizedError deliberately does not expose Unwrap. Its classification
// references are private and are compared by exact identity only; a caller
// cannot recover hidden credentials through chain traversal or arbitrary As.
type sanitizedError struct {
	message        string
	classification []error
	network        networkMetadata
}

func newSanitizedError(message string, classifications []error, network networkMetadata) *sanitizedError {
	return &sanitizedError{
		message:        message,
		classification: append([]error(nil), classifications...),
		network:        network,
	}
}

func (err *sanitizedError) Error() string { return err.message }

func (err *sanitizedError) Is(target error) bool {
	if !isSafeClassification(target) {
		return false
	}
	for _, candidate := range err.classification {
		// Do not invoke target.Is: custom classifiers may inspect or expose
		// credential-bearing state. sameClassification contains the interface
		// equality panic for comparable structs with an uncomparable dynamic
		// interface field.
		if sameClassification(candidate, target) {
			return true
		}
	}
	return false
}

// As exposes only fixed network metadata needed by the server's error
// classifier. It never forwards the source error or invokes a source As
// method, so credentials and arbitrary concrete error values remain hidden.
func (err *sanitizedError) As(target any) bool {
	switch destination := target.(type) {
	case **net.DNSError:
		if !err.network.dns {
			return false
		}
		*destination = &net.DNSError{
			Err:       "upstream DNS failure",
			IsTimeout: err.network.timeout,
		}
		return true
	case *net.Error:
		if !err.network.hasNetwork {
			return false
		}
		*destination = sanitizedNetworkError{
			timeout: err.network.timeout,
		}
		return true
	default:
		return false
	}
}

func (err *sanitizedError) networkMetadata() networkMetadata {
	return err.network
}

type sanitizedNetworkError struct {
	timeout bool
}

func (err sanitizedNetworkError) Error() string   { return "upstream connection failed" }
func (err sanitizedNetworkError) Timeout() bool   { return err.timeout }
func (err sanitizedNetworkError) Temporary() bool { return false }

func (err *sanitizedError) classifications() []error {
	return append([]error(nil), err.classification...)
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
	// Diagnostics are untrusted input. Refuse oversized values before URL
	// matching, regular-expression passes, or span allocation can turn them
	// into attacker-controlled work. Callers still get a fixed safe marker.
	if len(s) > maxScrubInputSize {
		return "REDACTED", RedactionEvidence{}
	}
	return scrubWithEvidenceDepth(s, 0)
}

func scrubWithEvidenceDepth(s string, depth int) (string, RedactionEvidence) {
	if s == "" {
		return s, RedactionEvidence{}
	}
	scrubbed, evidence := scrubCredentialURLsWithEvidenceDepth(s, depth)
	for _, step := range credentialAssignmentSteps() {
		var stepEvidence RedactionEvidence
		scrubbed, stepEvidence = replaceCredentialValues(scrubbed, step)
		evidence.values = append(evidence.values, stepEvidence.values...)
	}
	encoded, encodedEvidence := scrubEncodedAssignmentsWithEvidence(scrubbed)
	if encoded != scrubbed {
		scrubbed = encoded
	}
	evidence.values = append(evidence.values, encodedEvidence.values...)
	return scrubbed, evidence
}

type credentialAssignmentStep struct {
	expression  *regexp.Regexp
	prefixGroup int
	replacement string
	// openValue marks expressions whose value pattern can begin with a quote
	// but stops at the first space or delimiter inside it.
	openValue bool
}

func credentialAssignmentSteps() []credentialAssignmentStep {
	return []credentialAssignmentStep{
		{quotedSecretValue, 1, "${1}REDACTED", false},
		{singleQuotedSecretValue, 1, "${1}REDACTED", false},
		{quotedKeySchemeSecretValue, 1, "${1}REDACTED", false},
		{quotedKeyBareSecretValue, 1, "${1}REDACTED", false},
		{strongCredentialAssignment, 2, "${1}${2}REDACTED", true},
		{schemeSecretAssignment, 2, "${1}${2}REDACTED", false},
		{bareSecretEquals, 2, "${1}${2}REDACTED", true},
		{bareSecretColon, 2, "${1}${2}REDACTED", true},
	}
}

const (
	maxScrubInputSize        = 64 * 1024
	maxCredentialDecodeDepth = 3
	maxCredentialKeyRunes    = 128
)

type credentialValueRange struct {
	start int
	end   int
}

// scrubEncodedAssignmentsWithEvidence applies the free-form assignment rules
// to a bounded sequence of decoded views. Every decoded byte carries a span
// into the current source, so only the credential value is replaced and safe
// percent/JSON spelling around it remains intact.
func scrubEncodedAssignmentsWithEvidence(value string) (string, RedactionEvidence) {
	if value == "" {
		return value, RedactionEvidence{}
	}
	if len(value) > maxScrubInputSize {
		return "REDACTED", RedactionEvidence{}
	}
	var evidence RedactionEvidence
	view := rawEncodedView(value)
	rawRanges := make([]rawSpan, 0, 8)
	for depth := 0; depth <= maxCredentialDecodeDepth; depth++ {
		if depth > 0 {
			view = decodeEncodedViewLayer(view, true)
			if !view.changed {
				break
			}
		}
		valueRanges, viewEvidence := credentialValueRangesForView(view)
		evidence.values = append(evidence.values, viewEvidence.values...)
		for _, candidate := range valueRanges {
			start, end, ok := rawRangeForDecoded(view, candidate.start, candidate.end)
			if !ok || start < 0 || end > len(value) || end <= start {
				continue
			}
			rawRanges = append(rawRanges, rawSpan{start: start, end: end})
		}
		if depth != maxCredentialDecodeDepth {
			continue
		}

		// Inspect one additional decoded view only to distinguish a safe
		// redacted assignment whose delimiter is still encoded from an
		// assignment that is deeper than the supported budget. A value that
		// remains hidden behind another encoding layer is opaque by design.
		probe := decodeEncodedViewLayer(view, true)
		if !probe.changed {
			break
		}
		if probe.changed {
			return "REDACTED", evidence
		}
	}
	return replaceRawCredentialRanges(value, rawRanges), evidence
}

func credentialValueRangesForView(view encodedView) ([]credentialValueRange, RedactionEvidence) {
	ranges, evidence := regexCredentialValueRanges(view.text)
	canonicalRanges := canonicalCredentialValueRanges(view.text)
	ranges = append(ranges, canonicalRanges...)
	for _, candidate := range canonicalRanges {
		if candidate.start >= 0 && candidate.end <= len(view.text) && candidate.end > candidate.start {
			evidence.values = append(evidence.values, view.text[candidate.start:candidate.end])
		}
	}
	return ranges, evidence
}

func regexCredentialValueRanges(value string) ([]credentialValueRange, RedactionEvidence) {
	ranges := make([]credentialValueRange, 0, 8)
	var evidence RedactionEvidence
	for _, step := range credentialAssignmentSteps() {
		indices := step.expression.FindAllStringSubmatchIndex(value, -1)
		for _, index := range indices {
			prefixEnd, ok := credentialPrefixEnd(index, step.prefixGroup, len(value))
			if !ok {
				continue
			}
			if step.expression == schemeSecretAssignment || step.expression == quotedKeySchemeSecretValue {
				// Only the start is used here, so keep the credential end lookup
				// inside this match rather than scanning on to the next break.
				if credentialStart, _, ok := recognizedSchemeCredentialRange(value[:index[1]], prefixEnd, index[1], newBreakCursor(valueBreaks)); ok {
					prefixEnd = credentialStart
				}
			}
			if isQuotedValueStart(value, prefixEnd) {
				// The canonical scanner handles the complete quoted value, including
				// spaces and escaped delimiters. A regex match would otherwise claim
				// only the first word and leave the remainder visible.
				continue
			}
			credential := value[prefixEnd:index[1]]
			if shouldSkipCredentialRange(step.expression, credential) {
				// The broad authorization fallback protects custom schemes. A
				// standard scheme has a precise matcher below, so keep its safe
				// suffix/context instead of discarding the rest of the line.
				continue
			}
			if (step.expression == bareSecretEquals || step.expression == bareSecretColon) && IsCredentialScheme(strings.TrimSpace(credential)) {
				// The scheme-specific matcher redacts the following credential;
				// treating the scheme word itself as the credential would lose
				// useful diagnostic context.
				continue
			}
			if credential != "REDACTED" {
				ranges = append(ranges, credentialValueRange{start: prefixEnd, end: index[1]})
				evidence.values = append(evidence.values, credential)
			}
		}
	}
	return ranges, evidence
}

func credentialPrefixEnd(index []int, group, valueLength int) (int, bool) {
	position := group * 2
	if position < 0 || position+1 >= len(index) || len(index) < 2 || index[1] < 0 || index[position] < 0 || index[position+1] < 0 {
		return 0, false
	}
	prefixEnd := index[position+1]
	return prefixEnd, prefixEnd < valueLength && prefixEnd < index[1]
}

func shouldSkipCredentialRange(expression *regexp.Regexp, credential string) bool {
	if expression == strongCredentialAssignment {
		return hasRecognizedCredentialScheme(credential)
	}
	return (expression == bareSecretEquals || expression == bareSecretColon) &&
		IsCredentialScheme(strings.TrimSpace(credential))
}

func replaceRawCredentialRanges(value string, rawRanges []rawSpan) string {
	if len(rawRanges) == 0 {
		return value
	}
	sort.Slice(rawRanges, func(left, right int) bool {
		if rawRanges[left].start != rawRanges[right].start {
			return rawRanges[left].start < rawRanges[right].start
		}
		return rawRanges[left].end > rawRanges[right].end
	})
	var scrubbed strings.Builder
	scrubbed.Grow(len(value))
	last := 0
	changed := false
	for _, candidate := range rawRanges {
		if candidate.start < last {
			continue
		}
		scrubbed.WriteString(value[last:candidate.start])
		scrubbed.WriteString("REDACTED")
		last = candidate.end
		changed = true
	}
	if !changed {
		return value
	}
	scrubbed.WriteString(value[last:])
	return scrubbed.String()
}

// canonicalCredentialValueRanges catches bounded key obfuscation such as
// "to\u200bken=..." after a decoded view has been built. Format/default-
// ignorable runes are removed only while comparing the key; the source text
// and all unrelated prose remain byte-for-byte untouched.
func canonicalCredentialValueRanges(value string) []credentialValueRange {
	ranges := make([]credentialValueRange, 0, 2)
	ends := assignmentEnds{value: newBreakCursor(valueBreaks), authorization: newBreakCursor(authorizationBreaks)}
	for delimiter := 0; delimiter < len(value); delimiter++ {
		if value[delimiter] != '=' && value[delimiter] != ':' {
			continue
		}
		if candidate, ok := canonicalCredentialRangeAt(value, delimiter, ends); ok {
			ranges = append(ranges, candidate)
		}
	}
	return ranges
}

const (
	// valueBreaks end an unquoted assignment value. Authorization values keep
	// their spaces and run to the end of the line or query parameter.
	valueBreaks         = " \t\r\n,;&}"
	authorizationBreaks = "\r\n&"
)

// assignmentEnds holds the cursors one canonical scan uses to find where
// unquoted values end.
type assignmentEnds struct {
	value         *breakCursor
	authorization *breakCursor
}

// breakCursor returns the first break byte at or after a position.
// Adjacent assignments can share one unbroken value, and rescanning it for
// every delimiter made the canonical scan quadratic. That scan never asks for
// a position more than one byte before the furthest it has asked for, so
// remembering the break-free span found last keeps the total work linear.
type breakCursor struct {
	breaks string
	from   int // value[from:at] holds no break byte
	at     int // the first break at or after from, or len(value)
}

func newBreakCursor(breaks string) *breakCursor {
	return &breakCursor{breaks: breaks, at: -1}
}

func (cursor *breakCursor) next(value string, start int) int {
	switch {
	case start > cursor.at:
		cursor.from, cursor.at = start, len(value)
		if index := strings.IndexAny(value[start:], cursor.breaks); index >= 0 {
			cursor.at = start + index
		}
	case start < cursor.from:
		if index := strings.IndexAny(value[start:cursor.from], cursor.breaks); index >= 0 {
			return start + index
		}
		cursor.from = start
	}
	return cursor.at
}

// valueEnd matches assignmentValueEnd for a cursor over valueBreaks. A quoted
// scan stops at the next unescaped quote of its kind, and every quoted value
// opens with one, so quoted scans of different delimiters do not overlap.
func (cursor *breakCursor) valueEnd(value string, start int, quoted bool) int {
	if quoted && isQuotedValueStart(value, start) {
		return assignmentValueEnd(value, start, true)
	}
	return cursor.next(value, start)
}

func canonicalCredentialRangeAt(value string, delimiter int, ends assignmentEnds) (credentialValueRange, bool) {
	keyStart, keyEnd, quoted := assignmentKeyBounds(value, delimiter)
	if !validCanonicalAssignmentKey(value, keyStart, keyEnd, quoted) {
		return credentialValueRange{}, false
	}
	key := ""
	if !isOversizedAssignmentKey(keyStart, keyEnd) {
		key = canonicalCredentialKey(value[keyStart:keyEnd])
	}
	valueStart := skipCredentialSpace(value, delimiter+1)
	if valueStart >= len(value) {
		return credentialValueRange{}, false
	}
	valueQuoted := isQuotedValueStart(value, valueStart)
	valueEnd := ends.value.valueEnd(value, valueStart, valueQuoted)
	if valueQuoted {
		// Keep the string delimiters byte-for-byte; only the value inside
		// them is a replacement candidate.
		valueStart++
	}
	if isAuthorizationKey(key) {
		valueStart, valueEnd = authorizationValueRange(value, valueStart, valueEnd, quoted || valueQuoted, ends)
	}
	if valueEnd <= valueStart || value[valueStart:valueEnd] == "REDACTED" {
		return credentialValueRange{}, false
	}
	return credentialValueRange{start: valueStart, end: valueEnd}, true
}

func validCanonicalAssignmentKey(value string, start, end int, quoted bool) bool {
	if start < 0 || end <= start || end > len(value) {
		return false
	}
	if quoted && isOversizedAssignmentKey(start, end) {
		// assignmentKeyBounds stopped looking for the opening quote. Treat the
		// key as sensitive, as isSensitiveParam does for keys this large.
		return true
	}
	if !quoted && start > 0 {
		previous, _ := utf8.DecodeLastRuneInString(value[:start])
		if isCredentialKeyRune(previous) || previous == '/' || previous == '\\' {
			return false
		}
	}
	return isCanonicalCredentialKey(canonicalCredentialKey(value[start:end]))
}

func skipCredentialSpace(value string, start int) int {
	for start < len(value) {
		runeValue, width := utf8.DecodeRuneInString(value[start:])
		if !unicode.IsSpace(runeValue) {
			break
		}
		start += width
	}
	return start
}

func isQuotedValueStart(value string, start int) bool {
	return start < len(value) && (value[start] == '"' || value[start] == '\'')
}

func isAuthorizationKey(value string) bool {
	return value == "authorization" || value == "proxy-authorization" || value == "auth"
}

func authorizationValueRange(value string, start, end int, quoted bool, ends assignmentEnds) (int, int) {
	if !quoted {
		end = ends.authorization.next(value, start)
	}
	if schemeStart, schemeEnd, ok := recognizedSchemeCredentialRange(value, start, end, ends.value); ok {
		return schemeStart, schemeEnd
	}
	if quoted {
		return start, ends.value.valueEnd(value, start-1, true)
	}
	return start, end
}

func hasRecognizedCredentialScheme(value string) bool {
	start := skipCredentialSpace(value, 0)
	end := credentialWordEnd(value, start, len(value))
	return end > start && IsCredentialScheme(value[start:end])
}

// maxCredentialSchemeBytes bounds the scheme word scan. strings.EqualFold
// matches only words with as many runes as a scheme, so once a word has more
// runes than the longest scheme it cannot be one.
var maxCredentialSchemeBytes = func() int {
	longest := 0
	for _, scheme := range credentialSchemes {
		longest = max(longest, utf8.RuneCountInString(scheme))
	}
	return (longest + 1) * utf8.UTFMax
}()

func recognizedSchemeCredentialRange(value string, start, end int, valueEnds *breakCursor) (int, int, bool) {
	if start < 0 || end <= start || end > len(value) {
		return 0, 0, false
	}
	schemeStart := skipCredentialSpace(value[:end], start)
	schemeEnd := credentialWordEnd(value, schemeStart, min(end, schemeStart+maxCredentialSchemeBytes))
	if schemeEnd == schemeStart || !IsCredentialScheme(value[schemeStart:schemeEnd]) {
		return 0, 0, false
	}
	credentialStart := skipCredentialSpace(value[:end], schemeEnd)
	if credentialStart >= end {
		return 0, 0, false
	}
	credentialEnd := min(valueEnds.next(value, credentialStart), end)
	if credentialEnd <= credentialStart {
		return 0, 0, false
	}
	return credentialStart, credentialEnd, true
}

func credentialWordEnd(value string, start, limit int) int {
	end := start
	for end < limit {
		runeValue, width := utf8.DecodeRuneInString(value[end:limit])
		if unicode.IsSpace(runeValue) || runeValue == ',' || runeValue == ';' || runeValue == '}' {
			break
		}
		end += width
	}
	return end
}

func assignmentKeyBounds(value string, delimiter int) (int, int, bool) {
	end := delimiter
	for end > 0 {
		runeValue, width := utf8.DecodeLastRuneInString(value[:end])
		if !unicode.IsSpace(runeValue) {
			break
		}
		end -= width
	}
	if end <= 0 {
		return 0, 0, false
	}
	if value[end-1] == '"' || value[end-1] == '\'' {
		return quotedAssignmentKeyBounds(value, end-1)
	}
	start := end
	for start > 0 {
		runeValue, width := utf8.DecodeLastRuneInString(value[:start])
		if !isCredentialKeyRune(runeValue) {
			break
		}
		start -= width
		if delimiter-start > maxCredentialKeyRunes*utf8.UTFMax {
			return 0, 0, false
		}
	}
	return start, end, false
}

// quotedAssignmentKeyBounds finds the opening quote of a key whose closing
// quote is value[closing]. It looks back only as far as the longest key the
// scan canonicalizes; a key that would be longer comes back as a span just
// over that budget, which callers treat as sensitive. An unbounded search
// would rescan the text for every delimiter after an escaped quote.
func quotedAssignmentKeyBounds(value string, closing int) (int, int, bool) {
	floor := max(0, closing-maxCredentialKeyRunes*utf8.UTFMax-1)
	for search := closing; search > floor; {
		start := strings.LastIndexByte(value[floor:search], value[closing])
		if start < 0 {
			break
		}
		start += floor
		if !isEscapedByte(value, start) {
			return start + 1, closing, true
		}
		search = start
	}
	if floor > 0 {
		return floor, closing, true
	}
	return 0, 0, false
}

func isOversizedAssignmentKey(start, end int) bool {
	return end-start > maxCredentialKeyRunes*utf8.UTFMax
}

func isEscapedByte(value string, index int) bool {
	backslashes := 0
	for index > 0 && value[index-1] == '\\' {
		backslashes++
		index--
	}
	return backslashes%2 == 1
}

func isCredentialKeyRune(value rune) bool {
	return unicode.IsLetter(value) || unicode.IsNumber(value) || unicode.Is(unicode.Cf, value) || value == '_' || value == '-'
}

func canonicalCredentialKey(value string) string {
	value = strings.TrimSpace(value)
	var canonical strings.Builder
	for _, runeValue := range value {
		if unicode.Is(unicode.Cf, runeValue) {
			continue
		}
		canonical.WriteRune(unicode.ToLower(runeValue))
	}
	return canonical.String()
}

var canonicalCredentialSuffix = regexp.MustCompile(`^[a-z0-9_-]+(?:token|password|secret|signature)$`)

func isCanonicalCredentialKey(value string) bool {
	return isSensitiveParam(value) || canonicalCredentialSuffix.MatchString(value)
}

func assignmentValueEnd(value string, start int, quoted bool) int {
	if quoted && start < len(value) && (value[start] == '"' || value[start] == '\'') {
		quote := value[start]
		for index := start + 1; index < len(value); index++ {
			if value[index] == quote && !isEscapedByte(value, index) {
				return index
			}
		}
		return len(value)
	}
	for index := start; index < len(value); index++ {
		switch value[index] {
		case ' ', '\t', '\r', '\n', ',', ';', '&', '}':
			return index
		}
	}
	return len(value)
}

func replaceCredentialValues(value string, step credentialAssignmentStep) (string, RedactionEvidence) {
	var evidence RedactionEvidence
	indices := step.expression.FindAllStringSubmatchIndex(value, -1)
	if len(indices) == 0 {
		return value, evidence
	}
	var scrubbed strings.Builder
	last := 0
	for _, index := range indices {
		if index[0] < last {
			// The match begins inside a quoted value that an earlier match
			// already replaced through its closing quote.
			continue
		}
		scrubbed.WriteString(value[last:index[0]])
		scrubbed.Write(step.expression.ExpandString(nil, step.replacement, value, index))
		prefixEnd := index[step.prefixGroup*2+1]
		end := quotedCredentialEnd(value, step, prefixEnd, index[1])
		credential := value[prefixEnd:end]
		if credential != "REDACTED" {
			evidence.values = append(evidence.values, credential)
		}
		last = end
	}
	scrubbed.WriteString(value[last:])
	return scrubbed.String(), evidence
}

// quotedCredentialEnd extends an open-value match through the closing quote
// when the value is quoted. Such a match stops at the first space or delimiter
// inside the quotes, so replacing only the match would consume the opening
// quote and leave the rest of the value visible to every later pass. An
// unterminated quote extends to the end of the text.
func quotedCredentialEnd(value string, step credentialAssignmentStep, prefixEnd, matchEnd int) int {
	if !step.openValue || !isQuotedValueStart(value, prefixEnd) {
		return matchEnd
	}
	end := assignmentValueEnd(value, prefixEnd, true)
	if end < len(value) {
		end++ // the closing quote
	}
	return max(matchEnd, end)
}

// ScrubError returns the error's message with embedded credentials scrubbed.
// It returns "" for a nil error.
func ScrubError(err error) string {
	if err == nil {
		return ""
	}
	return Scrub(safeErrorMessage(err))
}
