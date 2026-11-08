package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
)

func readErrorBody(t *testing.T, body, token string) string {
	t.Helper()
	got, err := ReadSanitizedErrorBody(strings.NewReader(body), 512, token)
	if err != nil {
		t.Fatalf("ReadSanitizedErrorBody() error = %v", err)
	}
	return got
}

// alternateEscapes wraps the escapes in v in layers of alternating kinds: a
// JSON layer spells every '%' as \u0025 and a percent layer spells every '\'
// as %5C. Decoding needs both kinds, in turn, once per layer.
func alternateEscapes(v string, layers int) string {
	for i := 0; i < layers; i++ {
		if i%2 == 0 {
			v = strings.ReplaceAll(v, "%", "\\u0025")
		} else {
			v = strings.ReplaceAll(v, `\`, "%5C")
		}
	}
	return v
}

func TestReadSanitizedErrorBodyOmitsMixedDecodeOrderFragments(t *testing.T) {
	percentToken := strings.Repeat("%41", 12)
	jsonToken := strings.Repeat("\\u0041", 6)
	plusToken := strings.Repeat("ab+cd/", 8)
	tests := []struct {
		name, token, body string
	}{
		// Decoding both kinds at once turns the literal %41 runs into A, so
		// only a JSON-only view rebuilds the token.
		{"issue example", percentToken, strings.Repeat("%41\\u002541", 6)},
		{"nested JSON-only", percentToken, strings.Repeat("%41\\\\u002541", 6)},
		{"percent-only", jsonToken, strings.Repeat("\\u0041%5Cu0041", 4)},
		{"nested percent-only", jsonToken, strings.Repeat("\\u0041%255Cu0041", 4)},
		{"five alternating layers", longTestBearer(), "bad token " + alternateEscapes(percentEscapeEvery(longTestBearer()[:24]), 5)},
		{"encoded escaped spelling", plusToken, "bad token " + strings.ReplaceAll(url.QueryEscape(plusToken[:30]), "%", "%25")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.body) > 512 {
				t.Fatalf("test body is %d bytes, over the 512-byte cap", len(tt.body))
			}
			windows := newTokenWindows(tt.token)
			if windows.containsFragment(tt.body) {
				t.Fatalf("test body %q holds a raw token fragment", tt.body)
			}
			if !hasEncodedTokenFragment(tt.body, windows) {
				t.Fatalf("hasEncodedTokenFragment(%q) = false, want true", tt.body)
			}
			// secrets.RedactWithToken may already remove some of these
			// spellings; either way no view of the result may hold the token.
			got := readErrorBody(t, tt.body, tt.token)
			if windows.containsFragment(got) || hasEncodedTokenFragment(got, windows) {
				t.Fatalf("ReadSanitizedErrorBody() = %q kept token material", got)
			}
		})
	}
}

func TestHasEncodedTokenFragmentKeepsDeepBenignViews(t *testing.T) {
	// Before #303 the fragment search omitted any body still decoding after
	// three layers. secrets.Scrub still redacts deeply escaped text on its
	// own, so this checks the search directly.
	windows := newTokenWindows(longTestBearer())
	for _, body := range []string{
		`{"path":"/a%25252525252520b"}`,
		"note " + alternateEscapes("%41%42%43", 6),
	} {
		if hasEncodedTokenFragment(body, windows) {
			t.Fatalf("hasEncodedTokenFragment(%q) = true, want false", body)
		}
	}
}

// independentEscapeChains returns a body with a percent chain and a JSON
// chain that each need depth layers, so its decoded views number
// (depth+1)^2: every mix of percent-only, JSON-only and combined steps.
func independentEscapeChains(depth int) string {
	percent := "%" + strings.Repeat("25", depth-1) + "41"
	json := `\` + strings.Repeat("u005c", depth-1) + "u0041"
	return "a " + percent + " b " + json
}

func TestHasEncodedTokenFragmentBoundsDecodedViews(t *testing.T) {
	windows := newTokenWindows(longTestBearer())
	// 81 views stay under maxErrorBodyDecodeViews; 169 views exceed it and
	// fail closed.
	for depth, want := range map[int]bool{8: false, 12: true} {
		if got := hasEncodedTokenFragment(independentEscapeChains(depth), windows); got != want {
			t.Fatalf("hasEncodedTokenFragment(depth %d) = %v, want %v", depth, got, want)
		}
	}
}

type wrappedErrorReader struct{ err error }

func (r wrappedErrorReader) Read([]byte) (int, error) { return 0, r.err }

func TestReadSanitizedErrorBodyHidesReadErrorFragments(t *testing.T) {
	token := longTestBearer()
	fragment := token[100:140]
	tests := []struct {
		name  string
		err   error
		check func(error) bool
	}{
		{"timeout", fmt.Errorf("read near %s: %w", fragment, os.ErrDeadlineExceeded), func(err error) bool {
			var netErr net.Error
			return errors.Is(err, os.ErrDeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout())
		}},
		{"unexpected EOF", fmt.Errorf("body %s: %w", fragment, io.ErrUnexpectedEOF), func(err error) bool {
			return errors.Is(err, io.ErrUnexpectedEOF)
		}},
		{"canceled", fmt.Errorf("body %s: %w", url.PathEscape(fragment)+jsonEscapeAll(token[200:220]), context.Canceled), func(err error) bool {
			return errors.Is(err, context.Canceled)
		}},
		{"percent-coded fragment", fmt.Errorf("body %s: %w", percentEscapeEvery(fragment), io.ErrUnexpectedEOF), func(err error) bool {
			return errors.Is(err, io.ErrUnexpectedEOF)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ReadSanitizedErrorBody(wrappedErrorReader{err: tt.err}, 512, token)
			if err == nil {
				t.Fatalf("ReadSanitizedErrorBody() = %q, nil; want a read error", got)
			}
			message := err.Error()
			if !strings.HasPrefix(message, errorBodyReadFailed) {
				t.Fatalf("read error = %q, want the %q prefix", message, errorBodyReadFailed)
			}
			assertNoTokenFragment(t, message, token)
			if hasEncodedTokenFragment(message, newTokenWindows(token)) {
				t.Fatalf("read error %q kept an encoded token fragment", message)
			}
			if !tt.check(err) {
				t.Fatalf("read error %v lost its classification", err)
			}
		})
	}
}

func TestReadSanitizedErrorBodyKeepsBenignReadError(t *testing.T) {
	cause := fmt.Errorf("connection dropped: %w", io.ErrUnexpectedEOF)
	_, err := ReadSanitizedErrorBody(wrappedErrorReader{err: cause}, 512, longTestBearer())
	if err == nil || err.Error() != cause.Error() || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("ReadSanitizedErrorBody() error = %v, want %q with its classification", err, cause.Error())
	}
}
