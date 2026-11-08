package client

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"unicode/utf16"
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

// jsonEscapeAll spells every byte of s as a JSON \u escape.
func jsonEscapeAll(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		fmt.Fprintf(&b, `\u%04x`, s[i])
	}
	return b.String()
}

// percentEscapeEvery escapes every other byte of s, so no 16-byte window of
// the literal or fully escaped token appears.
func percentEscapeEvery(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if i%2 == 0 {
			fmt.Fprintf(&b, "%%%02X", s[i])
		} else {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func encodedPartialTokenBodies(token string) map[string]string {
	return map[string]string{
		"json-escaped prefix":       `{"error":"bad token ` + jsonEscapeAll(token[:40]) + `"}`,
		"selectively percent-coded": "bad token " + percentEscapeEvery(token[:40]),
		"double percent-coded":      "bad token " + strings.ReplaceAll(percentEscapeEvery(token[:40]), "%", "%25"),
	}
}

func TestReadSanitizedErrorBodyOmitsEncodedPartialToken(t *testing.T) {
	token := longTestBearer()
	for name, body := range encodedPartialTokenBodies(token) {
		t.Run(name, func(t *testing.T) {
			got, err := ReadSanitizedErrorBody(strings.NewReader(body), 512, token)
			if err != nil {
				t.Fatalf("ReadSanitizedErrorBody() error = %v", err)
			}
			if got != UpstreamErrorBodyOmitted {
				t.Fatalf("ReadSanitizedErrorBody() = %q, want %q", got, UpstreamErrorBodyOmitted)
			}
		})
	}
}

func TestReadSanitizedErrorBodyKeepsOrdinaryEscapes(t *testing.T) {
	const body = `{"error":"quota\nexceeded","path":"/a%20b"}`
	got, err := ReadSanitizedErrorBody(strings.NewReader(body), 512, longTestBearer())
	if err != nil {
		t.Fatalf("ReadSanitizedErrorBody() error = %v", err)
	}
	if got != body {
		t.Fatalf("ReadSanitizedErrorBody() = %q, want %q", got, body)
	}
}

type tokenErrorReader struct{ token string }

func (r tokenErrorReader) Read([]byte) (int, error) {
	return 0, errors.New("read failed near " + r.token)
}

func TestReadSanitizedErrorBodySanitizesReadError(t *testing.T) {
	token := longTestBearer()[:100]
	got, err := ReadSanitizedErrorBody(tokenErrorReader{token: token}, 512, token)
	if err == nil {
		t.Fatalf("ReadSanitizedErrorBody() = %q, nil; want a read error", got)
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("read error exposed the token: %v", err)
	}
}

func TestReadSanitizedErrorBodyOmitsSurrogatePairTokenFragment(t *testing.T) {
	// Four non-BMP characters are 16 UTF-8 bytes; JSON spells each as a
	// surrogate pair.
	token := strings.Repeat("\U0001F600", 4) + longTestBearer()[:200]
	var escaped strings.Builder
	for _, r := range token[:16] {
		high, low := utf16.EncodeRune(r)
		fmt.Fprintf(&escaped, `\u%04x\u%04x`, high, low)
	}
	got, err := ReadSanitizedErrorBody(strings.NewReader(`{"error":"`+escaped.String()+`"}`), 512, token)
	if err != nil {
		t.Fatalf("ReadSanitizedErrorBody() error = %v", err)
	}
	if got != UpstreamErrorBodyOmitted {
		t.Fatalf("ReadSanitizedErrorBody() = %q, want %q", got, UpstreamErrorBodyOmitted)
	}
}

func TestReadSanitizedErrorBodyKeepsBodyForTokenOver4KiB(t *testing.T) {
	token := strings.Repeat(longTestBearer(), 6)[:5000]
	const body = `{"error":"quota exceeded"}`
	got, err := ReadSanitizedErrorBody(strings.NewReader(body), 512, token)
	if err != nil || got != body {
		t.Fatalf("ReadSanitizedErrorBody() = %q, %v; want %q", got, err, body)
	}
	got, err = ReadSanitizedErrorBody(strings.NewReader("echo "+token[1000:1100]+" access_token=other-secret"), 512, token)
	if err != nil {
		t.Fatalf("ReadSanitizedErrorBody() error = %v", err)
	}
	assertNoTokenFragment(t, got, token)
	if strings.Contains(got, "other-secret") {
		t.Fatalf("ReadSanitizedErrorBody() = %q kept a generic credential", got)
	}
}
