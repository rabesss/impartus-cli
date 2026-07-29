package watch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/downloader"
	"github.com/rabesss/impartus-cli/internal/notebooklm"
)

// LectureSource lists lectures for a course.
type LectureSource interface {
	GetLectures(ctx context.Context, cfg *config.Config, course client.Course) (client.Lectures, error)
}

// AudioProducer downloads and joins a lecture into an audio file.
type AudioProducer interface {
	FetchLecturePlaylists(ctx context.Context, lectures []client.Lecture) ([]client.ParsedPlaylist, error)
	DownloadAndJoinPlaylist(ctx context.Context, playlist client.ParsedPlaylist, p any, tracker any) (downloader.JoinResult, error)
}

// SourceUploader uploads a local audio file to NotebookLM.
type SourceUploader interface {
	UploadToNotebook(ctx context.Context, notebookID, filePath, title string) (notebooklm.UploadResult, error)
	Doctor(ctx context.Context) error
}

// downloaderAdapter adapts *downloader.Downloader to AudioProducer without
// leaking mpb progress types into the watch package's interface surface.
type downloaderAdapter struct {
	inner *downloader.Downloader
}

func (a downloaderAdapter) FetchLecturePlaylists(ctx context.Context, lectures []client.Lecture) ([]client.ParsedPlaylist, error) {
	return a.inner.FetchLecturePlaylists(ctx, lectures)
}

func (a downloaderAdapter) DownloadAndJoinPlaylist(ctx context.Context, playlist client.ParsedPlaylist, _ any, _ any) (downloader.JoinResult, error) {
	return a.inner.DownloadAndJoinPlaylist(ctx, playlist, nil, nil)
}

// Options controls one watcher run.
type Options struct {
	Targets                []config.WatchTarget
	Once                   bool
	DryRun                 bool
	Upload                 bool
	Interval               time.Duration
	MaxRetries             int
	MaxLecturesPerCycle    int
	DeleteAudioAfterUpload bool
	// RetryBackoff, when set, replaces the default attempt^2 seconds backoff.
	RetryBackoff func(attempt int) time.Duration
	Log          io.Writer

	// Legacy single-target fields kept for older call sites/tests.
	SubjectID  int
	SessionID  int
	NotebookID string
}

// CycleResult summarizes one poll cycle.
type CycleResult struct {
	Listed     int      `json:"listed"`
	New        int      `json:"new"`
	Skipped    int      `json:"skipped"`
	Downloaded int      `json:"downloaded"`
	Uploaded   int      `json:"uploaded"`
	Failed     int      `json:"failed"`
	DryRun     bool     `json:"dryRun,omitempty"`
	Outputs    []string `json:"outputs,omitempty"`
	Errors     []string `json:"errors,omitempty"`
}

// Watcher polls Impartus for new lectures, downloads audio, and optionally uploads.
type Watcher struct {
	cfg      *config.Config
	source   LectureSource
	audio    AudioProducer
	uploader SourceUploader
	store    *Store
	opts     Options
}

// New constructs a Watcher. audio may be nil when DryRun is true.
func New(cfg *config.Config, source LectureSource, audio AudioProducer, uploader SourceUploader, store *Store, opts Options) *Watcher {
	opts = normalizeOptions(opts)
	return &Watcher{
		cfg:      cfg,
		source:   source,
		audio:    audio,
		uploader: uploader,
		store:    store,
		opts:     opts,
	}
}

func normalizeOptions(opts Options) Options {
	if opts.MaxRetries <= 0 {
		opts.MaxRetries = 3
	}
	if opts.Interval <= 0 {
		opts.Interval = 5 * time.Minute
	}
	if opts.MaxLecturesPerCycle <= 0 {
		opts.MaxLecturesPerCycle = 3
	}
	if opts.Log == nil {
		opts.Log = io.Discard
	}
	if len(opts.Targets) == 0 && opts.SubjectID > 0 && opts.SessionID > 0 {
		opts.Targets = []config.WatchTarget{{
			SubjectID:  opts.SubjectID,
			SessionID:  opts.SessionID,
			NotebookID: opts.NotebookID,
		}}
	}
	return opts
}

// NewFromDownloader wraps a real downloader for production use.
func NewFromDownloader(cfg *config.Config, apiClient *client.Client, uploader SourceUploader, store *Store, opts Options) *Watcher {
	d := downloader.New(cfg, apiClient)
	return New(cfg, apiClient, downloaderAdapter{inner: d}, uploader, store, opts)
}

func (w *Watcher) logf(format string, args ...any) {
	_, _ = fmt.Fprintf(w.opts.Log, format+"\n", args...) //nolint:errcheck // logging is best-effort
}

// Run loops until Once is set, the context is canceled, or an unrecoverable error.
func (w *Watcher) Run(ctx context.Context) (CycleResult, error) {
	var last CycleResult
	for {
		cycle, err := w.RunCycle(ctx)
		last = cycle
		if err != nil {
			return last, err
		}
		if w.opts.Once {
			return last, nil
		}
		timer := time.NewTimer(w.opts.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return last, ctx.Err()
		case <-timer.C:
		}
	}
}

// RunCycle performs one poll across all configured targets.
func (w *Watcher) RunCycle(ctx context.Context) (CycleResult, error) {
	result := CycleResult{DryRun: w.opts.DryRun}
	remaining := w.opts.MaxLecturesPerCycle

	for _, target := range w.opts.Targets {
		if remaining <= 0 {
			break
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		processed, err := w.runTarget(ctx, target, remaining, &result)
		if err != nil {
			if notebooklm.IsAuth(err) {
				return result, fmt.Errorf("notebooklm auth failure; aborting cycle: %w", err)
			}
			return result, err
		}
		remaining -= processed
	}
	return result, nil
}

func (w *Watcher) runTarget(ctx context.Context, target config.WatchTarget, limit int, result *CycleResult) (int, error) {
	course := client.Course{SubjectID: target.SubjectID, SessionID: target.SessionID}
	lectures, err := w.source.GetLectures(ctx, w.cfg, course)
	if err != nil {
		return 0, fmt.Errorf("list lectures for %s: %w", targetLabel(target), err)
	}
	lectures = filterEmptyLectures(lectures)
	selected, _, err := lectures.SelectForDownload(0, 0, true)
	if err != nil {
		if strings.Contains(err.Error(), "no lectures available after filtering") ||
			strings.Contains(err.Error(), "no lectures found") {
			w.logf("watch: %s: no lectures to process", targetLabel(target))
			return 0, nil
		}
		return 0, err
	}
	result.Listed += len(selected)

	processed := 0
	for _, lecture := range selected {
		if processed >= limit {
			break
		}
		if !w.store.NeedsWork(target.SubjectID, target.SessionID, lecture.TTID, w.opts.Upload) {
			result.Skipped++
			continue
		}
		result.New++
		processed++
		if err := w.processLecture(ctx, target, lecture, result); err != nil {
			if notebooklm.IsAuth(err) {
				return processed, err
			}
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s ttid=%d: %v", targetLabel(target), lecture.TTID, err))
			w.logf("watch: %s lecture ttid=%d failed: %v", targetLabel(target), lecture.TTID, err)
			continue
		}
	}
	return processed, nil
}

func (w *Watcher) processLecture(ctx context.Context, target config.WatchTarget, lecture client.Lecture, result *CycleResult) error {
	title := lectureTitle(lecture)
	w.logf("watch: %s new lecture seq=%d ttid=%d topic=%q", targetLabel(target), lecture.SeqNo, lecture.TTID, lecture.Topic)

	if w.opts.DryRun {
		w.logf("watch: dry-run would download+upload %q", title)
		return nil
	}
	if w.audio == nil {
		return fmt.Errorf("audio producer is not configured")
	}

	existing, _ := w.store.Get(target.SubjectID, target.SessionID, lecture.TTID)
	seen := SeenLecture{
		Status:     StatusPending,
		SeqNo:      lecture.SeqNo,
		Topic:      lecture.Topic,
		StartTime:  lecture.StartTime,
		NotebookID: firstNonEmpty(target.NotebookID, w.opts.NotebookID),
		Attempts:   existing.Attempts + 1,
	}
	if err := w.store.Mark(target.SubjectID, target.SessionID, seen, lecture.TTID); err != nil {
		return fmt.Errorf("persist pending state: %w", err)
	}

	output, err := w.ensureAudio(ctx, target, lecture, existing, &seen)
	if err != nil {
		seen.Status = StatusFailed
		seen.Error = err.Error()
		_ = w.store.Mark(target.SubjectID, target.SessionID, seen, lecture.TTID) //nolint:errcheck // preserve primary error
		return err
	}
	result.Downloaded++
	result.Outputs = append(result.Outputs, output)

	if !w.opts.Upload {
		seen.Status = StatusDownloaded
		seen.Error = ""
		return w.store.Mark(target.SubjectID, target.SessionID, seen, lecture.TTID)
	}
	if w.uploader == nil {
		return fmt.Errorf("uploader is not configured")
	}

	upload, uploadErr := w.uploadWithRetries(ctx, seen.NotebookID, output, title)
	if uploadErr != nil {
		seen.Status = StatusFailed
		seen.Error = uploadErr.Error()
		_ = w.store.Mark(target.SubjectID, target.SessionID, seen, lecture.TTID) //nolint:errcheck
		return uploadErr
	}
	seen.Status = StatusUploaded
	seen.Uploaded = true
	seen.SourceID = upload.SourceID
	seen.NotebookID = firstNonEmpty(upload.NotebookID, seen.NotebookID)
	seen.Error = ""
	result.Uploaded++
	w.logf("watch: uploaded %q as source %s", title, upload.SourceID)

	if err := w.store.Mark(target.SubjectID, target.SessionID, seen, lecture.TTID); err != nil {
		return fmt.Errorf("persist watch state: %w", err)
	}
	if w.opts.DeleteAudioAfterUpload {
		if rmErr := os.Remove(output); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			w.logf("watch: warning: failed to delete audio %s: %v", output, rmErr)
		}
	}
	return nil
}

func (w *Watcher) ensureAudio(
	ctx context.Context,
	target config.WatchTarget,
	lecture client.Lecture,
	existing SeenLecture,
	seen *SeenLecture,
) (string, error) {
	if existing.Status == StatusDownloaded && existing.OutputPath != "" {
		if _, err := os.Stat(existing.OutputPath); err == nil {
			seen.OutputPath = existing.OutputPath
			seen.Status = StatusDownloaded
			return existing.OutputPath, nil
		}
	}

	var join downloader.JoinResult
	err := withRetries(ctx, w.opts.MaxRetries, w.opts.RetryBackoff, w.opts.Log, func() error {
		playlists, fetchErr := w.audio.FetchLecturePlaylists(ctx, []client.Lecture{lecture})
		if fetchErr != nil {
			return fetchErr
		}
		if len(playlists) == 0 {
			return fmt.Errorf("no playlist returned for ttid=%d", lecture.TTID)
		}
		var joinErr error
		join, joinErr = w.audio.DownloadAndJoinPlaylist(ctx, playlists[0], nil, nil)
		return joinErr
	})
	if err != nil {
		return "", err
	}
	output := firstOutput(join)
	if output == "" {
		return "", fmt.Errorf("download produced no audio output for ttid=%d", lecture.TTID)
	}
	seen.OutputPath = output
	seen.Status = StatusDownloaded
	seen.Error = ""
	if err := w.store.Mark(target.SubjectID, target.SessionID, *seen, lecture.TTID); err != nil {
		return "", fmt.Errorf("persist downloaded state: %w", err)
	}
	return output, nil
}

func (w *Watcher) uploadWithRetries(ctx context.Context, notebookID, output, title string) (notebooklm.UploadResult, error) {
	var upload notebooklm.UploadResult
	err := withRetries(ctx, w.opts.MaxRetries, w.opts.RetryBackoff, w.opts.Log, func() error {
		var err error
		upload, err = w.uploader.UploadToNotebook(ctx, notebookID, output, title)
		if notebooklm.IsAuth(err) {
			return err // withRetries will stop because auth is not Retryable
		}
		return err
	})
	return upload, err
}

func withRetries(ctx context.Context, maxRetries int, backoffFn func(int) time.Duration, log io.Writer, fn func() error) error {
	if backoffFn == nil {
		backoffFn = func(attempt int) time.Duration {
			return time.Duration(attempt*attempt) * time.Second
		}
	}
	var last error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		last = fn()
		if last == nil {
			return nil
		}
		if notebooklm.IsAuth(last) || !isRetryable(last) || attempt == maxRetries {
			return last
		}
		backoff := backoffFn(attempt)
		_, _ = fmt.Fprintf(log, "watch: retryable error (attempt %d/%d): %v; sleeping %s\n", attempt, maxRetries, last, backoff) //nolint:errcheck
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return last
}

func isRetryable(err error) bool {
	var nlmErr *notebooklm.Error
	if errors.As(err, &nlmErr) {
		return nlmErr.Retryable()
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "temporar") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "429")
}

func filterEmptyLectures(lectures client.Lectures) client.Lectures {
	filtered := make(client.Lectures, 0, len(lectures))
	for _, lecture := range lectures {
		topic := strings.ToLower(strings.TrimSpace(lecture.Topic))
		if strings.Contains(topic, "no class") || strings.Contains(topic, "no lecture") {
			continue
		}
		filtered = append(filtered, lecture)
	}
	return filtered
}

func lectureTitle(lecture client.Lecture) string {
	topic := strings.TrimSpace(lecture.Topic)
	if topic == "" {
		topic = "Lecture"
	}
	return fmt.Sprintf("LEC %03d %s", lecture.SeqNo, topic)
}

func firstOutput(join downloader.JoinResult) string {
	paths := join.OutputPaths()
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func targetLabel(target config.WatchTarget) string {
	if strings.TrimSpace(target.Label) != "" {
		return target.Label
	}
	return fmt.Sprintf("%d/%d", target.SubjectID, target.SessionID)
}
