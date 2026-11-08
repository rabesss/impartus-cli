// Package secrets provides redaction helpers that keep sensitive data
// (notably auth tokens embedded in upstream URLs) out of logs and errors.
//
// It has no internal dependencies, so it can be imported by both
// internal/client and internal/downloader without creating an import cycle.
package secrets

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"net/url"
	"reflect"
	"regexp"
	"slices"
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
	"cookie2":             true,
	"set-cookie":          true,
	"set-cookie2":         true,
	"set_cookie2":         true,
	"setcookie2":          true,
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

// credentialKeySuffix lists the compound *_key names that carry credentials.
// It is explicit so that unrelated names such as cache_key or sort_key are not
// redacted.
const credentialKeySuffix = `[a-z0-9_-]*(?:api|private|secret|access|signing|encryption)[_-]?key` // #nosec G101 -- a key-name pattern, not a credential

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

// strongCredentialAssignment matches only the first byte of the value; the
// shared matcher finds where the value ends (its closing quote or the end of
// the line). Matching through the end of the line here would rescan the rest
// of the line for every short quoted header on it.
var strongCredentialAssignment = regexp.MustCompile(
	`(?i)(^|[^/\\a-z0-9_-])((?:authorization|proxy[-_]?authorization|auth|(?:x[-_])?api[-_]?key|cookie2?|set[-_]?cookie2?)\s*[:=]\s*)[^\r\n&]`,
)

// Header dumps sometimes separate the name from the value with whitespace
// only ("Authorization Bearer ...", "Cookie a=b"). These forms are accepted at
// the start of the text or after a structural delimiter, and only when the
// first word of the value has the shape of a credential (see the step's
// accept function), so prose such as "authorization failed" or "cookie
// consent" stays readable. The expressions match only that first word; an
// accepted Authorization or Cookie value is then extended to the end of the
// line, so a rejected one never costs a scan of the rest of the line.
const separatorlessBoundary = `(?i)(^|[\r\n,;:=(\[{"'])`

var separatorlessAuthorization = regexp.MustCompile(
	separatorlessBoundary + `(\s*(?:proxy[-_]?)?authorization[ \t]+)[^\s&]+`,
)
var separatorlessCookie = regexp.MustCompile(
	separatorlessBoundary + `(\s*(?:set[-_]?)?cookie2?[ \t]+)[^\s&]+`,
)
var separatorlessAPIKey = regexp.MustCompile(
	separatorlessBoundary + `(\s*(?:x[-_])?api[-_]?key[ \t]+)[^\s,;&}]`,
)

// bareBearerCredential redacts the token68 credential after a standalone
// "Bearer" scheme word that has no key in front of it.
var bareBearerCredential = regexp.MustCompile(
	`(?i)(^|[^a-z0-9_-])(bearer[ \t]+)[a-z0-9._~+/-]+=*`,
)
var schemeSecretAssignment = regexp.MustCompile(
	`(?i)(^|[^/\\a-z0-9_-])(` + sensitiveAssignmentKey + `\s*[:=]\s*)(?:` + strings.Join(credentialSchemes, "|") + `)\s+[^\s,;&}]+`,
)

// bareSecretEquals and bareSecretColon match only the first byte of the
// value. The value runs on to the next byte that openValueStop accepts, and
// the shared matcher finds that end only when it needs it: a quoted value
// resumes at its closing quote, so matching the whole run here would rescan
// the rest of a line of glued quoted assignments for each one.
var bareSecretEquals = regexp.MustCompile(
	`(?i)(^|[^/\\a-z0-9_-])(` + sensitiveAssignmentKey + `\s*=\s*)[^\s,;&}]`,
)
var bareSecretColon = regexp.MustCompile(
	`(?i)(^|[^/\\a-z0-9_-])(` + sensitiveAssignmentKey + `\s*:\s*)[^\s,;&}]`,
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
		// A view can see the marker through extra bytes (for example a
		// colored "REDACTED"); once normalized it is the marker, not a value.
		if credential != "" && credential != "REDACTED" && containsVisibleCredential(visible, credential) {
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
	keys = append(keys, "authorization", "password", "cookie2?", `set[-_]?cookie2?`, `(?:x[_-])?api[_-]?key`, `[a-z0-9_-]+(?:token|password|secret|signature)`, credentialKeySuffix)
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
	maxTokenVariantCount     = 128
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
	// A media URL can carry the bearer base64- or base64url-encoded in a query
	// value or path segment, which decodes trivially back to the token. Add the
	// whole-value encodings of each base spelling with one URL/JSON escaping
	// level, so padded and alphabet bytes (=, +, /) also match when escaped.
	// Matching stays whole-value: no alignment inside larger base64 blobs.
	if len(variants) < maxTokenVariantCount {
		for _, value := range values {
			for _, encoded := range base64TokenEncodings(value) {
				expandTokenVariantFrontier(
					expandTokenVariantFrontier([]string{encoded}, 1, &variants, seen),
					2, &variants, seen,
				)
				if len(variants) >= maxTokenVariantCount {
					break
				}
			}
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

// base64TokenEncodings returns the standard and URL-safe base64 spellings of
// value, padded and unpadded. Padding-free values are listed once.
func base64TokenEncodings(value string) []string {
	encodings := make([]string, 0, 4)
	for _, encoding := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding,
	} {
		encoded := encoding.EncodeToString([]byte(value))
		duplicate := false
		for _, existing := range encodings {
			duplicate = duplicate || existing == encoded
		}
		if !duplicate {
			encodings = append(encodings, encoded)
		}
	}
	return encodings
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

// credentialAssignmentStep is one free-form credential rule. The text of the
// match up to the end of capture group prefixGroup is kept; the rest of the
// match is the credential value that is replaced.
type credentialAssignmentStep struct {
	expression  *regexp.Regexp
	prefixGroup int
	// openValue marks expressions whose value pattern can begin with a quote
	// but stops at the first space or delimiter inside it.
	openValue bool
	// lineValue marks expressions whose unquoted value runs to the end of the
	// line. A quoted value still ends at its closing quote.
	lineValue bool
	// valueRun marks an expression that matches only the first byte of an
	// open value; the value runs on to the next openValueStop byte.
	valueRun bool
	// enclosed marks an expression whose first group may be a quote that
	// opens a string around the whole assignment ({"error":"X-Api-Key k"}).
	// The value then also ends at that string's unescaped closing quote.
	enclosed bool
	// accept, when set, rejects matches whose value does not have the shape of
	// a credential, so that rules without a key=value separator do not
	// redact ordinary prose. value[start:end] is the first word of the value.
	accept func(value string, start, end int) bool
}

var credentialAssignmentStepList = []credentialAssignmentStep{
	{expression: quotedSecretValue, prefixGroup: 1},
	{expression: singleQuotedSecretValue, prefixGroup: 1},
	{expression: quotedKeySchemeSecretValue, prefixGroup: 1},
	{expression: quotedKeyBareSecretValue, prefixGroup: 1},
	{expression: strongCredentialAssignment, prefixGroup: 2, openValue: true, lineValue: true, enclosed: true},
	{expression: separatorlessAuthorization, prefixGroup: 2, openValue: true, lineValue: true, enclosed: true, accept: looksLikeAuthorizationValue},
	{expression: separatorlessCookie, prefixGroup: 2, openValue: true, lineValue: true, enclosed: true, accept: firstWord(looksLikeCookieValue)},
	{expression: separatorlessAPIKey, prefixGroup: 2, openValue: true, valueRun: true, enclosed: true, accept: firstWord(looksLikeTokenValue)},
	{expression: schemeSecretAssignment, prefixGroup: 2},
	{expression: bareSecretEquals, prefixGroup: 2, openValue: true, valueRun: true},
	{expression: bareSecretColon, prefixGroup: 2, openValue: true, valueRun: true},
	{expression: bareBearerCredential, prefixGroup: 2, accept: firstWord(looksLikeTokenValue)},
}

func credentialAssignmentSteps() []credentialAssignmentStep {
	return credentialAssignmentStepList
}

// diagnosticCredentialWords are the words that follow "bearer" or "api key"
// in diagnostics rather than a credential ("bearer token missing", "api-key
// required"). Every other value fails closed, including short words, because
// a short alphabetic credential is still a credential.
var diagnosticCredentialWords = map[string]struct{}{
	"auth": {}, "authentication": {}, "authorization": {}, "credential": {},
	"credentials": {}, "error": {}, "expired": {}, "header": {}, "headers": {},
	"invalid": {}, "is": {}, "key": {}, "missing": {}, "not": {}, "prefix": {},
	"realm": {}, "redacted": {}, "required": {}, "scheme": {}, "token": {},
	"tokens": {}, "value": {}, "was": {},
}

// looksLikeTokenValue rejects the marker and the known diagnostic words above.
func looksLikeTokenValue(credential string) bool {
	credential = strings.TrimSpace(credential)
	if credential == "" {
		return false
	}
	end := credentialWordEnd(credential, 0, len(credential))
	if end != len(credential) {
		return true
	}
	_, diagnostic := diagnosticCredentialWords[strings.ToLower(credential)]
	return !diagnostic
}

func firstWord(accept func(string) bool) func(string, int, int) bool {
	return func(value string, start, end int) bool { return accept(value[start:end]) }
}

// looksLikeAuthorizationValue accepts a separator-less Authorization header
// whose first word, inside any opening quote, is a recognized scheme, or is
// a custom scheme word followed by a credential-shaped word on the same line
// ("Authorization Custom fake-secret"). Prose such as "authorization failed
// for lecture 42" starts with a diagnostic word instead of a scheme.
func looksLikeAuthorizationValue(value string, start, end int) bool {
	word := value[start:end]
	word = word[quoteOpenerWidth(word, 0):]
	if hasRecognizedCredentialScheme(word) {
		return true
	}
	if word == "" || isAuthorizationProseWord(word) {
		return false
	}
	// A credential with no scheme word ("Authorization eyJhbGci...") fails
	// closed.
	if looksLikeOpaqueCredential(word) {
		return true
	}
	if !isSchemeWord(word) {
		return false
	}
	next := end
	for next < len(value) && (value[next] == ' ' || value[next] == '\t') {
		next++
	}
	if next == end {
		return false
	}
	return looksLikeCredentialWord(value[next:credentialWordEnd(value, next, len(value))])
}

// authorizationProseWords start an "authorization ..." diagnostic rather than
// a custom scheme ("authorization failed request123").
var authorizationProseWords = map[string]struct{}{
	"denied": {}, "failed": {}, "failure": {}, "for": {}, "granted": {},
	"needed": {}, "refused": {}, "rejected": {}, "succeeded": {}, "with": {},
}

func isAuthorizationProseWord(word string) bool {
	word = strings.ToLower(word)
	_, prose := authorizationProseWords[word]
	_, diagnostic := diagnosticCredentialWords[word]
	return prose || diagnostic
}

func isSchemeWord(word string) bool {
	for index := 0; index < len(word); index++ {
		if !isCredentialKeyByte(word[index]) {
			return false
		}
	}
	return true
}

// minOpaqueCredentialBytes and maxPlainCredentialBytes describe a word that
// reads as a credential even with no scheme before it: long enough and
// carrying a digit, or long enough that no ordinary word matches.
const (
	minOpaqueCredentialBytes = 8
	maxPlainCredentialBytes  = 20
)

func looksLikeOpaqueCredential(word string) bool {
	word = strings.Trim(word, `"'\`)
	if len(word) >= maxPlainCredentialBytes {
		return true
	}
	return len(word) >= minOpaqueCredentialBytes && strings.ContainsAny(word, "0123456789")
}

// looksLikeCredentialWord reports whether word reads as a credential rather
// than prose: it carries a digit, punctuation or an inner capital letter, or
// it is longer than ordinary words.
func looksLikeCredentialWord(word string) bool {
	word = strings.Trim(word, `"'\`)
	if len(word) >= maxPlainCredentialBytes {
		return true
	}
	for index := 0; index < len(word); index++ {
		character := word[index]
		upper := character >= 'A' && character <= 'Z'
		if !upper && (character < 'a' || character > 'z') || upper && index > 0 {
			return true
		}
	}
	return false
}

// looksLikeCookieValue accepts a separator-less cookie header only when its
// first word is a name=value pair or the value is quoted.
func looksLikeCookieValue(credential string) bool {
	credential = strings.TrimSpace(credential)
	if isQuotedValueStart(credential, 0) {
		return true
	}
	end := credentialWordEnd(credential, 0, len(credential))
	return strings.Contains(credential[:end], "=")
}

// credentialMatch is one assignment that a step recognized. value[start:end]
// is the credential that is replaced; opener and closer are the quote
// delimiters around a quoted value, which the replacement keeps.
type credentialMatch struct {
	matchStart int
	prefixEnd  int
	start      int
	end        int
	opener     string
	closer     string
	// resume is where the next search for the same step starts.
	resume   int
	accepted bool
	// boundaryQuote is the quote that ends a string enclosing the whole
	// assignment ({"error":"Cookie a=b"}), or zero.
	boundaryQuote byte
}

// nextCredentialMatch finds the next match of step in value at or after from.
// The plain replacement pass and the decoded-view range pass both use it, so
// they agree on where a quoted or nested value ends.
func nextCredentialMatch(value string, step credentialAssignmentStep, from int) (credentialMatch, bool) {
	index := step.expression.FindStringSubmatchIndex(value[from:])
	if index == nil {
		return credentialMatch{}, false
	}
	for position := range index {
		if index[position] >= 0 {
			index[position] += from
		}
	}
	prefixEnd, ok := credentialPrefixEnd(index, step.prefixGroup, len(value))
	if !ok {
		// Every step requires a non-empty value after its prefix; resume after
		// the match so the scan always advances.
		return credentialMatch{matchStart: index[0], prefixEnd: index[1], resume: max(index[1], from+1)}, true
	}
	// When the assignment itself sits inside a quoted string ({"error":
	// "Authorization Bearer ..."}), its value ends at that string's closing
	// quote, so the delimiters and the rest of the line stay intact.
	var boundaryQuote byte
	if step.enclosed && index[3]-index[2] == 1 && (value[index[2]] == '"' || value[index[2]] == '\'') {
		boundaryQuote = value[index[2]]
	}
	if step.accept != nil {
		wordEnd := index[1]
		if step.valueRun {
			wordEnd = step.valueEnd(value, prefixEnd, index[1], boundaryQuote)
		}
		if !step.accept(value, prefixEnd, wordEnd) {
			// A rejected separator-less rule keeps its value visible. Its
			// first word is checked only once, so resuming after that word
			// keeps the scan linear and still finds a later credential.
			return credentialMatch{matchStart: index[0], prefixEnd: prefixEnd, start: prefixEnd, end: wordEnd, resume: wordEnd}, true
		}
	}
	// A line value ends at a line break or the enclosing string's quote.
	if step.lineValue && quoteOpenerWidth(value, prefixEnd) == 0 {
		index[1] = lineValueEnd(value, prefixEnd, boundaryQuote)
	}
	match := credentialMatch{matchStart: index[0], prefixEnd: prefixEnd, start: prefixEnd, end: index[1], resume: index[1], boundaryQuote: boundaryQuote}
	if step.openValue {
		match = openCredentialValue(value, step, match)
	}
	match.accepted = match.end > match.start
	return match, true
}

// openCredentialValue sets where an open value ends. A value that is exactly
// one quoted string keeps its delimiters; any other value runs through a
// quote that a nested assignment leaves open.
func openCredentialValue(value string, step credentialAssignmentStep, match credentialMatch) credentialMatch {
	prefixEnd := match.prefixEnd
	width := quoteOpenerWidth(value, prefixEnd)
	if width == 0 {
		match.end = extendUnmatchedQuote(value, prefixEnd, step.valueEnd(value, prefixEnd, match.end, match.boundaryQuote))
		match.resume = match.end
		return match
	}
	closeStart, closeWidth := quotedValueClose(value, prefixEnd)
	closeEnd := closeStart + closeWidth
	if step.quotedValueStands(value, prefixEnd, closeStart, closeEnd, match.end) {
		// Replace only the contents and keep the delimiters, so later passes
		// see a quoted REDACTED marker instead of an open value that runs on
		// to the next break.
		match.start = prefixEnd + width
		match.end = closeStart
		match.opener = value[prefixEnd:match.start]
		match.closer = value[closeStart:closeEnd]
		match.resume = closeEnd
		return match
	}
	// Text that is not another assignment is glued to the closing quote
	// (password="a"b...), so the quotes may be part of the value. Keep the
	// whole match redacted, through any quote it leaves open; a line value
	// runs on to the end of its line.
	if step.lineValue {
		match.end = lineValueEnd(value, closeEnd, match.boundaryQuote)
	} else {
		match.end = extendUnmatchedQuote(value, prefixEnd, step.valueEnd(value, prefixEnd, match.end, match.boundaryQuote))
	}
	match.resume = match.end
	return match
}

// valueEnd returns where an open value that starts at start ends. matchEnd is
// the end of the expression's match, which is the whole value unless the
// expression matched only its first byte. Such a value also ends at an
// unescaped boundaryQuote when it is not zero.
func (step credentialAssignmentStep) valueEnd(value string, start, matchEnd int, boundaryQuote byte) int {
	if !step.valueRun {
		return matchEnd
	}
	end := start
	for end < len(value) && !openValueStop(value[end]) {
		if boundaryQuote != 0 && value[end] == boundaryQuote && !isEscapedByte(value, end) {
			break
		}
		end++
	}
	return end
}

// quotedValueStands reports whether a quoted value that closes at closeEnd is
// the whole value: the open value ends at or before its closing quote, or
// another assignment is glued to that quote. For a valueRun step it looks no
// further than the key after the quote, so the scan stays linear.
func (step credentialAssignmentStep) quotedValueStands(value string, start, closeStart, closeEnd, matchEnd int) bool {
	switch {
	case step.lineValue:
		return lineQuotedValueStands(value, closeStart, closeEnd)
	case !step.valueRun:
		return closeEnd >= matchEnd || startsAssignment(value[closeEnd:matchEnd])
	case closeEnd >= len(value) || openValueStop(value[closeEnd]) || containsOpenValueStop(value[start:closeEnd]):
		return true
	}
	keyEnd := closeEnd
	for keyEnd < len(value) && isCredentialKeyByte(value[keyEnd]) {
		keyEnd++
	}
	return keyEnd > closeEnd && keyEnd < len(value) && (value[keyEnd] == '=' || value[keyEnd] == ':')
}

// lineQuotedValueStands reports whether a quoted line value is the whole
// value: its quote is unterminated or ends an enclosing string, or a
// delimiter, another quote or another assignment follows its closing quote.
// Other glued text may be part of the value, so the value then runs on to the
// end of the line.
func lineQuotedValueStands(value string, closeStart, closeEnd int) bool {
	return closeEnd == closeStart || closeEnd >= len(value) || openValueStop(value[closeEnd]) ||
		strings.IndexByte(`"'\`, value[closeEnd]) >= 0 || startsAssignment(value[closeEnd:])
}

func containsOpenValueStop(text string) bool {
	for index := 0; index < len(text); index++ {
		if openValueStop(text[index]) {
			return true
		}
	}
	return false
}

// openValueStop reports whether character ends a bare value, matching the
// [^\s,;&}] class of the bare assignment expressions.
func openValueStop(character byte) bool {
	switch character {
	case '\t', '\n', '\f', '\r', ' ', ',', ';', '&', '}':
		return true
	}
	return false
}

// lineValueEnd finds the end of a value that runs to the end of its line,
// stopping at a line break, a query separator like the strong assignment
// expression, or an unescaped boundaryQuote when it is not zero.
func lineValueEnd(value string, end int, boundaryQuote byte) int {
	for end < len(value) && value[end] != '\r' && value[end] != '\n' && value[end] != '&' {
		if boundaryQuote != 0 && value[end] == boundaryQuote && !isEscapedByte(value, end) {
			break
		}
		end++
	}
	return end
}

// startsAssignment reports whether text begins with a key followed by '=' or
// ':', as when a second assignment is glued to a closing quote.
func startsAssignment(text string) bool {
	index := 0
	for index < len(text) && isCredentialKeyByte(text[index]) {
		index++
	}
	if index == 0 {
		return false
	}
	for index < len(text) && (text[index] == ' ' || text[index] == '\t') {
		index++
	}
	return index < len(text) && (text[index] == '=' || text[index] == ':')
}

func isCredentialKeyByte(character byte) bool {
	lower := character | 0x20
	return lower >= 'a' && lower <= 'z' || character >= '0' && character <= '9' || character == '_' || character == '-' || character == '.'
}

// extendUnmatchedQuote extends an open value that ends inside a quote opened
// by a nested assignment (password=abc"sig="x y") through that quote's
// closing partner. Only a quote right after '=' or ':', possibly escaped
// (sig=\"x y\"), counts as an opener, so apostrophes in prose ("don't") do
// not extend the value.
func extendUnmatchedQuote(value string, start, end int) int {
	for index := start; index < end; index++ {
		character := value[index]
		if character != '"' && character != '\'' {
			continue
		}
		opener := index
		for opener > start && index-opener <= maxQuoteEscapeDepth && value[opener-1] == '\\' {
			opener--
		}
		if opener <= start || (value[opener-1] != '=' && value[opener-1] != ':') || quoteOpenerWidth(value, opener) == 0 {
			continue
		}
		closeStart, closeWidth := quotedValueClose(value, opener)
		if closeEnd := closeStart + closeWidth; closeEnd >= end {
			return max(end, closeEnd)
		} else if closeEnd > index {
			index = closeEnd - 1
		}
	}
	return end
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
	canonicalRanges := canonicalCredentialValueRanges(view.text)
	ranges, evidence := regexCredentialValueRanges(view.text, canonicalRanges)
	ranges = append(ranges, canonicalRanges...)
	for _, candidate := range canonicalRanges {
		if candidate.start >= 0 && candidate.end <= len(view.text) && candidate.end > candidate.start {
			evidence.values = append(evidence.values, view.text[candidate.start:candidate.end])
		}
	}
	return ranges, evidence
}

func regexCredentialValueRanges(value string, canonical []credentialValueRange) ([]credentialValueRange, RedactionEvidence) {
	ranges := make([]credentialValueRange, 0, 8)
	var evidence RedactionEvidence
	coverage := newCredentialRangeCoverage(canonical)
	for _, step := range credentialAssignmentSteps() {
		for from := 0; from < len(value); {
			match, ok := nextCredentialMatch(value, step, from)
			if !ok {
				break
			}
			from = match.resume
			if !match.accepted {
				continue
			}
			start := match.start
			if step.expression == schemeSecretAssignment || step.expression == quotedKeySchemeSecretValue {
				// Only the start is used here, so keep the credential end lookup
				// inside this match rather than scanning on to the next break.
				if credentialStart, _, ok := recognizedSchemeCredentialRange(value[:match.end], start, match.end, newBreakCursor(valueBreaks)); ok {
					start = credentialStart
				}
			}
			if match.opener != "" && coverage.covers(value, start, match.end) {
				// The canonical scanner already claimed this whole quoted value,
				// and for authorization keys it keeps a recognized scheme
				// visible. When it did not (for example, a key glued to a
				// non-ASCII letter, or only a nested assignment inside the
				// value), this pass claims the whole quoted value itself.
				continue
			}
			credential := value[start:match.end]
			if shouldSkipCredentialRange(step.expression, credential) {
				// The broad authorization fallback protects custom schemes. A
				// standard scheme has a precise matcher below, so keep its safe
				// suffix/context instead of discarding the rest of the line.
				continue
			}
			if credential != "REDACTED" {
				ranges = append(ranges, credentialValueRange{start: start, end: match.end})
				evidence.values = append(evidence.values, credential)
			}
		}
	}
	return ranges, evidence
}

// credentialRangeCoverage answers whether one range covers a span. Ranges are
// sorted by start with a running maximum end, so each query is a binary
// search rather than a scan of every range.
type credentialRangeCoverage struct {
	starts  []int
	maxEnds []int
}

func newCredentialRangeCoverage(ranges []credentialValueRange) credentialRangeCoverage {
	sorted := slices.Clone(ranges)
	slices.SortFunc(sorted, func(left, right credentialValueRange) int {
		return left.start - right.start
	})
	coverage := credentialRangeCoverage{starts: make([]int, len(sorted)), maxEnds: make([]int, len(sorted))}
	maxEnd := 0
	for index, candidate := range sorted {
		maxEnd = max(maxEnd, candidate.end)
		coverage.starts[index] = candidate.start
		coverage.maxEnds[index] = maxEnd
	}
	return coverage
}

// covers reports whether one range spans value[start:end]. A recognized
// scheme word and the space after it may stay outside the range, as the
// canonical scanner keeps them visible for authorization keys.
func (coverage credentialRangeCoverage) covers(value string, start, end int) bool {
	limit := start
	if hasRecognizedCredentialScheme(value[start:end]) {
		limit = skipCredentialSpace(value[:end], credentialWordEnd(value, skipCredentialSpace(value[:end], start), end))
	}
	count, _ := slices.BinarySearch(coverage.starts, limit+1)
	return count > 0 && coverage.maxEnds[count-1] >= end
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
			// Decoded views can produce partially overlapping ranges. Keep
			// their full union redacted, including any later range's tail.
			last = max(last, candidate.end)
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
	openerWidth := quoteOpenerWidth(value, valueStart)
	valueQuoted := openerWidth > 0
	valueEnd := ends.value.valueEnd(value, valueStart, valueQuoted)
	opener := valueStart - 1
	if valueQuoted {
		// Keep the string delimiters byte-for-byte; only the value inside
		// them is a replacement candidate.
		opener = valueStart
		valueStart += openerWidth
	}
	if isAuthorizationKey(key) {
		valueStart, valueEnd = authorizationValueRange(value, opener, valueStart, valueEnd, quoted || valueQuoted, ends)
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
	return quoteOpenerWidth(value, start) > 0
}

// maxQuoteEscapeDepth bounds how many backslashes may escape a quote that
// opens a value, as in JSON nested inside JSON (password=\\\"...\\\").
const maxQuoteEscapeDepth = 8

// quoteOpenerWidth returns the width of a quote that opens a value at start:
// 1 for '"' or '\”, or 1 plus the number of backslashes for a quote escaped
// once or more, as written inside (nested) JSON or C string literals. It is 0
// when the value is not quoted.
func quoteOpenerWidth(value string, start int) int {
	if start < 0 || start >= len(value) {
		return 0
	}
	run := 0
	for start+run < len(value) && value[start+run] == '\\' && run <= maxQuoteEscapeDepth {
		run++
	}
	if run > maxQuoteEscapeDepth || start+run >= len(value) {
		return 0
	}
	if quote := value[start+run]; quote != '"' && quote != '\'' {
		return 0
	}
	if run > 0 && start > 0 && value[start-1] == '\\' {
		// The backslashes continue an earlier run, so they do not open a value.
		return 0
	}
	return run + 1
}

// quotedValueClose finds the closing delimiter of the quoted value that opens
// at start and returns where it begins and its width. A plain quote closes at
// the next unescaped quote of its kind. A quote escaped by k backslashes
// closes at the next quote of its kind escaped by exactly k backslashes, or,
// without a width, where a quote escaped by fewer ends an enclosing string.
// An unterminated value runs to the end of the text.
func quotedValueClose(value string, start int) (int, int) {
	width := quoteOpenerWidth(value, start)
	if width == 0 {
		return start, 0
	}
	depth := width - 1
	quote := value[start+depth]
	for index := start + width; index < len(value); index++ {
		if value[index] != quote {
			continue
		}
		if depth == 0 {
			if !isEscapedByte(value, index) {
				return index, 1
			}
			continue
		}
		backslashes := 0
		for backslashes <= depth && index-backslashes-1 >= start+width && value[index-backslashes-1] == '\\' {
			backslashes++
		}
		switch {
		case backslashes == depth:
			return index - depth, width
		case backslashes < depth:
			return index, 0
		}
	}
	return len(value), 0
}

func isAuthorizationKey(value string) bool {
	return value == "authorization" || value == "proxy-authorization" || value == "auth"
}

func authorizationValueRange(value string, opener, start, end int, quoted bool, ends assignmentEnds) (int, int) {
	if !quoted {
		end = ends.authorization.next(value, start)
	}
	if schemeStart, schemeEnd, ok := recognizedSchemeCredentialRange(value, start, end, ends.value); ok {
		return schemeStart, schemeEnd
	}
	if quoted {
		return start, ends.value.valueEnd(value, opener, true)
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

var canonicalCredentialSuffix = regexp.MustCompile(`^(?:[a-z0-9_-]+(?:token|password|secret|signature)|` + credentialKeySuffix + `)$`)

func isCanonicalCredentialKey(value string) bool {
	return isSensitiveParam(value) || canonicalCredentialSuffix.MatchString(value)
}

func assignmentValueEnd(value string, start int, quoted bool) int {
	if quoted && isQuotedValueStart(value, start) {
		end, _ := quotedValueClose(value, start)
		return end
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
	var scrubbed strings.Builder
	last := 0
	// Search again from where each match says to resume rather than from the
	// end of each match. A quoted value is replaced through its closing quote,
	// so a key inside it is never seen, and a key right after that quote is
	// found with the start of the remaining text standing in for the quote as
	// its boundary. Any other match ends before a byte that cannot start a key,
	// so resuming there finds the same matches as one search over the whole
	// text.
	for last < len(value) {
		match, ok := nextCredentialMatch(value, step, last)
		if !ok {
			break
		}
		if !match.accepted {
			scrubbed.WriteString(value[last:match.resume])
			last = match.resume
			continue
		}
		scrubbed.WriteString(value[last:match.prefixEnd])
		scrubbed.WriteString(match.opener)
		scrubbed.WriteString("REDACTED")
		scrubbed.WriteString(match.closer)
		switch credential := value[match.start:match.end]; {
		case credential != "REDACTED":
			evidence.values = append(evidence.values, credential)
		case match.opener != "":
			// A quoted marker is still a recognized assignment. Count it, as
			// callers compare counts across views, but record no value: the
			// marker itself stays visible in the output by design.
			evidence.values = append(evidence.values, "")
		}
		last = match.resume
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
	return Scrub(safeErrorMessage(err))
}
