package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestPullfrogActionUsesImmutablePin(t *testing.T) {
	workflow, err := os.ReadFile(".github/workflows/pullfrog.yml")
	if err != nil {
		t.Fatalf("read pullfrog workflow: %v", err)
	}

	pinnedPullfrog := regexp.MustCompile(`^\s*uses: pullfrog/pullfrog@[0-9a-f]{40}(?:\s+#.*)?$`)
	for _, line := range strings.Split(string(workflow), "\n") {
		if !strings.Contains(line, "uses: pullfrog/pullfrog@") {
			continue
		}
		if !pinnedPullfrog.MatchString(line) {
			t.Errorf("pullfrog action is not pinned to an immutable commit SHA: %q", strings.TrimSpace(line))
		}
	}
}
