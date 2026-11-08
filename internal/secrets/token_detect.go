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
	if matchesTokenVariantsDirect(value, variants) {
		return true
	}
	firstDecoded, firstChanged := decodePercentView(value)
	decoded, changed := firstDecoded, firstChanged
	for depth := 0; changed; depth++ {
		if depth == maxRawPercentDecodeDepth {
			// Still percent-decoding after the budget.
			return true
		}
		if matchesTokenVariantsDirect(decoded, variants) {
			return true
		}
		decoded, changed = decodePercentView(decoded)
	}
	return containsInterleavedTokenVariant(firstDecoded, firstChanged, variants)
}

// containsInterleavedTokenVariant checks the views that strip a percent layer
// and then a JSON layer at each step, so a token wrapped in alternating
// percent and JSON escapes (such as a URL-escaped, double-escaped \uXXXX
// spelling) is found. The percent-only views alone miss it, since each is
// checked through just one JSON layer. A value still decoding after the
// budget counts as carrying the token. percentDecoded and percentChanged are
// the first percent layer of the value, which the caller has already decoded.
func containsInterleavedTokenVariant(percentDecoded string, percentChanged bool, variants []string) bool {
	for depth := 0; ; depth++ {
		decoded, jsonChanged := decodeJSONEscapes(percentDecoded)
		if !percentChanged && !jsonChanged {
			return false
		}
		if depth == maxRawPercentDecodeDepth {
			return true
		}
		// The JSON view of each view is the next decoded step, so plain
		// searches suffice here.
		if (percentChanged && containsTokenVariantSpelling(percentDecoded, variants)) || containsTokenVariantSpelling(decoded, variants) {
			return true
		}
		percentDecoded, percentChanged = decodePercentView(decoded)
	}
}

// decodePercentView is decodePercentEscapes without the copy it makes of a
// value that holds no escape.
func decodePercentView(value string) (string, bool) {
	if strings.IndexByte(value, '%') < 0 {
		return value, false
	}
	return decodePercentEscapes(value)
}

func containsTokenVariantSpelling(value string, variants []string) bool {
	for _, variant := range variants {
		if variant != "" && strings.Contains(value, variant) {
			return true
		}
	}
	return false
}

// matchesTokenVariantsDirect is the boolean form of
// replaceTokenVariantsDirect. It searches for the variants instead of
// comparing replaced output with its input, which would miss a token whose
// spelling equals the replacement marker.
func matchesTokenVariantsDirect(value string, variants []string) bool {
	if containsTokenVariantSpelling(value, variants) {
		return true
	}
	if !strings.Contains(value, "\\") {
		return false
	}
	view, changed := decodeJSONEscapes(value)
	return changed && containsTokenVariantSpelling(view, variants)
}

// decodeJSONEscapes decodes one layer of JSON string escapes, like
// decodeEncodedViewLayer without percent decoding, but keeps no per-byte
// source spans. A detector only needs the text, and the spans would cost
// sixteen bytes per input byte on the large values it accepts.
func decodeJSONEscapes(value string) (string, bool) {
	if strings.IndexByte(value, '\\') < 0 {
		return value, false
	}
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
