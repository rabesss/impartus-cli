package client

import (
	"io"
	"net/url"
	"strings"

	"github.com/rabesss/impartus-cli/internal/secrets"
)

// UpstreamErrorBodyOmitted replaces an upstream error body that is larger
// than the caller's read cap.
const UpstreamErrorBodyOmitted = "upstream response body omitted"

// minTokenFragment is the shortest run of bearer bytes that
// ReadSanitizedErrorBody treats as leaked token material. Shorter runs are
// indistinguishable from ordinary text.
const minTokenFragment = 16

// ReadSanitizedErrorBody reads an upstream error body of at most limit bytes
// and returns it with credentials and the caller's token redacted. A larger
// body is replaced by UpstreamErrorBodyOmitted rather than truncated, since a
// credential could start before the cap and continue past it. Besides whole
// token spellings, any run of minTokenFragment or more bytes that also occurs
// in the token is redacted, so an upstream that echoes a truncated or partial
// bearer cannot leak part of it. The token's length does not matter: a body
// that carries no token material is kept.
func ReadSanitizedErrorBody(body io.Reader, limit int64, token string) (string, error) {
	contents, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return "", secrets.SanitizeErrorWithToken(err, token)
	}
	if int64(len(contents)) > limit {
		return UpstreamErrorBodyOmitted, nil
	}
	redacted := redactTokenFragments(secrets.RedactWithToken(strings.TrimSpace(string(contents)), token), token)
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
	for i := 0; i < len(value); i++ {
		switch c := value[i]; {
		case c == '%' && i+2 < len(value) && isHexDigit(value[i+1]) && isHexDigit(value[i+2]):
			out.WriteByte(unhex(value[i+1])<<4 | unhex(value[i+2]))
			i += 2
			changed = true
		case c == '\\' && i+5 < len(value) && value[i+1] == 'u' &&
			isHexDigit(value[i+2]) && isHexDigit(value[i+3]) && isHexDigit(value[i+4]) && isHexDigit(value[i+5]):
			r := rune(unhex(value[i+2]))<<12 | rune(unhex(value[i+3]))<<8 | rune(unhex(value[i+4]))<<4 | rune(unhex(value[i+5]))
			out.WriteRune(r)
			i += 5
			changed = true
		case c == '\\' && i+1 < len(value) && strings.IndexByte(`"\/bfnrt`, value[i+1]) >= 0:
			out.WriteByte(jsonEscapeByte(value[i+1]))
			i++
			changed = true
		default:
			out.WriteByte(c)
		}
	}
	if !changed {
		return value, false
	}
	return out.String(), true
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
