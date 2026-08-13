//go:build !windows

package tuihost

import (
	"errors"
	"os"
	"os/exec"
	"time"
)

const childShutdownGrace = 2 * time.Second

// configureCancellation gives the terminal-owning child a short opportunity
// to restore terminal state before os/exec force-kills it.
func configureCancellation(command *exec.Cmd) {
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		err := command.Process.Signal(os.Interrupt)
		if errors.Is(err, os.ErrProcessDone) {
			return os.ErrProcessDone
		}
		return err
	}
	command.WaitDelay = childShutdownGrace
}
