package client

import (
	"net/url"
	"strings"
	"testing"
)

// longTestBearer is a 900-byte placeholder bearer, longer than every error
// body read cap.
func longTestBearer() string {
	var b strings.Builder
	for i := 0; b.Len() < 900; i++ {
		b.WriteString("placeholder-jwt-segment-")
		b.WriteByte(byte('a' + i%26))
	}
	return b.String()[:900]
}

func assertNoTokenFragment(t *testing.T, got, token string) {
	t.Helper()
	for i := 0; i+minTokenFragment <= len(token); i++ {
		if strings.Contains(got, token[i:i+minTokenFragment]) {
			t.Fatalf("sanitized body %q kept token run %q", got, token[i:i+minTokenFragment])
		}
	}
}

func TestReadSanitizedErrorBodyKeepsBodyForLongToken(t *testing.T) {
	const body = `{"error":"quota exceeded"}`
	for _, size := range []int{100, 512, 513, 900} {
		token := longTestBearer()[:size]
		got, err := readSanitizedErrorBody(strings.NewReader(body), token)
		if err != nil {
			t.Fatalf("token %d bytes: readSanitizedErrorBody() error = %v", size, err)
		}
		if got != body {
			t.Fatalf("token %d bytes: readSanitizedErrorBody() = %q, want %q", size, got, body)
		}
	}
}

func TestReadSanitizedErrorBodyRedactsPartialToken(t *testing.T) {
	token := longTestBearer()
	tests := []struct {
		name string
		body string
	}{
		{name: "token prefix at end", body: "upstream echoed: " + token[:200]},
		{name: "token middle", body: "before " + token[300:340] + " after"},
		{name: "percent-encoded prefix", body: "url=" + url.QueryEscape(token[:64])},
		{name: "bearer prefixed", body: "Authorization: Bearer " + token[:100]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ReadSanitizedErrorBody(strings.NewReader(tt.body), 512, token)
			if err != nil {
				t.Fatalf("ReadSanitizedErrorBody() error = %v", err)
			}
			assertNoTokenFragment(t, got, token)
			if !strings.Contains(got, "REDACTED") {
				t.Fatalf("ReadSanitizedErrorBody() = %q, want a redaction marker", got)
			}
		})
	}
}

func TestReadSanitizedErrorBodyKeepsTextAroundFragments(t *testing.T) {
	token := longTestBearer()
	got, err := ReadSanitizedErrorBody(strings.NewReader("quota exceeded for "+token[:200]+" at noon"), 512, token)
	if err != nil {
		t.Fatalf("ReadSanitizedErrorBody() error = %v", err)
	}
	if got != "quota exceeded for REDACTED at noon" {
		t.Fatalf("ReadSanitizedErrorBody() = %q", got)
	}
}

func TestReadSanitizedErrorBodyOmitsOversizedBody(t *testing.T) {
	got, err := ReadSanitizedErrorBody(strings.NewReader(strings.Repeat("x", 513)), 512, "short-token")
	if err != nil {
		t.Fatalf("ReadSanitizedErrorBody() error = %v", err)
	}
	if got != UpstreamErrorBodyOmitted {
		t.Fatalf("ReadSanitizedErrorBody() = %q, want %q", got, UpstreamErrorBodyOmitted)
	}
}
