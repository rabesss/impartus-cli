package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/library"
	"github.com/rabesss/impartus-cli/internal/player"
)

const (
	doctorStatusPass = "pass"
	doctorStatusWarn = "warn"
	doctorStatusFail = "fail"
)

type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type doctorReport struct {
	OK     bool          `json:"ok"`
	Checks []doctorCheck `json:"checks"`
}

type doctorOptions struct {
	lookPath     func(string) (string, error)
	configPath   string
	tokenPath    string
	stateDir     string
	mpvMode      string
	checkRuntime func() error
	checkLibrary func() error
}

func defaultDoctorOptions() (doctorOptions, error) {
	stateDir, err := defaultStateDirectory()
	if err != nil {
		return doctorOptions{}, err
	}
	return doctorOptions{
		lookPath:     exec.LookPath,
		configPath:   config.ConfigLocation,
		tokenPath:    ".token",
		stateDir:     stateDir,
		mpvMode:      defaultMPVModeForOS(runtime.GOOS),
		checkRuntime: func() error { return player.CheckRuntime("") },
		checkLibrary: checkDefaultLibrary,
	}, nil
}

func defaultStateDirectory() (string, error) {
	if base := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); base != "" {
		return filepath.Join(base, "impartus"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory for state: %w", err)
	}
	return filepath.Join(home, ".local", "state", "impartus"), nil
}

func runDoctor(args []string) error {
	report, err := getDoctorReport(args)
	if err != nil {
		return err
	}
	for _, check := range report.Checks {
		if _, writeErr := fmt.Fprintf(os.Stdout, "[%s] %s: %s\n", strings.ToUpper(check.Status), check.Name, check.Detail); writeErr != nil {
			return fmt.Errorf("write doctor report: %w", writeErr)
		}
	}
	if !report.OK {
		return errors.New("doctor found one or more blocking problems")
	}
	return nil
}

func getDoctorReport(args []string) (doctorReport, error) {
	if len(args) != 0 {
		return doctorReport{}, errors.New("doctor does not accept arguments")
	}
	options, err := defaultDoctorOptions()
	if err != nil {
		return doctorReport{}, err
	}
	return collectDoctorReport(options), nil
}

func collectDoctorReport(options doctorOptions) doctorReport {
	checks := make([]doctorCheck, 0, 7)
	checks = append(checks, checkExecutable(options.lookPath, "mpv"))
	checks = append(checks, checkExecutable(options.lookPath, "ffmpeg"))
	checks = append(checks, checkConfigFile(options.configPath))
	checks = append(checks, checkTokenFile(options.tokenPath))
	checks = append(checks, checkWritableStateDirectory(options.stateDir))
	checks = append(checks, checkRuntimeDirectory(options.checkRuntime, options.mpvMode))
	checks = append(checks, checkLibraryDatabase(options.checkLibrary))

	report := doctorReport{OK: true, Checks: checks}
	for _, check := range checks {
		if check.Status == doctorStatusFail {
			report.OK = false
			break
		}
	}
	return report
}

func checkTokenFile(path string) doctorCheck {
	if strings.TrimSpace(path) == "" {
		return doctorCheck{Name: "token", Status: doctorStatusFail, Detail: "stored token path is empty"}
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return doctorCheck{Name: "token", Status: doctorStatusWarn, Detail: ".token is absent; login will create it when needed"}
	}
	if err != nil {
		return doctorCheck{Name: "token", Status: doctorStatusFail, Detail: fmt.Sprintf("cannot inspect %s: %v", path, err)}
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return doctorCheck{Name: "token", Status: doctorStatusFail, Detail: fmt.Sprintf("%s must be a regular file, not a symlink", path)}
	}
	privacy := assessDoctorPrivateFilePermissions(runtime.GOOS, path, "contains a bearer token", info)
	return doctorCheck{Name: "token", Status: privacy.Status, Detail: privacy.Detail}
}

func checkDefaultLibrary() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	store, err := library.Open(ctx, library.Options{})
	if err != nil {
		return err
	}
	return errors.Join(store.Check(ctx), store.Close())
}

func checkLibraryDatabase(check func() error) doctorCheck {
	if check == nil {
		return doctorCheck{Name: "library", Status: doctorStatusFail, Detail: "library checker is unavailable"}
	}
	if err := check(); err != nil {
		return doctorCheck{Name: "library", Status: doctorStatusFail, Detail: err.Error()}
	}
	return doctorCheck{Name: "library", Status: doctorStatusPass, Detail: "SQLite migrations, WAL, and private permissions are healthy"}
}

func checkExecutable(lookPath func(string) (string, error), name string) doctorCheck {
	if lookPath == nil {
		return doctorCheck{Name: name, Status: doctorStatusFail, Detail: "dependency checker is unavailable"}
	}
	path, err := lookPath(name)
	if err != nil {
		return doctorCheck{Name: name, Status: doctorStatusFail, Detail: fmt.Sprintf("%s is not available on PATH", name)}
	}
	return doctorCheck{Name: name, Status: doctorStatusPass, Detail: path}
}

func checkConfigFile(path string) doctorCheck {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return doctorCheck{Name: "config", Status: doctorStatusWarn, Detail: "config.json is absent; environment-only configuration is supported"}
	}
	if err != nil {
		return doctorCheck{Name: "config", Status: doctorStatusFail, Detail: fmt.Sprintf("cannot inspect %s: %v", path, err)}
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return doctorCheck{Name: "config", Status: doctorStatusFail, Detail: fmt.Sprintf("%s must be a regular file, not a symlink", path)}
	}
	privacy := assessDoctorPrivateFilePermissions(runtime.GOOS, path, "contains credentials", info)
	return doctorCheck{Name: "config", Status: privacy.Status, Detail: privacy.Detail}
}

func checkWritableStateDirectory(path string) doctorCheck {
	absolute, err := prepareDoctorStateDirectory(path)
	if err != nil {
		return doctorCheck{Name: "state", Status: doctorStatusFail, Detail: err.Error()}
	}
	// #nosec G703 -- absolute is the normalized application state path returned by the preparer.
	info, err := os.Lstat(absolute)
	if err != nil {
		return doctorCheck{Name: "state", Status: doctorStatusFail, Detail: fmt.Sprintf("cannot inspect %s: %v", path, err)}
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return doctorCheck{Name: "state", Status: doctorStatusFail, Detail: fmt.Sprintf("%s must be a directory, not a symlink", path)}
	}
	privacy := assessDoctorPrivateDirectoryPermissions(runtime.GOOS, path, info)
	if privacy.Status == doctorStatusFail {
		return doctorCheck{Name: "state", Status: doctorStatusFail, Detail: privacy.Detail}
	}
	if ownerErr := validateDoctorStateDirectoryOwner(absolute, info); ownerErr != nil {
		return doctorCheck{Name: "state", Status: doctorStatusFail, Detail: ownerErr.Error()}
	}
	probe, err := os.CreateTemp(absolute, ".doctor-write-")
	if err != nil {
		return doctorCheck{Name: "state", Status: doctorStatusFail, Detail: fmt.Sprintf("%s is not writable: %v", path, err)}
	}
	probeName := probe.Name()
	probePrivacy := assessDoctorProbePrivacy(probe)
	if probePrivacy.Status == doctorStatusFail {
		// #nosec G703 -- probeName was returned by os.CreateTemp in the validated directory.
		cleanupErr := errors.Join(probe.Close(), os.Remove(probeName))
		detail := probePrivacy.Detail
		if cleanupErr != nil {
			detail += fmt.Sprintf("; cleanup also failed: %v", cleanupErr)
		}
		return doctorCheck{Name: "state", Status: doctorStatusFail, Detail: detail}
	}
	closeErr := probe.Close()
	// #nosec G703 -- probeName was returned by os.CreateTemp in the validated directory.
	removeErr := os.Remove(probeName)
	if closeErr != nil || removeErr != nil {
		return doctorCheck{Name: "state", Status: doctorStatusFail, Detail: fmt.Sprintf("write probe cleanup failed in %s: %v", path, errors.Join(closeErr, removeErr))}
	}
	status := privacy.Status
	if probePrivacy.Status == doctorStatusWarn {
		status = doctorStatusWarn
	}
	return doctorCheck{
		Name:   "state",
		Status: status,
		Detail: fmt.Sprintf("%s is writable; %s; %s", path, privacy.Detail, probePrivacy.Detail),
	}
}

type doctorPrivacyAssessment struct {
	Status string
	Detail string
}

type doctorACLEntry struct {
	Allowed bool
	Trusted bool
	Mask    uint32
}

const (
	doctorACLReadData       uint32 = 0x00000001
	doctorACLWriteData      uint32 = 0x00000002
	doctorACLAppendData     uint32 = 0x00000004
	doctorACLExecute        uint32 = 0x00000020
	doctorACLDeleteChild    uint32 = 0x00000040
	doctorACLReadAttributes uint32 = 0x00000080
	doctorACLDelete         uint32 = 0x00010000
	doctorACLWriteDAC       uint32 = 0x00040000
	doctorACLWriteOwner     uint32 = 0x00080000
	doctorACLGenericAll     uint32 = 0x10000000
	doctorACLGenericExecute uint32 = 0x20000000
	doctorACLGenericWrite   uint32 = 0x40000000
	doctorACLGenericRead    uint32 = 0x80000000
)

const doctorACLContentAccess = doctorACLReadData | doctorACLWriteData | doctorACLAppendData |
	doctorACLExecute | doctorACLDeleteChild | doctorACLDelete | doctorACLWriteDAC |
	doctorACLWriteOwner | doctorACLGenericAll |
	doctorACLGenericExecute | doctorACLGenericWrite | doctorACLGenericRead

func assessDoctorACLEntries(ownerIsCurrent bool, entries []doctorACLEntry) doctorPrivacyAssessment {
	if !ownerIsCurrent {
		return doctorPrivacyAssessment{Status: doctorStatusFail, Detail: "Windows ACL owner is not the current user"}
	}
	for _, entry := range entries {
		if entry.Allowed && !entry.Trusted && entry.Mask&doctorACLContentAccess != 0 {
			return doctorPrivacyAssessment{Status: doctorStatusFail, Detail: "Windows ACL grants file content access to another principal"}
		}
	}
	return doctorPrivacyAssessment{Status: doctorStatusPass, Detail: "Windows ACL is private to the current user and trusted system principals"}
}

func assessDoctorPrivateFilePermissions(goos, path, contents string, info os.FileInfo) doctorPrivacyAssessment {
	if goos == "windows" {
		return inspectDoctorWindowsACL(path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return doctorPrivacyAssessment{Status: doctorStatusFail, Detail: fmt.Sprintf("%s %s and must use mode 0600 or stricter; got %04o", path, contents, info.Mode().Perm())}
	}
	return doctorPrivacyAssessment{Status: doctorStatusPass, Detail: fmt.Sprintf("%s permissions are private (%04o)", path, info.Mode().Perm())}
}

func assessDoctorPrivateDirectoryPermissions(goos, path string, info os.FileInfo) doctorPrivacyAssessment {
	if goos == "windows" {
		return inspectDoctorWindowsACL(path)
	}
	if info.Mode().Perm() != 0o700 {
		return doctorPrivacyAssessment{Status: doctorStatusFail, Detail: fmt.Sprintf("%s must be a private directory with mode 0700; got %04o", path, info.Mode().Perm())}
	}
	return doctorPrivacyAssessment{Status: doctorStatusPass, Detail: fmt.Sprintf("permissions are private (%04o)", info.Mode().Perm())}
}

func prepareDoctorStateDirectory(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("state directory path is empty")
	}
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("cannot resolve %s: %w", path, err)
	}
	// #nosec G703 -- absolute is the configured application state path normalized above.
	_, inspectErr := os.Lstat(absolute)
	created := errors.Is(inspectErr, os.ErrNotExist)
	if inspectErr != nil && !created {
		return "", fmt.Errorf("cannot inspect %s: %w", path, inspectErr)
	}
	// #nosec G703 -- absolute is the configured application state path normalized above.
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return "", fmt.Errorf("cannot create %s: %w", path, err)
	}
	if created {
		// #nosec G302,G703 -- this is a normalized application state directory and must be private.
		if err := os.Chmod(absolute, 0o700); err != nil {
			return "", fmt.Errorf("cannot secure %s: %w", path, err)
		}
	}
	return absolute, nil
}

func checkRuntimeDirectory(check func() error, mpvMode string) doctorCheck {
	if strings.TrimSpace(mpvMode) == "legacy" {
		return doctorCheck{Name: "runtime", Status: doctorStatusWarn, Detail: "legacy mpv mode does not require a private IPC runtime"}
	}
	if check == nil {
		return doctorCheck{Name: "runtime", Status: doctorStatusFail, Detail: "runtime checker is unavailable"}
	}
	if err := check(); err != nil {
		return doctorCheck{Name: "runtime", Status: doctorStatusFail, Detail: err.Error()}
	}
	return doctorCheck{Name: "runtime", Status: doctorStatusPass, Detail: "private mpv IPC runtime is available"}
}
