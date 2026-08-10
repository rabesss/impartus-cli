package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/artifact"
	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/events"
	"github.com/rabesss/impartus-cli/internal/library"
	"github.com/rabesss/impartus-cli/internal/watch"
)

func TestParseWatchFlagsKeepsGenericSurface(t *testing.T) {
	t.Parallel()

	flags, err := parseWatchFlags([]string{
		"-s", "67", "-S", "8", "--interval", "10m", "--once", "--dry-run", "--events", "--force", "-o", "/tmp/watch",
	})
	if err != nil {
		t.Fatalf("parseWatchFlags() error = %v", err)
	}
	if flags.subject != 67 || flags.session != 8 || flags.interval != "10m" || !flags.once || !flags.dryRun || !flags.events || !flags.force || flags.output != "/tmp/watch" {
		t.Fatalf("flags = %+v", flags)
	}
	for _, forbidden := range [][]string{{"--upload"}, {"--check"}, {"--notebook", "id"}, {"--state", "file"}} {
		if _, err := parseWatchFlags(forbidden); err == nil {
			t.Fatalf("parseWatchFlags(%v) error = nil", forbidden)
		}
	}
}

func TestParseWatchFlagsRejectsNonpositiveExplicitTargets(t *testing.T) {
	t.Parallel()

	for _, arguments := range [][]string{
		{"--subject=-1", "--session=-2"},
		{"--subject=0", "--session=2"},
		{"--subject=1", "--session=0"},
		{"-s=-1", "-S=2"},
	} {
		if _, err := parseWatchFlags(arguments); err == nil || !strings.Contains(err.Error(), "positive") {
			t.Fatalf("parseWatchFlags(%v) error = %v, want positive-ID error", arguments, err)
		}
	}
}

func TestRootDispatchRoutesWatchInHumanAndJSONModes(t *testing.T) {
	restoreCLIState(t)
	humanArgs := []string(nil)
	runWatchFn = func(args []string) error {
		humanArgs = append([]string(nil), args...)
		return nil
	}
	if err := executeHuman([]string{"watch", "--once"}, "dev", ""); err != nil {
		t.Fatalf("executeHuman(watch) error = %v", err)
	}
	if !reflect.DeepEqual(humanArgs, []string{"--once"}) {
		t.Fatalf("human watch args = %v", humanArgs)
	}

	runWatchJSONFn = func(args []string) (watchResult, error) {
		if !reflect.DeepEqual(args, []string{"--once"}) {
			t.Fatalf("JSON watch args = %v", args)
		}
		return watchResult{Status: "completed"}, nil
	}
	output, err := captureStdout(t, func() error { return executeJSON([]string{"watch", "--once"}, "dev", "") })
	if err != nil || !strings.Contains(output, `"command":"watch"`) {
		t.Fatalf("JSON watch output = %q, error = %v", output, err)
	}
}

func TestWatchJSONAndEventsAreMutuallyExclusive(t *testing.T) {
	t.Parallel()

	_, err := executeWatchWithDependencies(context.Background(), []string{"--events", "-s", "1", "-S", "2"}, true, io.Discard, io.Discard, watchExecutionDependencies{})
	if err == nil || !strings.Contains(err.Error(), "cannot combine --json and --events") {
		t.Fatalf("executeWatchWithDependencies() error = %v", err)
	}
}

func TestWatchJSONAndNumericTrueEventsAreMutuallyExclusive(t *testing.T) {
	t.Parallel()

	_, err := executeWatchWithDependencies(context.Background(), []string{"--events=1", "-s", "1", "-S", "2"}, true, io.Discard, io.Discard, watchExecutionDependencies{})
	if err == nil || !strings.Contains(err.Error(), "cannot combine --json and --events") {
		t.Fatalf("executeWatchWithDependencies() error = %v", err)
	}
}

func TestWatchPreflightRecoversUnderLockBeforeLogin(t *testing.T) {
	t.Parallel()

	store := openCLIWatchStore(t)
	cfg := cliWatchConfig(t)
	order := make([]string, 0, 6)
	deps := watchExecutionDependencies{
		loadConfig:         func() (*config.Config, error) { return cfg, nil },
		defaultLibraryPath: func() (string, error) { return filepath.Join(t.TempDir(), "library.db"), nil },
		acquireLock: func(string) (io.Closer, error) {
			order = append(order, "lock")
			return closerFunc(func() error { return nil }), nil
		},
		openLibrary: func(context.Context, library.Options) (*library.Store, error) {
			order = append(order, "open")
			return store, nil
		},
		recoverJobs: func(context.Context, *library.Store) (library.RecoveryResult, error) {
			order = append(order, "recover")
			return library.RecoveryResult{}, nil
		},
		ensureFFmpeg: func() error { order = append(order, "ffmpeg"); return nil },
		login: func(context.Context, *config.Config) (*client.Client, error) {
			order = append(order, "login")
			return &client.Client{}, nil
		},
		run: func(context.Context, *config.Config, *client.Client, *library.Store, watch.Options) (watch.CycleResult, error) {
			order = append(order, "run")
			return watch.CycleResult{}, nil
		},
		now: time.Now,
	}
	if _, err := executeWatchWithDependencies(context.Background(), []string{"--once", "-s", "67", "-S", "8"}, false, io.Discard, io.Discard, deps); err != nil {
		t.Fatalf("executeWatchWithDependencies() error = %v", err)
	}
	want := []string{"lock", "open", "recover", "ffmpeg", "login", "run"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
}

func TestWatchDryRunSkipsDurableRecoveryAndFFmpegPreflight(t *testing.T) {
	t.Parallel()

	store := openCLIWatchStore(t)
	cfg := cliWatchConfig(t)
	order := make([]string, 0, 5)
	deps := watchExecutionDependencies{
		loadConfig:         func() (*config.Config, error) { return cfg, nil },
		defaultLibraryPath: func() (string, error) { return filepath.Join(t.TempDir(), "library.db"), nil },
		acquireLock: func(string) (io.Closer, error) {
			order = append(order, "lock")
			return closerFunc(func() error { return nil }), nil
		},
		openLibrary: func(context.Context, library.Options) (*library.Store, error) {
			order = append(order, "open")
			return store, nil
		},
		recoverJobs: func(context.Context, *library.Store) (library.RecoveryResult, error) {
			order = append(order, "recover")
			return library.RecoveryResult{}, nil
		},
		ensureFFmpeg: func() error { order = append(order, "ffmpeg"); return nil },
		login: func(context.Context, *config.Config) (*client.Client, error) {
			order = append(order, "login")
			return &client.Client{}, nil
		},
		run: func(context.Context, *config.Config, *client.Client, *library.Store, watch.Options) (watch.CycleResult, error) {
			order = append(order, "run")
			return watch.CycleResult{DryRun: true}, nil
		},
		now: time.Now,
	}
	if _, err := executeWatchWithDependencies(context.Background(), []string{"--dry-run", "-s", "67", "-S", "8"}, false, io.Discard, io.Discard, deps); err != nil {
		t.Fatalf("executeWatchWithDependencies() error = %v", err)
	}
	want := []string{"lock", "open", "login", "run"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
}

func TestWatchEventsEmitFailedTerminalForLoginFailure(t *testing.T) {
	t.Parallel()

	store := openCLIWatchStore(t)
	cfg := cliWatchConfig(t)
	deps := watchExecutionDependencies{
		loadConfig:         func() (*config.Config, error) { return cfg, nil },
		defaultLibraryPath: func() (string, error) { return filepath.Join(t.TempDir(), "library.db"), nil },
		acquireLock:        func(string) (io.Closer, error) { return closerFunc(func() error { return nil }), nil },
		openLibrary:        func(context.Context, library.Options) (*library.Store, error) { return store, nil },
		recoverJobs: func(context.Context, *library.Store) (library.RecoveryResult, error) {
			return library.RecoveryResult{}, nil
		},
		ensureFFmpeg: func() error { return nil },
		login: func(context.Context, *config.Config) (*client.Client, error) {
			return nil, errors.New("authentication expired")
		},
		now: func() time.Time { return time.Unix(1, 0).UTC() },
	}
	var output bytes.Buffer
	_, err := executeWatchWithDependencies(context.Background(), []string{"--events", "--once", "-s", "67", "-S", "8"}, false, &output, io.Discard, deps)
	if err == nil || !strings.Contains(err.Error(), "authentication expired") {
		t.Fatalf("executeWatchWithDependencies() error = %v", err)
	}
	decoded := decodeCLIEvents(t, output.String())
	if len(decoded) != 1 || decoded[0].Type != events.JobFailed {
		t.Fatalf("events = %+v", decoded)
	}
}

func TestWatchEventsRedactReturnedLoginFailure(t *testing.T) {
	t.Parallel()

	store := openCLIWatchStore(t)
	cfg := cliWatchConfig(t)
	deps := watchExecutionDependencies{
		loadConfig:         func() (*config.Config, error) { return cfg, nil },
		defaultLibraryPath: func() (string, error) { return filepath.Join(t.TempDir(), "library.db"), nil },
		acquireLock:        func(string) (io.Closer, error) { return closerFunc(func() error { return nil }), nil },
		openLibrary:        func(context.Context, library.Options) (*library.Store, error) { return store, nil },
		recoverJobs: func(context.Context, *library.Store) (library.RecoveryResult, error) {
			return library.RecoveryResult{}, nil
		},
		ensureFFmpeg: func() error { return nil },
		login: func(context.Context, *config.Config) (*client.Client, error) {
			return nil, errors.New("Authorization: Token secret-value")
		},
		now: func() time.Time { return time.Unix(1, 0).UTC() },
	}
	var output bytes.Buffer
	_, err := executeWatchWithDependencies(context.Background(), []string{"--events", "--once", "-s", "67", "-S", "8"}, false, &output, io.Discard, deps)
	if err == nil || strings.Contains(err.Error(), "secret-value") || !strings.Contains(err.Error(), "REDACTED") {
		t.Fatalf("returned login failure = %v", err)
	}
}

func TestWatchRecoveryFailureIsDurableAndFailedEvenWhenCanceled(t *testing.T) {
	t.Parallel()

	store := openCLIWatchStore(t)
	cfg := cliWatchConfig(t)
	deps := watchExecutionDependencies{
		loadConfig:         func() (*config.Config, error) { return cfg, nil },
		defaultLibraryPath: func() (string, error) { return filepath.Join(t.TempDir(), "library.db"), nil },
		acquireLock:        func(string) (io.Closer, error) { return closerFunc(func() error { return nil }), nil },
		openLibrary:        func(context.Context, library.Options) (*library.Store, error) { return store, nil },
		recoverJobs: func(context.Context, *library.Store) (library.RecoveryResult, error) {
			return library.RecoveryResult{}, errors.Join(context.Canceled, errors.New("recovery commit failed"))
		},
		now: func() time.Time { return time.Unix(1, 0).UTC() },
	}
	var output bytes.Buffer
	_, err := executeWatchWithDependencies(context.Background(), []string{"--events", "--once", "-s", "67", "-S", "8"}, false, &output, io.Discard, deps)
	if !errors.Is(err, watch.ErrDurableState) || ExitCode(watchCommandError(err)) != 1 {
		t.Fatalf("recovery error = %v, exit = %d", err, ExitCode(watchCommandError(err)))
	}
	decoded := decodeCLIEvents(t, output.String())
	if len(decoded) != 1 || decoded[0].Type != events.JobFailed {
		t.Fatalf("events = %+v", decoded)
	}
}

func TestWatchCleanupFailurePrecedesSingleFailedTerminal(t *testing.T) {
	t.Parallel()

	store := openCLIWatchStore(t)
	cfg := cliWatchConfig(t)
	closeCalls := 0
	deps := watchExecutionDependencies{
		loadConfig:         func() (*config.Config, error) { return cfg, nil },
		defaultLibraryPath: func() (string, error) { return filepath.Join(t.TempDir(), "library.db"), nil },
		acquireLock: func(string) (io.Closer, error) {
			return closerFunc(func() error {
				closeCalls++
				return errors.New("lock release failed")
			}), nil
		},
		openLibrary: func(context.Context, library.Options) (*library.Store, error) { return store, nil },
		recoverJobs: func(context.Context, *library.Store) (library.RecoveryResult, error) {
			return library.RecoveryResult{}, nil
		},
		ensureFFmpeg: func() error { return nil },
		login:        func(context.Context, *config.Config) (*client.Client, error) { return &client.Client{}, nil },
		run: func(_ context.Context, _ *config.Config, _ *client.Client, _ *library.Store, options watch.Options) (watch.CycleResult, error) {
			if !options.DeferTerminal {
				t.Fatal("CLI did not retain terminal-event ownership")
			}
			if err := options.Emitter.Emit(events.Event{Type: events.JobStarted, JobID: options.JobID, Command: "watch", Timestamp: time.Unix(1, 0).UTC()}); err != nil {
				t.Fatal(err)
			}
			return watch.CycleResult{Downloaded: 1}, nil
		},
		now: func() time.Time { return time.Unix(2, 0).UTC() },
	}
	var output bytes.Buffer
	_, err := executeWatchWithDependencies(context.Background(), []string{"--events", "--once", "-s", "67", "-S", "8"}, false, &output, io.Discard, deps)
	if !errors.Is(err, watch.ErrDurableState) || !strings.Contains(err.Error(), "lock release failed") {
		t.Fatalf("cleanup error = %v", err)
	}
	if closeCalls != 1 {
		t.Fatalf("lock close calls = %d, want 1", closeCalls)
	}
	decoded := decodeCLIEvents(t, output.String())
	if len(decoded) != 2 || decoded[0].Type != events.JobStarted || decoded[1].Type != events.JobFailed {
		t.Fatalf("events = %+v", decoded)
	}
}

func TestWatchEventsEmitOneFailedTerminalForLockConflict(t *testing.T) {
	t.Parallel()

	cfg := cliWatchConfig(t)
	deps := watchExecutionDependencies{
		loadConfig:         func() (*config.Config, error) { return cfg, nil },
		defaultLibraryPath: func() (string, error) { return filepath.Join(t.TempDir(), "library.db"), nil },
		acquireLock:        func(string) (io.Closer, error) { return nil, watch.ErrWatcherRunning },
		now:                func() time.Time { return time.Unix(2, 0).UTC() },
	}
	var output bytes.Buffer
	_, err := executeWatchWithDependencies(context.Background(), []string{"--events", "--once", "-s", "67", "-S", "8"}, false, &output, io.Discard, deps)
	if !errors.Is(err, watch.ErrWatcherRunning) {
		t.Fatalf("executeWatchWithDependencies() error = %v", err)
	}
	decoded := decodeCLIEvents(t, output.String())
	if len(decoded) != 1 || decoded[0].Type != events.JobFailed {
		t.Fatalf("events = %+v", decoded)
	}
}

func TestWatchEventsEmitOneFailedTerminalForInvalidInvocation(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	_, err := executeWatchWithDependencies(
		context.Background(),
		[]string{"--events", "--unknown"},
		false,
		&output,
		io.Discard,
		watchExecutionDependencies{now: func() time.Time { return time.Unix(3, 0).UTC() }},
	)
	if err == nil || !strings.Contains(err.Error(), "flag provided but not defined") {
		t.Fatalf("executeWatchWithDependencies() error = %v", err)
	}
	decoded := decodeCLIEvents(t, output.String())
	if len(decoded) != 1 || decoded[0].Type != events.JobFailed {
		t.Fatalf("events = %+v", decoded)
	}
}

func TestWatchEventsCancellationEmitsCanceledTerminal(t *testing.T) {
	t.Parallel()

	store := openCLIWatchStore(t)
	cfg := cliWatchConfig(t)
	partial := artifact.Manifest{
		SchemaVersion: 1, ArtifactID: "impartus:v1:partial",
		Files: []artifact.File{{Path: "/absolute/lecture.mp3"}},
	}
	deps := watchExecutionDependencies{
		loadConfig:         func() (*config.Config, error) { return cfg, nil },
		defaultLibraryPath: func() (string, error) { return filepath.Join(t.TempDir(), "library.db"), nil },
		acquireLock:        func(string) (io.Closer, error) { return closerFunc(func() error { return nil }), nil },
		openLibrary:        func(context.Context, library.Options) (*library.Store, error) { return store, nil },
		recoverJobs: func(context.Context, *library.Store) (library.RecoveryResult, error) {
			return library.RecoveryResult{}, nil
		},
		ensureFFmpeg: func() error { return nil },
		login:        func(context.Context, *config.Config) (*client.Client, error) { return &client.Client{}, nil },
		run: func(context.Context, *config.Config, *client.Client, *library.Store, watch.Options) (watch.CycleResult, error) {
			return watch.CycleResult{Downloaded: 1, Outputs: []string{"/absolute/lecture.mp3"}, Artifacts: []artifact.Manifest{partial}}, context.Canceled
		},
		now: func() time.Time { return time.Unix(4, 0).UTC() },
	}
	var output bytes.Buffer
	cycle, err := executeWatchWithDependencies(context.Background(), []string{"--events", "--once", "-s", "67", "-S", "8"}, false, &output, io.Discard, deps)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("executeWatchWithDependencies() error = %v, want context.Canceled", err)
	}
	if cycle.Cycle.Downloaded != 1 {
		t.Fatalf("cycle = %+v", cycle)
	}
	decoded := decodeCLIEvents(t, output.String())
	if len(decoded) != 1 || decoded[0].Type != events.JobCanceled || decoded[0].Status != "canceled" {
		t.Fatalf("events = %+v", decoded)
	}
	terminal := decoded[0]
	if len(terminal.Outputs) != 1 || terminal.Outputs[0] != "/absolute/lecture.mp3" ||
		len(terminal.Artifacts) != 1 || terminal.Artifacts[0].ArtifactID != partial.ArtifactID {
		t.Fatalf("canceled terminal partial completion = %+v", terminal)
	}
	details, ok := terminal.Details.(map[string]any)
	if !ok || details["downloaded"] != float64(1) {
		t.Fatalf("canceled terminal details = %#v", terminal.Details)
	}
}

func TestWatchEventsPreflightCancellationEmitsCanceledTerminal(t *testing.T) {
	t.Parallel()

	store := openCLIWatchStore(t)
	cfg := cliWatchConfig(t)
	deps := watchExecutionDependencies{
		loadConfig:         func() (*config.Config, error) { return cfg, nil },
		defaultLibraryPath: func() (string, error) { return filepath.Join(t.TempDir(), "library.db"), nil },
		acquireLock:        func(string) (io.Closer, error) { return closerFunc(func() error { return nil }), nil },
		openLibrary:        func(context.Context, library.Options) (*library.Store, error) { return store, nil },
		recoverJobs: func(context.Context, *library.Store) (library.RecoveryResult, error) {
			return library.RecoveryResult{}, nil
		},
		ensureFFmpeg: func() error { return nil },
		login: func(context.Context, *config.Config) (*client.Client, error) {
			return nil, context.Canceled
		},
		now: func() time.Time { return time.Unix(5, 0).UTC() },
	}
	var output bytes.Buffer
	_, err := executeWatchWithDependencies(
		context.Background(),
		[]string{"--events", "--once", "-s", "67", "-S", "8"},
		false,
		&output,
		io.Discard,
		deps,
	)
	if !errors.Is(err, context.Canceled) || ExitCode(watchCommandError(err)) != 130 {
		t.Fatalf("preflight error = %v, exit = %d", err, ExitCode(watchCommandError(err)))
	}
	decoded := decodeCLIEvents(t, output.String())
	if len(decoded) != 1 || decoded[0].Type != events.JobCanceled {
		t.Fatalf("events = %+v", decoded)
	}
}

func TestWatchPreflightCleanupFailureIsDurableAndFailedWhenLoginCanceled(t *testing.T) {
	t.Parallel()

	store := openCLIWatchStore(t)
	cfg := cliWatchConfig(t)
	closeCalls := 0
	deps := watchExecutionDependencies{
		loadConfig:         func() (*config.Config, error) { return cfg, nil },
		defaultLibraryPath: func() (string, error) { return filepath.Join(t.TempDir(), "library.db"), nil },
		acquireLock: func(string) (io.Closer, error) {
			return closerFunc(func() error {
				closeCalls++
				return errors.New("preflight lock release failed")
			}), nil
		},
		openLibrary: func(context.Context, library.Options) (*library.Store, error) { return store, nil },
		recoverJobs: func(context.Context, *library.Store) (library.RecoveryResult, error) {
			return library.RecoveryResult{}, nil
		},
		ensureFFmpeg: func() error { return nil },
		login: func(context.Context, *config.Config) (*client.Client, error) {
			return nil, context.Canceled
		},
		now: func() time.Time { return time.Unix(5, 0).UTC() },
	}
	var output bytes.Buffer
	_, err := executeWatchWithDependencies(
		context.Background(),
		[]string{"--events", "--once", "-s", "67", "-S", "8"},
		false,
		&output,
		io.Discard,
		deps,
	)
	if !errors.Is(err, watch.ErrDurableState) || !errors.Is(err, context.Canceled) || ExitCode(watchCommandError(err)) != 1 {
		t.Fatalf("preflight error = %v, exit = %d", err, ExitCode(watchCommandError(err)))
	}
	if closeCalls != 1 {
		t.Fatalf("lock close calls = %d, want 1", closeCalls)
	}
	decoded := decodeCLIEvents(t, output.String())
	if len(decoded) != 1 || decoded[0].Type != events.JobFailed {
		t.Fatalf("events = %+v", decoded)
	}
}

func TestWatchCancellationMapsToExit130(t *testing.T) {
	t.Parallel()

	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		err := watchCommandError(fmt.Errorf("watch interrupted: %w", cause))
		if !errors.Is(err, cause) || ExitCode(err) != 130 {
			t.Fatalf("watchCommandError(%v) = %v, exit = %d", cause, err, ExitCode(err))
		}
	}
	ordinary := errors.New("ordinary failure")
	if got := watchCommandError(ordinary); !errors.Is(got, ordinary) || ExitCode(got) != 1 {
		t.Fatalf("ordinary error = %v, exit = %d", got, ExitCode(got))
	}
	if got := watchCommandError(nil); got != nil {
		t.Fatalf("nil error = %v", got)
	}
	delivery := errors.Join(context.Canceled, watch.ErrEventDelivery)
	if got := watchCommandError(delivery); ExitCode(got) != 1 {
		t.Fatalf("delivery plus cancellation exit = %d, want 1", ExitCode(got))
	}
	durable := errors.Join(context.Canceled, watch.ErrDurableState)
	if got := watchCommandError(durable); ExitCode(got) != 1 {
		t.Fatalf("durable-state plus cancellation exit = %d, want 1", ExitCode(got))
	}
}

type closerFunc func() error

func (close closerFunc) Close() error { return close() }

func openCLIWatchStore(t *testing.T) *library.Store {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := library.Open(context.Background(), library.Options{Path: filepath.Join(directory, "library.db")})
	if err != nil {
		t.Fatal(err)
	}
	// executeWatchWithDependencies owns Close on success and preflight failure.
	return store
}

func cliWatchConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := &config.Config{
		Username: "user", Password: "pass", BaseURL: "https://example.test",
		DownloadLocation: t.TempDir(), TempDirLocation: t.TempDir(),
	}
	cfg.ApplyDefaults()
	return cfg
}
