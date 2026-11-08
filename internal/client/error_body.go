package client

import (
	"fmt"
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
		return "", sanitizeErrorBodyReadError(err, token)
	}
	if int64(len(contents)) > limit {
		return UpstreamErrorBodyOmitted, nil
	}
	if len(normalizeBearerToken(token)) > maxFragmentRedactionTokenSize {
		return UpstreamErrorBodyOmitted, nil
	}
	trimmed := strings.TrimSpace(string(contents))
	var windows tokenWindows
	if len(trimmed) >= minTokenFragment {
		// Decoding only shortens text, so a shorter body cannot hold a
		// fragment in any view.
		windows = newTokenWindows(token)
	}
	var redacted string
	if len(strings.TrimSpace(token)) > maxWholeTokenRedactionSize {
		// RedactWithToken replaces any body with a fixed marker for a token
		// this large. Every whole-token spelling it would remove holds a
		// minTokenFragment run that redactTokenFragments or
		// hasEncodedTokenFragment catches, so scrub the generic credentials
		// and rely on those instead.
		redacted = redactTokenFragments(secrets.Scrub(trimmed), windows)
	} else {
		redacted = redactTokenFragments(secrets.RedactWithToken(trimmed, token), windows)
	}
	if hasEncodedTokenFragment(redacted, windows) {
		// Part of the token survives behind percent or JSON escapes that a
		// byte-level redaction cannot map back to the body, so drop it all.
		return UpstreamErrorBodyOmitted, nil
	}
	return redacted, nil
}

// errorBodyReadFailed prefixes a read error whose text still carried token
// material after sanitizing; the text itself is dropped.
const errorBodyReadFailed = "upstream error body could not be read"

// sanitizeErrorBodyReadError scrubs an error from reading an upstream error
// body. SanitizeErrorWithToken removes whole token spellings, but a transport
// error can echo a truncated or escaped part of the bearer, so a message that
// still holds a minTokenFragment run, raw or behind escapes, is replaced
// entirely. Safe classifications such as timeouts, cancellation and transient
// network errors survive either way, so retry decisions are unchanged.
func sanitizeErrorBodyReadError(err error, token string) error {
	sanitized := secrets.SanitizeErrorWithToken(err, token)
	if len(normalizeBearerToken(token)) > maxFragmentRedactionTokenSize {
		return fmt.Errorf("%s: %w", errorBodyReadFailed, opaqueSanitizedError(err))
	}
	message := sanitized.Error()
	if len(message) < minTokenFragment {
		return sanitized
	}
	windows := newTokenWindows(token)
	if !windows.containsFragment(message) && !hasEncodedTokenFragment(message, windows) {
		return sanitized
	}
	return fmt.Errorf("%s: %w", errorBodyReadFailed, opaqueSanitizedError(err))
}

// opaqueSanitizedError returns err's safe classifications with its whole
// message redacted: the message is passed as the credential to remove.
func opaqueSanitizedError(err error) error {
	return secrets.SanitizeErrorWithCredentials(err, panicSafeErrorText(err))
}

func panicSafeErrorText(err error) (text string) {
	defer func() {
		if recover() != nil {
			text = ""
		}
	}()
	return err.Error()
}

// tokenWindows is the set of every minTokenFragment-byte run in any spelling
// of a bearer token (bare, query-escaped and path-escaped). Building it once
// makes each fragment check linear in the length of the checked text.
type tokenWindows map[[minTokenFragment]byte]struct{}

func newTokenWindows(token string) tokenWindows {
	spellings := tokenFragmentSpellings(token)
	if len(spellings) == 0 {
		return nil
	}
	size := 0
	for _, spelling := range spellings {
		size += max(0, len(spelling)-minTokenFragment+1)
	}
	windows := make(tokenWindows, size)
	for _, spelling := range spellings {
		for start := 0; start+minTokenFragment <= len(spelling); start++ {
			windows[[minTokenFragment]byte([]byte(spelling[start:start+minTokenFragment]))] = struct{}{}
		}
	}
	return windows
}

func (w tokenWindows) hasWindowAt(value string, start int) bool {
	_, ok := w[[minTokenFragment]byte([]byte(value[start:start+minTokenFragment]))]
	return ok
}

func (w tokenWindows) containsFragment(value string) bool {
	if len(w) == 0 {
		return false
	}
	for start := 0; start+minTokenFragment <= len(value); start++ {
		if w.hasWindowAt(value, start) {
			return true
		}
	}
	return false
}

// maxErrorBodyDecodeViews bounds how many distinct decoded views of a body
// hasEncodedTokenFragment explores. Every caller caps error bodies at a few
// hundred bytes, so the bound is generous for real escapes while keeping the
// search cheap; a body that still yields new views at the bound is treated
// as carrying token material.
const maxErrorBodyDecodeViews = 128

// errorBodyDecoders are the single-layer decodings hasEncodedTokenFragment
// applies in every order: percent escapes only, JSON escapes only, and both
// in one pass. Trying them separately catches spellings such as
// %41\u002541, where only alternating the two kinds reveals the token.
var errorBodyDecoders = []func(string) (string, bool){
	func(value string) (string, bool) { return decodeErrorBodyLayer(value, true, false) },
	func(value string) (string, bool) { return decodeErrorBodyLayer(value, false, true) },
	func(value string) (string, bool) { return decodeErrorBodyLayer(value, true, true) },
}

// hasEncodedTokenFragment reports whether any view of value reached by
// repeatedly decoding percent and JSON escapes, in any order, holds a
// minTokenFragment-byte run of a token spelling. Each decoding that changes
// a view makes it strictly shorter, so the search ends; it is also capped at
// maxErrorBodyDecodeViews views and fails closed beyond that.
func hasEncodedTokenFragment(value string, windows tokenWindows) bool {
	if len(windows) == 0 {
		return false
	}
	seen := map[string]struct{}{value: {}}
	pending := []string{value}
	for len(pending) > 0 {
		view := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		for _, decode := range errorBodyDecoders {
			decoded, changed := decode(view)
			if !changed {
				continue
			}
			if _, ok := seen[decoded]; ok {
				continue
			}
			if windows.containsFragment(decoded) {
				return true
			}
			if len(seen) >= maxErrorBodyDecodeViews {
				return true
			}
			seen[decoded] = struct{}{}
			pending = append(pending, decoded)
		}
	}
	return false
}

// decodeErrorBodyLayer decodes one layer of %XX escapes when percent is set
// and of the JSON string escapes (\uXXXX, \", \\, \/ and the control
// escapes) when json is set. Invalid escapes are kept as they are.
func decodeErrorBodyLayer(value string, percent, json bool) (string, bool) {
	if (!percent || strings.IndexByte(value, '%') < 0) && (!json || strings.IndexByte(value, '\\') < 0) {
		return value, false
	}
	var out strings.Builder
	out.Grow(len(value))
	changed := false
	for i := 0; i < len(value); {
		if n := decodeEscapeAt(&out, value, i, percent, json); n > 0 {
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

// decodeEscapeAt writes the decoding of the %XX (when percent is set) or JSON
// string escape (when json is set) that starts at value[i] to out and returns
// how many input bytes it spans. It returns 0 and writes nothing when value[i]
// does not start an enabled, valid escape.
func decodeEscapeAt(out *strings.Builder, value string, i int, percent, json bool) int {
	switch c := value[i]; {
	case percent && c == '%' && i+2 < len(value) && isHexDigit(value[i+1]) && isHexDigit(value[i+2]):
		out.WriteByte(unhex(value[i+1])<<4 | unhex(value[i+2]))
		return 3
	case json && c == '\\' && i+5 < len(value) && value[i+1] == 'u' && isHex4(value[i+2:i+6]):
		r, n := decodeJSONUnicodeEscape(value, i)
		out.WriteRune(r)
		return n
	case json && c == '\\' && i+1 < len(value) && strings.IndexByte(`"\/bfnrt`, value[i+1]) >= 0:
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
// minTokenFragment-byte windows that also occur in a spelling of the token.
func redactTokenFragments(value string, windows tokenWindows) string {
	if len(windows) == 0 || len(value) < minTokenFragment {
		return value
	}
	covered := make([]bool, len(value))
	found := false
	for start := 0; start+minTokenFragment <= len(value); start++ {
		if windows.hasWindowAt(value, start) {
			for i := start; i < start+minTokenFragment; i++ {
				covered[i] = true
			}
			found = true
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
