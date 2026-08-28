package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type workflowPullfrogRef struct {
	line int
	ref  string
}

func TestPullfrogActionTracksCurrentMajor(t *testing.T) {
	workflowPaths, err := pullfrogActionYAMLPaths(".github")
	if err != nil {
		t.Fatalf("list workflow and composite action files: %v", err)
	}

	for _, workflowPath := range workflowPaths {
		workflow, readErr := os.ReadFile(workflowPath)
		if readErr != nil {
			t.Fatalf("read workflow %s: %v", workflowPath, readErr)
		}
		findings, scanErr := scanUnsupportedPullfrogWorkflowRefs(string(workflow))
		if scanErr != nil {
			t.Fatalf("parse action YAML %s: %v", workflowPath, scanErr)
		}
		for _, finding := range findings {
			t.Errorf("%s:%d: Pullfrog action ref %q does not track the recommended v0 major", workflowPath, finding.line, finding.ref)
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

func TestPullfrogActionYAMLPathsFollowsLocalCompositeActions(t *testing.T) {
	repoRoot := t.TempDir()
	githubDir := filepath.Join(repoRoot, ".github")
	workflowPath := filepath.Join(githubDir, "workflows", "ci.yml")
	compositePath := filepath.Join(repoRoot, "tools", "pullfrog", "action.yml")
	rootCompositePath := filepath.Join(repoRoot, "action.yaml")
	fixtures := map[string]string{
		workflowPath:      "steps:\n  - uses: ./tools/pullfrog\n  - uses: ./\n",
		compositePath:     "runs:\n  using: composite\n  steps:\n    - uses: pullfrog/pullfrog@v0\n",
		rootCompositePath: "runs:\n  using: composite\n  steps:\n    - uses: pullfrog/pullfrog@v0\n",
	}
	for path, content := range fixtures {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create fixture directory: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write fixture %s: %v", path, err)
		}
	}

	paths, err := pullfrogActionYAMLPaths(githubDir)
	if err != nil {
		t.Fatalf("pullfrogActionYAMLPaths() error: %v", err)
	}
	if !slices.Contains(paths, compositePath) {
		t.Fatalf("pullfrogActionYAMLPaths() = %v, want referenced local manifest %s", paths, compositePath)
	}
	if !slices.Contains(paths, rootCompositePath) {
		t.Fatalf("pullfrogActionYAMLPaths() = %v, want referenced root manifest %s", paths, rootCompositePath)
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

	repoRoot := filepath.Dir(githubDir)
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		seen[path] = struct{}{}
	}
	for index := 0; index < len(paths); index++ {
		content, readErr := os.ReadFile(paths[index])
		if readErr != nil {
			return nil, readErr
		}
		references, parseErr := localActionReferences(content)
		if parseErr != nil {
			return nil, parseErr
		}
		for _, reference := range references {
			manifest, found, resolveErr := localActionManifestPath(repoRoot, reference)
			if resolveErr != nil {
				return nil, resolveErr
			}
			if !found {
				continue
			}
			if _, duplicate := seen[manifest]; duplicate {
				continue
			}
			seen[manifest] = struct{}{}
			paths = append(paths, manifest)
		}
	}
	return paths, nil
}

func localActionReferences(content []byte) ([]string, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(content, &document); err != nil {
		return nil, err
	}

	var references []string
	visitYAMLNode(&document, func(key, value *yaml.Node) {
		keyValue, keyOK := resolvedYAMLScalar(key)
		valueValue, valueOK := resolvedYAMLScalar(value)
		if keyOK && valueOK && keyValue == "uses" && (strings.HasPrefix(valueValue, "./") || strings.HasPrefix(valueValue, "$/")) {
			references = append(references, valueValue)
		}
	})
	return references, nil
}

func localActionManifestPath(repoRoot, reference string) (string, bool, error) {
	relative := strings.TrimPrefix(reference, "./")
	if strings.HasPrefix(reference, "$/") {
		relative = strings.TrimPrefix(reference, "$/")
	}
	relative = filepath.Clean(filepath.FromSlash(relative))
	if filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false, nil
	}

	actionDir := filepath.Join(repoRoot, relative)
	for _, name := range []string{"action.yml", "action.yaml"} {
		manifest := filepath.Join(actionDir, name)
		info, err := os.Stat(manifest)
		if err == nil && info.Mode().IsRegular() {
			return manifest, true, nil
		}
		if err != nil && !os.IsNotExist(err) {
			return "", false, err
		}
	}
	return "", false, nil
}

func TestWorkflowYAMLPathsRejectsEmptyWorkflowSet(t *testing.T) {
	workflowPaths, err := workflowYAMLPaths(filepath.Join(t.TempDir(), "*"))
	if err == nil {
		t.Fatalf("workflowYAMLPaths() = %v, want an error for an empty workflow set", workflowPaths)
	}
}

func TestUnsupportedPullfrogWorkflowRefs(t *testing.T) {
	tests := []struct {
		name        string
		workflow    string
		unsupported bool
	}{
		{name: "recommended major", workflow: "steps:\n  - uses: pullfrog/pullfrog@v0\n"},
		{name: "quoted recommended major", workflow: "steps:\n  - uses: \"pullfrog/pullfrog@v0\"\n"},
		{name: "subpath recommended major", workflow: "steps:\n  - uses: pullfrog/pullfrog/subdir@v0\n"},
		{name: "mixed case recommended major", workflow: "steps:\n  - uses: Pullfrog/Pullfrog@v0\n"},
		{name: "frozen commit", workflow: "steps:\n  - uses: pullfrog/pullfrog@c4d0ca6f15d12382ddd20d2010bc596b405f42f0\n", unsupported: true},
		{name: "wrong major", workflow: "steps:\n  - uses: pullfrog/pullfrog@v1\n", unsupported: true},
		{name: "branch", workflow: "steps:\n  - uses: pullfrog/pullfrog@main\n", unsupported: true},
		{name: "subpath branch", workflow: "steps:\n  - uses: pullfrog/pullfrog/subdir@main\n", unsupported: true},
		{name: "mixed case branch", workflow: "steps:\n  - uses: Pullfrog/Pullfrog@main\n", unsupported: true},
		{name: "folded recommended major", workflow: "steps:\n  - uses: >-\n      pullfrog/pullfrog@v0\n"},
		{name: "folded branch", workflow: "steps:\n  - uses: >-\n      pullfrog/pullfrog@main\n", unsupported: true},
		{name: "plain multiline recommended major", workflow: "steps:\n  - uses:\n      pullfrog/pullfrog@v0\n"},
		{name: "plain multiline branch", workflow: "steps:\n  - uses:\n      pullfrog/pullfrog@main\n", unsupported: true},
		{name: "quoted key multiline branch", workflow: "steps:\n  - \"uses\":\n      pullfrog/pullfrog@main\n", unsupported: true},
		{name: "flow multiline recommended major", workflow: "steps:\n  - {uses:\n      pullfrog/pullfrog@v0, with: {}}\n"},
		{name: "flow multiline branch", workflow: "steps:\n  - {uses:\n      pullfrog/pullfrog@main, with: {}}\n", unsupported: true},
		{name: "flow list equal indent branch", workflow: "steps: [{name: n,\n  uses:\n  pullfrog/pullfrog@main}]\n", unsupported: true},
		{name: "explicit flow key branch", workflow: "steps:\n  - {? uses: pullfrog/pullfrog@main}\n", unsupported: true},
		{name: "explicit flow key split before value", workflow: "steps: [{? uses\n  : pullfrog/pullfrog@main}]\n", unsupported: true},
		{name: "escaped quoted key recommended major", workflow: "steps:\n  - \"\\x75ses\": pullfrog/pullfrog@v0\n"},
		{name: "escaped quoted key branch", workflow: "steps:\n  - \"\\x75ses\": pullfrog/pullfrog@main\n", unsupported: true},
		{name: "escaped action value branch", workflow: "steps:\n  - uses: \"pullfrog/\\x70ullfrog@main\"\n", unsupported: true},
		{name: "anchored alias recommended major", workflow: "steps:\n  - uses: &pullfrog pullfrog/pullfrog@v0\n  - uses: *pullfrog\n"},
		{name: "anchored alias branch", workflow: "steps:\n  - uses: &pullfrog pullfrog/pullfrog@main\n  - uses: *pullfrog\n", unsupported: true},
		{name: "numeric anchored alias branch", workflow: "env:\n  ACTION_REF: &0 pullfrog/pullfrog@main\nsteps:\n  - uses: *0\n", unsupported: true},
		{name: "anchored folded branch", workflow: "steps:\n  - uses: &pullfrog >-\n      pullfrog/pullfrog@main\n  - name: *pullfrog\n    run: echo ok\n", unsupported: true},
		{name: "aliased plain multiline branch", workflow: "env:\n  ACTION_REF: &pullfrog pullfrog/pullfrog@main\nsteps:\n  - uses:\n      *pullfrog\n", unsupported: true},
		{name: "aliased uses key branch", workflow: "name: &uses-key uses\nsteps:\n  - *uses-key: pullfrog/pullfrog@main\n", unsupported: true},
		{name: "compact flow mapping branch", workflow: "steps: [uses: pullfrog/pullfrog@main]\n", unsupported: true},
		{name: "explicit block key recommended major", workflow: "steps:\n  - ? uses\n    : pullfrog/pullfrog@v0\n"},
		{name: "explicit block key branch", workflow: "steps:\n  - ? uses\n    : pullfrog/pullfrog@main\n", unsupported: true},
		{name: "explicit block key folded branch", workflow: "steps:\n  - ? uses\n    : >-\n      pullfrog/pullfrog@main\n", unsupported: true},
		{name: "block scalar sibling branch", workflow: "steps:\n  - name: >-\n      Run agent\n    uses: pullfrog/pullfrog@main\n", unsupported: true},
		{name: "ordinary quoted key folded branch", workflow: "steps:\n  - \"uses\": >-\n      pullfrog/pullfrog@main\n", unsupported: true},
		{name: "hash in flow scalar", workflow: "steps:\n  - {name: a#b, uses: pullfrog/pullfrog@main}\n", unsupported: true},
		{name: "apostrophe in flow scalar", workflow: "steps:\n  - {name: it's agent, uses: pullfrog/pullfrog@main}\n", unsupported: true},
		{name: "even backslashes before flow quote", workflow: "steps:\n  - {name: \"C:\\\\dir\\\\\", uses: pullfrog/pullfrog@main}\n", unsupported: true},
		{name: "plain bracket does not hide later branch", workflow: "name: build [1\nsteps:\n  - uses: pullfrog/pullfrog@v0\n  - uses: pullfrog/pullfrog@main\n", unsupported: true},
		{name: "ref containing at sign", workflow: "steps:\n  - uses: pullfrog/pullfrog@release@latest\n", unsupported: true},
		{name: "other action", workflow: "steps:\n  - uses: actions/checkout@v7\n"},
		{name: "run text", workflow: "steps:\n  - run: echo pullfrog/pullfrog@main\n"},
		{name: "run block text", workflow: "steps:\n  - run: |\n      uses: pullfrog/pullfrog@main\n"},
		{name: "recursive alias without uses", workflow: "value: &self [*self]\n"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := scanUnsupportedPullfrogWorkflowRefs(test.workflow)
			if err != nil {
				t.Fatalf("scanUnsupportedPullfrogWorkflowRefs() error: %v", err)
			}
			if (len(got) != 0) != test.unsupported {
				t.Fatalf("unsupported refs = %v, want unsupported=%t", got, test.unsupported)
			}
		})
	}

	t.Run("ordinary quoted key produces one diagnostic", func(t *testing.T) {
		got, err := scanUnsupportedPullfrogWorkflowRefs("steps:\n  - \"uses\": pullfrog/pullfrog@main\n")
		if err != nil {
			t.Fatalf("scanUnsupportedPullfrogWorkflowRefs() error: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("unsupported refs = %v, want exactly one finding", got)
		}
	})
}

func scanUnsupportedPullfrogWorkflowRefs(workflow string) ([]workflowPullfrogRef, error) {
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
		if ok && ref != "v0" {
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
	const repository = "pullfrog/pullfrog"
	lowerValue := strings.ToLower(value)
	if !strings.HasPrefix(lowerValue, repository) {
		return "", false
	}

	remainder := value[len(repository):]
	separator := len(repository)
	switch {
	case strings.HasPrefix(remainder, "@"):
	case strings.HasPrefix(remainder, "/"):
		relativeSeparator := strings.IndexByte(remainder, '@')
		if relativeSeparator < 0 {
			return "", false
		}
		separator += relativeSeparator
	default:
		return "", false
	}
	if separator == len(value)-1 {
		return "", false
	}
	action := value[:separator]
	ref := value[separator+1:]
	if strings.ContainsAny(action, " \t\r\n") || strings.ContainsAny(ref, " \t\r\n") {
		return "", false
	}
	return ref, true
}
