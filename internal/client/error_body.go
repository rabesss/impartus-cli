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
	redacted := secrets.RedactWithToken(strings.TrimSpace(string(contents)), token)
	return redactTokenFragments(redacted, token), nil
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
