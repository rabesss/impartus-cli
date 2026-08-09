package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rabesss/impartus-cli/internal/config"
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
	checkRuntime func() error
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
		checkRuntime: func() error { return player.CheckRuntime("") },
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
	checks := make([]doctorCheck, 0, 6)
	checks = append(checks, checkExecutable(options.lookPath, "mpv"))
	checks = append(checks, checkExecutable(options.lookPath, "ffmpeg"))
	checks = append(checks, checkConfigFile(options.configPath))
	checks = append(checks, checkTokenFile(options.tokenPath))
	checks = append(checks, checkWritableStateDirectory(options.stateDir))
	checks = append(checks, checkRuntimeDirectory(options.checkRuntime))

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
	if info.Mode().Perm()&0o077 != 0 {
		return doctorCheck{Name: "token", Status: doctorStatusFail, Detail: fmt.Sprintf("%s contains a bearer token and must use mode 0600 or stricter; got %04o", path, info.Mode().Perm())}
	}
	return doctorCheck{Name: "token", Status: doctorStatusPass, Detail: fmt.Sprintf("%s permissions are private (%04o)", path, info.Mode().Perm())}
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
	if info.Mode().Perm()&0o077 != 0 {
		return doctorCheck{Name: "config", Status: doctorStatusFail, Detail: fmt.Sprintf("%s contains credentials and must use mode 0600 or stricter; got %04o", path, info.Mode().Perm())}
	}
	return doctorCheck{Name: "config", Status: doctorStatusPass, Detail: fmt.Sprintf("%s permissions are private (%04o)", path, info.Mode().Perm())}
}

func checkWritableStateDirectory(path string) doctorCheck {
	if strings.TrimSpace(path) == "" {
		return doctorCheck{Name: "state", Status: doctorStatusFail, Detail: "state directory path is empty"}
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return doctorCheck{Name: "state", Status: doctorStatusFail, Detail: fmt.Sprintf("cannot create %s: %v", path, err)}
	}
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return doctorCheck{Name: "state", Status: doctorStatusFail, Detail: fmt.Sprintf("cannot resolve %s: %v", path, err)}
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return doctorCheck{Name: "state", Status: doctorStatusFail, Detail: fmt.Sprintf("cannot inspect %s: %v", path, err)}
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return doctorCheck{Name: "state", Status: doctorStatusFail, Detail: fmt.Sprintf("%s must be a private directory with mode 0700; got %04o", path, info.Mode().Perm())}
	}
	probe, err := os.CreateTemp(absolute, ".doctor-write-")
	if err != nil {
		return doctorCheck{Name: "state", Status: doctorStatusFail, Detail: fmt.Sprintf("%s is not writable: %v", path, err)}
	}
	probeName := probe.Name()
	if chmodErr := probe.Chmod(0o600); chmodErr != nil {
		cleanupErr := errors.Join(probe.Close(), os.Remove(probeName))
		detail := fmt.Sprintf("cannot secure files in %s: %v", path, chmodErr)
		if cleanupErr != nil {
			detail += fmt.Sprintf("; cleanup also failed: %v", cleanupErr)
		}
		return doctorCheck{Name: "state", Status: doctorStatusFail, Detail: detail}
	}
	closeErr := probe.Close()
	removeErr := os.Remove(probeName)
	if closeErr != nil || removeErr != nil {
		return doctorCheck{Name: "state", Status: doctorStatusFail, Detail: fmt.Sprintf("write probe cleanup failed in %s", path)}
	}
	return doctorCheck{Name: "state", Status: doctorStatusPass, Detail: fmt.Sprintf("%s is private and writable", path)}
}

func checkRuntimeDirectory(check func() error) doctorCheck {
	if check == nil {
		return doctorCheck{Name: "runtime", Status: doctorStatusFail, Detail: "runtime checker is unavailable"}
	}
	if err := check(); err != nil {
		return doctorCheck{Name: "runtime", Status: doctorStatusFail, Detail: err.Error()}
	}
	return doctorCheck{Name: "runtime", Status: doctorStatusPass, Detail: "private mpv IPC runtime is available"}
}
