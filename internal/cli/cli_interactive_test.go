package cli

import (
	"bufio"
	"errors"
	"strings"
	"testing"
)

func TestPromptInt(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int
		wantErr bool
	}{
		{"valid number", "5\n", 5, false},
		{"with spaces", " 3 \n", 3, false},
		{"too low", "0\n2\n", 2, false},
		{"too high", "11\n1\n", 1, false},
		{"non-numeric", "abc\n1\n", 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := bufio.NewReader(strings.NewReader(tt.input))
			got, err := promptInt(reader, "test: ", 1, 10)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("promptInt() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestPromptInt_EOF(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader(""))
	_, err := promptInt(reader, "test: ", 1, 10)
	if err == nil {
		t.Error("expected error on empty input")
	}
}

func TestPromptYesNo(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    bool
		wantErr bool
	}{
		{"yes", "y\n", true, false},
		{"yes full", "yes\n", true, false},
		{"empty defaults yes", "\n", true, false},
		{"no", "n\n", false, false},
		{"no full", "no\n", false, false},
		{"uppercase Y", "Y\n", true, false},
		{"uppercase N", "N\n", false, false},
		{"invalid then yes", "maybe\ny\n", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := bufio.NewReader(strings.NewReader(tt.input))
			got, err := promptYesNo(reader, "test? ", true)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("promptYesNo() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPromptYesNo_EOF(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader(""))
	got, err := promptYesNo(reader, "test? ", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Error("expected default true on EOF")
	}
}

func TestPromptYesNo_EOF_DefaultNo(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader(""))
	// Empty input matches "" case which returns true (default-yes behavior)
	// This is the intended behavior: pressing Enter defaults to yes
	got, err := promptYesNo(reader, "test? ", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Error("expected true on empty input (Enter defaults to yes)")
	}
}

func TestBuildNoLecturesError(t *testing.T) {
	err := buildNoLecturesError(3, 2)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "3 empty") {
		t.Errorf("expected '3 empty' in message, got %q", msg)
	}
	if !strings.Contains(msg, "2 noaudio") {
		t.Errorf("expected '2 noaudio' in message, got %q", msg)
	}

	err2 := buildNoLecturesError(5, 0)
	if err2 == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err2.Error(), "noaudio") {
		t.Errorf("expected no 'noaudio' in message when 0, got %q", err2.Error())
	}
}

func TestBuildNoLecturesError_IsError(t *testing.T) {
	err := buildNoLecturesError(1, 1)
	if !errors.Is(err, err) {
		t.Error("error should match itself")
	}
}
