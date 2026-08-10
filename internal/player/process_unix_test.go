//go:build !windows

package player

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestReserveRuntimeRejectsUnsafePaths(t *testing.T) {
	t.Run("symlink base", func(t *testing.T) {
		realBase := t.TempDir()
		linkBase := filepath.Join(t.TempDir(), "runtime-link")
		if err := os.Symlink(realBase, linkBase); err != nil {
			t.Fatal(err)
		}
		if _, err := reserveRuntime(Options{RuntimeBase: linkBase}); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("reserveRuntime() error = %v, want symlink rejection", err)
		}
	})

	t.Run("loose base permissions", func(t *testing.T) {
		base := t.TempDir()
		if err := os.Chmod(base, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := reserveRuntime(Options{RuntimeBase: base}); err == nil || !strings.Contains(err.Error(), "0700") {
			t.Fatalf("reserveRuntime() error = %v, want mode rejection", err)
		}
	})

	t.Run("overlong socket path", func(t *testing.T) {
		base := t.TempDir()
		longComponent := strings.Repeat("a", 80)
		longBase := filepath.Join(base, longComponent)
		if err := os.Mkdir(longBase, 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := reserveRuntime(Options{RuntimeBase: longBase}); err == nil || !strings.Contains(err.Error(), "too long") {
			t.Fatalf("reserveRuntime() error = %v, want socket length rejection", err)
		}
	})

	t.Run("safe intermediate symlink resolves to canonical base", func(t *testing.T) {
		realParent, err := os.MkdirTemp("", "imp-real-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if removeErr := os.RemoveAll(realParent); removeErr != nil {
				t.Errorf("remove real runtime parent: %v", removeErr)
			}
		})
		realBase := filepath.Join(realParent, "runtime")
		if mkdirErr := os.Mkdir(realBase, 0o700); mkdirErr != nil {
			t.Fatal(mkdirErr)
		}
		linkRoot, err := os.MkdirTemp("", "imp-link-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if removeErr := os.RemoveAll(linkRoot); removeErr != nil {
				t.Errorf("remove linked runtime parent: %v", removeErr)
			}
		})
		linkedParent := filepath.Join(linkRoot, "parent")
		if symlinkErr := os.Symlink(realParent, linkedParent); symlinkErr != nil {
			t.Skipf("symlinks unavailable: %v", symlinkErr)
		}
		reservation, err := reserveRuntime(Options{RuntimeBase: filepath.Join(linkedParent, "runtime")})
		if err != nil {
			t.Fatalf("reserveRuntime() rejected canonicalizable ancestor: %v", err)
		}
		if !strings.HasPrefix(reservation.socket, realBase+string(filepath.Separator)) {
			t.Fatalf("socket path = %q, want canonical base %q", reservation.socket, realBase)
		}
		if cleanupErr := reservation.cleanup(); cleanupErr != nil {
			t.Fatalf("cleanup runtime reservation: %v", cleanupErr)
		}
	})
}

func TestPrepareRuntimeDirectorySecuresNewXDGChildUnderRestrictiveUmask(t *testing.T) {
	const helperEnv = "IMPARTUS_TEST_RESTRICTIVE_RUNTIME_UMASK"
	if os.Getenv(helperEnv) == "1" {
		syscall.Umask(0o277)
		if _, _, err := prepareRuntimeDirectory(os.Getenv("IMPARTUS_TEST_RUNTIME_BASE")); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	base := t.TempDir()
	if err := os.Chmod(base, 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestPrepareRuntimeDirectorySecuresNewXDGChildUnderRestrictiveUmask$")
	command.Env = append(os.Environ(), helperEnv+"=1", "IMPARTUS_TEST_RUNTIME_BASE="+base)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("restrictive-umask helper failed: %v\n%s", err, output)
	}
}

func TestGeneratedRuntimePathFitsTypicalMacOSTempDirectory(t *testing.T) {
	typicalBase := filepath.Join("/private/var/folders/zz", strings.Repeat("a", 30), "T")
	socketName, err := privateSocketName("")
	if err != nil {
		t.Fatal(err)
	}
	socketPath := filepath.Join(typicalBase, temporaryRuntimePrefix+strings.Repeat("0", 10), socketName)
	if err := validateReservedSocketPath(socketPath); err != nil {
		t.Fatalf("generated socket path %q (%d bytes) is not portable: %v", socketPath, len([]byte(socketPath)), err)
	}
}

func TestStartLoadsCapabilityOnlyOverIPC(t *testing.T) {
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	loadPath := filepath.Join(t.TempDir(), "load.txt")
	options := fakeMPVOptions(t, "normal", argvPath, loadPath)

	session, err := Start(context.Background(), options)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupSession(t, session)
	socketPath := session.SocketPath()
	socketInfo, err := os.Lstat(socketPath)
	if err != nil {
		t.Fatalf("inspect live IPC socket: %v", err)
	}
	if socketInfo.Mode().Perm() != 0o600 {
		t.Fatalf("live IPC socket mode = %04o, want 0600", socketInfo.Mode().Perm())
	}
	secretURL := "http://127.0.0.1:43210/capability-secret/master.m3u8"
	if loadErr := session.Load(context.Background(), secretURL); loadErr != nil {
		t.Fatalf("Load() error = %v", loadErr)
	}
	if waitErr := session.WaitForEnd(context.Background()); waitErr != nil {
		t.Fatalf("WaitForEnd() error = %v", waitErr)
	}
	if closeErr := session.Close(context.Background()); closeErr != nil {
		t.Fatalf("Close() error = %v", closeErr)
	}

	argv, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatalf("read argv: %v", err)
	}
	argvText := string(argv)
	for _, required := range []string{"--idle=yes", "--no-config", "--load-scripts=no", "--no-terminal", "--input-ipc-server="} {
		if !strings.Contains(argvText, required) {
			t.Fatalf("argv missing %q: %s", required, argvText)
		}
	}
	if strings.Contains(argvText, "capability-secret") || strings.Contains(argvText, secretURL) {
		t.Fatalf("capability URL leaked into argv: %s", argvText)
	}
	loaded, err := os.ReadFile(loadPath)
	if err != nil {
		t.Fatalf("read loadfile capture: %v", err)
	}
	if string(loaded) != secretURL {
		t.Fatalf("loadfile URL = %q, want exact capability URL", loaded)
	}
	if _, err := os.Lstat(socketPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket remains after Close: %v", err)
	}
	if err := syscall.Kill(session.ProcessID(), 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("mpv child was not reaped: %v", err)
	}
}

func TestLoadRejectsNonLoopbackCapabilityWithoutLeakingIt(t *testing.T) {
	options := fakeMPVOptions(t, "normal", filepath.Join(t.TempDir(), "argv.txt"), filepath.Join(t.TempDir(), "load.txt"))
	session, err := Start(context.Background(), options)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupSession(t, session)

	const secret = "upstream-capability-secret"
	err = session.Load(context.Background(), "https://media.example/"+secret+"/master.m3u8")
	if err == nil || !strings.Contains(err.Error(), "invalid loopback playback URL") {
		t.Fatalf("Load() error = %v, want loopback validation failure", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("Load() error leaked rejected capability: %v", err)
	}
}

func TestCloseKillsAndReapsMPVThatIgnoresQuit(t *testing.T) {
	options := fakeMPVOptions(t, "ignore-quit", filepath.Join(t.TempDir(), "argv.txt"), filepath.Join(t.TempDir(), "load.txt"))
	options.QuitTimeout = 50 * time.Millisecond
	options.KillTimeout = time.Second
	session, err := Start(context.Background(), options)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	canaryCtx, cancelCanary := context.WithCancel(context.Background())
	canary := exec.CommandContext(canaryCtx, "sleep", "30")
	if err := canary.Start(); err != nil {
		t.Fatalf("start canary: %v", err)
	}
	t.Cleanup(func() {
		cancelCanary()
		if killErr := canary.Process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
			t.Errorf("kill canary: %v", killErr)
		}
		if waitErr := canary.Wait(); waitErr != nil {
			// A killed cleanup canary is expected to return an ExitError.
			var exitErr *exec.ExitError
			if !errors.As(waitErr, &exitErr) {
				t.Errorf("wait for canary: %v", waitErr)
			}
		}
	})

	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := syscall.Kill(session.ProcessID(), 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("ignored-quit child was not killed and reaped: %v", err)
	}
	if err := canary.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("caller-group canary was harmed: %v", err)
	}
}

func TestStartSurfacesSilentProcessDeath(t *testing.T) {
	options := fakeMPVOptions(t, "die-after-accept", filepath.Join(t.TempDir(), "argv.txt"), filepath.Join(t.TempDir(), "load.txt"))
	_, err := Start(context.Background(), options)
	if err == nil || !strings.Contains(err.Error(), "before IPC setup completed") {
		t.Fatalf("Start() error = %v, want silent death", err)
	}
}

func TestStartTimesOutAndReapsWhenSocketNeverAppears(t *testing.T) {
	options := fakeMPVOptions(t, "no-socket", filepath.Join(t.TempDir(), "argv.txt"), filepath.Join(t.TempDir(), "load.txt"))
	options.ConnectTimeout = 60 * time.Millisecond
	_, err := Start(context.Background(), options)
	if err == nil || !strings.Contains(err.Error(), "IPC readiness timeout") {
		t.Fatalf("Start() error = %v, want readiness timeout", err)
	}
	pidText, readErr := os.ReadFile(options.testPIDFile)
	if readErr != nil {
		t.Fatalf("read helper PID: %v", readErr)
	}
	pid, parseErr := strconv.Atoi(string(pidText))
	if parseErr != nil {
		t.Fatalf("parse helper PID: %v", parseErr)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("timed-out child was not reaped: %v", err)
	}
}

func TestStartCancellationReapsChildAndCleansSocket(t *testing.T) {
	options := fakeMPVOptions(t, "no-socket", filepath.Join(t.TempDir(), "argv.txt"), filepath.Join(t.TempDir(), "load.txt"))
	options.ConnectTimeout = time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := Start(ctx, options)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Start() error = %v, want context deadline", err)
	}
	pidText, readErr := os.ReadFile(options.testPIDFile)
	if readErr != nil {
		t.Fatalf("read helper PID: %v", readErr)
	}
	pid, parseErr := strconv.Atoi(string(pidText))
	if parseErr != nil {
		t.Fatalf("parse helper PID: %v", parseErr)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("canceled child was not reaped: %v", err)
	}
}

func TestLifecycleCancellationReapsChildAndCleansSocket(t *testing.T) {
	options := fakeMPVOptions(t, "spawn-child", filepath.Join(t.TempDir(), "argv.txt"), filepath.Join(t.TempDir(), "load.txt"))
	ctx, cancel := context.WithCancel(context.Background())
	session, err := Start(ctx, options)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	pid := session.ProcessID()
	socketPath := session.SocketPath()
	childPID := readProcessID(t, options.testChildPIDFile)
	t.Cleanup(func() {
		if killErr := syscall.Kill(childPID, syscall.SIGKILL); killErr != nil && !errors.Is(killErr, syscall.ESRCH) {
			t.Errorf("cleanup child process: %v", killErr)
		}
	})
	cancel()
	if waitErr := session.WaitForEnd(ctx); !errors.Is(waitErr, context.Canceled) {
		t.Fatalf("WaitForEnd() error = %v, want context cancellation", waitErr)
	}
	if closeErr := session.Close(context.Background()); closeErr != nil {
		t.Fatalf("Close() error = %v", closeErr)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("canceled lifecycle child was not reaped: %v", err)
	}
	if _, err := os.Lstat(socketPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canceled lifecycle socket remains: %v", err)
	}
	if err := waitForProcessGone(childPID, time.Second); err != nil {
		t.Fatalf("canceled lifecycle group child survived: %v", err)
	}
}

func TestWaitForEndSurfacesMPVEndFileError(t *testing.T) {
	options := fakeMPVOptions(t, "end-error", filepath.Join(t.TempDir(), "argv.txt"), filepath.Join(t.TempDir(), "load.txt"))
	session, err := Start(context.Background(), options)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupSession(t, session)
	if err := session.Load(context.Background(), "http://127.0.0.1:1234/token/master.m3u8"); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if waitErr := session.WaitForEnd(context.Background()); waitErr == nil || !strings.Contains(waitErr.Error(), "playback failed") {
		t.Fatalf("WaitForEnd() error = %v, want playback failure", waitErr)
	}
}

func TestEventsAndWaitForEndObserveTheSameTerminalEvent(t *testing.T) {
	options := fakeMPVOptions(t, "normal", filepath.Join(t.TempDir(), "argv.txt"), filepath.Join(t.TempDir(), "load.txt"))
	session, err := Start(context.Background(), options)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupSession(t, session)
	eventSeen := make(chan Event, 1)
	go func() {
		for event := range session.Events() {
			if event.Name == "end-file" {
				eventSeen <- event
				return
			}
		}
	}()
	if err := session.Load(context.Background(), "http://127.0.0.1:1234/token/master.m3u8"); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if waitErr := session.WaitForEnd(waitCtx); waitErr != nil {
		t.Fatalf("WaitForEnd() error = %v", waitErr)
	}
	select {
	case event := <-eventSeen:
		if event.Reason != "eof" {
			t.Fatalf("UI terminal event = %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("UI did not receive terminal event consumed by WaitForEnd")
	}
}

func TestWaitForEndAcceptsQuitEventBeforeImmediateIPCDisconnect(t *testing.T) {
	options := fakeMPVOptions(t, "quit-disconnect", filepath.Join(t.TempDir(), "argv.txt"), filepath.Join(t.TempDir(), "load.txt"))
	session, err := Start(context.Background(), options)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupSession(t, session)
	if err := session.Load(context.Background(), "http://127.0.0.1:1234/token/master.m3u8"); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if waitErr := session.WaitForEnd(waitCtx); waitErr != nil {
		t.Fatalf("WaitForEnd() error = %v, want clean quit before disconnect", waitErr)
	}
}

func TestPlaybackEndResultClassifiesLifecycleEvents(t *testing.T) {
	tests := []struct {
		name      string
		event     Event
		ended     bool
		errorText string
	}{
		{name: "eof", event: Event{Name: "end-file", Reason: "eof"}, ended: true},
		{name: "stop", event: Event{Name: "end-file", Reason: "stop"}, ended: true},
		{name: "quit", event: Event{Name: "end-file", Reason: "quit"}, ended: true},
		{name: "redirect", event: Event{Name: "end-file", Reason: "redirect"}},
		{name: "error", event: Event{Name: "end-file", Reason: "error"}, ended: true, errorText: "playback failed"},
		{name: "authorization error", event: Event{Name: "end-file", Reason: "error", FileError: "HTTP error 401"}, ended: true, errorText: "upstream authorization failed"},
		{name: "unexpected", event: Event{Name: "end-file", Reason: "unknown"}, ended: true, errorText: "ended unexpectedly"},
		{name: "eof reached", event: Event{Name: "property-change", Property: "eof-reached", Data: json.RawMessage("true")}},
		{name: "eof not reached", event: Event{Name: "property-change", Property: "eof-reached", Data: json.RawMessage("false")}},
		{name: "unrelated", event: Event{Name: "property-change", Property: "pause", Data: json.RawMessage("true")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ended, err := playbackEndResult(test.event)
			if ended != test.ended {
				t.Fatalf("ended = %t, want %t", ended, test.ended)
			}
			if test.errorText == "" && err != nil {
				t.Fatalf("error = %v, want nil", err)
			}
			if test.errorText != "" && (err == nil || !strings.Contains(err.Error(), test.errorText)) {
				t.Fatalf("error = %v, want %q", err, test.errorText)
			}
		})
	}
}

func TestPlaybackEndResultDoesNotEchoUntrustedFileError(t *testing.T) {
	const secret = "do-not-echo"
	_, err := playbackEndResult(Event{Name: "end-file", Reason: "error", FileError: "HTTP error 500: " + secret})
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("playbackEndResult() error = %v", err)
	}
}

func TestVerifyProcessIsolationRejectsCallerGroup(t *testing.T) {
	if err := verifyProcessIsolation(os.Getpid()); err == nil || !strings.Contains(err.Error(), "matches the caller") {
		t.Fatalf("verifyProcessIsolation(caller) error = %v", err)
	}
}

func fakeMPVOptions(t *testing.T, mode, argvPath, loadPath string) Options {
	t.Helper()
	runtimeBase, err := os.MkdirTemp("", "ipr-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if removeErr := os.RemoveAll(runtimeBase); removeErr != nil {
			t.Errorf("remove fake runtime: %v", removeErr)
		}
	})
	if err := os.Chmod(runtimeBase, 0o700); err != nil {
		t.Fatal(err)
	}
	pidPath := filepath.Join(t.TempDir(), "pid.txt")
	childPIDPath := filepath.Join(t.TempDir(), "child-pid.txt")
	return Options{
		Binary:         os.Args[0],
		RuntimeBase:    runtimeBase,
		ConnectTimeout: time.Second,
		CommandTimeout: time.Second,
		QuitTimeout:    200 * time.Millisecond,
		KillTimeout:    time.Second,
		testArgs:       []string{"-test.run=^TestMPVHelperProcess$", "--"},
		testEnvironment: []string{
			"IMPARTUS_FAKE_MPV=1",
			"IMPARTUS_FAKE_MPV_MODE=" + mode,
			"IMPARTUS_FAKE_MPV_ARGV=" + argvPath,
			"IMPARTUS_FAKE_MPV_LOAD=" + loadPath,
			"IMPARTUS_FAKE_MPV_PID=" + pidPath,
			"IMPARTUS_FAKE_MPV_CHILD_PID=" + childPIDPath,
		},
		testPIDFile:      pidPath,
		testChildPIDFile: childPIDPath,
	}
}

func TestMPVHelperProcess(t *testing.T) {
	if os.Getenv("IMPARTUS_FAKE_MPV") != "1" {
		return
	}
	os.Exit(runFakeMPV())
}

func runFakeMPV() int {
	if err := os.WriteFile(os.Getenv("IMPARTUS_FAKE_MPV_ARGV"), []byte(strings.Join(os.Args, "\n")), 0o600); err != nil {
		return 2
	}
	if err := os.WriteFile(os.Getenv("IMPARTUS_FAKE_MPV_PID"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		return 2
	}
	mode := os.Getenv("IMPARTUS_FAKE_MPV_MODE")
	if mode == "ignore-quit" {
		signal.Ignore(syscall.SIGTERM)
	}
	if mode == "no-socket" {
		select {}
	}
	socketPath := ""
	for _, argument := range os.Args {
		if strings.HasPrefix(argument, "--input-ipc-server=") {
			socketPath = strings.TrimPrefix(argument, "--input-ipc-server=")
		}
	}
	if socketPath == "" {
		return 3
	}
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(context.Background(), "unix", socketPath)
	if err != nil {
		return 4
	}
	defer func() {
		if closeErr := listener.Close(); closeErr != nil {
			return
		}
	}()
	connection, err := listener.Accept()
	if err != nil {
		return 5
	}
	defer func() {
		if closeErr := connection.Close(); closeErr != nil {
			return
		}
	}()
	if mode == "die-after-accept" {
		return 0
	}
	if mode == "spawn-child" {
		child := exec.CommandContext(context.Background(), "sleep", "30")
		if err := child.Start(); err != nil {
			return 13
		}
		if err := os.WriteFile(os.Getenv("IMPARTUS_FAKE_MPV_CHILD_PID"), []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
			return 14
		}
		defer func() {
			if killErr := child.Process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
				return
			}
			if waitErr := child.Wait(); waitErr != nil {
				return
			}
		}()
	}

	decoder := json.NewDecoder(connection)
	encoder := json.NewEncoder(connection)
	for {
		var request testRequest
		if err := decoder.Decode(&request); err != nil {
			return 0
		}
		var name string
		if len(request.Command) == 0 || json.Unmarshal(request.Command[0], &name) != nil {
			return 6
		}
		switch name {
		case "loadfile":
			var loadedURL string
			if len(request.Command) < 2 || json.Unmarshal(request.Command[1], &loadedURL) != nil {
				return 7
			}
			if err := os.WriteFile(os.Getenv("IMPARTUS_FAKE_MPV_LOAD"), []byte(loadedURL), 0o600); err != nil {
				return 8
			}
			if err := encoder.Encode(map[string]any{"request_id": request.RequestID, "error": "success"}); err != nil {
				return 9
			}
			reason := "eof"
			switch mode {
			case "end-error":
				reason = "error"
			case "quit-disconnect":
				reason = "quit"
			}
			if err := encoder.Encode(map[string]any{"event": "end-file", "reason": reason}); err != nil {
				return 10
			}
			if mode == "quit-disconnect" {
				if err := connection.Close(); err != nil {
					return 15
				}
				select {}
			}
		case "quit":
			if err := encoder.Encode(map[string]any{"request_id": request.RequestID, "error": "success"}); err != nil {
				return 11
			}
			if mode != "ignore-quit" && mode != "spawn-child" {
				return 0
			}
		default:
			if err := encoder.Encode(map[string]any{"request_id": request.RequestID, "error": "success"}); err != nil {
				return 12
			}
		}
	}
}

func readProcessID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		contents, err := os.ReadFile(path)
		if err == nil {
			pid, parseErr := strconv.Atoi(string(contents))
			if parseErr != nil {
				t.Fatalf("parse process ID from %s: %v", path, parseErr)
			}
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out reading process ID from %s", path)
	return 0
}

func waitForProcessGone(pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return nil
		}
		statusPath := filepath.Join("/proc", strconv.Itoa(pid), "stat")
		if status, err := os.ReadFile(statusPath); err == nil {
			closingParen := strings.LastIndexByte(string(status), ')')
			if closingParen >= 0 {
				fields := strings.Fields(string(status[closingParen+1:]))
				if len(fields) > 0 && fields[0] == "Z" {
					return nil
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("process %d still exists", pid)
}

func cleanupSession(t *testing.T, session *Session) {
	t.Helper()
	t.Cleanup(func() {
		if closeErr := session.Close(context.Background()); closeErr != nil {
			t.Errorf("close session: %v", closeErr)
		}
	})
}
