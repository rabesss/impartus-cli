//go:build !windows

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
)

type doctorOwnerOverrideFileInfo struct {
	os.FileInfo
	system any
}

func (info doctorOwnerOverrideFileInfo) Sys() any { return info.system }

func TestCheckWritableStateDirectorySecuresNewPathUnderRestrictiveUmask(t *testing.T) {
	const helperEnv = "IMPARTUS_TEST_RESTRICTIVE_STATE_UMASK"
	if os.Getenv(helperEnv) == "1" {
		syscall.Umask(0o277)
		check := checkWritableStateDirectory(os.Getenv("IMPARTUS_TEST_STATE_PATH"))
		if check.Status != doctorStatusPass {
			_, _ = fmt.Fprintln(os.Stderr, check.Detail)
			os.Exit(1)
		}
		os.Exit(0)
	}

	path := t.TempDir() + "/state"
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestCheckWritableStateDirectorySecuresNewPathUnderRestrictiveUmask$")
	command.Env = append(os.Environ(), helperEnv+"=1", "IMPARTUS_TEST_STATE_PATH="+path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("restrictive-umask helper failed: %v\n%s", err, output)
	}
}

func TestDoctorStateDirectoryRejectsForeignOwner(t *testing.T) {
	t.Parallel()

	path := t.TempDir()
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("os.Stat().Sys() = %T, want *syscall.Stat_t", info.Sys())
	}
	foreign := *stat
	foreign.Uid = uint32(os.Geteuid()) + 1
	info = doctorOwnerOverrideFileInfo{FileInfo: info, system: &foreign}

	err = validateDoctorStateDirectoryOwner(path, info)
	if err == nil || !strings.Contains(err.Error(), "owned by the current user") {
		t.Fatalf("validateDoctorStateDirectoryOwner() error = %v, want owner rejection", err)
	}
}
