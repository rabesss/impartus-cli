package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestExtractHeaders(t *testing.T) {
	content := strings.Join([]string{
		"# Overview",
		"## Install",
		"```markdown",
		"# Not a heading",
		"```",
		"~~~",
		"## Also not a heading",
		"~~~",
		"### Details",
		"#### Not included",
		"plain text",
	}, "\n")

	want := []string{"# Overview", "## Install", "### Details"}
	if got := extractHeaders(content); !reflect.DeepEqual(got, want) {
		t.Fatalf("extractHeaders() = %#v, want %#v", got, want)
	}
}

func TestGenerateAnchor(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "words", text: "Getting Started", want: "getting-started"},
		{name: "punctuation", text: "What's New? (v2)", want: "whats-new-v2"},
		{name: "unicode", text: "Résumé 你好", want: "résumé-你好"},
		{name: "hyphens", text: "API -- Reference", want: "api----reference"},
		{name: "empty", text: "?!", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := generateAnchor(tt.text); got != tt.want {
				t.Fatalf("generateAnchor(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestGenerateTOC(t *testing.T) {
	if got := generateTOC(nil); got != "" {
		t.Fatalf("generateTOC(nil) = %q, want empty", got)
	}

	want := "**Table of Contents**  *generated automatically*\n\n" +
		"<!---toc start-->\n\n" +
		"* [Overview](#overview)\n" +
		"  * [Install Guide](#install-guide)\n" +
		"    * [Options](#options)\n" +
		"\n<!---toc end-->\n"
	got := generateTOC([]string{"# Overview", "## Install Guide", "### Options"})
	if got != want {
		t.Fatalf("generateTOC() =\n%s\nwant:\n%s", got, want)
	}
}

func TestProcessFile(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		err := processFile(filepath.Join(t.TempDir(), "missing.md"))
		if err == nil || !strings.Contains(err.Error(), "reading file") {
			t.Fatalf("processFile() error = %v, want reading error", err)
		}
	})

	t.Run("no marker leaves file unchanged", func(t *testing.T) {
		path := writeTOCTestFile(t, "# Existing\n")
		if err := processFile(path); err != nil {
			t.Fatalf("processFile() unexpected error: %v", err)
		}
		if got := readTOCTestFile(t, path); got != "# Existing\n" {
			t.Fatalf("processFile() content = %q, want unchanged", got)
		}
	})

	t.Run("missing end marker leaves file unchanged", func(t *testing.T) {
		content := "prefix\n" + startMarker + "\nold toc\n"
		path := writeTOCTestFile(t, content)
		err := processFile(path)
		if err == nil || !strings.Contains(err.Error(), "no end marker") {
			t.Fatalf("processFile() error = %v, want missing-end error", err)
		}
		if got := readTOCTestFile(t, path); got != content {
			t.Fatalf("processFile() changed malformed file: %q", got)
		}
	})

	t.Run("replaces generated region from document headings", func(t *testing.T) {
		content := "Intro\n\n" + startMarker + "\nSTALE ENTRY\n" + endMarker + "\n\n" +
			"# Overview\n\n```markdown\n## Fenced\n```\n\n## Install Guide\n"
		path := writeTOCTestFile(t, content)
		if err := processFile(path); err != nil {
			t.Fatalf("processFile() unexpected error: %v", err)
		}

		got := readTOCTestFile(t, path)
		if strings.Contains(got, "STALE ENTRY") || strings.Contains(got, "Fenced](") {
			t.Fatalf("processFile() retained stale or fenced heading:\n%s", got)
		}
		for _, want := range []string{
			"Intro\n\n" + startMarker,
			"* [Overview](#overview)",
			"  * [Install Guide](#install-guide)",
			endMarker + "\n\n# Overview",
		} {
			if !strings.Contains(got, want) {
				t.Fatalf("processFile() output missing %q:\n%s", want, got)
			}
		}
	})
}

func writeTOCTestFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "doc.md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	return path
}

func readTOCTestFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read test file: %v", err)
	}
	return string(content)
}
