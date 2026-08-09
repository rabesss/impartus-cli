package player

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	defaultConnectTimeout = 5 * time.Second
	defaultQuitTimeout    = 2 * time.Second
	defaultKillTimeout    = 2 * time.Second
)

// ObservedProperties is the stable property set forwarded to the TUI.
var ObservedProperties = []string{
	"pause",
	"time-pos",
	"duration",
	"volume",
	"mute",
	"speed",
	"playlist-pos",
	"core-idle",
	"eof-reached",
}

// Options configures a supervised mpv process. RuntimeBase represents
// XDG_RUNTIME_DIR; when empty, the environment or an owner-private temporary
// directory is used.
type Options struct {
	Binary         string
	RuntimeBase    string
	ConnectTimeout time.Duration
	CommandTimeout time.Duration
	QuitTimeout    time.Duration
	KillTimeout    time.Duration
	VideoOutput    string

	// Tests use the current test binary as a deterministic fake mpv. Production
	// callers cannot set these fields from outside this package.
	testArgs         []string
	testEnvironment  []string
	testSocketName   string
	testPIDFile      string
	testChildPIDFile string
}

type runtimeReservation struct {
	directory  string
	socket     string
	removeDir  bool
	once       sync.Once
	cleanupErr error
}

func (reservation *runtimeReservation) cleanup() error {
	if reservation == nil {
		return nil
	}
	reservation.once.Do(func() {
		if reservation.socket != "" {
			reservation.cleanupErr = removeRuntimePath(reservation.socket)
		}
		if reservation.removeDir {
			reservation.cleanupErr = errors.Join(reservation.cleanupErr, removeRuntimePath(reservation.directory))
		}
	})
	return reservation.cleanupErr
}

func removeRuntimePath(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Session owns one mpv child, its process group, private IPC socket, and JSON
// client. Close always attempts to reap the child and is idempotent.
type Session struct {
	options      Options
	command      *exec.Cmd
	client       *Client
	runtime      *runtimeReservation
	lifecycle    context.Context
	processDone  chan struct{}
	events       chan Event
	playbackEnd  chan error
	eventMutex   sync.Mutex
	eventsClosed bool

	waitMutex sync.Mutex
	waitErr   error

	closeOnce sync.Once
	closeErr  error
}

// CheckBinary verifies that the configured mpv executable can be resolved.
func CheckBinary(binary string) error {
	if strings.TrimSpace(binary) == "" {
		binary = "mpv"
	}
	if _, err := exec.LookPath(binary); err != nil {
		return errors.New("please add mpv to your path")
	}
	return nil
}

// CheckRuntime verifies that a private mpv IPC path can be safely reserved and
// cleaned without launching a process. runtimeBase has the same semantics as
// Options.RuntimeBase.
func CheckRuntime(runtimeBase string) error {
	reservation, err := reserveRuntime(Options{RuntimeBase: runtimeBase})
	if err != nil {
		return err
	}
	return reservation.cleanup()
}

// Start launches mpv idle, connects to its private JSON IPC socket, and
// subscribes to the stable playback property set. No media URL is accepted by
// this function, so a capability URL cannot enter process argv.
func Start(ctx context.Context, options Options) (*Session, error) {
	if ctx == nil {
		return nil, errors.New("mpv start context is required")
	}
	options = normalizeOptions(options)
	if err := CheckBinary(options.Binary); err != nil {
		return nil, err
	}
	runtime, err := reserveRuntime(options)
	if err != nil {
		return nil, err
	}

	arguments := append([]string(nil), options.testArgs...)
	arguments = append(arguments,
		"--idle=yes",
		"--no-config",
		"--load-scripts=no",
		"--no-terminal",
		"--force-window=yes",
		"--input-ipc-server="+runtime.socket,
	)
	if options.VideoOutput != "" {
		arguments = append(arguments, "--vo="+options.VideoOutput)
	}

	// Process-group supervision owns lifecycle cancellation. Using a detached
	// command context prevents os/exec's default Cancel from killing only the
	// leader PID and orphaning same-group descendants.
	command := exec.CommandContext(context.WithoutCancel(ctx), options.Binary, arguments...) // #nosec G204 -- binary is the explicit local mpv executable; arguments are fixed controls
	configureProcess(command)
	command.Stdin = nil
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	command.Env = append(os.Environ(), options.testEnvironment...)
	if startErr := command.Start(); startErr != nil {
		return nil, errors.Join(fmt.Errorf("start mpv: %w", startErr), runtime.cleanup())
	}

	session := &Session{
		options:     options,
		command:     command,
		runtime:     runtime,
		lifecycle:   ctx,
		processDone: make(chan struct{}),
		events:      make(chan Event, defaultEventBuffer),
		playbackEnd: make(chan error, 1),
	}
	go session.reapProcess()
	if isolationErr := verifyProcessIsolation(command.Process.Pid); isolationErr != nil {
		killErr := command.Process.Kill()
		<-session.processDone
		return nil, errors.Join(isolationErr, killErr, runtime.cleanup())
	}

	connection, err := session.connect(ctx)
	if err != nil {
		return nil, errors.Join(err, session.abortStartup())
	}
	session.client = NewClient(connection, ClientOptions{
		CommandTimeout: options.CommandTimeout,
		eventHandler:   session.acceptEvent,
	})
	go session.closeEventsOnDisconnect()
	if err := session.observeProperties(ctx); err != nil {
		died := session.processFinished()
		abortErr := session.abortStartup()
		if died || errors.Is(err, ErrDisconnected) {
			return nil, errors.Join(errors.New("mpv exited before IPC setup completed"), abortErr)
		}
		return nil, errors.Join(fmt.Errorf("configure mpv IPC: %w", err), abortErr)
	}
	go session.closeOnLifecycleCancellation(ctx)
	return session, nil
}

func normalizeOptions(options Options) Options {
	if strings.TrimSpace(options.Binary) == "" {
		options.Binary = "mpv"
	}
	if options.ConnectTimeout <= 0 {
		options.ConnectTimeout = defaultConnectTimeout
	}
	if options.CommandTimeout <= 0 {
		options.CommandTimeout = defaultCommandTimeout
	}
	if options.QuitTimeout <= 0 {
		options.QuitTimeout = defaultQuitTimeout
	}
	if options.KillTimeout <= 0 {
		options.KillTimeout = defaultKillTimeout
	}
	options.VideoOutput = strings.TrimSpace(options.VideoOutput)
	return options
}

func (session *Session) connect(ctx context.Context) (net.Conn, error) {
	connectCtx, cancel := context.WithTimeout(ctx, session.options.ConnectTimeout)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if session.processFinished() {
			return nil, errors.New("mpv exited before IPC socket became ready")
		}
		connection, exists, err := connectVerifiedIPC(connectCtx, session.runtime.socket)
		if err != nil {
			return nil, err
		}
		if exists && connection != nil {
			return connection, nil
		}
		select {
		case <-connectCtx.Done():
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if errors.Is(connectCtx.Err(), context.DeadlineExceeded) {
				return nil, errors.New("mpv IPC readiness timeout")
			}
			return nil, connectCtx.Err()
		case <-session.processDone:
			return nil, errors.New("mpv exited before IPC socket became ready")
		case <-ticker.C:
		}
	}
}

func (session *Session) observeProperties(ctx context.Context) error {
	for index, property := range ObservedProperties {
		if _, err := session.client.Command(ctx, "observe_property", index+1, property); err != nil {
			return err
		}
	}
	return nil
}

// Load delivers a loopback capability URL exclusively through JSON IPC.
func (session *Session) Load(ctx context.Context, playbackURL string) error {
	if !validPlaybackURL(playbackURL) {
		return errors.New("invalid loopback playback URL")
	}
	if _, err := session.client.Command(ctx, "loadfile", playbackURL, "replace"); err != nil {
		return fmt.Errorf("load playback URL through mpv IPC: %w", err)
	}
	return nil
}

func validPlaybackURL(raw string) bool {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.Port() == "" {
		return false
	}
	return parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" && strings.HasSuffix(parsed.Path, "/master.m3u8")
}

// Events exposes a bounded copy of mpv property and lifecycle events. Internal
// completion detection has its own channel, so a UI cannot consume the event
// that WaitForEnd needs (or vice versa).
func (session *Session) Events() <-chan Event { return session.events }

func (session *Session) acceptEvent(event Event) {
	session.eventMutex.Lock()
	defer session.eventMutex.Unlock()
	if ended, endErr := playbackEndResult(event); ended {
		select {
		case session.playbackEnd <- endErr:
		default:
		}
	}
	if session.eventsClosed {
		return
	}
	publishNewestEvent(session.events, event)
}

func (session *Session) closeEventsOnDisconnect() {
	<-session.client.Done()
	session.eventMutex.Lock()
	defer session.eventMutex.Unlock()
	if session.eventsClosed {
		return
	}
	session.eventsClosed = true
	close(session.events)
}

func publishNewestEvent(events chan Event, event Event) {
	select {
	case events <- event:
		return
	default:
	}
	select {
	case <-events:
	default:
	}
	select {
	case events <- event:
	default:
	}
}

// WaitForEnd waits for normal media completion, a clean user-initiated mpv
// exit, context cancellation, or an IPC/process failure.
func (session *Session) WaitForEnd(ctx context.Context) error {
	if ctx == nil {
		return errors.New("playback wait context is required")
	}
	for {
		select {
		case endErr := <-session.playbackEnd:
			return endErr
		case <-session.client.Done():
			select {
			case endErr := <-session.playbackEnd:
				return endErr
			default:
			}
			if contextErr := ctx.Err(); contextErr != nil {
				return contextErr
			}
			if session.processFinished() {
				return session.processCompletion(ctx)
			}
			return session.client.Err()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (session *Session) processCompletion(ctx context.Context) error {
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	return session.processError()
}

func playbackEndResult(event Event) (bool, error) {
	if event.Name == "end-file" {
		switch event.Reason {
		case "eof", "stop", "quit":
			return true, nil
		case "redirect":
			// mpv replaces playlist-like entries with their expanded entries and
			// continues playback after this event.
			return false, nil
		case "error":
			return true, errors.New("mpv playback failed")
		default:
			return true, errors.New("mpv playback ended unexpectedly")
		}
	}
	if event.Name != "property-change" || event.Property != "eof-reached" {
		return false, nil
	}
	var reached bool
	return json.Unmarshal(event.Data, &reached) == nil && reached, nil
}

func (session *Session) closeOnLifecycleCancellation(ctx context.Context) {
	select {
	case <-ctx.Done():
		if closeErr := session.Close(context.WithoutCancel(ctx)); closeErr != nil {
			return
		}
	case <-session.processDone:
	}
}

// Pause changes mpv's pause property.
func (session *Session) Pause(ctx context.Context, paused bool) error {
	_, err := session.client.Command(ctx, "set_property", "pause", paused)
	return err
}

// SeekRelative seeks by a signed number of seconds.
func (session *Session) SeekRelative(ctx context.Context, seconds float64) error {
	_, err := session.client.Command(ctx, "seek", seconds, "relative")
	return err
}

// SetVolume sets mpv volume in percent.
func (session *Session) SetVolume(ctx context.Context, volume float64) error {
	_, err := session.client.Command(ctx, "set_property", "volume", volume)
	return err
}

// SetMute changes mpv's mute property.
func (session *Session) SetMute(ctx context.Context, muted bool) error {
	_, err := session.client.Command(ctx, "set_property", "mute", muted)
	return err
}

// SetSpeed changes playback speed.
func (session *Session) SetSpeed(ctx context.Context, speed float64) error {
	_, err := session.client.Command(ctx, "set_property", "speed", speed)
	return err
}

// CycleVideo advances mpv to the next video track/view.
func (session *Session) CycleVideo(ctx context.Context) error {
	_, err := session.client.Command(ctx, "cycle", "vid")
	return err
}

// ProcessID returns the supervised child PID for diagnostics and tests.
func (session *Session) ProcessID() int { return session.command.Process.Pid }

// SocketPath returns the private IPC path without any media capability.
func (session *Session) SocketPath() string { return session.runtime.socket }

// Close gracefully quits mpv, then terminates only its verified private process
// group if it ignores quit. Cleanup and reaping remain bounded even if ctx is
// already canceled.
func (session *Session) Close(_ context.Context) error {
	session.closeOnce.Do(func() {
		session.closeErr = session.closeInternal()
	})
	return session.closeErr
}

func (session *Session) closeInternal() error {
	if !session.processFinished() && session.client != nil {
		session.requestQuit()
	}

	killed := false
	if !waitForDone(session.processDone, session.options.QuitTimeout) {
		if err := signalProcessGroup(session.ProcessID(), false); err != nil {
			return session.killExactChildAndReap(err)
		}
		killed = true
		if !waitForDone(session.processDone, session.options.KillTimeout/2) {
			if err := signalProcessGroup(session.ProcessID(), true); err != nil {
				return session.killExactChildAndReap(err)
			}
			if !waitForDone(session.processDone, session.options.KillTimeout) {
				return session.finishClose(errors.New("mpv child did not exit after forced termination"))
			}
		}
	}
	cleanupErr := session.runtime.cleanup()
	if session.client != nil {
		cleanupErr = errors.Join(session.client.Close(), cleanupErr)
	}
	if killed {
		return cleanupErr
	}
	if session.lifecycle != nil && session.lifecycle.Err() != nil {
		return cleanupErr
	}
	return errors.Join(session.processError(), cleanupErr)
}

func (session *Session) requestQuit() {
	quitCtx, cancel := context.WithTimeout(context.Background(), session.options.QuitTimeout)
	defer cancel()
	if _, err := session.client.Command(quitCtx, "quit"); err != nil {
		// Shutdown still proceeds to the verified process-group fallback. A lost
		// reply is expected when mpv exits immediately after accepting quit.
		return
	}
}

func (session *Session) killExactChildAndReap(cause error) error {
	killErr := session.command.Process.Kill()
	if killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
		cause = errors.Join(cause, fmt.Errorf("kill exact mpv child: %w", killErr))
	}
	if !waitForDone(session.processDone, session.options.KillTimeout) {
		cause = errors.Join(cause, errors.New("mpv child could not be reaped after exact-process kill"))
	}
	return session.finishClose(cause)
}

func (session *Session) finishClose(err error) error {
	if session.client != nil {
		err = errors.Join(err, session.client.Close())
	}
	return errors.Join(err, session.runtime.cleanup())
}

func (session *Session) abortStartup() error {
	var result error
	if session.client != nil {
		result = errors.Join(result, session.client.Close())
	}
	if !session.processFinished() {
		result = errors.Join(result, signalProcessGroup(session.ProcessID(), true))
		if !waitForDone(session.processDone, session.options.KillTimeout) {
			killErr := session.command.Process.Kill()
			if killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
				result = errors.Join(result, fmt.Errorf("kill exact mpv child during startup cleanup: %w", killErr))
			}
			if !waitForDone(session.processDone, session.options.KillTimeout) {
				result = errors.Join(result, errors.New("mpv child could not be reaped during startup cleanup"))
			}
		}
	}
	return errors.Join(result, session.runtime.cleanup())
}

func (session *Session) reapProcess() {
	err := session.command.Wait()
	session.waitMutex.Lock()
	session.waitErr = err
	session.waitMutex.Unlock()
	close(session.processDone)
}

func (session *Session) processFinished() bool {
	select {
	case <-session.processDone:
		return true
	default:
		return false
	}
}

func (session *Session) processError() error {
	session.waitMutex.Lock()
	defer session.waitMutex.Unlock()
	return session.waitErr
}

func waitForDone(done <-chan struct{}, timeout time.Duration) bool {
	if timeout <= 0 {
		timeout = time.Millisecond
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}
