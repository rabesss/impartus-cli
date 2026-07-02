package cli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestCoursesRejectsPositionalArguments(t *testing.T) {
	_, err := getCourses([]string{"extra"})
	if err == nil {
		t.Fatal("expected positional argument rejection")
	}
	if !strings.Contains(err.Error(), "does not accept positional arguments") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVersionRejectsPositionalArgumentsJSON(t *testing.T) {
	restoreCLIState(t)
	os.Args = []string{"impartus", "version", "extra", "--json"}

	err := Execute("v1", "d1")
	if err == nil {
		t.Fatal("expected positional argument rejection error")
	}
	raw := err.Error()
	if !strings.Contains(raw, "version does not accept positional arguments") {
		t.Fatalf("expected error message about positional arguments, got: %v", err)
	}
	// Verify it's a proper JSON envelope
	var envelope map[string]json.RawMessage
	if unmarshalErr := json.Unmarshal([]byte(raw), &envelope); unmarshalErr != nil {
		t.Fatalf("expected JSON envelope, got raw text: %v", err)
	}
}

func TestServeRejectsPositionalArgumentsJSON(t *testing.T) {
	restoreCLIState(t)
	os.Args = []string{"impartus", "serve", "extra", "--json"}

	err := Execute("v1", "d1")
	if err == nil {
		t.Fatal("expected positional argument rejection error")
	}
	raw := err.Error()
	if !strings.Contains(raw, "serve does not accept positional arguments") {
		t.Fatalf("expected error message about positional arguments, got: %v", err)
	}
	// Verify it's a proper JSON envelope
	var envelope map[string]json.RawMessage
	if unmarshalErr := json.Unmarshal([]byte(raw), &envelope); unmarshalErr != nil {
		t.Fatalf("expected JSON envelope, got raw text: %v", err)
	}
}

func TestGetLecturesRejectsPositionalArguments(t *testing.T) {
	_, err := getLectures([]string{"--subject", "1", "--session", "2", "extra"})
	if err == nil {
		t.Fatal("expected positional argument rejection")
	}
	if !strings.Contains(err.Error(), "does not accept positional arguments") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPlayRejectsPositionalArguments(t *testing.T) {
	_, err := parsePlayFlags([]string{"extra_arg"})
	if err == nil {
		t.Fatal("expected positional argument error")
	}
	if !strings.Contains(err.Error(), "does not accept positional arguments") {
		t.Fatalf("unexpected error message: %v", err)
	}
}
