package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	pullfrogUse  = regexp.MustCompile(`(?i)^\s*(?:-\s*)?uses\s*:\s*["']?pullfrog/pullfrog@([^\s"'#]+)`)
	immutableRef = regexp.MustCompile(`(?i)^[0-9a-f]{40}$`)
)

func TestPullfrogActionUsesImmutablePin(t *testing.T) {
	workflowPaths, err := workflowYAMLPaths(".github/workflows/*")
	if err != nil {
		t.Fatalf("list workflow files: %v", err)
	}

	for _, workflowPath := range workflowPaths {
		workflow, readErr := os.ReadFile(workflowPath)
		if readErr != nil {
			t.Fatalf("read workflow %s: %v", workflowPath, readErr)
		}
		for lineIndex, line := range strings.Split(string(workflow), "\n") {
			for _, ref := range unpinnedPullfrogRefs(line) {
				t.Errorf("%s:%d: Pullfrog action ref %q is not an immutable commit SHA: %q", workflowPath, lineIndex+1, ref, strings.TrimSpace(line))
			}
		}
	}
}

func workflowYAMLPaths(pattern string) ([]string, error) {
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	workflowPaths := make([]string, 0, len(paths))
	for _, path := range paths {
		extension := filepath.Ext(path)
		if extension == ".yml" || extension == ".yaml" {
			workflowPaths = append(workflowPaths, path)
		}
	}
	if len(workflowPaths) == 0 {
		return nil, fmt.Errorf("no YAML workflows match %q", pattern)
	}
	return workflowPaths, nil
}

func TestUnpinnedPullfrogRefs(t *testing.T) {
	sha := "c4d0ca6f15d12382ddd20d2010bc596b405f42f0"
	tests := []struct {
		name     string
		line     string
		unpinned bool
	}{
		{name: "canonical pin", line: "uses: pullfrog/pullfrog@" + sha},
		{name: "quoted pin", line: `uses: "pullfrog/pullfrog@` + sha + `"`},
		{name: "dash form pin", line: "- uses: pullfrog/pullfrog@" + sha + " # v0"},
		{name: "uppercase pin", line: "uses: pullfrog/pullfrog@" + strings.ToUpper(sha)},
		{name: "floating ref", line: "uses: pullfrog/pullfrog@v0", unpinned: true},
		{name: "quoted floating ref", line: `uses: "pullfrog/pullfrog@v0"`, unpinned: true},
		{name: "extra spacing floating ref", line: "uses:   pullfrog/pullfrog@v0", unpinned: true},
		{name: "mixed case floating ref", line: "uses: Pullfrog/Pullfrog@v0", unpinned: true},
		{name: "comment only", line: "# uses: pullfrog/pullfrog@v0"},
		{name: "run command text", line: "run: echo pullfrog/pullfrog@v0"},
		{name: "container image digest", line: "image: pullfrog/pullfrog@sha256:deadbeef"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := unpinnedPullfrogRefs(test.line)
			if (len(got) != 0) != test.unpinned {
				t.Fatalf("unpinned refs = %v, want unpinned=%t", got, test.unpinned)
			}
		})
	}
}

func TestWorkflowYAMLPathsRejectsEmptyWorkflowSet(t *testing.T) {
	workflowPaths, err := workflowYAMLPaths(filepath.Join(t.TempDir(), "*"))
	if err == nil {
		t.Fatalf("workflowYAMLPaths() = %v, want an error for an empty workflow set", workflowPaths)
	}
}

func unpinnedPullfrogRefs(line string) []string {
	if strings.HasPrefix(strings.TrimSpace(line), "#") {
		return nil
	}

	var unpinned []string
	for _, match := range pullfrogUse.FindAllStringSubmatch(line, -1) {
		if !immutableRef.MatchString(match[1]) {
			unpinned = append(unpinned, match[1])
		}
	}
	return unpinned
}
