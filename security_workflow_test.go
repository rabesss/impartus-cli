package main

import (
	"regexp"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// These tests pin the analyzer setup from issue #309: gosec must fail the job
// when it cannot type-check the code, instead of passing with no findings, and
// the analyzers must be releases that support go.mod's Go version.

func gosecScanStep(t *testing.T) workflowStep {
	t.Helper()

	var workflow workflowFile
	if err := yaml.Unmarshal([]byte(readRepoFile(t, ".github/workflows/security.yml")), &workflow); err != nil {
		t.Fatalf("parse security workflow: %v", err)
	}
	job, ok := workflow.Jobs["gosec-security-scan"]
	if !ok {
		t.Fatal("security workflow has no gosec-security-scan job")
	}
	for _, step := range job.Steps {
		if strings.Contains(step.Run, "gosec -fmt json -out=gosec-results.json") {
			return step
		}
	}
	t.Fatal("gosec-security-scan job has no step writing gosec-results.json")
	return workflowStep{}
}

func TestGosecStepFailsOnInternalErrors(t *testing.T) {
	run := gosecScanStep(t).Run

	required := []string{
		// A missing or invalid report fails instead of skipping every gate.
		"if ! jq -e . gosec-results.json",
		// Packages gosec could not type-check fail the job.
		`.["Golang errors"]`,
		`if [ "$GOLANG_ERRORS" -ne 0 ]`,
		`if [ "$HIGH_SEV" -gt 0 ]`,
	}
	for _, snippet := range required {
		if !strings.Contains(run, snippet) {
			t.Errorf("gosec step is missing %q:\n%s", snippet, run)
		}
	}
	if strings.Contains(run, "if [ -f gosec-results.json ]") {
		t.Error("gosec step must not skip its gates when the report is missing")
	}
	if strings.Index(run, "Golang errors") > strings.Index(run, "HIGH_SEV=") {
		t.Error("check gosec's Golang errors before counting findings, which are incomplete when packages are skipped")
	}
}

var (
	gosecPinPattern         = regexp.MustCompile(`securego/gosec/v2/cmd/gosec@v(\d+\.\d+\.\d+)\b`)
	golangciLintPinPatterns = []*regexp.Regexp{
		regexp.MustCompile(`golangci_lint_version: v(\d+\.\d+\.\d+)\b`),
		regexp.MustCompile(`(?m)^\s+version: v(\d+\.\d+\.\d+)\s*$`),
		regexp.MustCompile(`golangci-lint/v2/cmd/golangci-lint@v(\d+\.\d+\.\d+)\b`),
	}
)

func TestAnalyzersSupportGo127(t *testing.T) {
	// gosec v2.29.0 and golangci-lint v2.13.1 are the first releases that
	// analyze Go 1.27 code; older ones fail or silently skip packages (#309).
	gosecPins := gosecPinPattern.FindAllStringSubmatch(readRepoFile(t, ".github/workflows/security.yml"), -1)
	if len(gosecPins) != 1 {
		t.Fatalf("security.yml has %d gosec install pins, want 1", len(gosecPins))
	}
	if !goVersionLessOrEqual(parseGoVersion(t, "2.29.0"), parseGoVersion(t, gosecPins[0][1])) {
		t.Errorf("gosec v%s does not support Go 1.27; use v2.29.0 or later", gosecPins[0][1])
	}

	ci := readRepoFile(t, ".github/workflows/ci.yml")
	var lintVersions []string
	for _, pattern := range golangciLintPinPatterns {
		pins := pattern.FindAllStringSubmatch(ci, -1)
		if len(pins) != 1 {
			t.Errorf("ci.yml has %d matches for golangci-lint pin %s, want 1", len(pins), pattern)
			continue
		}
		lintVersions = append(lintVersions, pins[0][1])
	}
	for _, version := range lintVersions {
		if version != lintVersions[0] {
			t.Errorf("golangci-lint pins differ in ci.yml: %v; bump all of them together", lintVersions)
			break
		}
		if !goVersionLessOrEqual(parseGoVersion(t, "2.13.1"), parseGoVersion(t, version)) {
			t.Errorf("golangci-lint v%s does not support Go 1.27; use v2.13.1 or later", version)
		}
	}
}
