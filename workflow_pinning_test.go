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
	pullfrogUses = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^\s*(?:-\s*)?uses\s*:\s*["']?pullfrog/pullfrog(?:/[^\s@"'#,}\]]+)?@([^\s"'#,}\]]+)`),
		regexp.MustCompile(`(?i)[{,]\s*uses\s*:\s*["']?pullfrog/pullfrog(?:/[^\s@"'#,}\]]+)?@([^\s"'#,}\]]+)`),
	}
	pullfrogValue     = regexp.MustCompile(`(?i)^\s*["']?pullfrog/pullfrog(?:/[^\s@"'#,}\]]+)?@([^\s"'#,}\]]+)`)
	blockScalarHeader = regexp.MustCompile(`^\s*(?:-\s*)?([A-Za-z_][A-Za-z0-9_-]*)\s*:\s*[>|][-+0-9]*\s*(?:#.*)?$`)
	immutableRef      = regexp.MustCompile(`(?i)^[0-9a-f]{40}$`)
)

type workflowPullfrogRef struct {
	line int
	ref  string
}

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
		for _, finding := range unpinnedPullfrogWorkflowRefs(string(workflow)) {
			t.Errorf("%s:%d: Pullfrog action ref %q is not an immutable commit SHA", workflowPath, finding.line, finding.ref)
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
		{name: "flow mapping pin", line: "- {uses: pullfrog/pullfrog@" + sha + "}"},
		{name: "subpath pin", line: "uses: pullfrog/pullfrog/subdir@" + sha},
		{name: "floating ref", line: "uses: pullfrog/pullfrog@v0", unpinned: true},
		{name: "quoted floating ref", line: `uses: "pullfrog/pullfrog@v0"`, unpinned: true},
		{name: "extra spacing floating ref", line: "uses:   pullfrog/pullfrog@v0", unpinned: true},
		{name: "mixed case floating ref", line: "uses: Pullfrog/Pullfrog@v0", unpinned: true},
		{name: "flow mapping floating ref", line: "- {uses: pullfrog/pullfrog@v0}", unpinned: true},
		{name: "flow list floating ref", line: "steps: [{uses: pullfrog/pullfrog@v0}]", unpinned: true},
		{name: "subpath floating ref", line: "uses: pullfrog/pullfrog/subdir@v0", unpinned: true},
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

func TestUnpinnedPullfrogWorkflowRefs(t *testing.T) {
	sha := "c4d0ca6f15d12382ddd20d2010bc596b405f42f0"
	tests := []struct {
		name     string
		workflow string
		unpinned bool
	}{
		{name: "folded floating ref", workflow: "steps:\n  - uses: >-\n      pullfrog/pullfrog@v0\n", unpinned: true},
		{name: "folded pinned ref", workflow: "steps:\n  - uses: >-\n      pullfrog/pullfrog@" + sha + "\n"},
		{name: "run block text", workflow: "steps:\n  - run: |\n      uses: pullfrog/pullfrog@v0\n"},
		{name: "quoted run flow text", workflow: "steps:\n  - run: \"echo {uses: pullfrog/pullfrog@v0}\"\n"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := unpinnedPullfrogWorkflowRefs(test.workflow)
			if (len(got) != 0) != test.unpinned {
				t.Fatalf("unpinned refs = %v, want unpinned=%t", got, test.unpinned)
			}
		})
	}
}

func unpinnedPullfrogWorkflowRefs(workflow string) []workflowPullfrogRef {
	type blockScalar struct {
		indent  int
		line    int
		uses    bool
		content []string
	}

	lines := strings.Split(workflow, "\n")
	var findings []workflowPullfrogRef
	var block *blockScalar
	flushBlock := func() {
		if block == nil || !block.uses {
			block = nil
			return
		}
		if match := pullfrogValue.FindStringSubmatch(strings.Join(block.content, " ")); match != nil && !immutableRef.MatchString(match[1]) {
			findings = append(findings, workflowPullfrogRef{line: block.line, ref: match[1]})
		}
		block = nil
	}

	for lineIndex := 0; lineIndex < len(lines); {
		line := lines[lineIndex]
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))

		if block != nil {
			if trimmed == "" || indent > block.indent {
				if block.uses {
					block.content = append(block.content, trimmed)
				}
				lineIndex++
				continue
			}
			flushBlock()
			continue
		}

		if match := blockScalarHeader.FindStringSubmatch(line); match != nil {
			block = &blockScalar{indent: indent, line: lineIndex + 1, uses: match[1] == "uses"}
			lineIndex++
			continue
		}

		for _, ref := range unpinnedPullfrogRefs(line) {
			findings = append(findings, workflowPullfrogRef{line: lineIndex + 1, ref: ref})
		}
		lineIndex++
	}
	flushBlock()
	return findings
}

func unpinnedPullfrogRefs(line string) []string {
	if strings.HasPrefix(strings.TrimSpace(line), "#") {
		return nil
	}

	var unpinned []string
	for _, pattern := range pullfrogUses {
		for _, match := range pattern.FindAllStringSubmatchIndex(line, -1) {
			if !yamlCodeAt(line, match[0]) {
				continue
			}
			ref := line[match[2]:match[3]]
			if !immutableRef.MatchString(ref) {
				unpinned = append(unpinned, ref)
			}
		}
	}
	return unpinned
}

func yamlCodeAt(line string, index int) bool {
	var singleQuoted, doubleQuoted bool
	for position := 0; position < index; position++ {
		switch line[position] {
		case '#':
			if !singleQuoted && !doubleQuoted {
				return false
			}
		case '\'':
			if !doubleQuoted {
				if singleQuoted && position+1 < index && line[position+1] == '\'' {
					position++
					continue
				}
				singleQuoted = !singleQuoted
			}
		case '"':
			if !singleQuoted && (position == 0 || line[position-1] != '\\') {
				doubleQuoted = !doubleQuoted
			}
		}
	}
	return !singleQuoted && !doubleQuoted
}
