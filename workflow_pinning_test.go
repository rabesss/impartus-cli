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
		regexp.MustCompile(`(?i)^\s*(?:-\s*)?["']?uses["']?\s*:\s*["']?pullfrog/pullfrog(?:/[^\s@"'#,}\]]+)?@([^\s"'#,}\]]+)`),
		regexp.MustCompile(`(?i)[{,]\s*(?:\?\s*)?["']?uses["']?\s*:\s*["']?pullfrog/pullfrog(?:/[^\s@"'#,}\]]+)?@([^\s"'#,}\]]+)`),
	}
	pullfrogValue     = regexp.MustCompile(`(?i)^\s*["']?pullfrog/pullfrog(?:/[^\s@"'#,}\]]+)?@([^\s"'#,}\]]+)`)
	blockScalarHeader = regexp.MustCompile(`^\s*(?:-\s*)?["']?([A-Za-z_][A-Za-z0-9_-]*)["']?\s*:\s*[>|][-+0-9]*\s*(?:#.*)?$`)
	plainUsesHeader   = regexp.MustCompile(`^\s*(?:-\s*)?(?:[{,]\s*)?["']?uses["']?\s*:\s*(?:#.*)?$`)
	flowUsesHeader    = regexp.MustCompile(`(?i)[{,]\s*["']?uses["']?\s*:\s*(?:#.*)?$`)
	explicitUsesKey   = regexp.MustCompile(`^\s*(?:-\s*)?\?\s*["']?uses["']?\s*(?:#.*)?$`)
	explicitValueLine = regexp.MustCompile(`^\s*:\s*(.*)$`)
	blockScalarValue  = regexp.MustCompile(`^[>|][-+0-9]*\s*(?:#.*)?$`)
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
		{name: "quoted key pin", line: `"uses": pullfrog/pullfrog@` + sha},
		{name: "quoted flow key pin", line: `- {"uses": pullfrog/pullfrog@` + sha + `}`},
		{name: "subpath pin", line: "uses: pullfrog/pullfrog/subdir@" + sha},
		{name: "floating ref", line: "uses: pullfrog/pullfrog@v0", unpinned: true},
		{name: "quoted floating ref", line: `uses: "pullfrog/pullfrog@v0"`, unpinned: true},
		{name: "extra spacing floating ref", line: "uses:   pullfrog/pullfrog@v0", unpinned: true},
		{name: "mixed case floating ref", line: "uses: Pullfrog/Pullfrog@v0", unpinned: true},
		{name: "flow mapping floating ref", line: "- {uses: pullfrog/pullfrog@v0}", unpinned: true},
		{name: "flow list floating ref", line: "steps: [{uses: pullfrog/pullfrog@v0}]", unpinned: true},
		{name: "quoted key floating ref", line: `"uses": pullfrog/pullfrog@v0`, unpinned: true},
		{name: "quoted flow key floating ref", line: `- {"uses": pullfrog/pullfrog@v0}`, unpinned: true},
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
		{name: "plain multiline floating ref", workflow: "steps:\n  - uses:\n      pullfrog/pullfrog@v0\n", unpinned: true},
		{name: "plain multiline pinned ref", workflow: "steps:\n  - uses:\n      pullfrog/pullfrog@" + sha + "\n"},
		{name: "quoted plain multiline floating ref", workflow: "steps:\n  - \"uses\":\n      pullfrog/pullfrog@v0\n", unpinned: true},
		{name: "quoted plain multiline pinned ref", workflow: "steps:\n  - \"uses\":\n      pullfrog/pullfrog@" + sha + "\n"},
		{name: "flow multiline floating ref", workflow: "steps:\n  - {uses:\n      pullfrog/pullfrog@v0, with: {}}\n", unpinned: true},
		{name: "flow multiline pinned ref", workflow: "steps:\n  - {uses:\n      pullfrog/pullfrog@" + sha + ", with: {}}\n"},
		{name: "flow multiline equal indent floating ref", workflow: "steps: [{name: n,\n  uses:\n  pullfrog/pullfrog@v0}]\n", unpinned: true},
		{name: "flow multiline equal indent pinned ref", workflow: "steps: [{name: n,\n  uses:\n  pullfrog/pullfrog@" + sha + "}]\n"},
		{name: "flow list midline header floating ref", workflow: "steps: [{uses:\n  pullfrog/pullfrog@v0}]\n", unpinned: true},
		{name: "flow list midline header pinned ref", workflow: "steps: [{uses:\n  pullfrog/pullfrog@" + sha + "}]\n"},
		{name: "flow sibling midline header floating ref", workflow: "steps:\n  - {name: agent, uses:\n      pullfrog/pullfrog@v0}\n", unpinned: true},
		{name: "flow sibling midline header pinned ref", workflow: "steps:\n  - {name: agent, uses:\n      pullfrog/pullfrog@" + sha + "}\n"},
		{name: "flow continuation comma floating ref", workflow: "steps: [{name: agent,\n  env: {FOO: bar}, uses: pullfrog/pullfrog@v0}]\n", unpinned: true},
		{name: "flow continuation comma pinned ref", workflow: "steps: [{name: agent,\n  env: {FOO: bar}, uses: pullfrog/pullfrog@" + sha + "}]\n"},
		{name: "flow continuation comma header floating ref", workflow: "steps: [{name: agent,\n  env: {FOO: bar}, uses:\n  pullfrog/pullfrog@v0}]\n", unpinned: true},
		{name: "flow continuation comma header pinned ref", workflow: "steps: [{name: agent,\n  env: {FOO: bar}, uses:\n  pullfrog/pullfrog@" + sha + "}]\n"},
		{name: "explicit flow key floating ref", workflow: "steps:\n  - {? uses: pullfrog/pullfrog@v0}\n", unpinned: true},
		{name: "explicit flow key pinned ref", workflow: "steps:\n  - {? uses: pullfrog/pullfrog@" + sha + "}\n"},
		{name: "explicit block key floating ref", workflow: "steps:\n  - ? uses\n    : pullfrog/pullfrog@v0\n", unpinned: true},
		{name: "explicit block key pinned ref", workflow: "steps:\n  - ? uses\n    : pullfrog/pullfrog@" + sha + "\n"},
		{name: "explicit block key plain multiline floating ref", workflow: "steps:\n  - ? uses\n    :\n      pullfrog/pullfrog@v0\n", unpinned: true},
		{name: "explicit block key plain multiline pinned ref", workflow: "steps:\n  - ? uses\n    :\n      pullfrog/pullfrog@" + sha + "\n"},
		{name: "explicit block key folded floating ref", workflow: "steps:\n  - ? uses\n    : >-\n      pullfrog/pullfrog@v0\n", unpinned: true},
		{name: "explicit block key folded pinned ref", workflow: "steps:\n  - ? uses\n    : >-\n      pullfrog/pullfrog@" + sha + "\n"},
		{name: "plain multiline comment floating ref", workflow: "steps:\n  - uses:\n      # TODO: pin\n      pullfrog/pullfrog@v0\n", unpinned: true},
		{name: "plain multiline comment pinned ref", workflow: "steps:\n  - uses:\n      # pinned below\n      pullfrog/pullfrog@" + sha + "\n"},
		{name: "block scalar sibling floating ref", workflow: "steps:\n  - name: >-\n      Run agent\n    uses: pullfrog/pullfrog@v0\n", unpinned: true},
		{name: "block scalar sibling pinned ref", workflow: "steps:\n  - name: >-\n      Run agent\n    uses: pullfrog/pullfrog@" + sha + "\n"},
		{name: "hash in flow plain scalar floating ref", workflow: "steps:\n  - {name: a#b, uses: pullfrog/pullfrog@v0}\n", unpinned: true},
		{name: "hash in flow plain scalar pinned ref", workflow: "steps:\n  - {name: a#b, uses: pullfrog/pullfrog@" + sha + "}\n"},
		{name: "apostrophe in flow plain scalar floating ref", workflow: "steps:\n  - {name: it's agent, uses: pullfrog/pullfrog@v0}\n", unpinned: true},
		{name: "apostrophe in flow plain scalar pinned ref", workflow: "steps:\n  - {name: it's agent, uses: pullfrog/pullfrog@" + sha + "}\n"},
		{name: "even backslashes before flow quote floating ref", workflow: "steps:\n  - {name: \"C:\\\\dir\\\\\", uses: pullfrog/pullfrog@v0}\n", unpinned: true},
		{name: "even backslashes before flow quote pinned ref", workflow: "steps:\n  - {name: \"C:\\\\dir\\\\\", uses: pullfrog/pullfrog@" + sha + "}\n"},
		{name: "plain bracket does not create stale flow", workflow: "name: build [1\nsteps:\n  - uses:\n      pullfrog/pullfrog@" + sha + "\n    with:\n      args: x\n  - uses: pullfrog/pullfrog@v1\n", unpinned: true},
		{name: "plain bracket with pinned uses", workflow: "name: build [1\nsteps:\n  - uses:\n      pullfrog/pullfrog@" + sha + "\n    with:\n      args: x\n  - uses: pullfrog/pullfrog@" + sha + "\n"},
		{name: "run block text", workflow: "steps:\n  - run: |\n      uses: pullfrog/pullfrog@v0\n"},
		{name: "quoted run flow text", workflow: "steps:\n  - run: \"echo {uses: pullfrog/pullfrog@v0}\"\n"},
		{name: "plain run flow text without colon spacing", workflow: "steps:\n  - run: echo {uses:pullfrog/pullfrog@v0}\n"},
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
		headerIndent  int
		contentIndent int
		line          int
		uses          bool
		flow          bool
		plain         bool
		explicit      bool
		content       []string
	}

	lines := strings.Split(workflow, "\n")
	var findings []workflowPullfrogRef
	var block *blockScalar
	flowDepth := 0
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
			if trimmed == "" {
				lineIndex++
				continue
			}
			if block.explicit {
				if strings.HasPrefix(trimmed, "#") {
					lineIndex++
					continue
				}
				match := explicitValueLine.FindStringSubmatch(line)
				if match == nil {
					flushBlock()
					continue
				}
				value := strings.TrimSpace(match[1])
				block.explicit = false
				switch {
				case blockScalarValue.MatchString(value):
					block.headerIndent = indent
					block.contentIndent = -1
					block.plain = false
				case value == "" || strings.HasPrefix(value, "#"):
					block.headerIndent = indent
					block.contentIndent = -1
					block.plain = true
				default:
					if valueMatch := pullfrogValue.FindStringSubmatch(value); valueMatch != nil && !immutableRef.MatchString(valueMatch[1]) {
						findings = append(findings, workflowPullfrogRef{line: block.line, ref: valueMatch[1]})
					}
					block = nil
				}
				lineIndex++
				continue
			}
			if block.plain && strings.HasPrefix(trimmed, "#") {
				lineIndex++
				continue
			}
			if block.flow {
				if flowDepth == 0 {
					flushBlock()
					continue
				}
				for _, ref := range unpinnedPullfrogRefsAtDepth(line, flowDepth) {
					findings = append(findings, workflowPullfrogRef{line: lineIndex + 1, ref: ref})
				}
				if block.uses {
					block.content = append(block.content, trimmed)
				}
				flowDepth = yamlFlowDepthAfter(line, flowDepth)
				lineIndex++
				if flowDepth == 0 {
					flushBlock()
				}
				continue
			}
			if block.contentIndent < 0 {
				if indent <= block.headerIndent {
					flushBlock()
					continue
				}
				block.contentIndent = indent
			} else if indent < block.contentIndent {
				flushBlock()
				continue
			}
			if block.uses {
				block.content = append(block.content, trimmed)
			}
			lineIndex++
			continue
		}

		if explicitUsesKey.MatchString(line) {
			block = &blockScalar{headerIndent: indent, contentIndent: -1, line: lineIndex + 1, uses: true, plain: true, explicit: true}
			lineIndex++
			continue
		}
		if match := blockScalarHeader.FindStringSubmatch(line); match != nil {
			block = &blockScalar{headerIndent: indent, contentIndent: -1, line: lineIndex + 1, uses: match[1] == "uses"}
			lineIndex++
			continue
		}
		if usesValueHeader(line, flowDepth) {
			nextFlowDepth := yamlFlowDepthAfter(line, flowDepth)
			block = &blockScalar{headerIndent: indent, contentIndent: -1, line: lineIndex + 1, uses: true, flow: nextFlowDepth > 0, plain: true}
			flowDepth = nextFlowDepth
			lineIndex++
			continue
		}

		for _, ref := range unpinnedPullfrogRefsAtDepth(line, flowDepth) {
			findings = append(findings, workflowPullfrogRef{line: lineIndex + 1, ref: ref})
		}
		flowDepth = yamlFlowDepthAfter(line, flowDepth)
		lineIndex++
	}
	flushBlock()
	return findings
}

func usesValueHeader(line string, flowDepth int) bool {
	if plainUsesHeader.MatchString(line) {
		return true
	}
	for _, match := range flowUsesHeader.FindAllStringIndex(line, -1) {
		if yamlCodeAt(line, match[0]) && yamlFlowSeparatorAt(line, match[0], flowDepth) {
			return true
		}
	}
	return false
}

func yamlFlowDepthAfter(line string, depth int) int {
	for index, character := range line {
		if !yamlCodeAt(line, index) {
			continue
		}
		switch character {
		case '[', '{':
			if depth > 0 || yamlFlowOpenerAt(line, index) {
				depth++
			}
		case ']', '}':
			if depth > 0 {
				depth--
			}
		}
	}
	return depth
}

func yamlFlowOpenerAt(line string, index int) bool {
	for position := index - 1; position >= 0; position-- {
		switch line[position] {
		case ' ', '\t':
			continue
		case ':', '-', '?', ',', '[', '{':
			return true
		default:
			return false
		}
	}
	return true
}

func unpinnedPullfrogRefs(line string) []string {
	return unpinnedPullfrogRefsAtDepth(line, 0)
}

func unpinnedPullfrogRefsAtDepth(line string, flowDepth int) []string {
	if strings.HasPrefix(strings.TrimSpace(line), "#") {
		return nil
	}

	var unpinned []string
	for _, pattern := range pullfrogUses {
		for _, match := range pattern.FindAllStringSubmatchIndex(line, -1) {
			if !yamlCodeAt(line, match[0]) || !yamlFlowSeparatorAt(line, match[0], flowDepth) {
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

func yamlFlowSeparatorAt(line string, position, flowDepth int) bool {
	if position >= len(line) {
		return false
	}
	switch line[position] {
	case '{':
		return yamlFlowOpenerAt(line, position)
	case ',':
		return flowDepth > 0 || strings.TrimSpace(line[:position]) == "" || yamlFlowDepthAfter(line[:position], 0) > 0
	default:
		return true
	}
}

func yamlCodeAt(line string, index int) bool {
	var singleQuoted, doubleQuoted bool
	for position := 0; position < index; position++ {
		switch line[position] {
		case '#':
			if !singleQuoted && !doubleQuoted && yamlCommentStart(line, position) {
				return false
			}
		case '\'':
			if !doubleQuoted {
				if singleQuoted {
					if position+1 < index && line[position+1] == '\'' {
						position++
						continue
					}
					singleQuoted = false
				} else if yamlQuotedScalarOpenerAt(line, position) {
					singleQuoted = true
				}
			}
		case '"':
			if !singleQuoted && !yamlDoubleQuoteEscaped(line, position) {
				doubleQuoted = !doubleQuoted
			}
		}
	}
	return !singleQuoted && !doubleQuoted
}

func yamlQuotedScalarOpenerAt(line string, position int) bool {
	for position--; position >= 0; position-- {
		switch line[position] {
		case ' ', '\t':
			continue
		case ':', '-', '?', ',', '[', '{':
			return true
		default:
			return false
		}
	}
	return true
}

func yamlCommentStart(line string, position int) bool {
	return position == 0 || line[position-1] == ' ' || line[position-1] == '\t'
}

func yamlDoubleQuoteEscaped(line string, position int) bool {
	backslashes := 0
	for position--; position >= 0 && line[position] == '\\'; position-- {
		backslashes++
	}
	return backslashes%2 == 1
}
