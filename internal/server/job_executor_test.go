package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/downloader"
)

func TestSanitizeUpstreamErr(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		want          string
		secretMarkers []string
	}{
		{"nil", nil, "", nil},
		{"canceled", context.Canceled, "job was canceled or timed out", nil},
		{"no media outputs", downloader.ErrNoMediaOutputs, "no media outputs available for selected lectures", nil},
		{"deadline", context.DeadlineExceeded, "job was canceled or timed out", nil},
		{"dns", &net.DNSError{Err: "no such host"}, "upstream connection failed", nil},
		{"http status", fmt.Errorf("request failed with status 503"), "upstream API returned HTTP 503", nil},
		{"untyped http 401 remains status", fmt.Errorf("request failed with status 401"), "upstream API returned HTTP 401", nil},
		{
			"typed authentication",
			&client.AuthenticationError{Operation: "login", StatusCode: 401},
			"upstream authentication failed",
			nil,
		},
		{
			"wrapped typed authentication",
			fmt.Errorf("request failed with response-marker-secret: %w", &client.AuthenticationError{Operation: "subjects", StatusCode: 401}),
			"upstream authentication failed",
			[]string{"response-marker-secret"},
		},
		{
			"joined typed authentication",
			errors.Join(errors.New("joined-body-secret"), &client.AuthenticationError{Operation: "playlist", StatusCode: 401}),
			"upstream authentication failed",
			[]string{"joined-body-secret"},
		},
		{"auth scrubs token value", fmt.Errorf("invalid token abc123secret"), "upstream authentication failed", []string{"abc123secret"}},
		{"generic", errors.New("something broke"), "upstream API error", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeUpstreamErr(tt.err)
			if got != tt.want {
				t.Errorf("sanitizeUpstreamErr(%v) = %q, want %q", tt.err, got, tt.want)
			}
			for _, marker := range tt.secretMarkers {
				if strings.Contains(got, marker) {
					t.Errorf("sanitized message leaked sensitive data %q: %q", marker, got)
				}
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
