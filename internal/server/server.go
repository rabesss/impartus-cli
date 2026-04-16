package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/downloader"
)

type JobStore struct {
	jobs            map[string]*Job
	idempotencyKeys map[string]string // idempotencyKey -> jobID
	mu              sync.RWMutex
	persistence     *jobPersistence
}

// NewJobStore creates an in-memory job store with no persistence.
func NewJobStore() *JobStore {
	return &JobStore{
		jobs:            make(map[string]*Job),
		idempotencyKeys: make(map[string]string),
	}
}

// NewJobStoreWithPersistence creates a job store that persists to the given file path.
// If path is empty, defaults to ".jobs.json". Jobs are loaded from the file on creation.
func NewJobStoreWithPersistence(path string) *JobStore {
	js := &JobStore{
		jobs:            make(map[string]*Job),
		idempotencyKeys: make(map[string]string),
		persistence:     newJobPersistence(path),
	}
	js.loadFromDisk()
	return js
}

func (js *JobStore) CreateJob(subjectID, sessionID, startIndex, endIndex int, cfg *config.Config) *Job {
	js.mu.Lock()
	defer js.mu.Unlock()

	jobID := fmt.Sprintf("job-%d", time.Now().UnixNano())
	ctx, cancel := context.WithCancel(context.Background())
	job := &Job{
		ID:         jobID,
		SubjectID:  subjectID,
		SessionID:  sessionID,
		StartIndex: startIndex,
		EndIndex:   endIndex,
		Status:     "pending",
		Progress:   0,
		Config:     runtimeConfigFrom(cfg),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		ctx:        ctx,
		cancel:     cancel,
		cfg:        cloneConfig(cfg),
	}

	js.jobs[jobID] = job
	js.saveToDisk()
	return job
}

// CreateJobWithKey creates a new job with an optional idempotency key.
// If the idempotency key is non-empty and already exists, it returns the
// existing job instead of creating a new one. This prevents duplicate job
// creation on network retries. Returns the job and a boolean indicating
// whether the job was newly created (true) or returned from the idempotency
// cache (false).
func (js *JobStore) CreateJobWithKey(subjectID, sessionID, startIndex, endIndex int, cfg *config.Config, idempotencyKey string) (*Job, bool) {
	js.mu.Lock()
	defer js.mu.Unlock()

	// Check idempotency key for existing job
	if idempotencyKey != "" {
		if existingID, ok := js.idempotencyKeys[idempotencyKey]; ok {
			if job, ok := js.jobs[existingID]; ok {
				return job, false
			}
		}
	}

	jobID := fmt.Sprintf("job-%d", time.Now().UnixNano())
	ctx, cancel := context.WithCancel(context.Background())
	job := &Job{
		ID:             jobID,
		SubjectID:      subjectID,
		SessionID:      sessionID,
		StartIndex:     startIndex,
		EndIndex:       endIndex,
		Status:         "pending",
		Progress:       0,
		Config:         runtimeConfigFrom(cfg),
		IdempotencyKey: idempotencyKey,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		ctx:            ctx,
		cancel:         cancel,
		cfg:            cloneConfig(cfg),
	}

	js.jobs[jobID] = job

	// Register idempotency key mapping
	if idempotencyKey != "" {
		js.idempotencyKeys[idempotencyKey] = jobID
	}

	js.saveToDisk()
	return job, true
}

// GetJobByIdempotencyKey looks up a job by its idempotency key.
func (js *JobStore) GetJobByIdempotencyKey(key string) (*Job, bool) {
	js.mu.RLock()
	defer js.mu.RUnlock()
	jobID, ok := js.idempotencyKeys[key]
	if !ok {
		return nil, false
	}
	job, ok := js.jobs[jobID]
	return job, ok
}

func (js *JobStore) GetJob(id string) (*Job, bool) {
	js.mu.RLock()
	defer js.mu.RUnlock()
	job, ok := js.jobs[id]
	return job, ok
}

func (js *JobStore) ListJobs() []*Job {
	js.mu.RLock()
	defer js.mu.RUnlock()

	jobs := make([]*Job, 0, len(js.jobs))
	for _, job := range js.jobs {
		jobs = append(jobs, job)
	}
	return jobs
}

func (js *JobStore) UpdateJob(id, status string, progress float64, errMsg string) {
	js.mu.Lock()
	defer js.mu.Unlock()

	job, ok := js.jobs[id]
	if !ok {
		return
	}

	job.Status = status
	job.Progress = progress
	job.Error = errMsg
	job.UpdatedAt = time.Now()
	js.saveToDisk()
}

func (js *JobStore) SetLectureProgress(id string, completed, total int) {
	js.mu.Lock()
	defer js.mu.Unlock()

	job, ok := js.jobs[id]
	if !ok {
		return
	}
	job.CompletedLectures = completed
	job.TotalLectures = total
	job.UpdatedAt = time.Now()
	js.saveToDisk()
}

func (js *JobStore) SetOutputs(id string, outputs []string) {
	js.mu.Lock()
	defer js.mu.Unlock()

	job, ok := js.jobs[id]
	if !ok {
		return
	}
	job.Outputs = append([]string{}, outputs...)
	job.UpdatedAt = time.Now()
	js.saveToDisk()
}

func (js *JobStore) CancelJob(id string) (*Job, error) {
	js.mu.Lock()
	defer js.mu.Unlock()

	job, ok := js.jobs[id]
	if !ok {
		return nil, errors.New("not_found")
	}

	if job.Status == "completed" || job.Status == "failed" || job.Status == statusCanceled {
		return nil, fmt.Errorf("terminal:%s", job.Status)
	}

	job.Status = statusCanceled
	job.UpdatedAt = time.Now()
	job.cancel()
	js.saveToDisk()
	return job, nil
}

// loadFromDisk loads previously persisted jobs from the persistence file.
// Jobs that were in a terminal state (completed, failed, canceled) are restored
// with their preserved state. Running/pending jobs are restored as "failed" since
// they cannot be resumed after a restart.
func (js *JobStore) loadFromDisk() {
	if js.persistence == nil {
		return
	}

	persisted := js.persistence.load()
	if persisted == nil {
		return
	}

	for _, pj := range persisted {
		createdAt, err := time.Parse(persistedTimeFormat, pj.CreatedAt)
		if err != nil {
			createdAt = time.Time{}
		}
		updatedAt, err := time.Parse(persistedTimeFormat, pj.UpdatedAt)
		if err != nil {
			updatedAt = time.Time{}
		}

		// Jobs that were running/pending at shutdown cannot be resumed
		status := pj.Status
		if status == "pending" || status == "running" {
			status = "failed"
			if pj.Error == "" {
				pj.Error = "job interrupted by server restart"
			}
		}

		ctx, cancel := context.WithCancel(context.Background())
		js.jobs[pj.ID] = &Job{
			ID:                pj.ID,
			SubjectID:         pj.SubjectID,
			SessionID:         pj.SessionID,
			StartIndex:        pj.StartIndex,
			EndIndex:          pj.EndIndex,
			Status:            status,
			Progress:          pj.Progress,
			Error:             pj.Error,
			TotalLectures:     pj.TotalLectures,
			CompletedLectures: pj.CompletedLectures,
			FilteredLectures:  pj.FilteredLectures,
			Outputs:           append([]string{}, pj.Outputs...),
			Config:            pj.Config,
			IdempotencyKey:    pj.IdempotencyKey,
			CreatedAt:         createdAt,
			UpdatedAt:         updatedAt,
			ctx:               ctx,
			cancel:            cancel,
		}

		// Rebuild idempotency key index
		if pj.IdempotencyKey != "" {
			js.idempotencyKeys[pj.IdempotencyKey] = pj.ID
		}
	}
}

// saveToDisk persists all jobs to the persistence file.
func (js *JobStore) saveToDisk() {
	if js.persistence == nil {
		return
	}
	if err := js.persistence.save(js.jobs); err != nil {
		log.Printf("warning: failed to persist jobs to %s: %v", js.persistence.path, err)
	}
}

func defaultUpstreamLogin(ctx context.Context, cfg *config.Config) (*client.Client, *config.Config, error) {
	apiClient := client.New(nil, nil)
	if err := apiClient.LoginAndSetToken(ctx, cfg); err != nil {
		return nil, nil, err
	}
	return apiClient, cfg, nil
}

func NewAPIServer(port string, cfg *config.Config) *APIServer {
	return NewAPIServerWithLogin(port, cfg, nil)
}

// NewAPIServerWithPersistence creates an APIServer with job persistence enabled.
// Jobs are persisted to the given file path (defaults to ".jobs.json" if empty).
func NewAPIServerWithPersistence(port string, cfg *config.Config, persistencePath string) *APIServer {
	return newAPIServerFull(port, cfg, nil, persistencePath, true)
}

// NewAPIServerWithLogin creates an APIServer with a custom upstream login function.
// If loginFn is nil, the default real upstream login is used.
func NewAPIServerWithLogin(port string, cfg *config.Config, loginFn UpstreamLoginFunc) *APIServer {
	return newAPIServerFull(port, cfg, loginFn, "", false)
}

// newAPIServerFull is the internal constructor with all options.
func newAPIServerFull(port string, cfg *config.Config, loginFn UpstreamLoginFunc, persistencePath string, persistenceEnabled bool) *APIServer {
	baseCfg := cloneConfig(cfg)
	if baseCfg == nil {
		baseCfg = &config.Config{}
	}
	baseCfg.ApplyDefaults()
	if baseCfg.DownloadLocation == "" {
		baseCfg.DownloadLocation = "./downloads"
	}
	if baseCfg.TempDirLocation == "" {
		baseCfg.TempDirLocation = "./temp"
	}

	if loginFn == nil {
		loginFn = defaultUpstreamLogin
	}

	s := &APIServer{
		cfg:        baseCfg,
		wsHub:      NewWSHub(),
		tokenStore: NewTokenStore(),
		upgrader: websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
			return true
		}},
		router:        mux.NewRouter(),
		port:          port,
		upstreamLogin: loginFn,
	}

	// Initialize job store (with persistence if enabled)
	if persistenceEnabled {
		s.jobStore = NewJobStoreWithPersistence(persistencePath)
	} else {
		s.jobStore = NewJobStore()
	}

	StartTokenCleanup(s.tokenStore)
	s.registerRoutes()
	return s
}

func (s *APIServer) Start(ctxs ...context.Context) error {
	logFile, err := os.OpenFile("api.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o666)
	if err == nil {
		defer logFile.Close()
		log.SetOutput(logFile)
	}

	addr := ":" + s.port
	server := &http.Server{
		Addr:         addr,
		Handler:      s.router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	if len(ctxs) > 0 && ctxs[0] != nil {
		go func() {
			<-ctxs[0].Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("server shutdown failed: %v", err)
			}
		}()
	}

	log.Printf("Starting API server on %s", addr)
	return server.ListenAndServe()
}

func (s *APIServer) getOrRefreshUpstreamClient(ctx context.Context) (*client.Client, *config.Config, error) {
	// Fast path: check if we have a valid cached entry
	s.upstreamCacheMu.RLock()
	cached := s.upstreamCache
	if cached != nil {
		valid := cached.expiresAt.After(time.Now()) && cached.token != ""
		if valid {
			cfg := cloneConfig(s.cfg)
			cfg.Token = cached.token
			s.upstreamCacheMu.RUnlock()
			return cached.client, cfg, nil
		}
	}
	s.upstreamCacheMu.RUnlock()

	// Slow path: need to login and cache - acquire write lock
	s.upstreamCacheMu.Lock()
	defer s.upstreamCacheMu.Unlock()

	// Double-check after acquiring write lock (another goroutine might have refreshed)
	cached = s.upstreamCache
	if cached != nil {
		valid := cached.expiresAt.After(time.Now()) && cached.token != ""
		if valid {
			cfg := cloneConfig(s.cfg)
			cfg.Token = cached.token
			return cached.client, cfg, nil
		}
	}

	// No cache or expired - do fresh login via injectable login function
	cfg := cloneConfig(s.cfg)
	apiClient, loginCfg, err := s.upstreamLogin(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}

	// Cache the result (token stored in cfg.Token by LoginAndSetToken)
	token := loginCfg.Token
	newEntry := &upstreamCacheEntry{
		client:    apiClient,
		cfg:       loginCfg,
		token:     token,
		expiresAt: time.Now().Add(23 * time.Hour), // Token typically valid for 24h
	}

	s.upstreamCache = newEntry

	return apiClient, loginCfg, nil
}

func (s *APIServer) executeJob(jobID string) {
	job, ok := s.jobStore.GetJob(jobID)
	if !ok {
		return
	}

	jobCtx := job.ctx
	ctx, cancelLocal := context.WithCancel(jobCtx)
	defer cancelLocal()

	if !s.startJob(jobID) {
		return
	}

	cfg, apiClient, ok := s.prepareJobRuntime(ctx, jobID, job.ctx, job.cfg)
	if !ok {
		return
	}
	selected, ok := s.fetchSelectedLectures(ctx, jobID, jobCtx, apiClient, cfg, job)
	if !ok {
		return
	}
	s.jobStore.SetLectureProgress(jobID, 0, len(selected))

	s.maybeDownloadSlides(ctx, jobID, jobCtx, apiClient, cfg, selected)
	playlists, downloadCfg, ok := s.prepareDownload(ctx, jobID, jobCtx, apiClient, cfg, selected)
	if !ok {
		return
	}
	finalOutputs, ok := s.runPlaylistDownloads(ctx, cancelLocal, jobCtx, jobID, apiClient, downloadCfg, playlists)
	if !ok {
		return
	}
	s.completeJob(jobID, finalOutputs)
}

func (s *APIServer) updateRunningProgress(jobID string, progress float64, phase string, details any) bool {
	job, ok := s.jobStore.GetJob(jobID)
	if !ok {
		return false
	}
	if job.ctx.Err() != nil || job.Status == statusCanceled {
		s.jobStore.UpdateJob(jobID, statusCanceled, progress, "")
		evt := newWSEvent("job.cancelled", jobID)
		evt.Status = statusCanceled
		evt.Progress = progress
		broadcastEvent(s.wsHub, evt)
		return false
	}

	s.jobStore.UpdateJob(jobID, "running", progress, "")
	evt := newWSEvent("job.progress", jobID)
	evt.Status = "running"
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
	job, ok := s.jobStore.GetJob(jobID)
	if ok {
		s.jobStore.UpdateJob(jobID, statusCanceled, job.Progress, "")
	} else {
		s.jobStore.UpdateJob(jobID, statusCanceled, 0, "")
	}

	evt := newWSEvent("job.cancelled", jobID)
	evt.Status = statusCanceled
	broadcastEvent(s.wsHub, evt)
	return true
}

func (s *APIServer) startJob(jobID string) bool {
	if !s.updateRunningProgress(jobID, 2, "initializing", nil) {
		return false
	}
	evt := newWSEvent("job.started", jobID)
	evt.Status = "running"
	broadcastEvent(s.wsHub, evt)
	return true
}

func (s *APIServer) prepareJobRuntime(ctx context.Context, jobID string, jobCtx context.Context, jobCfg *config.Config) (*config.Config, *client.Client, bool) {
	cfg := cloneConfig(jobCfg)
	if cfg == nil {
		s.failJob(jobID, "missing job config")
		return nil, nil, false
	}
	if cfg.DownloadLocation == "" {
		cfg.DownloadLocation = "./downloads"
	}
	if cfg.TempDirLocation == "" {
		cfg.TempDirLocation = "./temp"
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
		s.failJob(jobID, loginErr.Error())
		return nil, nil, false
	}
	// Use the token from cached config to ensure consistency
	cfg.Token = cachedCfg.Token
	return cfg, apiClient, true
}

func ensureJobDirectories(cfg *config.Config) error {
	if err := os.MkdirAll(cfg.DownloadLocation, 0o755); err != nil {
		return err
	}
	return os.MkdirAll(cfg.TempDirLocation, 0o755)
}

func (s *APIServer) fetchSelectedLectures(ctx context.Context, jobID string, jobCtx context.Context, apiClient *client.Client, cfg *config.Config, job *Job) (client.Lectures, bool) {
	if !s.updateRunningProgress(jobID, 15, "fetching_lectures", nil) {
		return nil, false
	}
	lectures, err := apiClient.GetLectures(ctx, cfg, client.Course{SubjectID: job.SubjectID, SessionID: job.SessionID})
	if err != nil {
		if s.handleCancelIfNeeded(jobID, jobCtx.Err()) {
			return nil, false
		}
		s.failJob(jobID, err.Error())
		return nil, false
	}
	selected, selectErr := selectJobLectures(job, lectures)
	if selectErr != nil {
		s.failJob(jobID, selectErr.Error())
		return nil, false
	}
	return selected, true
}

func selectJobLectures(job *Job, lectures client.Lectures) (client.Lectures, error) {
	if len(lectures) == 0 {
		return nil, errors.New("no lectures found")
	}
	// Reverse lectures to match CLI's selectLectureRange behavior
	// CLI reverses before slicing, so API must do the same for index alignment
	reversed := lectures.Reverse()

	// Apply default range handling (matching CLI's selectLectureRange behavior)
	// If start <= 0, default to 1; if end <= 0, default to all available
	start := job.StartIndex
	end := job.EndIndex
	if start <= 0 {
		start = 1
	}
	if end <= 0 {
		end = len(reversed)
	}

	// Job stores 1-based indices (matching CLI and API contract)
	// Convert to 0-based for internal slice access
	startZeroBased := start - 1
	endZeroBased := end - 1
	if startZeroBased >= len(reversed) {
		return nil, fmt.Errorf("startIndex %d out of range for %d lectures", start, len(reversed))
	}
	endIdx := endZeroBased
	if endIdx >= len(reversed) {
		endIdx = len(reversed) - 1
	}
	selected := reversed[startZeroBased : endIdx+1]

	// Apply noaudio filter if configured
	if job.Config.SkipNoAudio {
		totalLectures := len(selected)
		selected = selected.FilterNoAudio()
		// Update job with filtered count
		job.FilteredLectures = totalLectures - len(selected)
	}

	if len(selected) == 0 {
		return nil, errors.New("no lectures available after filtering (all lectures have noaudio=1 in the selected range)")
	}

	return selected, nil
}

func (s *APIServer) maybeDownloadSlides(ctx context.Context, jobID string, jobCtx context.Context, apiClient *client.Client, cfg *config.Config, lectures client.Lectures) {
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

func (s *APIServer) prepareDownload(ctx context.Context, jobID string, jobCtx context.Context, apiClient *client.Client, cfg *config.Config, selected client.Lectures) ([]client.ParsedPlaylist, *config.Config, bool) {
	if !s.updateRunningProgress(jobID, 30, "fetching_playlists", nil) {
		return nil, nil, false
	}
	clientPlaylists, err := apiClient.GetPlaylists(ctx, cfg, selected)
	if err != nil {
		if s.handleCancelIfNeeded(jobID, jobCtx.Err()) {
			return nil, nil, false
		}
		s.failJob(jobID, err.Error())
		return nil, nil, false
	}
	playlists := clientPlaylists
	if len(playlists) == 0 {
		s.failJob(jobID, "no downloadable playlists found")
		return nil, nil, false
	}
	downloadCfg := cloneConfig(cfg)
	downloadCfg.Views = mapViewsForDownloader(downloadCfg.Views)
	return playlists, downloadCfg, true
}

func (s *APIServer) runPlaylistDownloads(ctx context.Context, cancelLocal context.CancelFunc, jobCtx context.Context, jobID string, apiClient *client.Client, downloadCfg *config.Config, playlists []client.ParsedPlaylist) ([]string, bool) {
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
		s.failJob(jobID, err.Error())
		return nil, false
	}
	if s.handleCancelIfNeeded(jobID, jobCtx.Err()) {
		return nil, false
	}
	return outputs, true
}

func (s *APIServer) completeJob(jobID string, finalOutputs []string) {
	s.jobStore.SetOutputs(jobID, finalOutputs)
	s.jobStore.UpdateJob(jobID, "completed", 100, "")
	evt := newWSEvent("job.completed", jobID)
	evt.Status = "completed"
	evt.Progress = 100
	evt.Outputs = finalOutputs
	broadcastEvent(s.wsHub, evt)
}

func (s *APIServer) failJob(jobID, errMsg string) {
	s.jobStore.UpdateJob(jobID, "failed", 0, errMsg)
	evt := newWSEvent("job.failed", jobID)
	evt.Status = "failed"
	evt.Error = errMsg
	broadcastEvent(s.wsHub, evt)
}

func mergeConfigWithJobOptions(globalCfg *config.Config, opts *JobConfigOptions) (*config.Config, error) {
	cfg := cloneConfig(globalCfg)
	if cfg == nil {
		cfg = &config.Config{}
	}
	cfg.ApplyDefaults()

	if cfg.DownloadLocation == "" {
		cfg.DownloadLocation = "./downloads"
	}
	if cfg.TempDirLocation == "" {
		cfg.TempDirLocation = "./temp"
	}

	applyJobConfigOverrides(cfg, opts)

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
		Views:                     cfg.Views,
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

func mapViewsForDownloader(views string) string {
	switch views {
	case "first":
		return "left"
	case "second":
		return "right"
	default:
		return "both"
	}
}

func extractJoinOutputs(result downloader.JoinResult) []string {
	outputs := make([]string, 0, 3)
	if result.LeftOutput != "" {
		outputs = append(outputs, result.LeftOutput)
	}
	if result.RightOutput != "" {
		outputs = append(outputs, result.RightOutput)
	}
	if result.BothOutput != "" {
		outputs = append(outputs, result.BothOutput)
	}
	return outputs
}

func downloadLectureSlide(ctx context.Context, c *client.Client, cfg *config.Config, lecture client.Lecture) error {
	if cfg.BaseURL == "" {
		return errors.New("baseUrl is required")
	}

	if err := os.MkdirAll(cfg.DownloadLocation, 0o755); err != nil {
		return err
	}

	url := fmt.Sprintf("%s/videos/%d/auto-generated-pdf", cfg.BaseURL, lecture.VideoID)
	resp, err := c.GetAuthorizedWithToken(ctx, url, cfg.Token)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("slide download failed for lecture %d with status %d and unreadable body: %w", lecture.SeqNo, resp.StatusCode, readErr)
		}
		return fmt.Errorf("slide download failed for lecture %d with status %d: %s", lecture.SeqNo, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	filePath := filepath.Join(cfg.DownloadLocation, fmt.Sprintf("LEC %03d %s.pdf", lecture.SeqNo, lecture.Topic))
	f, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

func applyJobConfigOverrides(cfg *config.Config, opts *JobConfigOptions) {
	if opts == nil {
		return
	}
	if opts.Quality != nil {
		cfg.Quality = *opts.Quality
	}
	if opts.Views != nil {
		cfg.Views = *opts.Views
	}
	if opts.AudioOnly != nil {
		cfg.AudioOnly = *opts.AudioOnly
	}
	if opts.AudioFormat != nil {
		cfg.AudioFormat = *opts.AudioFormat
	}
	if opts.OutputPath != nil {
		cfg.DownloadLocation = strings.TrimSpace(*opts.OutputPath)
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
}
