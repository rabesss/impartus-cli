package secrets

import (
	"fmt"
	"net/url"
	"runtime"
	"strings"
	"testing"
)

func TestContainsTokenIgnoresValueSizeAlone(t *testing.T) {
	const token = "secret-bearer-abc123"
	long := strings.Repeat("a", maxEncodedTokenViewSize+1024)
	tests := []struct {
		name  string
		value string
		token string
		want  bool
	}{
		{name: "large without token", value: long, token: token, want: false},
		{name: "large with token", value: long + token, token: token, want: true},
		{name: "large with query-escaped token", value: long + url.QueryEscape("Bearer "+token), token: token, want: true},
		{name: "large with partly percent-encoded token", value: long + strings.ReplaceAll(token, "c", "%63"), token: token, want: true},
		{name: "large with twice-encoded token", value: long + strings.ReplaceAll(token, "c", "%2563"), token: token, want: true},
		{name: "still encoded after budget", value: long + "%25252525", token: token, want: true},
		{name: "large with URL-escaped double JSON-escaped token", value: long + url.QueryEscape(doubleJSONEscaped(token)), token: token, want: true},
		{name: "large with JSON-escaped percent-encoded token", value: long + jsonUnicodeEscaped(strings.ReplaceAll(token, "c", "%63")), token: token, want: true},
		{name: "large with JSON-escaped benign text", value: long + jsonUnicodeEscaped("lecture-segment-0001"), token: token, want: false},
		{name: "large still JSON-decoding after budget", value: long + strings.Repeat(`\`, 16), token: token, want: true},
		{name: "oversized token fails closed", value: "short", token: strings.Repeat("t", maxTokenVariantInputSize+1), want: true},
		{name: "empty value", value: "", token: token, want: false},
		{name: "empty token", value: long, token: " ", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ContainsToken(tt.value, tt.token); got != tt.want {
				t.Fatalf("ContainsToken() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContainsTokenMatchesRedactorForSmallValues(t *testing.T) {
	const token = "secret-bearer-abc123"
	for _, value := range []string{
		"plain",
		"/seg/" + token,
		"x=" + url.QueryEscape(token),
		`{"t":"` + token + `"}`,
		strings.ReplaceAll(token, "-", "%2D"),
		"%2525252541",
	} {
		want := replaceTokenVariants(value, token) != value
		if got := ContainsToken(value, token); got != want {
			t.Errorf("ContainsToken(%q) = %v, redactor reports %v", value, got, want)
		}
	}
}

func TestScrubInputTooLarge(t *testing.T) {
	if ScrubInputTooLarge(strings.Repeat("a", maxScrubInputSize)) {
		t.Fatal("ScrubInputTooLarge(at cap) = true")
	}
	if !ScrubInputTooLarge(strings.Repeat("a", maxScrubInputSize+1)) {
		t.Fatal("ScrubInputTooLarge(over cap) = false")
	}
}

func TestContainsTokenDetectsMarkerSpelledToken(t *testing.T) {
	long := strings.Repeat("a", maxEncodedTokenViewSize+1)
	for _, value := range []string{"REDACTED", "/x/REDACTED", long + "REDACTED", long + `REDACTED`} {
		if !ContainsToken(value, "REDACTED") {
			t.Errorf("ContainsToken(%.40q, REDACTED) = false", value)
		}
	}
}

func TestDecodeJSONEscapesMatchesEncodedView(t *testing.T) {
	for _, value := range []string{`plain`, `a\u0062c`, `\"q\" \\ \/ \n`, `\ud83d\ude00x`, `bad \u12 tail\`} {
		view := decodeEncodedViewLayer(rawEncodedView(value), false)
		got, changed := decodeJSONEscapes(value)
		if got != view.text || changed != view.changed {
			t.Errorf("decodeJSONEscapes(%q) = %q, %v; encoded view = %q, %v", value, got, changed, view.text, view.changed)
		}
	}
}

func TestContainsTokenLargeValueAllocationIsBounded(t *testing.T) {
	const token = "secret-bearer-abc123"
	value := strings.Repeat(`a\n%41`, (4<<20)/6)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	if ContainsToken(value, token) {
		t.Fatal("ContainsToken() = true, want false")
	}
	runtime.ReadMemStats(&after)
	// Each of the few decoded views costs about one or two bytes per input
	// byte. Per-byte source spans cost sixteen per view and pushed this past
	// thirty times the input.
	if grown, limit := after.TotalAlloc-before.TotalAlloc, uint64(16*len(value)); grown > limit {
		t.Fatalf("ContainsToken(%d bytes) allocated %d bytes, want at most %d", len(value), grown, limit)
	}
}

// jsonUnicodeEscaped spells every byte of s as a \uXXXX escape.
func jsonUnicodeEscaped(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		fmt.Fprintf(&b, `\u%04x`, s[i])
	}
	return b.String()
}

// doubleJSONEscaped spells every byte of s as a \uXXXX escape whose
// backslash is itself JSON-escaped, so two JSON layers recover s.
func doubleJSONEscaped(s string) string {
	return strings.ReplaceAll(jsonUnicodeEscaped(s), `\`, `\\`)
}
