package main

import (
	"os"
	"regexp"
	"testing"
)

func TestPullfrogActionUsesImmutablePin(t *testing.T) {
	workflow, err := os.ReadFile(".github/workflows/pullfrog.yml")
	if err != nil {
		t.Fatalf("read pullfrog workflow: %v", err)
	}

	pinnedPullfrog := regexp.MustCompile(`(?m)^\s*uses: pullfrog/pullfrog@[0-9a-f]{40}(?:\s+#.*)?$`)
	if !pinnedPullfrog.Match(workflow) {
		t.Fatal("pullfrog action must be pinned to an immutable commit SHA")
	}
}
