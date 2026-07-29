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
	fs.IntVar(&f.subject, "subject", 0, "Subject ID")
	fs.IntVar(&f.subject, "s", 0, "Subject ID")
	fs.IntVar(&f.session, "session", 0, "Session ID")
	fs.IntVar(&f.session, "S", 0, "Session ID")
	fs.StringVar(&f.interval, "interval", "", "Poll interval (e.g. 5m)")
	fs.StringVar(&f.statePath, "state", "", "Watch state file path")
	fs.StringVar(&f.notebookID, "notebook", "", "NotebookLM notebook ID")
	fs.StringVar(&f.output, "output", "", "Output directory override")
	fs.StringVar(&f.output, "o", "", "Output directory override")
	fs.BoolVar(&f.once, "once", false, "Run a single poll cycle then exit")
	fs.BoolVar(&f.dryRun, "dry-run", false, "List new lectures without downloading or uploading")
	fs.BoolVar(&f.check, "check", false, "Validate ffmpeg, NotebookLM auth, and config then exit")
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

	uploader := notebooklm.New(notebooklm.Config{
		NotebookID:  cfg.NotebookLM.NotebookID,
		CLIPath:     cfg.NotebookLM.CLIPath,
		AuthProfile: cfg.NotebookLM.AuthProfile,
	})

	if f.check {
		return runWatchCheck(ctx, cfg, cfg.Watch.Upload, uploader, jsonMode)
	}

	if err = ensureWatchRuntime(ctx, f, cfg.Watch.Upload, uploader); err != nil {
		return watchResult{}, err
	}

	return runWatchLoop(ctx, cfg, apiClient, uploader, f, jsonMode)
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

func ensureWatchRuntime(ctx context.Context, f watchFlags, uploadEnabled bool, uploader *notebooklm.Uploader) error {
	if !f.dryRun {
		if err := ensureFFmpeg(); err != nil {
			return err
		}
	}
	if uploadEnabled && !f.dryRun {
		if err := uploader.Doctor(ctx); err != nil {
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
	store, err := watch.LoadStore(cfg.Watch.StatePath)
	if err != nil {
		return watchResult{}, err
	}

	interval, err := time.ParseDuration(cfg.Watch.Interval)
	if err != nil {
		return watchResult{}, fmt.Errorf("invalid watch interval: %w", err)
	}

	logWriter := io.Writer(os.Stderr)
	if jsonMode {
		logWriter = io.Discard
	}

	opts := watch.Options{
		SubjectID:  cfg.Watch.SubjectID,
		SessionID:  cfg.Watch.SessionID,
		Once:       f.once || jsonMode,
		DryRun:     f.dryRun,
		Upload:     cfg.Watch.Upload,
		NotebookID: cfg.NotebookLM.NotebookID,
		Interval:   interval,
		Log:        logWriter,
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

func applyWatchFlags(cfg *config.Config, f watchFlags) (*config.Config, error) {
	if f.subject > 0 {
		cfg.Watch.SubjectID = f.subject
	}
	if f.session > 0 {
		cfg.Watch.SessionID = f.session
	}
	if f.interval != "" {
		cfg.Watch.Interval = f.interval
	}
	if f.statePath != "" {
		cfg.Watch.StatePath = f.statePath
	}
	if f.notebookID != "" {
		cfg.NotebookLM.NotebookID = f.notebookID
	}
	if f.output != "" {
		cfg.DownloadLocation = f.output
	}
	cfg.Watch.Enabled = true
	switch {
	case f.noUpload:
		cfg.Watch.Upload = false
	case f.upload:
		cfg.Watch.Upload = true
	}
	if cfg.Watch.SubjectID <= 0 || cfg.Watch.SessionID <= 0 {
		return cfg, errors.New("watch requires --subject/-s and --session/-S (or watch.subjectId/sessionId in config)")
	}
	if cfg.Watch.Upload && cfg.NotebookLM.NotebookID == "" && !f.check && !f.dryRun {
		return cfg, errors.New("watch --upload requires --notebook or notebooklm.notebookId")
	}
	return cfg, nil
}

func runWatchCheck(ctx context.Context, cfg *config.Config, uploadEnabled bool, uploader *notebooklm.Uploader, jsonMode bool) (watchResult, error) {
	if err := ensureFFmpeg(); err != nil {
		return watchResult{}, err
	}
	checks := map[string]string{
		"ffmpeg":   "ok",
		"config":   "ok",
		"subject":  fmt.Sprintf("%d", cfg.Watch.SubjectID),
		"session":  fmt.Sprintf("%d", cfg.Watch.SessionID),
		"state":    cfg.Watch.StatePath,
		"interval": cfg.Watch.Interval,
		"quality":  cfg.Quality,
		"views":    cfg.Views,
		"upload":   fmt.Sprintf("%v", uploadEnabled),
	}
	if uploadEnabled {
		if err := uploader.Doctor(ctx); err != nil {
			return watchResult{}, err
		}
		checks["notebooklm"] = "ok"
		checks["notebookId"] = cfg.NotebookLM.NotebookID
	}
	if jsonMode {
		return watchResult{Status: "ok", Cycle: watch.CycleResult{}}, nil
	}
	fmt.Fprintln(os.Stderr, "watch check passed:")
	for _, key := range []string{"ffmpeg", "config", "subject", "session", "state", "interval", "quality", "views", "upload", "notebooklm", "notebookId"} {
		if val, ok := checks[key]; ok {
			fmt.Fprintf(os.Stderr, "  %-12s %s\n", key+":", val)
		}
	}
	return watchResult{Status: "ok"}, nil
}
