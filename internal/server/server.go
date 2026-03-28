package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/downloader"
)

const statusCanceled = "canceled"

type JobConfigOptions struct {
	Quality                   *string `json:"quality,omitempty"`
	Views                     *string `json:"views,omitempty"`
	AudioOnly                 *bool   `json:"audioOnly,omitempty"`
	AudioFormat               *string `json:"audioFormat,omitempty"`
	OutputPath                *string `json:"outputPath,omitempty"`
	EnablePipeline            *bool   `json:"enablePipeline,omitempty"`
	NumWorkers                *int    `json:"numWorkers,omitempty"`
	DownloadWorkersPerLecture *int    `json:"downloadWorkersPerLecture,omitempty"`
	DecryptWorkersPerLecture  *int    `json:"decryptWorkersPerLecture,omitempty"`
	SkipNoAudio               *bool   `json:"skipNoAudio,omitempty"`
}

type createJobRequest struct {
	SubjectID      int    `json:"subjectId"`
	SessionID      int    `json:"sessionId"`
	StartIndex     int    `json:"startIndex"`
	EndIndex       int    `json:"endIndex"`
	IdempotencyKey string `json:"idempotencyKey,omitempty"`

	JobConfig *JobConfigOptions `json:"jobConfig,omitempty"`

	Quality                   *string `json:"quality,omitempty"`
	Views                     *string `json:"views,omitempty"`
	AudioOnly                 *bool   `json:"audioOnly,omitempty"`
	AudioFormat               *string `json:"audioFormat,omitempty"`
	OutputPath                *string `json:"outputPath,omitempty"`
	EnablePipeline            *bool   `json:"enablePipeline,omitempty"`
	NumWorkers                *int    `json:"numWorkers,omitempty"`
	DownloadWorkersPerLecture *int    `json:"downloadWorkersPerLecture,omitempty"`
	DecryptWorkersPerLecture  *int    `json:"decryptWorkersPerLecture,omitempty"`
	SkipNoAudio               *bool   `json:"skipNoAudio,omitempty"`
}

func (r createJobRequest) effectiveJobConfig() *JobConfigOptions {
	if r.JobConfig != nil {
		return r.JobConfig
	}

	if r.Quality == nil &&
		r.Views == nil &&
		r.AudioOnly == nil &&
		r.AudioFormat == nil &&
		r.OutputPath == nil &&
		r.EnablePipeline == nil &&
		r.NumWorkers == nil &&
		r.DownloadWorkersPerLecture == nil &&
		r.DecryptWorkersPerLecture == nil &&
		r.SkipNoAudio == nil {
		return nil
	}

	return &JobConfigOptions{
		Quality:                   r.Quality,
		Views:                     r.Views,
		AudioOnly:                 r.AudioOnly,
		AudioFormat:               r.AudioFormat,
		OutputPath:                r.OutputPath,
		EnablePipeline:            r.EnablePipeline,
		NumWorkers:                r.NumWorkers,
		DownloadWorkersPerLecture: r.DownloadWorkersPerLecture,
		DecryptWorkersPerLecture:  r.DecryptWorkersPerLecture,
		SkipNoAudio:               r.SkipNoAudio,
	}
}

type JobRuntimeConfig struct {
	Quality                   string `json:"quality"`
	Views                     string `json:"views"`
	AudioOnly                 bool   `json:"audioOnly"`
	AudioFormat               string `json:"audioFormat"`
	OutputPath                string `json:"outputPath"`
	EnablePipeline            bool   `json:"enablePipeline"`
	NumWorkers                int    `json:"numWorkers"`
	DownloadWorkersPerLecture int    `json:"downloadWorkersPerLecture"`
	DecryptWorkersPerLecture  int    `json:"decryptWorkersPerLecture"`
	Slides                    bool   `json:"slides"`
	SkipNoAudio               bool   `json:"skipNoAudio"`
}

type Job struct {
	ID                string           `json:"id"`
	SubjectID         int              `json:"subjectId"`
	SessionID         int              `json:"sessionId"`
	StartIndex        int              `json:"startIndex"`
	EndIndex          int              `json:"endIndex"`
	Status            string           `json:"status"`
	Progress          float64          `json:"progress"`
	Error             string           `json:"error,omitempty"`
	TotalLectures     int              `json:"totalLectures,omitempty"`
	CompletedLectures int              `json:"completedLectures,omitempty"`
	FilteredLectures  int              `json:"filteredLectures,omitempty"`
	Outputs           []string         `json:"outputs,omitempty"`
	Config            JobRuntimeConfig `json:"config"`
	IdempotencyKey    string           `json:"idempotencyKey,omitempty"`
	CreatedAt         time.Time        `json:"createdAt"`
	UpdatedAt         time.Time        `json:"updatedAt"`

	ctx    context.Context    `json:"-"`
	cancel context.CancelFunc `json:"-"`
	cfg    *config.Config     `json:"-"`
}

const maxIdempotencyKeyLength = 256

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

// GetJobByldempotencyKey looks up a job by its idempotency key.
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

type WSHub struct {
	clients map[*websocket.Conn]bool
	mu      sync.Mutex
}

func NewWSHub() *WSHub {
	return &WSHub{clients: make(map[*websocket.Conn]bool)}
}

func (h *WSHub) Register(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[conn] = true
}

func (h *WSHub) Unregister(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, conn)
}

func (h *WSHub) Broadcast(msg map[string]any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	for conn := range h.clients {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			_ = conn.Close()
			delete(h.clients, conn)
		}
	}

	return nil
}

// upstreamCacheEntry holds a cached upstream client and token info.
// The token expires field is used to determine when to refresh.
type upstreamCacheEntry struct {
	client    *client.Client
	cfg       *config.Config
	token     string
	expiresAt time.Time
}

// UpstreamLoginFunc is the signature for upstream login operations.
// It receives a context and config, and returns a client with token set.
// This allows injecting mock login functions in tests.
type UpstreamLoginFunc func(ctx context.Context, cfg *config.Config) (*client.Client, *config.Config, error)

type APIServer struct {
	cfg             *config.Config
	jobStore        *JobStore
	wsHub           *WSHub
	tokenStore      *TokenStore
	upgrader        websocket.Upgrader
	router          *mux.Router
	port            string
	upstreamCache   *upstreamCacheEntry
	upstreamCacheMu sync.RWMutex
	upstreamLogin   UpstreamLoginFunc
}

// defaultUpstreamLogin is the real upstream login using the Impartus API.
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

func (s *APIServer) registerRoutes() {
	s.router.Use(requestIDMiddleware)
	s.router.Use(corsMiddleware)

	api := s.router.PathPrefix("/api/v1").Subrouter()
	api.HandleFunc("/health", s.healthHandler).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/auth/login", s.loginHandler).Methods(http.MethodPost, http.MethodOptions)

	protected := api.PathPrefix("").Subrouter()
	protected.Use(s.authMiddleware)
	protected.HandleFunc("/ws", s.websocketHandler).Methods(http.MethodGet)
	protected.HandleFunc("/courses", s.coursesHandler).Methods(http.MethodGet, http.MethodOptions)
	protected.HandleFunc("/lectures", s.lecturesHandler).Methods(http.MethodGet, http.MethodOptions)
	protected.HandleFunc("/jobs", s.createJobHandler).Methods(http.MethodPost, http.MethodOptions)
	protected.HandleFunc("/jobs", s.listJobsHandler).Methods(http.MethodGet, http.MethodOptions)
	protected.HandleFunc("/jobs/{id}", s.getJobHandler).Methods(http.MethodGet, http.MethodOptions)
	protected.HandleFunc("/jobs/{id}", s.deleteJobHandler).Methods(http.MethodDelete, http.MethodOptions)
}

// requestIDMiddleware adds a unique request ID to each request for distributed tracing.
// It propagates existing X-Request-ID headers or generates a new UUID.
func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.NewString()
		}

		// Set the request ID in response headers for client correlation
		w.Header().Set("X-Request-ID", requestID)

		// Add request ID to request context for downstream handlers
		ctx := context.WithValue(r.Context(), requestIDKey{}, requestID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requestIDKey is the context key for request IDs
type requestIDKey struct{}

// GetRequestID retrieves the request ID from the request context
func GetRequestID(r *http.Request) string {
	if id, ok := r.Context().Value(requestIDKey{}).(string); ok {
		return id
	}
	return ""
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID")
		w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID")
		w.Header().Set("Access-Control-Max-Age", "86400")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *APIServer) healthHandler(w http.ResponseWriter, _ *http.Request) {
	configStatus := s.checkConfigStatus()
	upstreamStatus := s.checkUpstreamStatus()
	ffmpegStatus := s.checkFFmpegStatus()

	overallStatus := "ok"
	if configStatus["status"] != "ok" || upstreamStatus["status"] != "reachable" || ffmpegStatus["status"] != "available" {
		overallStatus = "degraded"
	}

	respondWithEnvelope(w, http.StatusOK, "health", map[string]any{
		"status":   overallStatus,
		"config":   configStatus,
		"upstream": upstreamStatus,
		"ffmpeg":   ffmpegStatus,
	})
}

// checkConfigStatus verifies that required config fields are set
func (s *APIServer) checkConfigStatus() map[string]any {
	result := map[string]any{
		"status": "ok",
	}

	if s.cfg == nil {
		result["status"] = "misconfigured"
		result["username"] = "missing"
		result["password"] = "missing"
		result["baseUrl"] = "missing"
		return result
	}

	usernameStatus := "ok"
	if s.cfg.Username == "" {
		usernameStatus = "missing"
		result["status"] = "misconfigured"
	}

	passwordStatus := "ok"
	if s.cfg.Password == "" {
		passwordStatus = "missing"
		result["status"] = "misconfigured"
	}

	baseUrlStatus := "ok"
	if s.cfg.BaseUrl == "" {
		baseUrlStatus = "missing"
		result["status"] = "misconfigured"
	}

	result["username"] = usernameStatus
	result["password"] = passwordStatus
	result["baseUrl"] = baseUrlStatus

	return result
}

// checkUpstreamStatus attempts to reach the Impartus upstream server
// Uses cached token if available, otherwise does a TCP dial check
func (s *APIServer) checkUpstreamStatus() map[string]any {
	result := map[string]any{
		"status": "unreachable",
	}

	// If we have a cached token and config, try using it
	s.upstreamCacheMu.RLock()
	cached := s.upstreamCache
	s.upstreamCacheMu.RUnlock()

	if cached != nil && cached.token != "" && s.cfg != nil && s.cfg.BaseUrl != "" {
		// Try a lightweight HTTP request to check reachability
		// We just check if we can make a connection, not full auth
		baseURL := s.cfg.BaseUrl
		if !strings.HasPrefix(baseURL, "http") {
			baseURL = "https://" + baseURL
		}
		url := strings.TrimSuffix(baseURL, "/") + "/user/profile"

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+cached.token)
			client := &http.Client{Timeout: 5 * time.Second}
			resp, err := client.Do(req)
			if err == nil {
				defer resp.Body.Close()
				// Any response (even 401/403) means server is reachable
				// Only network errors mean unreachable
				result["status"] = "reachable"
				return result
			}
		}
	}

	// Fallback: try TCP dial to the base URL host
	if s.cfg != nil && s.cfg.BaseUrl != "" {
		baseURL := s.cfg.BaseUrl
		if !strings.HasPrefix(baseURL, "http") {
			baseURL = "https://" + baseURL
		}

		u, err := parseURL(baseURL)
		if err == nil {
			host := u.Host
			if !strings.Contains(host, ":") {
				if u.Scheme == "https" {
					host = net.JoinHostPort(host, "443")
				} else {
					host = net.JoinHostPort(host, "80")
				}
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			dialer := &net.Dialer{}
			conn, err := dialer.DialContext(ctx, "tcp", host)
			if err == nil {
				conn.Close()
				result["status"] = "reachable"
			}
		}
	}

	return result
}

// checkFFmpegStatus checks if FFmpeg is available in PATH
func (s *APIServer) checkFFmpegStatus() map[string]any {
	result := map[string]any{
		"status": "not_found",
	}

	if _, err := exec.LookPath("ffmpeg"); err == nil {
		result["status"] = "available"
	}

	return result
}

// parseURL is a simple URL parser that handles host extraction
func parseURL(rawURL string) (*url.URL, error) {
	return url.Parse(rawURL)
}

// getOrRefreshUpstreamClient returns a cached upstream client or creates a new one.
// It is thread-safe and handles token expiration by refreshing when needed.
// Returns the client, cloned config with token set, and any error.
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

func (s *APIServer) coursesHandler(w http.ResponseWriter, r *http.Request) {
	apiClient, cfg, err := s.getOrRefreshUpstreamClient(r.Context())
	if err != nil {
		respondWithError(w, http.StatusBadGateway, "LOGIN_FAILED", err.Error(), "courses", &retryHint{Retryable: true, RetryAfter: 30})
		return
	}

	courses, err := apiClient.GetCourses(r.Context(), cfg)
	if err != nil {
		respondWithError(w, http.StatusBadGateway, "COURSES_FETCH_FAILED", err.Error(), "courses", &retryHint{Retryable: true, RetryAfter: 30})
		return
	}

	respondWithEnvelope(w, http.StatusOK, "courses", courses)
}

func (s *APIServer) lecturesHandler(w http.ResponseWriter, r *http.Request) {
	subjectID := r.URL.Query().Get("subject_id")
	if subjectID == "" {
		subjectID = r.URL.Query().Get("subjectId")
	}
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		sessionID = r.URL.Query().Get("sessionId")
	}

	if subjectID == "" || sessionID == "" {
		respondWithError(w, http.StatusBadRequest, "MISSING_PARAMETER", "subject_id and session_id query parameters required", "lectures", nil)
		return
	}

	subjectInt, err := strconv.Atoi(subjectID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "INVALID_REQUEST", "subjectId must be a valid integer", "lectures", nil)
		return
	}
	sessionInt, err := strconv.Atoi(sessionID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "INVALID_REQUEST", "sessionId must be a valid integer", "lectures", nil)
		return
	}

	apiClient, cfg, loginErr := s.getOrRefreshUpstreamClient(r.Context())
	if loginErr != nil {
		respondWithError(w, http.StatusBadGateway, "LOGIN_FAILED", loginErr.Error(), "lectures", &retryHint{Retryable: true, RetryAfter: 30})
		return
	}

	lectures, err := apiClient.GetLectures(r.Context(), cfg, client.Course{SubjectID: subjectInt, SessionID: sessionInt})
	if err != nil {
		respondWithError(w, http.StatusBadGateway, "LECTURES_FETCH_FAILED", err.Error(), "lectures", &retryHint{Retryable: true, RetryAfter: 30})
		return
	}

	respondWithEnvelope(w, http.StatusOK, "lectures", lectures)
}

func (s *APIServer) createJobHandler(w http.ResponseWriter, r *http.Request) {
	var req createJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body", "createJob", nil)
		return
	}

	if req.SubjectID <= 0 {
		respondWithError(w, http.StatusBadRequest, "MISSING_PARAMETER", "subjectId is required and must be greater than 0", "createJob", nil)
		return
	}
	if req.SessionID <= 0 {
		respondWithError(w, http.StatusBadRequest, "MISSING_PARAMETER", "sessionId is required and must be greater than 0", "createJob", nil)
		return
	}
	// API uses 1-based indexing to match CLI semantics (--start/--end are 1-based)
	// Validate 1-based input, store 1-based in Job, convert to 0-based for execution
	if req.StartIndex < 1 {
		respondWithError(w, http.StatusBadRequest, "INVALID_REQUEST", "startIndex must be 1 or greater (1-based, matching CLI --start)", "createJob", nil)
		return
	}
	if req.EndIndex < req.StartIndex {
		respondWithError(w, http.StatusBadRequest, "INVALID_REQUEST", "endIndex must be greater than or equal to startIndex", "createJob", nil)
		return
	}

	// Validate idempotency key if provided
	if req.IdempotencyKey != "" {
		if len(req.IdempotencyKey) > maxIdempotencyKeyLength {
			respondWithError(w, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", fmt.Sprintf("idempotencyKey must be at most %d characters", maxIdempotencyKeyLength), "createJob", nil)
			return
		}
	}

	mergedCfg, err := mergeConfigWithJobOptions(s.cfg, req.effectiveJobConfig())
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "INVALID_JOB_CONFIG", err.Error(), "createJob", nil)
		return
	}

	// Store 1-based indices in Job (will be converted to 0-based during execution)
	job, created := s.jobStore.CreateJobWithKey(req.SubjectID, req.SessionID, req.StartIndex, req.EndIndex, mergedCfg, req.IdempotencyKey)

	if !created {
		// Duplicate idempotency key - return existing job with 409 Conflict
		respondWithEnvelope(w, http.StatusConflict, "createJob", map[string]any{
			"job":       job,
			"duplicate": true,
		})
		return
	}

	go s.executeJob(job.ID)

	respondWithEnvelope(w, http.StatusCreated, "createJob", job)
}

func (s *APIServer) listJobsHandler(w http.ResponseWriter, _ *http.Request) {
	respondWithEnvelope(w, http.StatusOK, "listJobs", s.jobStore.ListJobs())
}

func (s *APIServer) getJobHandler(w http.ResponseWriter, r *http.Request) {
	jobID := mux.Vars(r)["id"]
	if jobID == "" {
		respondWithError(w, http.StatusBadRequest, "MISSING_PARAMETER", "Job ID is required", "getJob", nil)
		return
	}

	job, ok := s.jobStore.GetJob(jobID)
	if !ok {
		respondWithError(w, http.StatusNotFound, "JOB_NOT_FOUND", "Job not found", "getJob", nil)
		return
	}

	respondWithEnvelope(w, http.StatusOK, "getJob", job)
}

func (s *APIServer) deleteJobHandler(w http.ResponseWriter, r *http.Request) {
	jobID := mux.Vars(r)["id"]
	if jobID == "" {
		respondWithError(w, http.StatusBadRequest, "MISSING_PARAMETER", "Job ID is required", "cancelJob", nil)
		return
	}

	job, err := s.jobStore.CancelJob(jobID)
	if err != nil {
		if err.Error() == "not_found" {
			respondWithError(w, http.StatusNotFound, "JOB_NOT_FOUND", "Job not found", "cancelJob", nil)
			return
		}
		if strings.HasPrefix(err.Error(), "terminal:") {
			respondWithError(w, http.StatusBadRequest, "JOB_CANNOT_CANCEL", "Cannot cancel job in terminal state", "cancelJob", nil, map[string]string{"status": strings.TrimPrefix(err.Error(), "terminal:")})
			return
		}
		respondWithError(w, http.StatusInternalServerError, "CANCEL_FAILED", err.Error(), "cancelJob", &retryHint{Retryable: true, RetryAfter: 10})
		return
	}

	broadcastEvent(s.wsHub, map[string]any{
		"type":      "job.cancelled",
		"jobId":     jobID,
		"status":    statusCanceled,
		"progress":  job.Progress,
		"timestamp": time.Now().Unix(),
	})

	respondWithSuccess(w, "cancelJob", map[string]any{"id": jobID, "status": statusCanceled})
}

func (s *APIServer) websocketHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	s.wsHub.Register(conn)
	defer s.wsHub.Unregister(conn)

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
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

func (s *APIServer) updateRunningProgress(jobID string, progress float64, phase string, details map[string]any) bool {
	job, ok := s.jobStore.GetJob(jobID)
	if !ok {
		return false
	}
	if job.ctx.Err() != nil || job.Status == statusCanceled {
		s.jobStore.UpdateJob(jobID, statusCanceled, progress, "")
		broadcastEvent(s.wsHub, map[string]any{
			"type":      "job.cancelled",
			"jobId":     jobID,
			"status":    statusCanceled,
			"progress":  progress,
			"timestamp": time.Now().Unix(),
		})
		return false
	}

	s.jobStore.UpdateJob(jobID, "running", progress, "")
	payload := map[string]any{
		"type":      "job.progress",
		"jobId":     jobID,
		"status":    "running",
		"progress":  progress,
		"phase":     phase,
		"timestamp": time.Now().Unix(),
	}
	if details != nil {
		payload["details"] = details
	}
	broadcastEvent(s.wsHub, payload)
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

	broadcastEvent(s.wsHub, map[string]any{
		"type":      "job.cancelled",
		"jobId":     jobID,
		"status":    statusCanceled,
		"timestamp": time.Now().Unix(),
	})
	return true
}

func (s *APIServer) startJob(jobID string) bool {
	if !s.updateRunningProgress(jobID, 2, "initializing", nil) {
		return false
	}
	broadcastEvent(s.wsHub, map[string]any{
		"type":      "job.started",
		"jobId":     jobID,
		"status":    "running",
		"timestamp": time.Now().Unix(),
	})
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
	reversed := reverseLecturesHelper(lectures)

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
		selected = filterNoAudioLectures(selected)
		// Update job with filtered count
		job.FilteredLectures = totalLectures - len(selected)
	}

	if len(selected) == 0 {
		return nil, errors.New("no lectures available after filtering (all lectures have noaudio=1 in the selected range)")
	}

	return selected, nil
}

// reverseLecturesHelper reverses the order of lectures slice.
// This matches CLI's selectLectureRange behavior for index alignment.
func reverseLecturesHelper(lectures client.Lectures) client.Lectures {
	reversed := make(client.Lectures, len(lectures))
	for i := range lectures {
		reversed[i] = lectures[len(lectures)-1-i]
	}
	return reversed
}

// filterNoAudioLectures filters out lectures with noaudio=1.
func filterNoAudioLectures(lectures client.Lectures) client.Lectures {
	filtered := make(client.Lectures, 0, len(lectures))
	for _, lecture := range lectures {
		if lecture.Noaudio == 1 {
			continue
		}
		filtered = append(filtered, lecture)
	}
	return filtered
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

func (s *APIServer) prepareDownload(ctx context.Context, jobID string, jobCtx context.Context, apiClient *client.Client, cfg *config.Config, selected client.Lectures) ([]downloader.ParsedPlaylist, *config.Config, bool) {
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
	playlists := toDownloaderPlaylists(clientPlaylists)
	if len(playlists) == 0 {
		s.failJob(jobID, "no downloadable playlists found")
		return nil, nil, false
	}
	downloadCfg := cloneConfig(cfg)
	downloadCfg.Views = mapViewsForDownloader(downloadCfg.Views)
	return playlists, downloadCfg, true
}

func (s *APIServer) runPlaylistDownloads(ctx context.Context, cancelLocal context.CancelFunc, jobCtx context.Context, jobID string, apiClient *client.Client, downloadCfg *config.Config, playlists []downloader.ParsedPlaylist) ([]string, bool) {
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
	broadcastEvent(s.wsHub, map[string]any{
		"type":      "job.completed",
		"jobId":     jobID,
		"status":    "completed",
		"progress":  100,
		"outputs":   finalOutputs,
		"timestamp": time.Now().Unix(),
	})
}

func (s *APIServer) failJob(jobID, errMsg string) {
	s.jobStore.UpdateJob(jobID, "failed", 0, errMsg)
	broadcastEvent(s.wsHub, map[string]any{
		"type":      "job.failed",
		"jobId":     jobID,
		"status":    "failed",
		"error":     errMsg,
		"timestamp": time.Now().Unix(),
	})
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
	if clone.BaseUrl == "" && clone.BaseURL != "" {
		clone.BaseUrl = clone.BaseURL
	}
	if clone.BaseURL == "" && clone.BaseUrl != "" {
		clone.BaseURL = clone.BaseUrl
	}
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

func toDownloaderPlaylists(in []client.ParsedPlaylist) []downloader.ParsedPlaylist {
	out := make([]downloader.ParsedPlaylist, 0, len(in))
	for _, playlist := range in {
		out = append(out, downloader.ParsedPlaylist{
			KeyURL:           playlist.KeyURL,
			Title:            playlist.Title,
			FirstViewURLs:    playlist.FirstViewURLs,
			SecondViewURLs:   playlist.SecondViewURLs,
			ID:               playlist.Id,
			SeqNo:            playlist.SeqNo,
			HasMultipleViews: playlist.HasMultipleViews,
		})
	}
	return out
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
	baseURL := cfg.BaseUrl
	if baseURL == "" {
		baseURL = cfg.BaseURL
	}

	if err := os.MkdirAll(cfg.DownloadLocation, 0o755); err != nil {
		return err
	}

	url := fmt.Sprintf("%s/videos/%d/auto-generated-pdf", baseURL, lecture.VideoID)
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

func broadcastEvent(hub *WSHub, payload map[string]any) {
	if err := hub.Broadcast(payload); err != nil {
		log.Printf("websocket broadcast failed: %v", err)
	}
}
