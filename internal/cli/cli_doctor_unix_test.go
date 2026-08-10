//go:build !windows

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
)

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
