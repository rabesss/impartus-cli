package logutil

import (
	"testing"
)

func TestRedactSensitive_Auth(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains string
		excludes string
	}{
		{
			name:     "auth assignment",
			input:    "pwd=testval",
			contains: "***REDACTED***",
			excludes: "testval",
		},
		{
			name:     "JSON auth field",
			input:    `{"username":"user","pwd":"testval"}`,
			contains: `"pwd":"***REDACTED***"`,
			excludes: `"pwd":"testval"`,
		},
		{
			name:     "Bearer auth",
			input:    "Authorization: Bearer testtok",
			contains: "***REDACTED***",
			excludes: "testtok",
		},
		{
			name:     "URL with auth data",
			input:    "https://redact:me@host.test/api",
			contains: "***:***@",
			excludes: "redact:me",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RedactSensitive(tt.input)
			if tt.contains != "" && !contains(result, tt.contains) {
				t.Errorf("expected result to contain %q, got %q", tt.contains, result)
			}
			if tt.excludes != "" && contains(result, tt.excludes) {
				t.Errorf("expected result to NOT contain %q, got %q", tt.excludes, result)
			}
		})
	}
}

func TestRedactSensitive_Email(t *testing.T) {
	input := "user@example.com logged in"
	result := RedactSensitive(input)
	if contains(result, "user@") {
		t.Errorf("expected email to be redacted, got %q", result)
	}
	if !contains(result, "@example.com") {
		t.Errorf("expected domain to be preserved, got %q", result)
	}
}

func TestRedactSensitivef(t *testing.T) {
	result := RedactSensitivef("User login: pwd=%s", "testval")
	if contains(result, "testval") {
		t.Errorf("expected value to be redacted, got %q", result)
	}
	if !contains(result, "***REDACTED***") {
		t.Errorf("expected redaction marker, got %q", result)
	}
}

func TestSanitizeMap(t *testing.T) {
	input := map[string]any{
		"username": "testuser",
		"pwd":      "testval",
		"tok":      "testtok",
		"port":     8080,
	}

	result := SanitizeMap(input)

	if result["username"] != "testuser" {
		t.Errorf("expected username to be preserved, got %v", result["username"])
	}
	if result["pwd"] != "***REDACTED***" {
		t.Errorf("expected pwd to be redacted, got %v", result["pwd"])
	}
	if result["tok"] != "***REDACTED***" {
		t.Errorf("expected tok to be redacted, got %v", result["tok"])
	}
	if result["port"] != 8080 {
		t.Errorf("expected port to be preserved, got %v", result["port"])
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
