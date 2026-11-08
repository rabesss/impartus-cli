package secrets

import (
	"net/url"
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
