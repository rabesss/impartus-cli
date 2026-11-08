package secrets

import "strings"

// containsTokenVariant reports whether value carries token in any spelling
// that replaceTokenVariants would redact: the raw, Bearer, percent-encoded
// and JSON-escaped variants, checked on value and on a bounded number of
// percent-decoded views of it.
//
// Unlike replaceTokenVariants it has no cap on the size of value. A redactor
// may replace a large value with a fixed marker because it cannot afford to
// keep it, but a detector that reported every large value as carrying the
// token would make request policy refuse or rewrite benign long URLs. The scan
// stays linear in len(value) for each of at most maxTokenVariantCount
// variants. It still fails closed where the redactor does: for a token larger
// than maxTokenVariantInputSize, and for a value that is still
// percent-decoding after the decode budget.
func containsTokenVariant(value, token string) bool {
	trimmed := strings.TrimSpace(token)
	if value == "" || trimmed == "" {
		return false
	}
	if len(trimmed) > maxTokenVariantInputSize {
		return true
	}
	candidate := value
	if replaceTokenVariantsDirect(candidate, token) != candidate {
		return true
	}
	for depth := 0; depth < maxRawPercentDecodeDepth; depth++ {
		decoded, changed := decodePercentEscapes(candidate)
		if !changed {
			return false
		}
		if replaceTokenVariantsDirect(decoded, token) != decoded {
			return true
		}
		candidate = decoded
	}
	_, stillEncoded := decodePercentEscapes(candidate)
	return stillEncoded
}

// ScrubInputTooLarge reports whether Scrub replaces s with a fixed marker
// because of its size alone, rather than because s holds a credential.
func ScrubInputTooLarge(s string) bool {
	return len(s) > maxScrubInputSize
}
