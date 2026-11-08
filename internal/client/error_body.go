package client

import (
	"io"
	"net/url"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/rabesss/impartus-cli/internal/secrets"
)

// UpstreamErrorBodyOmitted replaces an upstream error body that is larger
// than the caller's read cap.
const UpstreamErrorBodyOmitted = "upstream response body omitted"

// minTokenFragment is the shortest run of bearer bytes that
// ReadSanitizedErrorBody treats as leaked token material. Shorter runs are
// indistinguishable from ordinary text.
const minTokenFragment = 16

// maxFragmentRedactionTokenSize is the largest bearer, after any Bearer
// prefix is removed, whose fragments ReadSanitizedErrorBody searches for.
// Fragment matching costs time in proportion to the token's length for every
// body, so a body is omitted rather than searched for an outsized token.
const maxFragmentRedactionTokenSize = 64 * 1024

// maxWholeTokenRedactionSize is the largest token secrets.RedactWithToken
// matches by spelling instead of replacing the whole value.
const maxWholeTokenRedactionSize = 4 * 1024

// ReadSanitizedErrorBody reads an upstream error body of at most limit bytes
// and returns it with credentials and the caller's token redacted. A larger
// body is replaced by UpstreamErrorBodyOmitted rather than truncated, since a
// credential could start before the cap and continue past it. Besides whole
// token spellings, any run of minTokenFragment or more bytes that also occurs
// in the token is redacted, so an upstream that echoes a truncated or partial
// bearer cannot leak part of it. A body that carries no token material is
// kept whatever the token's length, up to maxFragmentRedactionTokenSize.
func ReadSanitizedErrorBody(body io.Reader, limit int64, token string) (string, error) {
	contents, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return "", secrets.SanitizeErrorWithToken(err, token)
	}
	if int64(len(contents)) > limit {
		return UpstreamErrorBodyOmitted, nil
	}
	if len(normalizeBearerToken(token)) > maxFragmentRedactionTokenSize {
		return UpstreamErrorBodyOmitted, nil
	}
	trimmed := strings.TrimSpace(string(contents))
	var redacted string
	if len(strings.TrimSpace(token)) > maxWholeTokenRedactionSize {
		// RedactWithToken replaces any body with a fixed marker for a token
		// this large. Every whole-token spelling it would remove holds a
		// minTokenFragment run that redactTokenFragments or
		// hasEncodedTokenFragment catches, so scrub the generic credentials
		// and rely on those instead.
		redacted = redactTokenFragments(secrets.Scrub(trimmed), token)
	} else {
		redacted = redactTokenFragments(secrets.RedactWithToken(trimmed, token), token)
	}
	if hasEncodedTokenFragment(redacted, token) {
		// Part of the token survives behind percent or JSON escapes that a
		// byte-level redaction cannot map back to the body, so drop it all.
		return UpstreamErrorBodyOmitted, nil
	}
	return redacted, nil
}

// maxErrorBodyDecodeDepth bounds how many layers of percent and JSON escapes
// hasEncodedTokenFragment unwraps.
const maxErrorBodyDecodeDepth = 3

// hasEncodedTokenFragment reports whether a percent- or JSON-decoded view of
// value still holds a minTokenFragment-byte run of the token, or whether value
// is still decoding after the bounded number of layers.
func hasEncodedTokenFragment(value, token string) bool {
	bare := normalizeBearerToken(token)
	if len(bare) < minTokenFragment {
		return false
	}
	view := value
	for depth := 0; depth <= maxErrorBodyDecodeDepth; depth++ {
		decoded, changed := decodeErrorBodyLayer(view)
		if !changed {
			return false
		}
		if depth == maxErrorBodyDecodeDepth {
			return true
		}
		if containsTokenFragment(decoded, []string{bare}) {
			return true
		}
		view = decoded
	}
	return false
}

func containsTokenFragment(value string, spellings []string) bool {
	for start := 0; start+minTokenFragment <= len(value); start++ {
		window := value[start : start+minTokenFragment]
		for _, spelling := range spellings {
			if strings.Contains(spelling, window) {
				return true
			}
		}
	}
	return false
}

// decodeErrorBodyLayer decodes one layer of %XX escapes and of the JSON
// string escapes (\uXXXX, \", \\, \/ and the control escapes). Invalid
// escapes are kept as they are.
func decodeErrorBodyLayer(value string) (string, bool) {
	if !strings.ContainsAny(value, "%\\") {
		return value, false
	}
	var out strings.Builder
	out.Grow(len(value))
	changed := false
	for i := 0; i < len(value); {
		if n := decodeEscapeAt(&out, value, i); n > 0 {
			i += n
			changed = true
			continue
		}
		out.WriteByte(value[i])
		i++
	}
	if !changed {
		return value, false
	}
	return out.String(), true
}

// decodeEscapeAt writes the decoding of the %XX or JSON string escape that
// starts at value[i] to out and returns how many input bytes it spans. It
// returns 0 and writes nothing when value[i] does not start a valid escape.
func decodeEscapeAt(out *strings.Builder, value string, i int) int {
	switch c := value[i]; {
	case c == '%' && i+2 < len(value) && isHexDigit(value[i+1]) && isHexDigit(value[i+2]):
		out.WriteByte(unhex(value[i+1])<<4 | unhex(value[i+2]))
		return 3
	case c == '\\' && i+5 < len(value) && value[i+1] == 'u' && isHex4(value[i+2:i+6]):
		r, n := decodeJSONUnicodeEscape(value, i)
		out.WriteRune(r)
		return n
	case c == '\\' && i+1 < len(value) && strings.IndexByte(`"\/bfnrt`, value[i+1]) >= 0:
		out.WriteByte(jsonEscapeByte(value[i+1]))
		return 2
	default:
		return 0
	}
}

// decodeJSONUnicodeEscape decodes the \uXXXX escape at value[i] and returns
// the character and how many input bytes it spans. A JSON decoder joins a
// UTF-16 surrogate pair into one character, so this view does too.
func decodeJSONUnicodeEscape(value string, i int) (rune, int) {
	r := unhex4(value[i+2 : i+6])
	if utf16.IsSurrogate(r) && i+11 < len(value) && value[i+6] == '\\' && value[i+7] == 'u' && isHex4(value[i+8:i+12]) {
		if joined := utf16.DecodeRune(r, unhex4(value[i+8:i+12])); joined != utf8.RuneError {
			return joined, 12
		}
	}
	return r, 6
}

func jsonEscapeByte(c byte) byte {
	switch c {
	case 'b':
		return '\b'
	case 'f':
		return '\f'
	case 'n':
		return '\n'
	case 'r':
		return '\r'
	case 't':
		return '\t'
	default:
		return c
	}
}

func isHex4(s string) bool {
	return len(s) == 4 && isHexDigit(s[0]) && isHexDigit(s[1]) && isHexDigit(s[2]) && isHexDigit(s[3])
}

func unhex4(s string) rune {
	return rune(unhex(s[0]))<<12 | rune(unhex(s[1]))<<8 | rune(unhex(s[2]))<<4 | rune(unhex(s[3]))
}

func isHexDigit(c byte) bool {
	return ('0' <= c && c <= '9') || ('a' <= c && c <= 'f') || ('A' <= c && c <= 'F')
}

func unhex(c byte) byte {
	switch {
	case '0' <= c && c <= '9':
		return c - '0'
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10
	default:
		return c - 'A' + 10
	}
}

// redactTokenFragments replaces every maximal run of value covered by
// minTokenFragment-byte windows that also occur in a spelling of token.
func redactTokenFragments(value, token string) string {
	spellings := tokenFragmentSpellings(token)
	if len(spellings) == 0 || len(value) < minTokenFragment {
		return value
	}
	covered := make([]bool, len(value))
	found := false
	for start := 0; start+minTokenFragment <= len(value); start++ {
		window := value[start : start+minTokenFragment]
		for _, spelling := range spellings {
			if strings.Contains(spelling, window) {
				for i := start; i < start+minTokenFragment; i++ {
					covered[i] = true
				}
				found = true
				break
			}
		}
	}
	if !found {
		return value
	}
	var out strings.Builder
	out.Grow(len(value))
	for i := 0; i < len(value); {
		if !covered[i] {
			out.WriteByte(value[i])
			i++
			continue
		}
		out.WriteString("REDACTED")
		for i < len(value) && covered[i] {
			i++
		}
	}
	return out.String()
}

func tokenFragmentSpellings(token string) []string {
	bare := normalizeBearerToken(token)
	if len(bare) < minTokenFragment {
		return nil
	}
	spellings := make([]string, 0, 3)
	seen := make(map[string]struct{}, 3)
	for _, spelling := range []string{bare, url.QueryEscape(bare), url.PathEscape(bare)} {
		if _, ok := seen[spelling]; ok {
			continue
		}
		seen[spelling] = struct{}{}
		spellings = append(spellings, spelling)
	}
	return spellings
}
