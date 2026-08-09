//go:build windows

package player

import (
	"context"
	"errors"
	"net"
	"os/exec"
)

func reserveRuntime(Options) (*runtimeReservation, error) {
	return nil, errors.New("supervised mpv JSON IPC is not supported on Windows yet")
}

func configureProcess(*exec.Cmd) {}

func verifyProcessIsolation(int) error {
	return errors.New("supervised mpv process isolation is not supported on Windows yet")
}

func signalProcessGroup(int, bool) error {
	return errors.New("supervised mpv process isolation is not supported on Windows yet")
}

func connectVerifiedIPC(context.Context, string) (net.Conn, bool, error) {
	return nil, false, errors.New("supervised mpv JSON IPC is not supported on Windows yet")
}
