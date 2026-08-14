//go:build windows

package tuihost

import (
	"os"
	"os/exec"
	"time"
)

const childShutdownGrace = 2 * time.Second

// Windows does not implement os.Interrupt for arbitrary child processes. The
// child normally exits through its raw-terminal key handler; parent context
// cancellation uses the bounded os/exec kill path.
func configureCancellation(command *exec.Cmd) {
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		return command.Process.Kill()
	}
	command.WaitDelay = childShutdownGrace
}
