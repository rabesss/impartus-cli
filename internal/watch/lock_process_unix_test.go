//go:build !windows

package watch

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestWatcherLockIsReleasedAfterSIGKILL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "watch.lock")
	command := exec.CommandContext(context.Background(), os.Args[0], "-test.run=TestWatcherLockHelperProcess") // #nosec G204 -- test binary and fixed argument
	command.Env = append(os.Environ(), "IMPARTUS_TEST_WATCH_LOCK_HELPER=1", "IMPARTUS_TEST_WATCH_LOCK_PATH="+path)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if startErr := command.Start(); startErr != nil {
		t.Fatal(startErr)
	}
	reader := bufio.NewReader(stdout)
	line, err := reader.ReadString('\n')
	if err != nil || line != "locked\n" {
		killErr := command.Process.Kill()
		if killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
			t.Errorf("kill unready helper: %v", killErr)
		}
		if waitErr := command.Wait(); waitErr != nil && !strings.Contains(waitErr.Error(), "signal: killed") {
			t.Errorf("wait for unready helper: %v", waitErr)
		}
		t.Fatalf("helper readiness = %q, %v", line, err)
	}
	if signalErr := command.Process.Signal(syscall.SIGKILL); signalErr != nil {
		t.Fatal(signalErr)
	}
	if waitErr := command.Wait(); waitErr == nil {
		t.Fatal("SIGKILLed helper exited successfully")
	}

	lock, err := AcquireLock(path)
	if err != nil {
		t.Fatalf("AcquireLock(after SIGKILL) error = %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestWatcherLockHelperProcess(t *testing.T) {
	if os.Getenv("IMPARTUS_TEST_WATCH_LOCK_HELPER") != "1" {
		return
	}
	lock, err := AcquireLock(os.Getenv("IMPARTUS_TEST_WATCH_LOCK_PATH"))
	if err != nil {
		os.Exit(2)
	}
	defer func() {
		if closeErr := lock.Close(); closeErr != nil {
			t.Errorf("Close(helper lock) error = %v", closeErr)
		}
	}()
	if _, writeErr := fmt.Fprintln(os.Stdout, "locked"); writeErr != nil {
		t.Fatal(writeErr)
	}
	select {}
}
