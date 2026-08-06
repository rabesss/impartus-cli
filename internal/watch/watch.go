package watch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/downloader"
	"github.com/rabesss/impartus-cli/internal/notebooklm"
)

var errSafetyStop = errors.New("watch stopped to preserve upload safety")

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
	UploadToNotebook(ctx context.Context, notebookID, filePath, title, idempotencyKey string) (notebooklm.UploadResult, error)
	ReconcileUpload(ctx context.Context, notebookID, title, idempotencyKey string) (notebooklm.UploadResult, error)
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

// Run loops until Once is set, the context is canceled, or an unrecoverable
// authentication or state-safety error occurs. Poll failures are logged and
// retried next cycle.
func (w *Watcher) Run(ctx context.Context) (CycleResult, error) {
	var last CycleResult
	for {
		cycle, err := w.RunCycle(ctx)
		last = cycle
		if err != nil {
			if w.opts.Once || notebooklm.IsAuth(err) || errors.Is(err, errSafetyStop) || ctx.Err() != nil {
				return last, err
			}
			w.logf("watch: cycle failed; retrying after %s: %v", w.opts.Interval, err)
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
	var targetErrs []error

	for _, target := range w.opts.Targets {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		limit := remaining
		if limit < 0 {
			limit = 0
		}
		processed, err := w.runTarget(ctx, target, limit, &result)
		if err != nil {
			if notebooklm.IsAuth(err) {
				return result, fmt.Errorf("notebooklm auth failure; aborting cycle: %w", err)
			}
			if errors.Is(err, errSafetyStop) {
				return result, err
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return result, ctxErr
			}
			result.Failed++
			result.Errors = append(result.Errors, err.Error())
			w.logf("watch: target %s failed: %v", targetLabel(target), err)
			targetErrs = append(targetErrs, err)
			remaining -= processed
			continue
		}
		remaining -= processed
	}
	return result, errors.Join(targetErrs...)
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
		if errors.Is(err, client.ErrNoLectures) || errors.Is(err, client.ErrNoLecturesAfterFiltering) {
			w.logf("watch: %s: no lectures to process", targetLabel(target))
			return 0, nil
		}
		return 0, err
	}
	result.Listed += len(selected)

	processed := 0
	for _, lecture := range selected {
		existing, exists := w.store.Get(target.SubjectID, target.SessionID, lecture.TTID)
		reconcileOnly := exists && existing.Status == StatusAmbiguous
		if !w.store.NeedsWork(target.SubjectID, target.SessionID, lecture.TTID, w.opts.Upload) {
			result.Skipped++
			continue
		}
		if !reconcileOnly {
			if processed >= limit {
				continue
			}
			result.New++
			processed++
		}
		if err := w.processLecture(ctx, target, lecture, result); err != nil {
			if notebooklm.IsAuth(err) {
				return processed, err
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return processed, ctxErr
			}
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s ttid=%d: %v", targetLabel(target), lecture.TTID, err))
			w.logf("watch: %s lecture ttid=%d failed: %v", targetLabel(target), lecture.TTID, err)
			if errors.Is(err, errSafetyStop) {
				return processed, err
			}
			continue
		}
	}
	return processed, nil
}

func (w *Watcher) processLecture(ctx context.Context, target config.WatchTarget, lecture client.Lecture, result *CycleResult) error {
	title, existing, seen := w.pendingLecture(target, lecture)
	reconcileOnly := existing.Status == StatusAmbiguous
	w.logf("watch: %s new lecture seq=%d ttid=%d topic=%q", targetLabel(target), lecture.SeqNo, lecture.TTID, lecture.Topic)

	if w.opts.DryRun {
		w.logf("watch: dry-run new lecture %q", title)
		return nil
	}

	if err := w.store.Mark(target.SubjectID, target.SessionID, seen, lecture.TTID); err != nil {
		return fmt.Errorf("persist pending state: %w", err)
	}

	if reconcileOnly {
		if w.uploader == nil {
			return fmt.Errorf("uploader is not configured")
		}
		// Reconciliation only needs the durable notebook id, title, and upload
		// key. Audio may have been pruned or this state may have moved to a new
		// host; never re-download it merely to list provider sources.
		return w.processUpload(
			ctx, target, lecture, existing.OutputPath, title, true, seen, result,
		)
	}
	if w.audio == nil {
		return fmt.Errorf("audio producer is not configured")
	}

	output, downloaded, err := w.ensureAudio(ctx, target, lecture, existing, &seen)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		seen.Status = StatusFailed
		seen.Error = err.Error()
		_ = w.store.Mark(target.SubjectID, target.SessionID, seen, lecture.TTID) //nolint:errcheck // preserve primary error
		return err
	}
	if downloaded {
		result.Downloaded++
	}
	result.Outputs = append(result.Outputs, output)

	if !w.opts.Upload {
		seen.Status = StatusDownloaded
		seen.Error = ""
		return w.store.Mark(target.SubjectID, target.SessionID, seen, lecture.TTID)
	}
	if w.uploader == nil {
		return fmt.Errorf("uploader is not configured")
	}
	return w.processUpload(ctx, target, lecture, output, title, reconcileOnly, seen, result)
}

func (w *Watcher) processUpload(
	ctx context.Context,
	target config.WatchTarget,
	lecture client.Lecture,
	output, title string,
	reconcileOnly bool,
	seen SeenLecture,
	result *CycleResult,
) error {
	upload, uploadErr := w.uploadOrReconcile(ctx, target, lecture, output, title, reconcileOnly, &seen)
	if uploadErr != nil {
		return w.persistUploadError(ctx, target, lecture, upload.Outcome, seen, uploadErr)
	}
	if upload.Outcome != notebooklm.UploadCreated && upload.Outcome != notebooklm.UploadFound {
		return w.persistUploadError(
			ctx,
			target,
			lecture,
			notebooklm.UploadAmbiguous,
			seen,
			fmt.Errorf("notebooklm provider returned invalid successful outcome %q", upload.Outcome),
		)
	}
	seen.Status = StatusUploaded
	seen.SourceID = upload.SourceID
	seen.NotebookID = firstNonEmpty(upload.NotebookID, seen.NotebookID)
	seen.ReconcileAttempts = 0
	seen.Error = ""

	if err := w.store.Mark(target.SubjectID, target.SessionID, seen, lecture.TTID); err != nil {
		return fmt.Errorf("persist watch state after successful upload (source=%s): %w", upload.SourceID, err)
	}
	result.Uploaded++
	w.logf("watch: uploaded %q as source %s", title, upload.SourceID)
	if w.opts.DeleteAudioAfterUpload {
		if rmErr := os.Remove(output); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			w.logf("watch: warning: failed to delete audio %s: %v", output, rmErr)
		}
	}
	return nil
}

func (w *Watcher) uploadOrReconcile(
	ctx context.Context,
	target config.WatchTarget,
	lecture client.Lecture,
	output, title string,
	reconcileOnly bool,
	seen *SeenLecture,
) (notebooklm.UploadResult, error) {
	var upload notebooklm.UploadResult
	var uploadErr error
	if reconcileOnly {
		upload, uploadErr = w.uploader.ReconcileUpload(
			ctx, seen.NotebookID, title, seen.UploadKey,
		)
		if uploadErr == nil && upload.Outcome != notebooklm.UploadFound {
			uploadErr = fmt.Errorf("notebooklm provider returned invalid successful reconciliation outcome %q", upload.Outcome)
		}
		if uploadErr != nil {
			seen.ReconcileAttempts++
			upload.Outcome = notebooklm.UploadAmbiguous
		}
	} else {
		// Persist the uncertain/in-flight phase before crossing the provider
		// boundary. A process crash during source add must resume by listing,
		// never by issuing a second add.
		seen.Status = StatusAmbiguous
		seen.Error = "NotebookLM upload is in flight; reconcile before another add"
		if err := w.store.Mark(target.SubjectID, target.SessionID, *seen, lecture.TTID); err != nil {
			return notebooklm.UploadResult{}, fmt.Errorf("persist watch state before NotebookLM upload: %w", err)
		}
		upload, uploadErr = w.uploadWithRetries(ctx, seen.NotebookID, output, title, seen.UploadKey)
	}
	return upload, uploadErr
}

func (w *Watcher) persistUploadError(
	ctx context.Context,
	target config.WatchTarget,
	lecture client.Lecture,
	outcome notebooklm.UploadOutcome,
	seen SeenLecture,
	uploadErr error,
) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if outcome == notebooklm.UploadAmbiguous {
		seen.Status = StatusAmbiguous
	} else {
		seen.Status = StatusFailed
		seen.ReconcileAttempts = 0
	}
	seen.Error = uploadErr.Error()
	if persistErr := w.store.Mark(target.SubjectID, target.SessionID, seen, lecture.TTID); persistErr != nil {
		if outcome == notebooklm.UploadRejected {
			retryErr := w.store.Mark(target.SubjectID, target.SessionID, seen, lecture.TTID)
			if retryErr == nil {
				return uploadErr
			}
			return errors.Join(
				errSafetyStop,
				uploadErr,
				fmt.Errorf("persist failed watch state: %w", persistErr),
				fmt.Errorf("persist failed watch state after retry: %w", retryErr),
			)
		}
		return errors.Join(
			uploadErr,
			fmt.Errorf("persist watch state after upload failure: %w", persistErr),
		)
	}
	return uploadErr
}

func (w *Watcher) pendingLecture(
	target config.WatchTarget,
	lecture client.Lecture,
) (string, SeenLecture, SeenLecture) {
	existing, _ := w.store.Get(target.SubjectID, target.SessionID, lecture.TTID)
	key := existing.UploadKey
	if key == "" {
		key = lectureUploadKey(target, lecture)
	}
	status := StatusPending
	notebookID := firstNonEmpty(target.NotebookID, existing.NotebookID)
	if existing.Status == StatusAmbiguous {
		status = StatusAmbiguous
		// An in-flight add belongs to the notebook selected when the provider
		// boundary was crossed. Configuration changes must not redirect its
		// reconciliation to another notebook.
		notebookID = firstNonEmpty(existing.NotebookID, target.NotebookID)
	}
	reconcileAttempts := 0
	if existing.Status == StatusAmbiguous {
		reconcileAttempts = existing.ReconcileAttempts
	}
	return lectureTitle(lecture, key), existing, SeenLecture{
		Status:            status,
		SeqNo:             lecture.SeqNo,
		Topic:             lecture.Topic,
		StartTime:         lecture.StartTime,
		NotebookID:        notebookID,
		UploadKey:         key,
		Attempts:          existing.Attempts + 1,
		ReconcileAttempts: reconcileAttempts,
	}
}

func (w *Watcher) ensureAudio(
	ctx context.Context,
	target config.WatchTarget,
	lecture client.Lecture,
	existing SeenLecture,
	seen *SeenLecture,
) (string, bool, error) {
	if existing.OutputPath != "" {
		if _, err := os.Stat(existing.OutputPath); err == nil {
			seen.OutputPath = existing.OutputPath
			if seen.Status != StatusAmbiguous {
				seen.Status = StatusDownloaded
				seen.Error = ""
			}
			if err := w.store.Mark(target.SubjectID, target.SessionID, *seen, lecture.TTID); err != nil {
				return "", false, fmt.Errorf("persist resumed download state: %w", err)
			}
			return existing.OutputPath, false, nil
		}
	}

	if err := os.MkdirAll(w.cfg.DownloadLocation, 0o755); err != nil { // #nosec G301 -- user-owned download directory
		return "", false, fmt.Errorf("create watch download directory: %w", err)
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
		return "", false, err
	}
	output := firstOutput(join)
	if output == "" {
		return "", false, fmt.Errorf("download produced no audio output for ttid=%d", lecture.TTID)
	}
	seen.OutputPath = output
	if seen.Status != StatusAmbiguous {
		seen.Status = StatusDownloaded
		seen.Error = ""
	}
	if err := w.store.Mark(target.SubjectID, target.SessionID, *seen, lecture.TTID); err != nil {
		return "", false, fmt.Errorf("persist downloaded state: %w", err)
	}
	return output, true, nil
}

func (w *Watcher) uploadWithRetries(ctx context.Context, notebookID, output, title, uploadKey string) (notebooklm.UploadResult, error) {
	var upload notebooklm.UploadResult
	err := withRetries(ctx, w.opts.MaxRetries, w.opts.RetryBackoff, w.opts.Log, func() error {
		var err error
		upload, err = w.uploader.UploadToNotebook(ctx, notebookID, output, title, uploadKey)
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
	if notebooklm.IsAmbiguous(err) {
		return false
	}
	var nlmErr *notebooklm.Error
	if errors.As(err, &nlmErr) {
		return nlmErr.Retryable()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var networkErr net.Error
	if errors.As(err, &networkErr) && networkErr.Timeout() {
		return true
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
		topic := strings.Trim(strings.ToLower(strings.TrimSpace(lecture.Topic)), " \t\r\n.!?:;,_-–—")
		topic = strings.NewReplacer("–", "-", "—", "-").Replace(topic)
		topic = strings.NewReplacer("!", " ", ":", " ", ";", " ", ",", " ").Replace(topic)
		topic = strings.Join(strings.Fields(topic), " ")
		topic = strings.NewReplacer(" - ", "-", " -", "-", "- ", "-").Replace(topic)
		switch topic {
		case "no class", "no class today", "no class scheduled",
			"no class-holiday", "no class holiday",
			"there will be no class",
			"no lecture", "no lecture today", "no lecture scheduled",
			"no lecture-holiday", "no lecture holiday",
			"there will be no lecture":
			continue
		}
		filtered = append(filtered, lecture)
	}
	return filtered
}

func lectureTitle(lecture client.Lecture, uploadKey string) string {
	topic := strings.TrimSpace(lecture.Topic)
	if topic == "" {
		topic = "Lecture"
	}
	return fmt.Sprintf("[%s] LEC %03d %s", uploadKey, lecture.SeqNo, topic)
}

func lectureUploadKey(target config.WatchTarget, lecture client.Lecture) string {
	return fmt.Sprintf("impartus:%d:%d:%d", target.SubjectID, target.SessionID, lecture.TTID)
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
