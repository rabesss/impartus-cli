//go:build !windows

package player

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// 103 bytes is safe across the Unix platforms supported by Go and leaves room
// for the terminating NUL in sockaddr_un.
const maxUnixSocketPathBytes = 103

func reserveRuntime(options Options) (*runtimeReservation, error) {
	directory, removeDir, err := prepareRuntimeDirectory(options.RuntimeBase)
	if err != nil {
		return nil, err
	}
	reservation := &runtimeReservation{directory: directory, removeDir: removeDir}
	socketName, err := privateSocketName(options.testSocketName)
	if err != nil {
		return nil, errors.Join(err, reservation.cleanup())
	}
	reservation.socket = filepath.Join(directory, socketName)
	if err := validateReservedSocketPath(reservation.socket); err != nil {
		return nil, errors.Join(err, reservation.cleanup())
	}
	return reservation, nil
}

func prepareRuntimeDirectory(optionBase string) (string, bool, error) {
	base := strings.TrimSpace(optionBase)
	if base == "" {
		base = strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR"))
	}
	if base == "" {
		created, err := os.MkdirTemp("", "impartus-runtime-")
		if err != nil {
			return "", false, fmt.Errorf("create private mpv runtime directory: %w", err)
		}
		// #nosec G302 -- this is a directory and the contract requires mode 0700.
		if secureErr := os.Chmod(created, 0o700); secureErr != nil {
			return "", false, errors.Join(fmt.Errorf("secure private mpv runtime directory: %w", secureErr), removeRuntimePath(created))
		}
		return validatePreparedRuntimeDirectory(created, true)
	}
	canonicalBase, err := validatePrivateDirectory(base)
	if err != nil {
		return "", false, fmt.Errorf("unsafe runtime base: %w", err)
	}
	directory := filepath.Join(canonicalBase, "impartus")
	// #nosec G703 -- base was resolved, ownership-checked, symlink-rejected, and mode-checked above.
	if err := os.Mkdir(directory, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", false, fmt.Errorf("create Impartus runtime directory: %w", err)
	}
	// The XDG child is shared by concurrent sessions; keep the empty 0700
	// directory and remove only each session's socket.
	return validatePreparedRuntimeDirectory(directory, false)
}

func validatePreparedRuntimeDirectory(directory string, removeDir bool) (string, bool, error) {
	canonicalDirectory, err := validatePrivateDirectory(directory)
	if err != nil {
		reservation := &runtimeReservation{directory: directory, removeDir: removeDir}
		return "", false, errors.Join(fmt.Errorf("unsafe Impartus runtime directory: %w", err), reservation.cleanup())
	}
	return canonicalDirectory, removeDir, nil
}

func privateSocketName(testName string) (string, error) {
	if testName != "" {
		if filepath.Base(testName) != testName || testName == "." {
			return "", errors.New("invalid mpv socket name")
		}
		return testName, nil
	}
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate private mpv socket name: %w", err)
	}
	return fmt.Sprintf("mpv-%d-%s.sock", os.Getpid(), hex.EncodeToString(random)), nil
}

func validateReservedSocketPath(socketPath string) error {
	if len([]byte(socketPath)) > maxUnixSocketPathBytes {
		return fmt.Errorf("mpv IPC socket path is too long (%d > %d bytes)", len([]byte(socketPath)), maxUnixSocketPathBytes)
	}
	// #nosec G703 -- the parent is private and verified; the basename is generated or test-validated.
	if _, err := os.Lstat(socketPath); err == nil {
		return errors.New("mpv IPC socket path already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect mpv IPC socket path: %w", err)
	}
	return nil
}

func validatePrivateDirectory(path string) (string, error) {
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("resolve runtime directory: %w", err)
	}
	leafInfo, err := os.Lstat(absolute)
	if err != nil {
		return "", err
	}
	if leafInfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("runtime directory is a symlink")
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve runtime directory symlinks: %w", err)
	}
	info, err := os.Lstat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("runtime path is not a directory")
	}
	if info.Mode().Perm() != 0o700 {
		return "", fmt.Errorf("runtime directory must have mode 0700, got %04o", info.Mode().Perm())
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", errors.New("runtime directory ownership is unavailable")
	}
	if stat.Uid != uint32(os.Getuid()) { // #nosec G115 -- UID values are unsigned platform identifiers
		return "", fmt.Errorf("runtime directory is owned by uid %d, not current uid %d", stat.Uid, os.Getuid())
	}
	return resolved, nil
}

func configureProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func verifyProcessIsolation(pid int) error {
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		return fmt.Errorf("inspect mpv process group: %w", err)
	}
	callerPGID := syscall.Getpgrp()
	if pgid == callerPGID {
		return errors.New("mpv process group unexpectedly matches the caller")
	}
	if pgid != pid {
		return fmt.Errorf("mpv process group %d does not match child pid %d", pgid, pid)
	}
	return nil
}

func signalProcessGroup(pid int, force bool) error {
	pgid, err := syscall.Getpgid(pid)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect mpv process group before termination: %w", err)
	}
	callerPGID := syscall.Getpgrp()
	if pgid <= 0 || pgid == callerPGID || pgid != pid {
		return fmt.Errorf("refusing to signal unverified mpv process group %d for pid %d", pgid, pid)
	}
	signal := syscall.SIGTERM
	if force {
		signal = syscall.SIGKILL
	}
	if err := syscall.Kill(-pgid, signal); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("signal mpv process group: %w", err)
	}
	return nil
}

func connectVerifiedIPC(ctx context.Context, socketPath string) (net.Conn, bool, error) {
	info, err := os.Lstat(socketPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("inspect mpv IPC socket: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeSocket == 0 {
		return nil, false, errors.New("mpv IPC path is not a Unix socket")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, false, errors.New("mpv IPC socket ownership is unavailable")
	}
	if stat.Uid != uint32(os.Getuid()) { // #nosec G115 -- UID values are unsigned platform identifiers
		return nil, false, fmt.Errorf("mpv IPC socket is owned by uid %d, not current uid %d", stat.Uid, os.Getuid())
	}
	// #nosec G703 -- socketPath is reserved beneath an owner-private verified directory.
	if secureErr := os.Chmod(socketPath, 0o600); secureErr != nil {
		return nil, false, fmt.Errorf("secure mpv IPC socket: %w", secureErr)
	}
	dialer := net.Dialer{}
	connection, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		// The socket can become visible just before mpv starts accepting. Treat
		// connection refusal as not-ready and retry within the caller's deadline.
		if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ENOENT) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("connect to mpv IPC socket: %w", err)
	}
	return connection, true, nil
}
