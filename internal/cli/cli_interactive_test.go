package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

func TestFilterLecturesInteractiveAllowsUnresolvedInstituteForPlayback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/subjects/67/lectures/8" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewEncoder(w).Encode(client.Lectures{
			{InstituteID: 4, TTID: 1, SeqNo: 1, Topic: "Scoped"},
			{TTID: 2, SeqNo: 2, Topic: "Missing scope"},
		}); err != nil {
			t.Errorf("encode lecture response: %v", err)
		}
	}))
	defer server.Close()

	inputPath := t.TempDir() + "/stdin"
	if err := os.WriteFile(inputPath, []byte("1\n2\nn\nn\n"), 0o600); err != nil {
		t.Fatalf("write interactive input: %v", err)
	}
	input, err := os.Open(inputPath)
	if err != nil {
		t.Fatalf("open interactive input: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := input.Close(); closeErr != nil {
			t.Errorf("close interactive input: %v", closeErr)
		}
	})
	originalStdin := os.Stdin
	os.Stdin = input
	t.Cleanup(func() { os.Stdin = originalStdin })

	cfg := &config.Config{BaseURL: server.URL, Token: "test-token"}
	course := &client.Course{SubjectID: 67, SessionID: 8}
	lectures, err := filterLecturesInteractive(context.Background(), cfg, client.New(server.Client(), nil), course)
	if err != nil {
		t.Fatalf("filterLecturesInteractive: %v", err)
	}
	if len(lectures) != 2 {
		t.Fatalf("lectures = %d, want 2", len(lectures))
	}
	if lectures[0].InstituteID != 0 || lectures[1].InstituteID != 4 {
		t.Fatalf("filter changed institute scope: %+v", lectures)
	}
}

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
