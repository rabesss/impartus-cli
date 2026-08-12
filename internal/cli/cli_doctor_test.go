package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectDoctorReportChecksDependenciesAndPaths(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	if err := os.WriteFile(configPath, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	stateDir := filepath.Join(root, "state", "impartus")
	tokenPath := filepath.Join(root, ".token")
	if err := os.WriteFile(tokenPath, []byte("token"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	report := collectDoctorReport(doctorOptions{
		lookPath:   func(name string) (string, error) { return "/usr/bin/" + name, nil },
		configPath: configPath,
		tokenPath:  tokenPath,
		stateDir:   stateDir,
		checkRuntime: func() error {
			return nil
		},
		checkLibrary: func() error { return nil },
	})
	if !report.OK {
		t.Fatalf("report should be healthy: %+v", report)
	}
	for _, name := range []string{"mpv", "ffmpeg", "config", "token", "state", "runtime", "library"} {
		check := doctorCheckNamed(t, report, name)
		if check.Status != doctorStatusPass {
			t.Fatalf("check %q status = %q, want pass: %+v", name, check.Status, check)
		}
	}
	info, err := os.Stat(stateDir)
	if err != nil {
		t.Fatalf("state directory was not prepared: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("state mode = %04o, want 0700", info.Mode().Perm())
	}
}

func TestFinishCommittedLibraryOperationDoesNotRewriteSuccessfulOutcome(t *testing.T) {
	t.Parallel()

	closeErr := errors.New("close acknowledgment failed")
	if err := finishCommittedLibraryOperation(nil, func() error { return closeErr }); err != nil {
		t.Fatalf("finishCommittedLibraryOperation(success) error = %v, want nil", err)
	}
	operationErr := errors.New("operation failed")
	err := finishCommittedLibraryOperation(operationErr, func() error { return closeErr })
	if !errors.Is(err, operationErr) || !errors.Is(err, closeErr) {
		t.Fatalf("finishCommittedLibraryOperation(failure) error = %v, want operation and close causes", err)
	}
}

func TestCollectDoctorReportFailsUnsafeConfigAndMissingDependency(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	if err := os.WriteFile(configPath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	tokenPath := filepath.Join(root, ".token")
	if err := os.WriteFile(tokenPath, []byte("secret"), 0o644); err != nil {
		t.Fatalf("write token: %v", err)
	}

	report := collectDoctorReport(doctorOptions{
		lookPath: func(name string) (string, error) {
			if name == "mpv" {
				return "", errors.New("not found")
			}
			return "/usr/bin/" + name, nil
		},
		configPath: configPath,
		tokenPath:  tokenPath,
		stateDir:   filepath.Join(root, "state"),
		checkRuntime: func() error {
			return errors.New("unsafe runtime")
		},
		checkLibrary: func() error { return errors.New("database corrupt") },
	})
	if report.OK {
		t.Fatalf("report should fail: %+v", report)
	}
	for _, name := range []string{"mpv", "config", "token", "runtime", "library"} {
		check := doctorCheckNamed(t, report, name)
		if check.Status != doctorStatusFail {
			t.Fatalf("check %q status = %q, want fail: %+v", name, check.Status, check)
		}
	}
	if detail := doctorCheckNamed(t, report, "config").Detail; !strings.Contains(detail, "0600") {
		t.Fatalf("config failure is not actionable: %q", detail)
	}
	if detail := doctorCheckNamed(t, report, "token").Detail; !strings.Contains(detail, "bearer token") {
		t.Fatalf("token failure is not actionable: %q", detail)
	}
}

func TestDoctorPermissionPolicyWarnsWhenWindowsACLsCannotBeInspected(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	filePath := filepath.Join(root, "credentials.json")
	if err := os.WriteFile(filePath, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	fileAssessment := assessDoctorPrivateFilePermissions("windows", filePath, "contains credentials", fileInfo)
	if fileAssessment.Status != doctorStatusWarn || !strings.Contains(fileAssessment.Detail, "could not be verified") {
		t.Fatalf("Windows file policy = %+v, want explicit unverified-ACL warning", fileAssessment)
	}

	directoryInfo, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	directoryAssessment := assessDoctorPrivateDirectoryPermissions("windows", root, directoryInfo)
	if directoryAssessment.Status != doctorStatusWarn || !strings.Contains(directoryAssessment.Detail, "could not be verified") {
		t.Fatalf("Windows directory policy = %+v, want explicit unverified-ACL warning", directoryAssessment)
	}
}

func TestDoctorWindowsACLPolicyRejectsBroadContentAccess(t *testing.T) {
	t.Parallel()

	assessment := assessDoctorACLEntries(true, []doctorACLEntry{
		{Allowed: true, Trusted: true, Mask: doctorACLReadData},
		{Allowed: true, Trusted: false, Mask: doctorACLReadData},
	})
	if assessment.Status != doctorStatusFail {
		t.Fatalf("broad ACL policy = %+v, want fail", assessment)
	}

	assessment = assessDoctorACLEntries(true, []doctorACLEntry{
		{Allowed: true, Trusted: true, Mask: doctorACLReadData | doctorACLWriteData},
		{Allowed: true, Trusted: false, Mask: doctorACLReadAttributes},
	})
	if assessment.Status != doctorStatusPass {
		t.Fatalf("private ACL policy = %+v, want pass", assessment)
	}

	assessment = assessDoctorACLEntries(false, nil)
	if assessment.Status != doctorStatusFail {
		t.Fatalf("foreign owner policy = %+v, want fail", assessment)
	}

	for _, mask := range []uint32{doctorACLWriteDAC, doctorACLWriteOwner} {
		assessment = assessDoctorACLEntries(true, []doctorACLEntry{{Allowed: true, Trusted: false, Mask: mask}})
		if assessment.Status != doctorStatusFail {
			t.Fatalf("ACL ownership-control mask %#x = %+v, want fail", mask, assessment)
		}
	}
}

func TestCollectDoctorReportAllowsMissingConfig(t *testing.T) {
	root := t.TempDir()
	report := collectDoctorReport(doctorOptions{
		lookPath:     func(name string) (string, error) { return "/usr/bin/" + name, nil },
		configPath:   filepath.Join(root, "missing.json"),
		tokenPath:    filepath.Join(root, "missing.token"),
		stateDir:     filepath.Join(root, "state"),
		checkRuntime: func() error { return nil },
		checkLibrary: func() error { return nil },
	})
	if !report.OK {
		t.Fatalf("environment-only configuration should remain valid: %+v", report)
	}
	if got := doctorCheckNamed(t, report, "config").Status; got != doctorStatusWarn {
		t.Fatalf("missing config status = %q, want warn", got)
	}
}

func TestCollectDoctorReportAllowsLegacyModeWithoutIPCRuntime(t *testing.T) {
	root := t.TempDir()
	report := collectDoctorReport(doctorOptions{
		lookPath:     func(name string) (string, error) { return "/usr/bin/" + name, nil },
		configPath:   filepath.Join(root, "missing.json"),
		tokenPath:    filepath.Join(root, "missing.token"),
		stateDir:     filepath.Join(root, "state"),
		mpvMode:      "legacy",
		checkRuntime: func() error { return errors.New("IPC unsupported") },
		checkLibrary: func() error { return nil },
	})
	if !report.OK {
		t.Fatalf("legacy mode should not require IPC runtime: %+v", report)
	}
	check := doctorCheckNamed(t, report, "runtime")
	if check.Status != doctorStatusWarn || !strings.Contains(check.Detail, "legacy") {
		t.Fatalf("runtime check = %+v, want legacy warning", check)
	}
}

func TestCollectDoctorReportAllowsSymlinkedStateAncestor(t *testing.T) {
	realParent := t.TempDir()
	linkedParent := filepath.Join(t.TempDir(), "state-link")
	if err := os.Symlink(realParent, linkedParent); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	report := collectDoctorReport(doctorOptions{
		lookPath:     func(name string) (string, error) { return "/usr/bin/" + name, nil },
		configPath:   filepath.Join(t.TempDir(), "missing.json"),
		tokenPath:    filepath.Join(t.TempDir(), "missing.token"),
		stateDir:     filepath.Join(linkedParent, "impartus"),
		checkRuntime: func() error { return nil },
		checkLibrary: func() error { return nil },
	})
	if got := doctorCheckNamed(t, report, "state").Status; got != doctorStatusPass {
		t.Fatalf("state status = %q, want pass through safe ancestor symlink: %+v", got, report)
	}
}

func TestExecuteJSONDoctorFailureIncludesChecks(t *testing.T) {
	restoreCLIState(t)
	os.Args = []string{"impartus", "doctor", "--json"}
	t.Setenv("PATH", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", "")

	stdout, err := captureStdout(t, func() error { return Execute("dev", "") })
	if err == nil {
		t.Fatal("expected failed doctor JSON envelope")
	}
	if stdout != "" {
		t.Fatalf("failed JSON doctor wrote stdout: %q", stdout)
	}
	var envelope struct {
		Success bool         `json:"success"`
		Data    doctorReport `json:"data"`
		Error   *jsonErr     `json:"error"`
	}
	if decodeErr := json.Unmarshal([]byte(err.Error()), &envelope); decodeErr != nil {
		t.Fatalf("decode doctor error envelope: %v", decodeErr)
	}
	if envelope.Success || envelope.Error == nil || envelope.Data.OK || len(envelope.Data.Checks) != 7 {
		t.Fatalf("failed doctor envelope lost diagnostics: %+v", envelope)
	}
}

func TestExecuteDispatchesDoctor(t *testing.T) {
	restoreCLIState(t)
	var got []string
	runDoctorFn = func(args []string) error {
		got = append([]string(nil), args...)
		return nil
	}
	os.Args = []string{"impartus", "doctor", "--verbose"}
	if err := Execute("dev", ""); err != nil {
		t.Fatalf("Execute doctor: %v", err)
	}
	if len(got) != 1 || got[0] != "--verbose" {
		t.Fatalf("doctor args = %v", got)
	}
}

func doctorCheckNamed(t *testing.T, report doctorReport, name string) doctorCheck {
	t.Helper()
	for _, check := range report.Checks {
		if check.Name == name {
			return check
		}
	}
	t.Fatalf("missing doctor check %q in %+v", name, report)
	return doctorCheck{}
}
