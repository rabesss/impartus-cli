// Package secrets provides redaction helpers that keep sensitive data
// (notably auth tokens embedded in upstream URLs) out of logs and errors.
//
// It has no internal dependencies, so it can be imported by both
// internal/client and internal/downloader without creating an import cycle.
package secrets

import (
	"bytes"
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
	if depth >= maxRawPercentDecodeDepth {
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
	safe, ok := sanitized.(*sanitizedError)
	if !ok {
		return &sanitizedError{message: redacted}
	}
	return newSanitizedError(redacted, safe.classifications(), safe.network)
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
	if redacted != value {
		return redacted
	}
	// URL parsers and upstream diagnostics can percent-encode only selected
	// bytes (for example, "a%62c"). Inspect a small, bounded number of decoded
	// views so mixed encodings are covered without enumerating combinations.
	candidate := value
	for depth := 0; depth < maxRawPercentDecodeDepth; depth++ {
		decoded, changed := decodePercentEscapes(candidate)
		if !changed {
			return value
		}
		decodedRedacted := replaceTokenVariantsDirect(decoded, token)
		if decodedRedacted != decoded {
			return decodedRedacted
		}
		candidate = decoded
	}
	// A representation that is still changing after the decode budget is
	// exhausted is intentionally opaque. Returning the original value here
	// would make a deep percent-encoding an escape hatch around token removal.
	if _, changed := decodePercentEscapes(candidate); changed {
		return "REDACTED"
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
// JSON escape spelling. It keeps a mapping from decoded bytes back to their
// original byte spans, so replacements never split a UTF-8 sequence or leave
// the escaped spelling behind. The caller caps the diagnostic size before
// entering this helper.
func replaceJSONEscapedToken(value, token string) string {
	if value == "" || token == "" || !strings.Contains(value, "\\") {
		return value
	}
	tokenBytes := []byte(token)
	units := decodeJSONUnits(value)
	if len(units) == 0 {
		return value
	}

	var scrubbed strings.Builder
	lastRaw := 0
	changed := false
	for start := 0; start < len(units); {
		end, ok := matchJSONToken(units, start, tokenBytes)
		if !ok {
			start++
			continue
		}
		rawStart := units[start].rawStart
		rawEnd := units[end-1].rawEnd
		if rawStart < lastRaw || rawEnd <= rawStart {
			// Units are generated in source order; this is defensive only, but
			// refusing an invalid span keeps this boundary fail-closed.
			start++
			continue
		}
		scrubbed.WriteString(value[lastRaw:rawStart])
		scrubbed.WriteString("REDACTED")
		lastRaw = rawEnd
		changed = true
		start = end
	}
	if !changed {
		return value
	}
	scrubbed.WriteString(value[lastRaw:])
	return scrubbed.String()
}

type jsonDecodedUnit struct {
	rawStart int
	rawEnd   int
	decoded  []byte
}

func decodeJSONUnits(value string) []jsonDecodedUnit {
	units := make([]jsonDecodedUnit, 0, len(value))
	for index := 0; index < len(value); {
		if decoded, width, ok := decodeJSONEscapeAt(value, index); ok {
			units = append(units, jsonDecodedUnit{
				rawStart: index,
				rawEnd:   index + width,
				decoded:  decoded,
			})
			index += width
			continue
		}
		units = append(units, jsonDecodedUnit{
			rawStart: index,
			rawEnd:   index + 1,
			decoded:  []byte{value[index]},
		})
		index++
	}
	return units
}

func matchJSONToken(units []jsonDecodedUnit, start int, token []byte) (int, bool) {
	if len(token) == 0 || start >= len(units) {
		return 0, false
	}
	matched := 0
	for end := start; end < len(units) && matched < len(token); end++ {
		decoded := units[end].decoded
		if len(decoded) > len(token)-matched || !bytes.Equal(decoded, token[matched:matched+len(decoded)]) {
			return 0, false
		}
		matched += len(decoded)
		if matched == len(token) {
			return end + 1, true
		}
	}
	return 0, false
}

func decodeJSONEscapeAt(value string, index int) ([]byte, int, bool) {
	if index < 0 || index+1 >= len(value) || value[index] != '\\' {
		return nil, 0, false
	}
	switch value[index+1] {
	case '"':
		return []byte{'"'}, 2, true
	case '\\':
		return []byte{'\\'}, 2, true
	case '/':
		return []byte{'/'}, 2, true
	case 'b':
		return []byte{'\b'}, 2, true
	case 'f':
		return []byte{'\f'}, 2, true
	case 'n':
		return []byte{'\n'}, 2, true
	case 'r':
		return []byte{'\r'}, 2, true
	case 't':
		return []byte{'\t'}, 2, true
	case 'u':
		return decodeJSONUnicodeEscape(value, index)
	default:
		return nil, 0, false
	}
}

func decodeJSONUnicodeEscape(value string, index int) ([]byte, int, bool) {
	if index+6 > len(value) {
		return nil, 0, false
	}
	code, ok := parseJSONHex4(value[index+2 : index+6])
	if !ok {
		return nil, 0, false
	}
	if isJSONHighSurrogate(code) && index+12 <= len(value) && value[index+6] == '\\' && value[index+7] == 'u' {
		low, lowOK := parseJSONHex4(value[index+8 : index+12])
		if lowOK && isJSONLowSurrogate(low) {
			combined := uint32(0x10000) + (uint32(code-0xd800)<<10 | uint32(low-0xdc00))
			return encodeJSONCodePoint(combined), 12, true
		}
	}
	if isJSONSurrogate(code) {
		// encoding/json replaces an unpaired surrogate with U+FFFD. Match
		// that behavior instead of emitting invalid UTF-8.
		code = 0xfffd
	}
	return encodeJSONCodePoint(uint32(code)), 6, true
}

func encodeJSONCodePoint(code uint32) []byte {
	if code > utf8.MaxRune {
		code = utf8.RuneError
	}
	var encoded [utf8.UTFMax]byte
	count := utf8.EncodeRune(encoded[:], rune(code))
	return encoded[:count]
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
		if existing == candidate {
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
		// Both dynamic values are comparable, so interface equality cannot panic.
		// Do not invoke target.Is: custom classifiers may inspect or expose
		// credential-bearing state.
		if candidate == target {
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
	return scrubWithEvidenceDepth(s, 0)
}

func scrubWithEvidenceDepth(s string, depth int) (string, RedactionEvidence) {
	if s == "" {
		return s, RedactionEvidence{}
	}
	scrubbed, evidence := scrubCredentialURLsWithEvidenceDepth(s, depth)
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
	return Scrub(safeErrorMessage(err))
}
