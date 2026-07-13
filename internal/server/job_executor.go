package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/downloader"
	"github.com/rabesss/impartus-cli/internal/paths"
)

func (s *APIServer) executeJob(jobID string) {
	job, ok := s.jobStore.runtimeJobSnapshot(jobID)
	if !ok {
		return
	}

	jobCtx := job.ctx
	ctx, cancelLocal := context.WithCancel(jobCtx)
	defer cancelLocal()

	if !s.startJob(jobID) {
		return
	}

	cfg, apiClient, ok := s.prepareJobRuntime(ctx, job.ctx, jobID, job.cfg)
	if !ok {
		return
	}
	selected, ok := s.fetchSelectedLectures(ctx, jobCtx, jobID, apiClient, cfg, job)
	if !ok {
		return
	}
	s.jobStore.SetLectureProgress(jobID, 0, len(selected))

	s.maybeDownloadSlides(ctx, jobCtx, jobID, apiClient, cfg, selected)
	playlists, downloadCfg, ok := s.prepareDownload(ctx, jobCtx, jobID, apiClient, cfg, selected)
	if !ok {
		return
	}
	finalOutputs, ok := s.runPlaylistDownloads(ctx, jobCtx, cancelLocal, jobID, apiClient, downloadCfg, playlists)
	if !ok {
		return
	}
	s.completeJob(jobID, finalOutputs)
}

func (s *APIServer) updateRunningProgress(jobID string, progress float64, phase string, details any) bool {
	job, ok := s.jobStore.runtimeJobSnapshot(jobID)
	if !ok {
		return false
	}
	status, _ := s.jobStore.GetJobStatus(jobID)
	if job.ctx.Err() != nil || status == StatusCanceled {
		if err := s.jobStore.updateJobDurable(jobID, StatusCanceled, progress, ""); err != nil {
			log.Printf("failed to persist canceled job %s: %v", jobID, err)
		}
		return false
	}

	s.jobStore.UpdateJob(jobID, StatusRunning, progress, "")
	evt := newWSEvent("job.progress", jobID)
	evt.Status = StatusRunning
	evt.Progress = progress
	evt.Phase = phase
	evt.Details = details
	broadcastEvent(s.wsHub, evt)
	return true
}

func (s *APIServer) handleCancelIfNeeded(jobID string, jobErr error) bool {
	if jobErr == nil {
		return false
	}
	job, ok := s.jobStore.CopyJob(jobID)
	if ok {
		if err := s.jobStore.updateJobDurable(jobID, StatusCanceled, job.Progress, ""); err != nil {
			log.Printf("failed to persist canceled job %s: %v", jobID, err)
		}
	} else {
		if err := s.jobStore.updateJobDurable(jobID, StatusCanceled, 0, ""); err != nil {
			log.Printf("failed to persist canceled job %s: %v", jobID, err)
		}
	}

	return true
}

func (s *APIServer) startJob(jobID string) bool {
	if !s.updateRunningProgress(jobID, 2, "initializing", nil) {
		return false
	}
	evt := newWSEvent("job.started", jobID)
	evt.Status = StatusRunning
	broadcastEvent(s.wsHub, evt)
	return true
}

func (s *APIServer) prepareJobRuntime(ctx context.Context, jobCtx context.Context, jobID string, jobCfg *config.Config) (*config.Config, *client.Client, bool) {
	cfg := cloneConfig(jobCfg)
	if cfg == nil {
		s.failJob(jobID, "missing job config")
		return nil, nil, false
	}
	if err := ensureJobDirectories(cfg); err != nil {
		s.failJob(jobID, err.Error())
		return nil, nil, false
	}
	if !s.updateRunningProgress(jobID, 8, "logging_in", nil) {
		return nil, nil, false
	}
	apiClient, cachedCfg, loginErr := s.getOrRefreshUpstreamClient(ctx)
	if loginErr != nil {
		if s.handleCancelIfNeeded(jobID, jobCtx.Err()) {
			return nil, nil, false
		}
		s.failJob(jobID, sanitizeUpstreamErr(loginErr))
		return nil, nil, false
	}
	// Use the token from cached config to ensure consistency
	cfg.Token = cachedCfg.Token
	return cfg, apiClient, true
}

func ensureJobDirectories(cfg *config.Config) error {
	// G301: 0755 is standard for user download directories
	// #nosec G301
	if err := os.MkdirAll(cfg.DownloadLocation, 0o755); err != nil {
		return err
	}
	// G301: 0755 is standard for temp directories
	// #nosec G301
	return os.MkdirAll(cfg.TempDirLocation, 0o755)
}

func (s *APIServer) fetchSelectedLectures(ctx context.Context, jobCtx context.Context, jobID string, apiClient *client.Client, cfg *config.Config, job *Job) (client.Lectures, bool) {
	if !s.updateRunningProgress(jobID, 15, "fetching_lectures", nil) {
		return nil, false
	}
	lectures, err := apiClient.GetLectures(ctx, cfg, client.Course{SubjectID: job.SubjectID, SessionID: job.SessionID})
	if err != nil {
		if s.handleCancelIfNeeded(jobID, jobCtx.Err()) {
			return nil, false
		}
		s.failJob(jobID, sanitizeUpstreamErr(err))
		return nil, false
	}
	selected, filteredLectures, selectErr := selectJobLectures(job, lectures)
	if selectErr != nil {
		s.failJob(jobID, selectErr.Error())
		return nil, false
	}
	s.jobStore.SetFilteredLectures(jobID, filteredLectures)
	return selected, true
}

func selectJobLectures(job *Job, lectures client.Lectures) (client.Lectures, int, error) {
	return lectures.SelectForDownload(job.StartIndex, job.EndIndex, job.Config.SkipNoAudio)
}

func (s *APIServer) maybeDownloadSlides(ctx context.Context, jobCtx context.Context, jobID string, apiClient *client.Client, cfg *config.Config, lectures client.Lectures) {
	if !cfg.Slides {
		return
	}
	for i, lecture := range lectures {
		progress := 15 + (float64(i+1)/float64(len(lectures)))*10
		if !s.updateRunningProgress(jobID, progress, "downloading_slides", map[string]any{"lectureSeqNo": lecture.SeqNo}) {
			return
		}
		slideErr := downloadLectureSlide(ctx, apiClient, cfg, lecture)
		if slideErr != nil && jobCtx.Err() == nil {
			log.Printf("slide download failed for lecture %d: %v", lecture.SeqNo, slideErr)
		}
	}
}

func (s *APIServer) prepareDownload(ctx context.Context, jobCtx context.Context, jobID string, apiClient *client.Client, cfg *config.Config, selected client.Lectures) ([]client.ParsedPlaylist, *config.Config, bool) {
	if !s.updateRunningProgress(jobID, 30, "fetching_playlists", nil) {
		return nil, nil, false
	}
	clientPlaylists, err := apiClient.GetPlaylists(ctx, cfg, selected)
	if err != nil {
		if s.handleCancelIfNeeded(jobID, jobCtx.Err()) {
			return nil, nil, false
		}
		s.failJob(jobID, sanitizeUpstreamErr(err))
		return nil, nil, false
	}
	playlists := clientPlaylists
	if len(playlists) == 0 {
		s.failJob(jobID, "no downloadable playlists found")
		return nil, nil, false
	}
	downloadCfg := cloneConfig(cfg)
	downloadCfg.Views = config.NormalizeViews(downloadCfg.Views)
	return playlists, downloadCfg, true
}

func (s *APIServer) runPlaylistDownloads(ctx context.Context, jobCtx context.Context, cancelLocal context.CancelFunc, jobID string, apiClient *client.Client, downloadCfg *config.Config, playlists []client.ParsedPlaylist) ([]string, bool) {
	total := len(playlists)
	workers := downloadCfg.NumWorkers
	if workers < 1 {
		workers = 1
	}
	if !s.updateRunningProgress(jobID, 40, "downloading", map[string]any{"totalLectures": total}) {
		return nil, false
	}
	d := downloader.New(downloadCfg, apiClient)
	runner := newPlaylistDownloadRunner(workers)
	outputs, err := runner.run(ctx, cancelLocal, d, playlists, func(done int) bool {
		s.jobStore.SetLectureProgress(jobID, done, total)
		progress := 40 + (float64(done)/float64(total))*55
		return s.updateRunningProgress(jobID, progress, "downloading", map[string]any{"completedLectures": done, "totalLectures": total})
	})
	if err != nil {
		if s.handleCancelIfNeeded(jobID, jobCtx.Err()) {
			return nil, false
		}
		s.failJob(jobID, sanitizeUpstreamErr(err))
		return nil, false
	}
	if s.handleCancelIfNeeded(jobID, jobCtx.Err()) {
		return nil, false
	}
	return outputs, true
}

func (s *APIServer) completeJob(jobID string, finalOutputs []string) {
	if err := s.jobStore.CompleteJob(jobID, finalOutputs); err != nil {
		log.Printf("failed to durably complete job %s: %v", jobID, err)
		return
	}
	evt := newWSEvent("job.completed", jobID)
	evt.Status = StatusCompleted
	evt.Progress = 100
	evt.Outputs = finalOutputs
	broadcastEvent(s.wsHub, evt)
}

func (s *APIServer) failJob(jobID, errMsg string) {
	if err := s.jobStore.updateJobDurable(jobID, StatusFailed, 0, errMsg); err != nil {
		log.Printf("failed to durably fail job %s: %v", jobID, err)
		return
	}
	evt := newWSEvent("job.failed", jobID)
	evt.Status = StatusFailed
	evt.Error = errMsg
	broadcastEvent(s.wsHub, evt)
}

var httpStatusRe = regexp.MustCompile(`status (\d{3})`)

// sanitizeUpstreamErr returns a generic sanitized message for upstream errors
// that may contain sensitive data (e.g., auth tokens in upstream API responses).
func sanitizeUpstreamErr(err error) string {
	if err == nil {
		return ""
	}
	// Context cancellation/timeout
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "job was canceled or timed out"
	}
	// DNS errors
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "upstream connection failed"
	}
	// Network timeout errors
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "upstream connection failed"
	}
	// HTTP status code errors — extract status code from formatted error messages
	errStr := err.Error()
	if match := httpStatusRe.FindStringSubmatch(errStr); len(match) > 1 {
		return fmt.Sprintf("upstream API returned HTTP %s", match[1])
	}
	// Auth errors
	if containsAny(errStr, []string{"login", "authenticate", "token", "unauthorized", "forbidden", "auth"}) {
		return "upstream authentication failed"
	}
	return "upstream API error"
}

func containsAny(s string, substrs []string) bool {
	for _, sub := range substrs {
		if strings.Contains(strings.ToLower(s), sub) {
			return true
		}
	}
	return false
}

func mergeConfigWithJobOptions(globalCfg *config.Config, opts *JobConfigOptions) (*config.Config, error) {
	cfg := cloneConfig(globalCfg)
	if cfg == nil {
		cfg = &config.Config{}
	}
	cfg.ApplyDefaults()

	if err := applyJobConfigOverrides(cfg, opts); err != nil {
		return nil, err
	}

	if cfg.DownloadLocation == "" {
		return nil, errors.New("outputPath/downloadLocation cannot be empty")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func runtimeConfigFrom(cfg *config.Config) JobRuntimeConfig {
	if cfg == nil {
		return JobRuntimeConfig{}
	}
	return JobRuntimeConfig{
		Quality:                   cfg.Quality,
		Views:                     config.NormalizeViews(cfg.Views),
		AudioOnly:                 cfg.AudioOnly,
		AudioFormat:               cfg.AudioFormat,
		OutputPath:                cfg.DownloadLocation,
		EnablePipeline:            cfg.EnablePipeline,
		NumWorkers:                cfg.NumWorkers,
		DownloadWorkersPerLecture: cfg.DownloadWorkersPerLecture,
		DecryptWorkersPerLecture:  cfg.DecryptWorkersPerLecture,
		Slides:                    cfg.Slides,
		SkipNoAudio:               cfg.SkipNoAudio,
	}
}

func cloneConfig(cfg *config.Config) *config.Config {
	if cfg == nil {
		return nil
	}
	clone := *cfg
	return &clone
}

// sanitizeFilename strips path separators and applies filepath.Base to prevent
// path traversal when interpolating untrusted values (e.g. lecture.Topic) into filenames.
func sanitizeFilename(s string) string {
	s = strings.ReplaceAll(s, string(filepath.Separator), "_")
	s = strings.ReplaceAll(s, "/", "_")
	return filepath.Base(s)
}

// maxSlideSize caps a single generated slide PDF at 100 MiB. The streaming
// check remains necessary when upstream omits or misstates Content-Length.
const maxSlideSize int64 = 100 * 1024 * 1024

var errSlideSizeLimit = errors.New("slide exceeds size limit")

func downloadLectureSlide(ctx context.Context, c *client.Client, cfg *config.Config, lecture client.Lecture) error {
	return downloadLectureSlideWithLimit(ctx, c, cfg, lecture, maxSlideSize)
}

func downloadLectureSlideWithLimit(ctx context.Context, c *client.Client, cfg *config.Config, lecture client.Lecture, limit int64) error {
	if cfg.BaseURL == "" {
		return errors.New("baseUrl is required")
	}

	// G301: 0755 is standard for user download directories
	// #nosec G301
	if err := os.MkdirAll(cfg.DownloadLocation, 0o755); err != nil {
		return err
	}

	url := fmt.Sprintf("%s/videos/%d/auto-generated-pdf", cfg.BaseURL, lecture.VideoID)
	resp, err := c.GetAuthorizedWithToken(ctx, url, cfg.Token)
	if err != nil {
		return err
	}
	bodyClosed := false
	defer func() {
		if !bodyClosed {
			_ = resp.Body.Close() //nolint:errcheck
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 512))
		if readErr != nil {
			return fmt.Errorf("slide download failed for lecture %d with status %d and unreadable body: %w", lecture.SeqNo, resp.StatusCode, readErr)
		}
		return fmt.Errorf("slide download failed for lecture %d with status %d: %s", lecture.SeqNo, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if resp.ContentLength > limit {
		return fmt.Errorf("slide download exceeds max size %d bytes: %w", limit, errSlideSizeLimit)
	}

	filePath := filepath.Join(cfg.DownloadLocation, fmt.Sprintf("LEC %03d %s.pdf", lecture.SeqNo, sanitizeFilename(lecture.Topic)))
	// G304: file paths are constructed from validated config and internal data.
	// #nosec G304
	f, err := os.CreateTemp(cfg.DownloadLocation, ".slide-*.part")
	if err != nil {
		return err
	}
	partPath := f.Name()
	removePart := true
	defer func() {
		_ = f.Close() //nolint:errcheck
		if removePart {
			_ = os.Remove(partPath) //nolint:errcheck
		}
	}()

	written, copyErr := io.Copy(f, io.LimitReader(resp.Body, limit+1))
	if copyErr != nil {
		return fmt.Errorf("write slide download: %w", copyErr)
	}
	if written > limit {
		return fmt.Errorf("slide download exceeds max size %d bytes: %w", limit, errSlideSizeLimit)
	}
	if closeErr := f.Close(); closeErr != nil {
		return fmt.Errorf("close slide download: %w", closeErr)
	}
	if closeErr := resp.Body.Close(); closeErr != nil {
		return fmt.Errorf("close slide response: %w", closeErr)
	}
	bodyClosed = true
	if renameErr := os.Rename(partPath, filePath); renameErr != nil {
		return fmt.Errorf("finalize slide download: %w", renameErr)
	}
	removePart = false
	return nil
}

func applyOutputPathOverride(cfg *config.Config, outputPath *string) error {
	if outputPath == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*outputPath)
	if trimmed == "" {
		return nil
	}
	// API overrides are untrusted: reject absolute paths and traversal escapes
	// instead of silently discarding them (the caller surfaces the error).
	location, err := paths.ValidateDownloadLocation(trimmed, false)
	if err != nil {
		return err
	}
	cfg.DownloadLocation = location
	return nil
}

func applyJobConfigOverrides(cfg *config.Config, opts *JobConfigOptions) error {
	if opts == nil {
		return nil
	}
	if opts.Quality != nil {
		cfg.Quality = *opts.Quality
	}
	if opts.Views != nil {
		cfg.Views = config.NormalizeViews(*opts.Views)
	}
	if opts.AudioOnly != nil {
		cfg.AudioOnly = *opts.AudioOnly
	}
	if opts.AudioFormat != nil {
		cfg.AudioFormat = *opts.AudioFormat
	}
	if err := applyOutputPathOverride(cfg, opts.OutputPath); err != nil {
		return err
	}
	if opts.EnablePipeline != nil {
		cfg.EnablePipeline = *opts.EnablePipeline
	}
	if opts.NumWorkers != nil {
		cfg.NumWorkers = *opts.NumWorkers
	}
	if opts.DownloadWorkersPerLecture != nil {
		cfg.DownloadWorkersPerLecture = *opts.DownloadWorkersPerLecture
	}
	if opts.DecryptWorkersPerLecture != nil {
		cfg.DecryptWorkersPerLecture = *opts.DecryptWorkersPerLecture
	}
	if opts.SkipNoAudio != nil {
		cfg.SkipNoAudio = *opts.SkipNoAudio
	}
	return nil
}
