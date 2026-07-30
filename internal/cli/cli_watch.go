package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/notebooklm"
	"github.com/rabesss/impartus-cli/internal/paths"
	"github.com/rabesss/impartus-cli/internal/watch"
)

type watchFlags struct {
	subject    int
	session    int
	interval   string
	statePath  string
	notebookID string
	output     string
	once       bool
	dryRun     bool
	check      bool
	noUpload   bool
	upload     bool
}

type watchResult struct {
	Status string            `json:"status"`
	Cycle  watch.CycleResult `json:"cycle"`
	Checks map[string]string `json:"checks,omitempty"`
}

func runWatch(args []string) error {
	_, err := executeWatch(args, false)
	return err
}

func runWatchJSON(args []string) (watchResult, error) {
	return executeWatch(args, true)
}

func parseWatchFlags(args []string) (watchFlags, error) {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var f watchFlags
	fs.IntVar(&f.subject, "subject", 0, "Subject ID (single-target override)")
	fs.IntVar(&f.subject, "s", 0, "Subject ID")
	fs.IntVar(&f.session, "session", 0, "Session ID (single-target override)")
	fs.IntVar(&f.session, "S", 0, "Session ID")
	fs.StringVar(&f.interval, "interval", "", "Poll interval (e.g. 30m)")
	fs.StringVar(&f.statePath, "state", "", "Watch state file path")
	fs.StringVar(&f.notebookID, "notebook", "", "NotebookLM notebook ID")
	fs.StringVar(&f.output, "output", "", "Output directory override")
	fs.StringVar(&f.output, "o", "", "Output directory override")
	fs.BoolVar(&f.once, "once", false, "Run a single poll cycle then exit")
	fs.BoolVar(&f.dryRun, "dry-run", false, "List new lectures without downloading or uploading")
	fs.BoolVar(&f.check, "check", false, "Validate ffmpeg/config and NotebookLM auth when upload is enabled, then exit")
	fs.BoolVar(&f.noUpload, "no-upload", false, "Download audio but skip NotebookLM upload")
	fs.BoolVar(&f.upload, "upload", false, "Upload downloaded audio to NotebookLM")

	if err := fs.Parse(args); err != nil {
		return watchFlags{}, err
	}
	if fs.NArg() > 0 {
		return watchFlags{}, errors.New("watch does not accept positional arguments")
	}
	if f.upload && f.noUpload {
		return watchFlags{}, errors.New("cannot combine --upload and --no-upload")
	}
	return f, nil
}

func executeWatch(args []string, jsonMode bool) (watchResult, error) {
	f, err := parseWatchFlags(args)
	if err != nil {
		return watchResult{}, err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, apiClient, err := prepareWatchConfig(ctx, f)
	if err != nil {
		return watchResult{}, err
	}

	uploader := newWatchUploader(cfg)

	if f.check {
		return runWatchCheck(ctx, cfg, cfg.Watch.Upload, uploader, jsonMode)
	}

	if err = ensureWatchRuntime(ctx, f, cfg, uploader); err != nil {
		return watchResult{}, err
	}

	return runWatchLoop(ctx, cfg, apiClient, uploader, f, jsonMode)
}

func newWatchUploader(cfg *config.Config) *notebooklm.Uploader {
	nlm := cfg.Watch.NotebookLM
	timeout := 30 * time.Minute
	if nlm.UploadTimeout != "" {
		if parsed, err := time.ParseDuration(nlm.UploadTimeout); err == nil {
			timeout = parsed
		}
	}
	return notebooklm.New(notebooklm.Config{
		Provider:              notebooklm.Provider(nlm.Provider),
		NotebookID:            nlm.NotebookID,
		CLIPath:               nlm.Command,
		AuthProfile:           nlm.Profile,
		UploadTimeout:         timeout,
		MaxSourcesPerNotebook: nlm.MaxSourcesPerNotebook,
	})
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func prepareWatchConfig(ctx context.Context, f watchFlags) (*config.Config, *client.Client, error) {
	cfg, apiClient, err := initClient(ctx)
	if err != nil {
		return nil, nil, err
	}
	cfg, err = applyWatchFlags(cfg, f)
	if err != nil {
		return nil, nil, err
	}
	cfg.ApplyWatchMediaDefaults()
	if err = validateFlagOverrides(cfg); err != nil {
		return nil, nil, err
	}
	if err = cfg.Validate(); err != nil {
		return nil, nil, err
	}
	return cfg, apiClient, nil
}

func ensureWatchRuntime(ctx context.Context, f watchFlags, cfg *config.Config, uploader *notebooklm.Uploader) error {
	if !f.dryRun {
		if err := ensureFFmpeg(); err != nil {
			return err
		}
	}
	if cfg.Watch.Upload && !f.dryRun {
		if err := uploader.DoctorNotebooks(ctx, watchNotebookIDs(cfg)); err != nil {
			return err
		}
	}
	return nil
}

func runWatchLoop(
	ctx context.Context,
	cfg *config.Config,
	apiClient *client.Client,
	uploader *notebooklm.Uploader,
	f watchFlags,
	jsonMode bool,
) (watchResult, error) {
	store, err := watch.LoadStore(cfg.Watch.StateFile)
	if err != nil {
		return watchResult{}, err
	}

	interval, err := time.ParseDuration(cfg.Watch.PollInterval)
	if err != nil {
		return watchResult{}, fmt.Errorf("invalid watch interval: %w", err)
	}

	logWriter := io.Writer(os.Stderr)
	if jsonMode {
		logWriter = io.Discard
	}

	opts := watch.Options{
		Targets:                cfg.ResolvedTargets(),
		Once:                   watchRunsOnce(f, jsonMode),
		DryRun:                 f.dryRun,
		Upload:                 cfg.Watch.Upload,
		Interval:               interval,
		MaxRetries:             cfg.Watch.MaxUploadRetries,
		MaxLecturesPerCycle:    cfg.Watch.MaxLecturesPerCycle,
		DeleteAudioAfterUpload: cfg.Watch.DeleteAudioAfterUpload,
		Log:                    logWriter,
	}

	var watcher *watch.Watcher
	if f.dryRun {
		watcher = watch.New(cfg, apiClient, nil, uploader, store, opts)
	} else {
		watcher = watch.NewFromDownloader(cfg, apiClient, uploader, store, opts)
	}

	cycle, err := watcher.Run(ctx)
	if err != nil && !errors.Is(err, context.Canceled) {
		return watchResult{}, err
	}
	result := watchResult{Status: "ok", Cycle: cycle}
	if jsonMode {
		return result, nil
	}
	fmt.Fprintf(os.Stderr, "watch: listed=%d new=%d skipped=%d downloaded=%d uploaded=%d failed=%d\n",
		cycle.Listed, cycle.New, cycle.Skipped, cycle.Downloaded, cycle.Uploaded, cycle.Failed)
	return result, nil
}

func watchRunsOnce(f watchFlags, jsonMode bool) bool {
	return f.once || f.dryRun || jsonMode
}

func applyWatchFlags(cfg *config.Config, f watchFlags) (*config.Config, error) {
	if (f.subject > 0) != (f.session > 0) {
		return cfg, errors.New("--subject/-s and --session/-S must be provided together")
	}
	applyWatchCourseFlags(cfg, f)
	if err := applyWatchIOFlags(cfg, f); err != nil {
		return cfg, err
	}
	cfg.Watch.Enabled = true
	switch {
	case f.noUpload:
		cfg.Watch.Upload = false
	case f.upload:
		cfg.Watch.Upload = true
	}
	return validateWatchFlags(cfg)
}

func applyWatchCourseFlags(cfg *config.Config, f watchFlags) {
	if f.notebookID != "" {
		cfg.Watch.NotebookLM.NotebookID = f.notebookID
		if len(cfg.Watch.Targets) == 1 {
			cfg.Watch.Targets[0].NotebookID = f.notebookID
		}
	}
	if f.subject > 0 && f.session > 0 {
		cfg.Watch.Targets = []config.WatchTarget{{
			SubjectID:  f.subject,
			SessionID:  f.session,
			NotebookID: firstNonEmpty(f.notebookID, cfg.Watch.NotebookLM.NotebookID),
		}}
	}
}

func applyWatchIOFlags(cfg *config.Config, f watchFlags) error {
	if f.interval != "" {
		cfg.Watch.PollInterval = f.interval
	}
	if f.statePath != "" {
		cfg.Watch.StateFile = f.statePath
	}
	if f.output != "" {
		location, err := paths.ValidateDownloadLocation(f.output, true)
		if err != nil {
			return err
		}
		cfg.DownloadLocation = location
	}
	return nil
}

func validateWatchFlags(cfg *config.Config) (*config.Config, error) {
	if len(cfg.ResolvedTargets()) == 0 {
		return cfg, errors.New("watch requires --subject/-s and --session/-S, or watch.targets in config")
	}
	if cfg.Watch.Upload {
		for i, target := range cfg.ResolvedTargets() {
			if target.NotebookID == "" {
				return cfg, fmt.Errorf("watch target[%d] requires notebookId (or --notebook)", i)
			}
		}
	}
	return cfg, nil
}

func runWatchCheck(ctx context.Context, cfg *config.Config, uploadEnabled bool, uploader *notebooklm.Uploader, jsonMode bool) (watchResult, error) {
	if err := ensureFFmpeg(); err != nil {
		return watchResult{}, err
	}
	targets := cfg.ResolvedTargets()
	checks := map[string]string{
		"ffmpeg":   "ok",
		"config":   "ok",
		"targets":  fmt.Sprintf("%d", len(targets)),
		"state":    cfg.Watch.StateFile,
		"interval": cfg.Watch.PollInterval,
		"quality":  cfg.Quality,
		"views":    cfg.Views,
		"upload":   fmt.Sprintf("%v", uploadEnabled),
		"provider": cfg.Watch.NotebookLM.Provider,
	}
	if uploadEnabled {
		if err := uploader.DoctorNotebooks(ctx, watchNotebookIDs(cfg)); err != nil {
			return watchResult{}, err
		}
		checks["notebooklm"] = "ok"
	}
	if jsonMode {
		return watchResult{Status: "ok", Checks: checks}, nil
	}
	fmt.Fprintln(os.Stderr, "watch check passed:")
	for _, key := range []string{"ffmpeg", "config", "targets", "state", "interval", "quality", "views", "upload", "provider", "notebooklm"} {
		if val, ok := checks[key]; ok {
			fmt.Fprintf(os.Stderr, "  %-12s %s\n", key+":", val)
		}
	}
	return watchResult{Status: "ok"}, nil
}

func watchNotebookIDs(cfg *config.Config) []string {
	targets := cfg.ResolvedTargets()
	notebookIDs := make([]string, 0, len(targets))
	for _, target := range targets {
		if target.NotebookID != "" {
			notebookIDs = append(notebookIDs, target.NotebookID)
		}
	}
	if len(notebookIDs) == 0 {
		if cfg.Watch.NotebookLM.NotebookID != "" {
			notebookIDs = append(notebookIDs, cfg.Watch.NotebookLM.NotebookID)
		}
	}
	return notebookIDs
}
