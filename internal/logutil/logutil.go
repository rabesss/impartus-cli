// Package logutil provides logging utilities with sensitive data sanitization.
// This package implements log scrubbing to prevent accidental leakage of
// passwords, tokens, and other sensitive information in logs.
package logutil

import (
	"fmt"
	"regexp"
	"strings"
)

// Sensitive patterns to redact from logs
var sensitivePatterns = []struct {
	pattern     *regexp.Regexp
	replacement string
}{
	// Password patterns
	{regexp.MustCompile(`(?i)(password|passwd|pwd)\s*[:=]\s*\S+`), "$1=***REDACTED***"},
	{regexp.MustCompile(`(?i)"password"\s*:\s*"[^"]*"`), `"password":"***REDACTED***"`},

	// Token patterns
	{regexp.MustCompile(`(?i)(token|api_key|apikey|secret)\s*[:=]\s*\S+`), "$1=***REDACTED***"},
	{regexp.MustCompile(`(?i)bearer\s+[a-zA-Z0-9\-._~+/]+=*`), "Bearer ***REDACTED***"},
	{regexp.MustCompile(`(?i)(authorization)\s*:\s*\S+`), "$1: ***REDACTED***"},

	// Email patterns (partial redaction)
	{regexp.MustCompile(`([a-zA-Z0-9_.+-]+)@([a-zA-Z0-9-]+\.[a-zA-Z0-9-.]+)`), "***@$2"},

	// URL with credentials
	{regexp.MustCompile(`(?i)(https?://)([^:@]+):([^@]+)@`), "$1***:***@"},

	// JSON field patterns
	{regexp.MustCompile(`(?i)"(token|password|passwd|pwd|secret|api_key|apikey)"\s*:\s*"[^"]*"`), `"$1":"***REDACTED***"`},
}

// RedactSensitive redacts sensitive information from log messages.
// It replaces passwords, tokens, and other sensitive data with ***REDACTED***.
func RedactSensitive(message string) string {
	result := message
	for _, sp := range sensitivePatterns {
		result = sp.pattern.ReplaceAllString(result, sp.replacement)
	}
	return result
}

// RedactSensitivef formats and redacts sensitive information from log messages.
// It behaves like fmt.Sprintf but applies redaction to the result.
func RedactSensitivef(format string, args ...interface{}) string {
	message := fmt.Sprintf(format, args...)
	return RedactSensitive(message)
}

// SanitizeMap redacts sensitive keys in a map and returns a safe copy.
// Useful for logging configuration or request data.
func SanitizeMap(data map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	sensitiveKeys := map[string]bool{
		"password":      true,
		"passwd":        true,
		"pwd":           true,
		"token":         true,
		"tok":           true,
		"secret":        true,
		"api_key":       true,
		"apikey":        true,
		"authorization": true,
		"credentials":   true,
	}

	for key, value := range data {
		lowerKey := strings.ToLower(key)
		if sensitiveKeys[lowerKey] {
			result[key] = "***REDACTED***"
		} else {
			result[key] = value
		}
	}
	return result
}

// SanitizeString redacts common sensitive patterns from a string.
// This is an alias for RedactSensitive for clarity.
func SanitizeString(s string) string {
	return RedactSensitive(s)
}
