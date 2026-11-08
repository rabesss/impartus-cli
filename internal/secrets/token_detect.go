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
	variants := tokenVariants(token)
	candidate := value
	if matchesTokenVariantsDirect(candidate, variants) {
		return true
	}
	for depth := 0; depth < maxRawPercentDecodeDepth; depth++ {
		decoded, changed := decodePercentEscapes(candidate)
		if !changed {
			return false
		}
		if matchesTokenVariantsDirect(decoded, variants) {
			return true
		}
		candidate = decoded
	}
	_, stillEncoded := decodePercentEscapes(candidate)
	return stillEncoded
}

// matchesTokenVariantsDirect is the boolean form of
// replaceTokenVariantsDirect. It searches for the variants instead of
// comparing replaced output with its input, which would miss a token whose
// spelling equals the replacement marker.
func matchesTokenVariantsDirect(value string, variants []string) bool {
	for _, variant := range variants {
		if variant != "" && strings.Contains(value, variant) {
			return true
		}
	}
	if !strings.Contains(value, "\\") {
		return false
	}
	view, changed := decodeJSONEscapes(value)
	if !changed {
		return false
	}
	for _, variant := range variants {
		if variant != "" && strings.Contains(view, variant) {
			return true
		}
	}
	return false
}

// decodeJSONEscapes decodes one layer of JSON string escapes, like
// decodeEncodedViewLayer without percent decoding, but keeps no per-byte
// source spans. A detector only needs the text, and the spans would cost
// sixteen bytes per input byte on the large values it accepts.
func decodeJSONEscapes(value string) (string, bool) {
	decoded := make([]byte, 0, len(value))
	changed := false
	for index := 0; index < len(value); {
		var width int
		var ok bool
		decoded, width, ok = appendJSONEscape(decoded, value, index)
		if ok {
			index += width
			changed = true
			continue
		}
		decoded = append(decoded, value[index])
		index++
	}
	if !changed {
		return value, false
	}
	return string(decoded), true
}

// ScrubInputTooLarge reports whether Scrub replaces s with a fixed marker
// because of its size alone, rather than because s holds a credential.
func ScrubInputTooLarge(s string) bool {
	return len(s) > maxScrubInputSize
}
