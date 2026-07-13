package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
)

var httpStatusRe = regexp.MustCompile(`status (\d{3})`)

// sanitizeUpstreamErr returns a generic sanitized message for upstream errors
// that may contain sensitive data (e.g., auth tokens in upstream API responses).
func sanitizeUpstreamErr(err error) string {
	if err == nil {
		return ""
	}
	// Context cancellation/timeout
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "job was canceled or timed out"
	}
	// DNS errors
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "upstream connection failed"
	}
	// Network timeout errors
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "upstream connection failed"
	}
	// HTTP status code errors — extract status code from formatted error messages
	errStr := err.Error()
	if match := httpStatusRe.FindStringSubmatch(errStr); len(match) > 1 {
		return fmt.Sprintf("upstream API returned HTTP %s", match[1])
	}
	// Auth errors
	if containsAny(errStr, []string{"login", "authenticate", "token", "unauthorized", "forbidden", "auth"}) {
		return "upstream authentication failed"
	}
	return "upstream API error"
}

func containsAny(s string, substrs []string) bool {
	for _, sub := range substrs {
		if strings.Contains(strings.ToLower(s), sub) {
			return true
		}
	}
	return false
}
