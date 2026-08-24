package main

import (
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type workflowPullfrogRef struct {
	line int
	ref  string
}

func TestPullfrogActionUsesImmutablePin(t *testing.T) {
	workflowPaths, err := pullfrogActionYAMLPaths(".github")
	if err != nil {
		t.Fatalf("list workflow and composite action files: %v", err)
	}

	for _, workflowPath := range workflowPaths {
		workflow, readErr := os.ReadFile(workflowPath)
		if readErr != nil {
			t.Fatalf("read workflow %s: %v", workflowPath, readErr)
		}
		findings, scanErr := scanUnpinnedPullfrogWorkflowRefs(string(workflow))
		if scanErr != nil {
			t.Fatalf("parse action YAML %s: %v", workflowPath, scanErr)
		}
		for _, finding := range findings {
			t.Errorf("%s:%d: Pullfrog action ref %q is not an immutable commit SHA", workflowPath, finding.line, finding.ref)
		}
	}
}

func TestPullfrogActionYAMLPathsIncludesCompositeActions(t *testing.T) {
	githubDir := filepath.Join(t.TempDir(), ".github")
	workflowPath := filepath.Join(githubDir, "workflows", "ci.yml")
	compositePath := filepath.Join(githubDir, "actions", "nested", "action.yaml")
	ignoredPath := filepath.Join(githubDir, "actions", "nested", "metadata.yml")
	for _, path := range []string{workflowPath, compositePath, ignoredPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create fixture directory: %v", err)
		}
		if err := os.WriteFile(path, []byte("uses: pullfrog/pullfrog@v0\n"), 0o600); err != nil {
			t.Fatalf("write fixture %s: %v", path, err)
		}
	}

	paths, err := pullfrogActionYAMLPaths(githubDir)
	if err != nil {
		t.Fatalf("pullfrogActionYAMLPaths() error: %v", err)
	}
	want := map[string]bool{workflowPath: true, compositePath: true}
	for _, path := range paths {
		delete(want, path)
		if path == ignoredPath {
			t.Fatalf("pullfrogActionYAMLPaths() included non-manifest %s", path)
		}
	}
	if len(want) != 0 {
		t.Fatalf("pullfrogActionYAMLPaths() missed paths: %v", want)
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

func pullfrogActionYAMLPaths(githubDir string) ([]string, error) {
	paths, err := workflowYAMLPaths(filepath.Join(githubDir, "workflows", "*"))
	if err != nil {
		return nil, err
	}

	actionsDir := filepath.Join(githubDir, "actions")
	err = filepath.WalkDir(actionsDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Name() == "action.yml" || entry.Name() == "action.yaml" {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return paths, nil
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
		{name: "explicit flow key multiline floating ref", workflow: "steps:\n  - {? uses:\n      pullfrog/pullfrog@v0}\n", unpinned: true},
		{name: "explicit flow key multiline pinned ref", workflow: "steps:\n  - {? uses:\n      pullfrog/pullfrog@" + sha + "}\n"},
		{name: "explicit flow key split before value floating ref", workflow: "steps: [{? uses\n  : pullfrog/pullfrog@v0}]\n", unpinned: true},
		{name: "explicit flow key split before value pinned ref", workflow: "steps: [{? uses\n  : pullfrog/pullfrog@" + sha + "}]\n"},
		{name: "escaped quoted key floating ref", workflow: "steps:\n  - \"\\x75ses\": pullfrog/pullfrog@v0\n", unpinned: true},
		{name: "escaped quoted key pinned ref", workflow: "steps:\n  - \"\\x75ses\": pullfrog/pullfrog@" + sha + "\n"},
		{name: "escaped quoted key folded floating ref", workflow: "steps:\n  - \"\\x75ses\": >-\n      pullfrog/pullfrog@v0\n", unpinned: true},
		{name: "escaped quoted key folded pinned ref", workflow: "steps:\n  - \"\\x75ses\": >-\n      pullfrog/pullfrog@" + sha + "\n"},
		{name: "escaped quoted key plain multiline floating ref", workflow: "steps:\n  - \"\\x75ses\":\n      pullfrog/pullfrog@v0\n", unpinned: true},
		{name: "escaped quoted key plain multiline pinned ref", workflow: "steps:\n  - \"\\x75ses\":\n      pullfrog/pullfrog@" + sha + "\n"},
		{name: "escaped quoted action value floating ref", workflow: "steps:\n  - uses: \"pullfrog/\\x70ullfrog@v0\"\n", unpinned: true},
		{name: "escaped quoted action value pinned ref", workflow: "steps:\n  - uses: \"pullfrog/\\x70ullfrog@" + sha + "\"\n"},
		{name: "anchored alias floating ref", workflow: "steps:\n  - uses: &pullfrog pullfrog/pullfrog@v0\n  - uses: *pullfrog\n", unpinned: true},
		{name: "anchored alias pinned ref", workflow: "steps:\n  - uses: &pullfrog pullfrog/pullfrog@" + sha + "\n  - uses: *pullfrog\n"},
		{name: "numeric anchored alias floating ref", workflow: "env:\n  ACTION_REF: &0 pullfrog/pullfrog@v0\nsteps:\n  - uses: *0\n", unpinned: true},
		{name: "numeric anchored alias pinned ref", workflow: "env:\n  ACTION_REF: &0 pullfrog/pullfrog@" + sha + "\nsteps:\n  - uses: *0\n"},
		{name: "anchored direct floating ref", workflow: "steps:\n  - uses: &pullfrog pullfrog/pullfrog@v0\n  - name: *pullfrog\n    run: echo ok\n", unpinned: true},
		{name: "anchored direct pinned ref", workflow: "steps:\n  - uses: &pullfrog pullfrog/pullfrog@" + sha + "\n  - name: *pullfrog\n    run: echo ok\n"},
		{name: "anchored folded floating ref", workflow: "steps:\n  - uses: &pullfrog >-\n      pullfrog/pullfrog@v0\n  - name: *pullfrog\n    run: echo ok\n", unpinned: true},
		{name: "anchored folded pinned ref", workflow: "steps:\n  - uses: &pullfrog >-\n      pullfrog/pullfrog@" + sha + "\n  - name: *pullfrog\n    run: echo ok\n"},
		{name: "anchored plain multiline floating ref", workflow: "steps:\n  - uses:\n      &pullfrog pullfrog/pullfrog@v0\n  - name: *pullfrog\n    run: echo ok\n", unpinned: true},
		{name: "anchored plain multiline pinned ref", workflow: "steps:\n  - uses:\n      &pullfrog pullfrog/pullfrog@" + sha + "\n  - name: *pullfrog\n    run: echo ok\n"},
		{name: "aliased plain multiline floating ref", workflow: "env:\n  ACTION_REF: &pullfrog pullfrog/pullfrog@v0\nsteps:\n  - uses:\n      *pullfrog\n", unpinned: true},
		{name: "aliased plain multiline pinned ref", workflow: "env:\n  ACTION_REF: &pullfrog pullfrog/pullfrog@" + sha + "\nsteps:\n  - uses:\n      *pullfrog\n"},
		{name: "aliased uses key floating ref", workflow: "name: &uses-key uses\nsteps:\n  - *uses-key: pullfrog/pullfrog@v0\n", unpinned: true},
		{name: "aliased uses key pinned ref", workflow: "name: &uses-key uses\nsteps:\n  - *uses-key: pullfrog/pullfrog@" + sha + "\n"},
		{name: "compact flow mapping floating ref", workflow: "steps: [uses: pullfrog/pullfrog@v0]\n", unpinned: true},
		{name: "compact flow mapping pinned ref", workflow: "steps: [uses: pullfrog/pullfrog@" + sha + "]\n"},
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
		{name: "ordinary quoted key folded floating ref", workflow: "steps:\n  - \"uses\": >-\n      pullfrog/pullfrog@v0\n", unpinned: true},
		{name: "ordinary quoted key folded pinned ref", workflow: "steps:\n  - \"uses\": >-\n      pullfrog/pullfrog@" + sha + "\n"},
		{name: "hash in flow plain scalar floating ref", workflow: "steps:\n  - {name: a#b, uses: pullfrog/pullfrog@v0}\n", unpinned: true},
		{name: "hash in flow plain scalar pinned ref", workflow: "steps:\n  - {name: a#b, uses: pullfrog/pullfrog@" + sha + "}\n"},
		{name: "apostrophe in flow plain scalar floating ref", workflow: "steps:\n  - {name: it's agent, uses: pullfrog/pullfrog@v0}\n", unpinned: true},
		{name: "apostrophe in flow plain scalar pinned ref", workflow: "steps:\n  - {name: it's agent, uses: pullfrog/pullfrog@" + sha + "}\n"},
		{name: "indicator-adjacent apostrophe floating ref", workflow: "steps:\n  - {name: it-'s, uses: pullfrog/pullfrog@v0}\n", unpinned: true},
		{name: "indicator-adjacent apostrophe pinned ref", workflow: "steps:\n  - {name: it-'s, uses: pullfrog/pullfrog@" + sha + "}\n"},
		{name: "even backslashes before flow quote floating ref", workflow: "steps:\n  - {name: \"C:\\\\dir\\\\\", uses: pullfrog/pullfrog@v0}\n", unpinned: true},
		{name: "even backslashes before flow quote pinned ref", workflow: "steps:\n  - {name: \"C:\\\\dir\\\\\", uses: pullfrog/pullfrog@" + sha + "}\n"},
		{name: "plain bracket does not create stale flow", workflow: "name: build [1\nsteps:\n  - uses:\n      pullfrog/pullfrog@" + sha + "\n    with:\n      args: x\n  - uses: pullfrog/pullfrog@v1\n", unpinned: true},
		{name: "plain bracket with pinned uses", workflow: "name: build [1\nsteps:\n  - uses:\n      pullfrog/pullfrog@" + sha + "\n    with:\n      args: x\n  - uses: pullfrog/pullfrog@" + sha + "\n"},
		{name: "run block text", workflow: "steps:\n  - run: |\n      uses: pullfrog/pullfrog@v0\n"},
		{name: "quoted run flow text", workflow: "steps:\n  - run: \"echo {uses: pullfrog/pullfrog@v0}\"\n"},
		{name: "plain run flow text without colon spacing", workflow: "steps:\n  - run: echo {uses:pullfrog/pullfrog@v0}\n"},
		{name: "multiline quoted run line-start comma text", workflow: "steps:\n  - run: \"echo\n      , uses: pullfrog/pullfrog@v0\"\n"},
		{name: "multiline quoted run direct uses text", workflow: "steps:\n  - run: \"echo\n      uses: pullfrog/pullfrog@v0\"\n"},
		{name: "recursive alias without uses", workflow: "value: &self [*self]\n"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := scanUnpinnedPullfrogWorkflowRefs(test.workflow)
			if err != nil {
				t.Fatalf("scanUnpinnedPullfrogWorkflowRefs() error: %v", err)
			}
			if (len(got) != 0) != test.unpinned {
				t.Fatalf("unpinned refs = %v, want unpinned=%t", got, test.unpinned)
			}
		})
	}

	t.Run("ordinary quoted key produces one diagnostic", func(t *testing.T) {
		got, err := scanUnpinnedPullfrogWorkflowRefs("steps:\n  - \"uses\": pullfrog/pullfrog@v0\n")
		if err != nil {
			t.Fatalf("scanUnpinnedPullfrogWorkflowRefs() error: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("unpinned refs = %v, want exactly one finding", got)
		}
	})
}

func scanUnpinnedPullfrogWorkflowRefs(workflow string) ([]workflowPullfrogRef, error) {
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(workflow), &document); err != nil {
		return nil, err
	}

	var findings []workflowPullfrogRef
	visitYAMLNode(&document, func(key, value *yaml.Node) {
		keyValue, ok := resolvedYAMLScalar(key)
		if !ok || keyValue != "uses" {
			return
		}
		actionValue, ok := resolvedYAMLScalar(value)
		if !ok {
			return
		}
		ref, ok := pullfrogActionRef(actionValue)
		if ok && !isImmutableCommitRef(ref) {
			findings = append(findings, workflowPullfrogRef{line: key.Line, ref: ref})
		}
	})
	return findings, nil
}

func visitYAMLNode(node *yaml.Node, visitMapping func(key, value *yaml.Node)) {
	seen := make(map[*yaml.Node]struct{})
	var walk func(*yaml.Node)
	walk = func(current *yaml.Node) {
		if current == nil {
			return
		}
		if _, duplicate := seen[current]; duplicate {
			return
		}
		seen[current] = struct{}{}

		switch current.Kind {
		case yaml.DocumentNode, yaml.SequenceNode:
			for _, child := range current.Content {
				walk(child)
			}
		case yaml.MappingNode:
			for index := 0; index+1 < len(current.Content); index += 2 {
				key := current.Content[index]
				value := current.Content[index+1]
				visitMapping(key, value)
				walk(value)
			}
		case yaml.AliasNode:
			walk(current.Alias)
		case yaml.ScalarNode:
			return
		}
	}
	walk(node)
}

func resolvedYAMLScalar(node *yaml.Node) (string, bool) {
	seen := make(map[*yaml.Node]struct{})
	for node != nil && node.Kind == yaml.AliasNode {
		if _, duplicate := seen[node]; duplicate {
			return "", false
		}
		seen[node] = struct{}{}
		node = node.Alias
	}
	if node == nil || node.Kind != yaml.ScalarNode {
		return "", false
	}
	return node.Value, true
}

func pullfrogActionRef(value string) (string, bool) {
	value = strings.TrimSpace(value)
	separator := strings.LastIndexByte(value, '@')
	if separator <= 0 || separator == len(value)-1 {
		return "", false
	}

	action := strings.ToLower(value[:separator])
	if action != "pullfrog/pullfrog" && !strings.HasPrefix(action, "pullfrog/pullfrog/") {
		return "", false
	}
	ref := value[separator+1:]
	if strings.ContainsAny(action, " \t\r\n") || strings.ContainsAny(ref, " \t\r\n") {
		return "", false
	}
	return ref, true
}

func isImmutableCommitRef(ref string) bool {
	if len(ref) != 40 {
		return false
	}
	_, err := hex.DecodeString(ref)
	return err == nil
}

func unpinnedPullfrogRefs(line string) []string {
	findings, err := scanUnpinnedPullfrogWorkflowRefs(line)
	if err != nil {
		return []string{"invalid YAML"}
	}
	refs := make([]string, 0, len(findings))
	for _, finding := range findings {
		refs = append(refs, finding.ref)
	}
	return refs
}
