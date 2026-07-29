package watch

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	UploadFile(ctx context.Context, filePath, title string) (notebooklm.UploadResult, error)
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
	SubjectID  int
	SessionID  int
	Once       bool
	DryRun     bool
	Upload     bool
	NotebookID string
	Interval   time.Duration
	MaxRetries int
	// RetryBackoff, when set, replaces the default attempt^2 seconds backoff.
	RetryBackoff func(attempt int) time.Duration
	Log          io.Writer
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
	if opts.MaxRetries <= 0 {
		opts.MaxRetries = 3
	}
	if opts.Interval <= 0 {
		opts.Interval = 5 * time.Minute
	}
	if opts.Log == nil {
		opts.Log = io.Discard
	}
	return &Watcher{
		cfg:      cfg,
		source:   source,
		audio:    audio,
		uploader: uploader,
		store:    store,
		opts:     opts,
	}
}

// NewFromDownloader wraps a real downloader for production use.
func NewFromDownloader(cfg *config.Config, apiClient *client.Client, uploader SourceUploader, store *Store, opts Options) *Watcher {
	d := downloader.New(cfg, apiClient)
	return New(cfg, apiClient, downloaderAdapter{inner: d}, uploader, store, opts)
}

func (w *Watcher) logf(format string, args ...any) {
	_, _ = fmt.Fprintf(w.opts.Log, format+"\n", args...) //nolint:errcheck // logging is best-effort
}

// Run loops until Once is set, the context is cancelled, or an unrecoverable error.
func (w *Watcher) Run(ctx context.Context) (CycleResult, error) {
	var last CycleResult
	for {
		result, err := w.RunCycle(ctx)
		last = result
		if err != nil {
			return last, err
		}
		if w.opts.Once {
			return last, nil
		}
		w.logf("watch: sleeping %s until next poll", w.opts.Interval)
		timer := time.NewTimer(w.opts.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return last, ctx.Err()
		case <-timer.C:
		}
	}
}

// RunCycle performs one poll / download / upload pass.
func (w *Watcher) RunCycle(ctx context.Context) (CycleResult, error) {
	result := CycleResult{DryRun: w.opts.DryRun}
	course := client.Course{SubjectID: w.opts.SubjectID, SessionID: w.opts.SessionID}

	lectures, err := w.source.GetLectures(ctx, w.cfg, course)
	if err != nil {
		return result, fmt.Errorf("list lectures: %w", err)
	}
	lectures = filterEmptyLectures(lectures)
	selected, _, err := lectures.SelectForDownload(0, 0, true)
	if err != nil {
		// Empty after filtering is a successful idle cycle, not a hard failure.
		if strings.Contains(err.Error(), "no lectures available after filtering") ||
			strings.Contains(err.Error(), "no lectures found") {
			w.logf("watch: no lectures to process")
			return result, nil
		}
		return result, err
	}
	result.Listed = len(selected)

	for _, lecture := range selected {
		if w.store.Has(w.opts.SubjectID, w.opts.SessionID, lecture.TTID) {
			result.Skipped++
			continue
		}
		result.New++
		if err := w.processLecture(ctx, lecture, &result); err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("ttid=%d: %v", lecture.TTID, err))
			w.logf("watch: lecture ttid=%d failed: %v", lecture.TTID, err)
			continue
		}
	}
	return result, nil
}

func (w *Watcher) processLecture(ctx context.Context, lecture client.Lecture, result *CycleResult) error {
	title := lectureTitle(lecture)
	w.logf("watch: new lecture seq=%d ttid=%d topic=%q", lecture.SeqNo, lecture.TTID, lecture.Topic)

	if w.opts.DryRun {
		w.logf("watch: dry-run would download+upload %q", title)
		return nil
	}
	if w.audio == nil {
		return fmt.Errorf("audio producer is not configured")
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
		_ = w.store.Mark(w.opts.SubjectID, w.opts.SessionID, SeenLecture{ //nolint:errcheck // best-effort failure record
			SeqNo: lecture.SeqNo, Topic: lecture.Topic, StartTime: lecture.StartTime, Error: err.Error(),
		}, lecture.TTID)
		return err
	}

	output := firstOutput(join)
	if output == "" {
		return fmt.Errorf("download produced no audio output for ttid=%d", lecture.TTID)
	}
	result.Downloaded++
	result.Outputs = append(result.Outputs, output)

	seen := SeenLecture{
		SeqNo:      lecture.SeqNo,
		Topic:      lecture.Topic,
		StartTime:  lecture.StartTime,
		OutputPath: output,
	}

	if w.opts.Upload {
		if w.uploader == nil {
			return fmt.Errorf("uploader is not configured")
		}
		var upload notebooklm.UploadResult
		uploadErr := withRetries(ctx, w.opts.MaxRetries, w.opts.RetryBackoff, w.opts.Log, func() error {
			var err error
			upload, err = w.uploader.UploadFile(ctx, output, title)
			return err
		})
		if uploadErr != nil {
			seen.Error = uploadErr.Error()
			_ = w.store.Mark(w.opts.SubjectID, w.opts.SessionID, seen, lecture.TTID) //nolint:errcheck
			return uploadErr
		}
		seen.Uploaded = true
		seen.NotebookID = firstNonEmpty(upload.NotebookID, w.opts.NotebookID)
		seen.SourceID = upload.SourceID
		result.Uploaded++
		w.logf("watch: uploaded %q as source %s", title, upload.SourceID)
	}

	if err := w.store.Mark(w.opts.SubjectID, w.opts.SessionID, seen, lecture.TTID); err != nil {
		return fmt.Errorf("persist watch state: %w", err)
	}
	return nil
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
		if !isRetryable(last) || attempt == maxRetries {
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
