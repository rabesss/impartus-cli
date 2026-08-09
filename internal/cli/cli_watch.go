package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/events"
	"github.com/rabesss/impartus-cli/internal/library"
	"github.com/rabesss/impartus-cli/internal/paths"
	"github.com/rabesss/impartus-cli/internal/watch"
)

type watchFlags struct {
	subject, session int
	interval, output string
	once, dryRun     bool
	events, force    bool
}

type watchResult struct {
	Status string            `json:"status"`
	Cycle  watch.CycleResult `json:"cycle"`
}

type watchExecutionDependencies struct {
	loadConfig         func() (*config.Config, error)
	defaultLibraryPath func() (string, error)
	acquireLock        func(string) (io.Closer, error)
	openLibrary        func(context.Context, library.Options) (*library.Store, error)
	recoverJobs        func(context.Context, *library.Store) (library.RecoveryResult, error)
	ensureFFmpeg       func() error
	login              func(context.Context, *config.Config) (*client.Client, error)
	run                func(context.Context, *config.Config, *client.Client, *library.Store, watch.Options) (watch.CycleResult, error)
	now                func() time.Time
}

type preparedWatch struct {
	cfg       *config.Config
	apiClient *client.Client
	store     *library.Store
	lock      io.Closer
	recovery  library.RecoveryResult
	interval  time.Duration
}

func (prepared *preparedWatch) close() error {
	if prepared == nil {
		return nil
	}
	var storeErr, lockErr error
	if prepared.store != nil {
		storeErr = prepared.store.Close()
		prepared.store = nil
	}
	if prepared.lock != nil {
		lockErr = prepared.lock.Close()
		prepared.lock = nil
	}
	return errors.Join(storeErr, lockErr)
}

func defaultWatchExecutionDependencies() watchExecutionDependencies {
	return watchExecutionDependencies{
		loadConfig:         loadConfig,
		defaultLibraryPath: library.DefaultPath,
		acquireLock: func(path string) (io.Closer, error) {
			return watch.AcquireLock(path)
		},
		openLibrary: library.Open,
		recoverJobs: func(ctx context.Context, store *library.Store) (library.RecoveryResult, error) {
			return store.RecoverInterruptedJobs(ctx)
		},
		ensureFFmpeg: ensureFFmpeg,
		login:        newLoggedInFn,
		run: func(ctx context.Context, cfg *config.Config, apiClient *client.Client, store *library.Store, options watch.Options) (watch.CycleResult, error) {
			return watch.NewFromDownloader(cfg, apiClient, store, options).Run(ctx)
		},
		now: time.Now,
	}
}

func parseWatchFlags(args []string) (watchFlags, error) {
	set := flag.NewFlagSet("watch", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	var values watchFlags
	set.IntVar(&values.subject, "subject", 0, "Subject ID (single-target override)")
	set.IntVar(&values.subject, "s", 0, "Subject ID")
	set.IntVar(&values.session, "session", 0, "Session ID (single-target override)")
	set.IntVar(&values.session, "S", 0, "Session ID")
	set.StringVar(&values.interval, "interval", "", "Poll interval (for example 10m)")
	set.StringVar(&values.output, "output", "", "Output directory override")
	set.StringVar(&values.output, "o", "", "Output directory override")
	set.BoolVar(&values.once, "once", false, "Run one poll cycle and exit")
	set.BoolVar(&values.dryRun, "dry-run", false, "List new lectures without downloading")
	set.BoolVar(&values.events, "events", false, "Emit newline-delimited JSON lifecycle events")
	set.BoolVar(&values.force, "force", false, "Redownload an already committed artifact")
	if err := set.Parse(args); err != nil {
		return watchFlags{}, err
	}
	if set.NArg() > 0 {
		return watchFlags{}, errors.New("watch does not accept positional arguments")
	}
	var subjectSet, sessionSet bool
	set.Visit(func(option *flag.Flag) {
		switch option.Name {
		case "subject", "s":
			subjectSet = true
		case "session", "S":
			sessionSet = true
		}
	})
	if subjectSet && values.subject <= 0 {
		return watchFlags{}, errors.New("--subject/-s must be a positive integer")
	}
	if sessionSet && values.session <= 0 {
		return watchFlags{}, errors.New("--session/-S must be a positive integer")
	}
	if subjectSet != sessionSet {
		return watchFlags{}, errors.New("--subject/-s and --session/-S must be provided together")
	}
	return values, nil
}

func runWatch(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	_, err := executeWatchWithDependencies(ctx, args, false, os.Stdout, os.Stderr, defaultWatchExecutionDependencies())
	return watchCommandError(err)
}

func watchCommandError(err error) error {
	if errors.Is(err, watch.ErrEventDelivery) || errors.Is(err, watch.ErrDurableState) {
		return err
	}
	if errors.Is(err, context.Canceled) {
		return &exitCodeError{code: 130, err: err}
	}
	return err
}

func runWatchJSON(args []string) (watchResult, error) {
	return executeWatchWithDependencies(context.Background(), args, true, io.Discard, io.Discard, defaultWatchExecutionDependencies())
}

func executeWatchWithDependencies(
	ctx context.Context,
	args []string,
	jsonMode bool,
	eventOutput, logOutput io.Writer,
	dependencies watchExecutionDependencies,
) (result watchResult, returnErr error) {
	dependencies = fillWatchDependencies(dependencies)
	jobID := "job-" + uuid.NewString()
	writer := requestedWatchEventWriter(args, eventOutput)
	finishPreflight := func(cause error) error {
		return ensureWatchTerminal(writer, jobID, watch.CycleResult{}, cause, dependencies.now)
	}
	flags, parseErr := parseWatchFlags(args)
	if parseErr != nil {
		return result, finishPreflight(parseErr)
	}
	writer, emitter := selectWatchEventMode(flags.events, writer)
	if jsonMode && flags.events {
		return result, finishPreflight(errors.New("cannot combine --json and --events"))
	}
	prepared, prepareErr := prepareWatch(ctx, flags, dependencies)
	if prepareErr != nil {
		return result, finishPreflight(prepareErr)
	}
	cycle, runErr := dependencies.run(ctx, prepared.cfg, prepared.apiClient, prepared.store, watch.Options{
		Targets: prepared.cfg.Watch.Targets, Once: flags.once || flags.dryRun || jsonMode,
		DryRun: flags.dryRun, Force: flags.force, Interval: prepared.interval,
		MaxRetries: prepared.cfg.Watch.MaxRetries, MaxLecturesPerCycle: prepared.cfg.Watch.MaxLecturesPerCycle,
		Emitter: emitter, Log: logOutput, Now: dependencies.now, JobID: jobID, StartupRecovery: &prepared.recovery,
		DeferTerminal: true,
	})
	if closeErr := prepared.close(); closeErr != nil {
		runErr = errors.Join(runErr, watch.ErrDurableState, fmt.Errorf("close watch state: %w", closeErr))
	}
	if !jsonMode && !flags.events && (flags.once || flags.dryRun) {
		_, writeErr := fmt.Fprintf(logOutput, "watch: listed=%d new=%d skipped=%d downloaded=%d failed=%d\n", cycle.Listed, cycle.New, cycle.Skipped, cycle.Downloaded, cycle.Failed)
		runErr = errors.Join(runErr, writeErr)
	}
	result = watchResult{Status: "completed", Cycle: cycle}
	if runErr != nil {
		result.Status = "failed"
	}
	runErr = ensureWatchTerminal(writer, jobID, cycle, runErr, dependencies.now)
	return result, runErr
}

func requestedWatchEventWriter(args []string, output io.Writer) *events.Writer {
	if !requestedEvents(args) {
		return nil
	}
	return events.NewWriter(output)
}

func selectWatchEventMode(enabled bool, writer *events.Writer) (*events.Writer, events.Emitter) {
	if !enabled {
		return nil, nil
	}
	return writer, writer
}

func prepareWatch(ctx context.Context, flags watchFlags, dependencies watchExecutionDependencies) (preparedWatch, error) {
	var prepared preparedWatch
	cfg, loadErr := dependencies.loadConfig()
	if loadErr != nil {
		return prepared, loadErr
	}
	if applyErr := applyWatchFlags(cfg, flags); applyErr != nil {
		return prepared, applyErr
	}
	prepared.cfg = cfg
	libraryPath, pathErr := dependencies.defaultLibraryPath()
	if pathErr != nil {
		return prepared, pathErr
	}
	lock, lockErr := dependencies.acquireLock(filepath.Join(filepath.Dir(libraryPath), "watch.lock"))
	if lockErr != nil {
		return prepared, lockErr
	}
	prepared.lock = lock
	store, openErr := dependencies.openLibrary(ctx, library.Options{Path: libraryPath})
	if openErr != nil {
		return prepared, errors.Join(openErr, prepared.close())
	}
	prepared.store = store
	if !flags.dryRun {
		recovery, recoveryErr := dependencies.recoverJobs(context.WithoutCancel(ctx), store)
		if recoveryErr != nil {
			return prepared, errors.Join(
				watch.ErrDurableState,
				fmt.Errorf("recover interrupted watch jobs: %w", recoveryErr),
				prepared.close(),
			)
		}
		prepared.recovery = recovery
	}
	if !flags.dryRun {
		if ffmpegErr := dependencies.ensureFFmpeg(); ffmpegErr != nil {
			return prepared, errors.Join(ffmpegErr, prepared.close())
		}
	}
	apiClient, loginErr := dependencies.login(ctx, cfg)
	if loginErr != nil {
		return prepared, errors.Join(loginErr, prepared.close())
	}
	prepared.apiClient = apiClient
	interval, parseErr := time.ParseDuration(cfg.Watch.PollInterval)
	if parseErr != nil {
		return prepared, errors.Join(parseErr, prepared.close())
	}
	prepared.interval = interval
	return prepared, nil
}

func ensureWatchTerminal(writer *events.Writer, jobID string, cycle watch.CycleResult, cause error, now func() time.Time) error {
	if writer == nil || writer.TerminalAttempted() {
		return events.RedactedError(cause)
	}
	var emitErr error
	if cause != nil {
		if (errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded)) &&
			!errors.Is(cause, watch.ErrEventDelivery) && !errors.Is(cause, watch.ErrDurableState) {
			emitErr = writer.Emit(events.Cancellation(jobID, "watch", cause, now()))
		} else {
			emitErr = writer.Emit(events.Failure(jobID, "watch", cause, now()))
		}
	} else {
		emitErr = writer.Emit(events.Event{
			Type: events.JobCompleted, JobID: jobID, Command: "watch", Status: "completed",
			Timestamp: now().UTC(), Outputs: append([]string(nil), cycle.Outputs...), Details: cycle,
		})
	}
	if emitErr != nil {
		return errors.Join(events.RedactedError(cause), watch.ErrEventDelivery, events.RedactedError(emitErr))
	}
	return events.RedactedError(cause)
}

func applyWatchFlags(cfg *config.Config, flags watchFlags) error {
	if cfg == nil {
		return errors.New("watch configuration is required")
	}
	if flags.subject > 0 {
		cfg.Watch.Targets = []config.WatchTarget{{SubjectID: flags.subject, SessionID: flags.session}}
	}
	if interval := strings.TrimSpace(flags.interval); interval != "" {
		cfg.Watch.PollInterval = interval
	}
	if output := strings.TrimSpace(flags.output); output != "" {
		location, err := paths.ValidateDownloadLocation(output, true)
		if err != nil {
			return err
		}
		cfg.DownloadLocation = location
	}
	cfg.Watch.Enabled = true
	cfg.ApplyDefaults()
	cfg.ApplyWatchMediaDefaults()
	if len(cfg.Watch.Targets) == 0 {
		return errors.New("watch requires --subject/-s and --session/-S, or watch.targets in config")
	}
	return cfg.Validate()
}

func fillWatchDependencies(dependencies watchExecutionDependencies) watchExecutionDependencies {
	defaults := defaultWatchExecutionDependencies()
	if dependencies.loadConfig == nil {
		dependencies.loadConfig = defaults.loadConfig
	}
	if dependencies.defaultLibraryPath == nil {
		dependencies.defaultLibraryPath = defaults.defaultLibraryPath
	}
	if dependencies.acquireLock == nil {
		dependencies.acquireLock = defaults.acquireLock
	}
	if dependencies.openLibrary == nil {
		dependencies.openLibrary = defaults.openLibrary
	}
	if dependencies.recoverJobs == nil {
		dependencies.recoverJobs = defaults.recoverJobs
	}
	if dependencies.ensureFFmpeg == nil {
		dependencies.ensureFFmpeg = defaults.ensureFFmpeg
	}
	if dependencies.login == nil {
		dependencies.login = defaults.login
	}
	if dependencies.run == nil {
		dependencies.run = defaults.run
	}
	if dependencies.now == nil {
		dependencies.now = defaults.now
	}
	return dependencies
}
