package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
)

func TestSanitizeUpstreamErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"canceled", context.Canceled, "job was canceled or timed out"},
		{"deadline", context.DeadlineExceeded, "job was canceled or timed out"},
		{"dns", &net.DNSError{Err: "no such host"}, "upstream connection failed"},
		{"http status", fmt.Errorf("request failed with status 503"), "upstream API returned HTTP 503"},
		{"auth scrubs token value", fmt.Errorf("invalid token abc123secret"), "upstream authentication failed"},
		{"generic", errors.New("something broke"), "upstream API error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeUpstreamErr(tt.err)
			if got != tt.want {
				t.Errorf("sanitizeUpstreamErr(%v) = %q, want %q", tt.err, got, tt.want)
			}
			if strings.Contains(got, "abc123secret") {
				t.Errorf("sanitized message leaked sensitive data: %q", got)
			}
		})
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestSanitizeUpstreamErrNetworkTimeout(t *testing.T) {
	err := &net.OpError{Op: "dial", Err: timeoutErr{}}
	if got := sanitizeUpstreamErr(err); got != "upstream connection failed" {
		t.Errorf("sanitizeUpstreamErr(timeout) = %q, want %q", got, "upstream connection failed")
	}
}

func TestContainsAny(t *testing.T) {
	if !containsAny("Has TOKEN here", []string{"token"}) {
		t.Error("expected case-insensitive substring match")
	}
	if containsAny("clean message", []string{"token", "auth"}) {
		t.Error("expected no match for clean message")
	}
}
