package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

const runLoggedScript = "scripts/run-logged.sh"

type runLoggedResult struct {
	status int
	stdout string
	stderr string
}

// runLogged runs the helper from a `bash -e` parent, the shell GitHub uses
// for a run step without an explicit shell, so the status the test sees is the
// status the step would see.
func runLogged(t *testing.T, logPath string, arguments ...string) runLoggedResult {
	t.Helper()
	command := runLoggedCommand(t, logPath, arguments...)
	var stdout, stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr
	return runLoggedResult{status: exitStatus(t, command.Run()), stdout: stdout.String(), stderr: stderr.String()}
}

func runLoggedCommand(t *testing.T, logPath string, arguments ...string) *exec.Cmd {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the audit log helper runs in the Linux Security workflow")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is unavailable")
	}
	script, err := filepath.Abs(runLoggedScript)
	if err != nil {
		t.Fatalf("resolve helper path: %v", err)
	}
	parentArguments := append([]string{"-e", "-c", `"$@"`, "bash", script, logPath}, arguments...)
	command := exec.CommandContext(t.Context(), bash, parentArguments...) // #nosec G204 -- test runs the repository helper with fixed arguments
	command.Dir = t.TempDir()
	return command
}

func exitStatus(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("run helper: %v", err)
	}
	return exitErr.ExitCode()
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path) // #nosec G304 -- test-owned log under t.TempDir
	if err != nil {
		t.Fatalf("read log %s: %v", path, err)
	}
	return string(content)
}

func TestRunLoggedHelperIsExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable bits are not tracked on Windows checkouts")
	}
	info, err := os.Stat(runLoggedScript)
	if err != nil {
		t.Fatalf("stat audit log helper: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Error("audit log helper is not executable")
	}
}

func TestRunLoggedPreservesNonzeroProducerStatus(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "audit.txt")
	result := runLogged(t, logPath, "bash", "-c", "echo to-stdout; echo to-stderr >&2; exit 3")
	if result.status != 3 {
		t.Fatalf("status = %d, want the producer's 3\nstderr:\n%s", result.status, result.stderr)
	}
	log := readLog(t, logPath)
	for _, want := range []string{"to-stdout", "to-stderr"} {
		if !strings.Contains(log, want) {
			t.Errorf("log = %q, want it to hold %q", log, want)
		}
		if !strings.Contains(result.stdout, want) {
			t.Errorf("streamed output = %q, want it to hold %q", result.stdout, want)
		}
	}
	if !strings.Contains(result.stderr, "::error::bash exited with status 3") {
		t.Errorf("stderr = %q, want an error annotation naming the status", result.stderr)
	}
	if strings.Contains(result.stderr, "succeeded") {
		t.Errorf("failed run printed the success line: %q", result.stderr)
	}
}

// TestRunLoggedSucceedsWithClosedStderr checks that the success line on
// stderr cannot turn a successful run into a failure.
func TestRunLoggedSucceedsWithClosedStderr(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "audit.txt")
	command := runLoggedCommand(t, logPath, "bash", "-c", "echo No vulnerabilities found.")
	if command.Args[3] != `"$@"` {
		t.Fatalf("runLoggedCommand arguments changed: %q", command.Args)
	}
	command.Args[3] = `"$@" 2>&-`
	var stdout strings.Builder
	command.Stdout = &stdout
	if status := exitStatus(t, command.Run()); status != 0 {
		t.Fatalf("status = %d, want 0 with stderr closed", status)
	}
	if log := readLog(t, logPath); log != "No vulnerabilities found.\n" {
		t.Fatalf("log = %q", log)
	}
}

func TestRunLoggedPassesSuccessfulProducer(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "audit.txt")
	result := runLogged(t, logPath, "bash", "-c", "echo No vulnerabilities found.")
	if result.status != 0 {
		t.Fatalf("status = %d, want 0\nstderr:\n%s", result.status, result.stderr)
	}
	if log := readLog(t, logPath); log != "No vulnerabilities found.\n" {
		t.Fatalf("log = %q", log)
	}
	if strings.Contains(result.stderr, "::error::") {
		t.Fatalf("successful run emitted an error annotation: %q", result.stderr)
	}
	if want := "run-logged: bash succeeded; log saved to " + logPath; !strings.Contains(result.stderr, want) {
		t.Fatalf("stderr = %q, want the success line %q", result.stderr, want)
	}
	if strings.Contains(result.stdout, "run-logged:") {
		t.Fatalf("success line leaked into the streamed output: %q", result.stdout)
	}
}

func TestRunLoggedFailsWhenTheLogCannotBeWritten(t *testing.T) {
	for _, test := range []struct {
		name     string
		producer string
		want     int
	}{
		{name: "successful producer", producer: "echo audit-output", want: 1},
		{name: "failing producer keeps its status", producer: "echo audit-output; exit 5", want: 5},
	} {
		t.Run(test.name, func(t *testing.T) {
			logPath := filepath.Join(t.TempDir(), "missing-directory", "audit.txt")
			result := runLogged(t, logPath, "bash", "-c", test.producer)
			if result.status != test.want {
				t.Fatalf("status = %d, want %d\nstderr:\n%s", result.status, test.want, result.stderr)
			}
			if !strings.Contains(result.stderr, "::error::tee failed writing the log") {
				t.Fatalf("stderr = %q, want an explicit log-write error", result.stderr)
			}
			if strings.Contains(result.stderr, "succeeded") {
				t.Fatalf("run with an unwritable log printed the success line: %q", result.stderr)
			}
			if !strings.Contains(result.stdout, "audit-output") {
				t.Fatalf("streamed output = %q, want the producer output despite the log failure", result.stdout)
			}
		})
	}
}

func TestRunLoggedKeepsProducerAndLogWhenStdoutCloses(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "audit.txt")
	// The read end is closed, so tee's first write to stdout fails with EPIPE;
	// 4096 lines of 64 bytes (more than a pipe buffer) make sure the producer
	// keeps writing well after that.
	command := runLoggedCommand(t, logPath, "bash", "-c",
		`for i in $(seq 1 4096); do printf '%063d\n' "$i"; done; echo producer-finished; exit 4`)
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	if closeErr := reader.Close(); closeErr != nil {
		t.Fatalf("close stdout reader: %v", closeErr)
	}
	command.Stdout = writer
	var stderr strings.Builder
	command.Stderr = &stderr
	runErr := command.Run()
	if closeErr := writer.Close(); closeErr != nil {
		t.Fatalf("close stdout writer: %v", closeErr)
	}
	if status := exitStatus(t, runErr); status != 4 {
		t.Fatalf("status = %d, want the producer's 4\nstderr:\n%s", status, stderr.String())
	}
	log := readLog(t, logPath)
	if !strings.HasSuffix(log, "producer-finished\n") || strings.Count(log, "\n") != 4097 {
		t.Fatalf("log has %d lines, want all 4097 lines through producer-finished", strings.Count(log, "\n"))
	}
}

func TestRunLoggedKeepsProducerSIGPIPEBehaviour(t *testing.T) {
	// Only tee ignores SIGPIPE; a producer killed by it must still fail with
	// its own signal status rather than survive and report success.
	logPath := filepath.Join(t.TempDir(), "audit.txt")
	result := runLogged(t, logPath, "bash", "-c", "kill -PIPE $$; exit 0")
	if result.status != 128+13 {
		t.Fatalf("status = %d, want 141 from the producer's SIGPIPE\nstderr:\n%s", result.status, result.stderr)
	}
}

func TestRunLoggedPassesArgumentsVerbatim(t *testing.T) {
	directory := t.TempDir()
	logPath := filepath.Join(directory, "-audit.txt")
	command := runLoggedCommand(t, "-audit.txt", "printf", "[%s]", "two words", "", "--flag")
	command.Dir = directory
	var stderr strings.Builder
	command.Stderr = &stderr
	if status := exitStatus(t, command.Run()); status != 0 {
		t.Fatalf("status = %d, want 0\nstderr:\n%s", status, stderr.String())
	}
	if log := readLog(t, logPath); log != "[two words][][--flag]" {
		t.Fatalf("log = %q, want each argument passed through unchanged", log)
	}
}

func TestRunLoggedRejectsMissingCommand(t *testing.T) {
	result := runLogged(t, filepath.Join(t.TempDir(), "audit.txt"))
	if result.status != 2 {
		t.Fatalf("status = %d, want usage error 2", result.status)
	}
	if !strings.Contains(result.stderr, "usage:") {
		t.Fatalf("stderr = %q, want usage text", result.stderr)
	}
}

type workflowFile struct {
	Jobs map[string]workflowJob `yaml:"jobs"`
}

type workflowJob struct {
	Needs []string       `yaml:"needs"`
	If    string         `yaml:"if"`
	Steps []workflowStep `yaml:"steps"`
}

type workflowStep struct {
	ID              string            `yaml:"id"`
	ContinueOnError bool              `yaml:"continue-on-error"`
	Name            string            `yaml:"name"`
	If              string            `yaml:"if"`
	Run             string            `yaml:"run"`
	Uses            string            `yaml:"uses"`
	With            map[string]string `yaml:"with"`
	Env             map[string]string `yaml:"env"`
}

func readSecurityWorkflow(t *testing.T) workflowFile {
	t.Helper()
	content, err := os.ReadFile(".github/workflows/security.yml")
	if err != nil {
		t.Fatalf("read security workflow: %v", err)
	}
	var workflow workflowFile
	if err := yaml.Unmarshal(content, &workflow); err != nil {
		t.Fatalf("parse security workflow: %v", err)
	}
	return workflow
}

func TestDependencyAuditKeepsGovulncheckStatusAndLog(t *testing.T) {
	workflow := readSecurityWorkflow(t)
	job, ok := workflow.Jobs["dependency-audit"]
	if !ok {
		t.Fatal("security workflow has no dependency-audit job")
	}
	var audit, upload *workflowStep
	for index := range job.Steps {
		step := &job.Steps[index]
		switch {
		case step.ID == "govulncheck":
			audit = step
		case strings.HasPrefix(step.Uses, "actions/upload-artifact@") && step.With["name"] == "govulncheck-results":
			upload = step
		}
	}
	if audit == nil {
		t.Fatal("dependency-audit job has no govulncheck step")
	}
	if !strings.Contains(audit.Run, "bash scripts/run-logged.sh govulncheck-results.txt govulncheck ./...") {
		t.Errorf("govulncheck step does not run through the status-preserving helper:\n%s", audit.Run)
	}
	if strings.Contains(audit.Run, "| tee") || strings.Contains(audit.Run, "$?") {
		t.Errorf("govulncheck step still captures a pipeline status by hand:\n%s", audit.Run)
	}
	if upload == nil {
		t.Fatal("dependency-audit job no longer uploads govulncheck results")
	}
	if upload.If != "always()" || upload.With["path"] != "govulncheck-results.txt" {
		t.Errorf("upload step if=%q path=%q, want always() and govulncheck-results.txt", upload.If, upload.With["path"])
	}
}

// The summary job runs on a fresh runner with no checkout, so the govulncheck
// log only exists there if it downloads the dependency-audit artifact first.
func TestSecurityReportDownloadsGovulncheckLogBeforeReadingIt(t *testing.T) {
	workflow := readSecurityWorkflow(t)
	job, ok := workflow.Jobs["security-report"]
	if !ok {
		t.Fatal("security workflow has no security-report job")
	}
	if !slices.Contains(job.Needs, "dependency-audit") {
		t.Errorf("security-report needs = %v, want it to wait for dependency-audit", job.Needs)
	}
	if job.If != "always()" {
		t.Errorf("security-report if = %q, want always() so failed scans are still summarized", job.If)
	}
	download, summary := -1, -1
	for index, step := range job.Steps {
		switch {
		case strings.HasPrefix(step.Uses, "actions/download-artifact@") && step.With["name"] == "govulncheck-results":
			download = index
			if !pinnedActionRef.MatchString(step.Uses) {
				t.Errorf("download step uses %q, want a full commit SHA pin", step.Uses)
			}
			if step.ID != govulncheckDownloadID {
				t.Errorf("download step id = %q, want %q (the summary reads its outcome)", step.ID, govulncheckDownloadID)
			}
			if !step.ContinueOnError {
				t.Error("download step must continue on error: the artifact is missing when the audit fails before logging")
			}
			if path := step.With["path"]; path != "" && path != "." {
				t.Errorf("download path = %q, want the workspace root the summary reads", path)
			}
		case strings.Contains(step.Run, "govulncheck-results.txt"):
			summary = index
			if !strings.Contains(step.Run, "$GOVULNCHECK_DOWNLOAD") || !strings.Contains(step.Run, "$DEPENDENCY_AUDIT_RESULT") {
				t.Errorf("summary does not read the download outcome and audit result:\n%s", step.Run)
			}
		}
	}
	if download < 0 {
		t.Fatal("security-report never downloads the govulncheck-results artifact")
	}
	if summary < 0 {
		t.Fatal("security-report no longer summarizes govulncheck-results.txt")
	}
	if download > summary {
		t.Errorf("download step %d runs after the summary step %d that reads the log", download, summary)
	}
}

var pinnedActionRef = regexp.MustCompile(`@[0-9a-f]{40}$`)

const govulncheckDownloadID = "govulncheck-log"

// govulncheckSummaryStep returns the summary step and checks that it receives
// the download outcome and the audit job result through its environment.
func govulncheckSummaryStep(t *testing.T) workflowStep {
	t.Helper()
	job := readSecurityWorkflow(t).Jobs["security-report"]
	for _, step := range job.Steps {
		if !strings.Contains(step.Run, "govulncheck-results.txt") {
			continue
		}
		wantEnv := map[string]string{
			"GOVULNCHECK_DOWNLOAD":    "${{ steps." + govulncheckDownloadID + ".outcome }}",
			"DEPENDENCY_AUDIT_RESULT": "${{ needs.dependency-audit.result }}",
		}
		for name, want := range wantEnv {
			if got := step.Env[name]; got != want {
				t.Errorf("summary env %s = %q, want %q", name, got, want)
			}
		}
		return step
	}
	t.Fatal("security-report has no step that reads govulncheck-results.txt")
	return workflowStep{}
}

// TestSecurityReportClassifiesGovulncheckResult runs the summary script the
// way the runner would and checks the Go Vulnerability Check line it writes.
func TestSecurityReportClassifiesGovulncheckResult(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the summary runs in the Linux Security workflow")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is unavailable")
	}
	script := strings.ReplaceAll(govulncheckSummaryStep(t).Run, "${{ github.repository }}", "owner/repo")
	if strings.Contains(script, "${{") {
		t.Fatalf("summary script uses an expression the test does not substitute:\n%s", script)
	}
	const clean = "No vulnerabilities found.\n"
	for _, test := range []struct {
		name, download, audit, log string
		hasLog                     bool
		want                       string
	}{
		{name: "clean scan", download: "success", audit: "success", log: clean, hasLog: true, want: "✓ No Go vulnerabilities detected"},
		{name: "findings fail the audit", download: "success", audit: "failure", log: "Vulnerability #1: GO-0000-0000\n", hasLog: true, want: "⚠️ govulncheck reported vulnerabilities or failed (Dependency Audit: failure)"},
		{name: "failed audit with a clean line", download: "success", audit: "failure", log: clean, hasLog: true, want: "⚠️ govulncheck reported vulnerabilities or failed (Dependency Audit: failure)"},
		{name: "passing audit without the clean line", download: "success", audit: "success", log: "unexpected\n", hasLog: true, want: "⚠️ govulncheck passed (no called vulnerabilities) but printed no clean-scan line"},
		{name: "cancelled audit leaves a log", download: "success", audit: "cancelled", log: clean, hasLog: true, want: "⚠️ Dependency Audit cancelled - no govulncheck verdict"},
		{name: "skipped audit leaves a log", download: "success", audit: "skipped", log: clean, hasLog: true, want: "⚠️ Dependency Audit skipped - no govulncheck verdict"},
		{name: "missing artifact", download: "failure", audit: "failure", want: "⚠️ govulncheck log unavailable (Dependency Audit: failure)"},
		{name: "failed download leaves a file", download: "failure", audit: "success", log: clean, hasLog: true, want: "⚠️ govulncheck log unavailable (Dependency Audit: success)"},
		{name: "download succeeded without the file", download: "success", audit: "success", want: "⚠️ govulncheck log unavailable (Dependency Audit: success)"},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			if test.hasLog {
				if err := os.WriteFile(filepath.Join(directory, "govulncheck-results.txt"), []byte(test.log), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			summaryPath := filepath.Join(t.TempDir(), "summary.md")
			command := exec.CommandContext(t.Context(), bash, "-e", "-c", script) // #nosec G204 -- test runs the repository workflow script
			command.Dir = directory
			command.Env = append(os.Environ(),
				"GITHUB_STEP_SUMMARY="+summaryPath,
				"GOVULNCHECK_DOWNLOAD="+test.download,
				"DEPENDENCY_AUDIT_RESULT="+test.audit,
			)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("summary script failed: %v\n%s", err, output)
			}
			summary := readLog(t, summaryPath)
			_, section, found := strings.Cut(summary, "### Go Vulnerability Check\n")
			if !found {
				t.Fatalf("summary has no Go Vulnerability Check section:\n%s", summary)
			}
			if line, _, _ := strings.Cut(section, "\n"); !strings.HasPrefix(line, test.want) {
				t.Fatalf("Go Vulnerability Check line = %q, want prefix %q", line, test.want)
			}
		})
	}
}
