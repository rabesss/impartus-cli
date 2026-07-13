package server

import (
	"context"
	"errors"
	"log"
	"os"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/downloader"
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
	s.jobEventMu.Lock()
	defer s.jobEventMu.Unlock()
	return s.updateRunningProgressLocked(jobID, progress, phase, details)
}

func (s *APIServer) updateRunningProgressLocked(jobID string, progress float64, phase string, details any) bool {
	applied, err := s.jobStore.UpdateRunningIfActive(jobID, progress)
	if err != nil {
		log.Printf("failed to persist running job %s: %v", jobID, err)
		return false
	}
	if !applied {
		return false
	}
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
	s.jobEventMu.Lock()
	_, err := s.jobStore.CancelJob(jobID)
	s.jobEventMu.Unlock()
	if err != nil {
		var terminalErr *TerminalStatusError
		if !errors.As(err, &terminalErr) && !errors.Is(err, ErrJobNotFound) {
			log.Printf("failed to persist canceled job %s: %v", jobID, err)
		}
	}

	return true
}

func (s *APIServer) startJob(jobID string) bool {
	s.jobEventMu.Lock()
	defer s.jobEventMu.Unlock()
	if !s.updateRunningProgressLocked(jobID, 2, "initializing", nil) {
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
	s.jobEventMu.Lock()
	defer s.jobEventMu.Unlock()
	if err := s.jobStore.CompleteJob(jobID, finalOutputs); err != nil {
		var terminalErr *TerminalStatusError
		if !errors.As(err, &terminalErr) {
			log.Printf("failed to durably complete job %s: %v", jobID, err)
		}
		return
	}
	evt := newWSEvent("job.completed", jobID)
	evt.Status = StatusCompleted
	evt.Progress = 100
	evt.Outputs = finalOutputs
	broadcastEvent(s.wsHub, evt)
}

func (s *APIServer) failJob(jobID, errMsg string) {
	s.jobEventMu.Lock()
	defer s.jobEventMu.Unlock()
	applied, err := s.jobStore.FailJobIfNonTerminal(jobID, errMsg)
	if err != nil {
		log.Printf("failed to durably fail job %s: %v", jobID, err)
		return
	}
	if !applied {
		return
	}
	evt := newWSEvent("job.failed", jobID)
	evt.Status = StatusFailed
	evt.Error = errMsg
	broadcastEvent(s.wsHub, evt)
}
