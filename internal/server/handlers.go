package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/secrets"
)

const (
	healthCommand                 = "health"
	jobCancelledEventType         = "job.cancelled"
	lectureSubjectQueryParam      = "subjectId"
	lectureSubjectLegacyParam     = "subject_id"
	lectureSessionQueryParam      = "sessionId"
	lectureSessionLegacyParam     = "session_id"
	upstreamLoginFailedMessage    = "Failed to authenticate with Impartus API"
	upstreamCoursesFailedMessage  = "Failed to fetch courses from Impartus"
	upstreamLecturesFailedMessage = "Failed to fetch lectures from Impartus"
)

func (s *APIServer) registerRoutes() {
	s.router.Use(requestIDMiddleware)
	s.router.Use(s.corsMiddleware)

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

func (s *APIServer) healthHandler(w http.ResponseWriter, _ *http.Request) {
	configStatus := s.checkConfigStatus()
	upstreamStatus := s.checkUpstreamStatus()
	ffmpegStatus := s.checkFFmpegStatus()

	overallStatus := "ok"
	if configStatus.Status != "ok" || upstreamStatus.Status != "reachable" || ffmpegStatus.Status != "available" {
		overallStatus = "degraded"
	}

	respondWithEnvelope(w, http.StatusOK, healthCommand, healthResponse{
		Status:   overallStatus,
		Config:   configStatus,
		Upstream: upstreamStatus,
		FFmpeg:   ffmpegStatus,
	})
}

func (s *APIServer) checkConfigStatus() configCheckResult {
	if s.cfg == nil {
		return configCheckResult{Status: "misconfigured"}
	}
	// Expose only an aggregate status: field-level "ok"/"missing" hints (and
	// even configured/missing counts) let an unauthenticated caller probe which
	// credentials are configured. Operators diagnose details via config/logs.
	for _, value := range []string{s.cfg.Username, s.cfg.Password, s.cfg.BaseURL} {
		if value == "" {
			return configCheckResult{Status: "misconfigured"}
		}
	}
	return configCheckResult{Status: "ok"}
}

func (s *APIServer) checkUpstreamStatus() statusCheckResult {
	if s.cfg == nil || s.cfg.BaseURL == "" {
		return statusCheckResult{Status: "not_configured"}
	}

	reachable, probed := s.probeUpstreamHTTP()
	if !probed {
		// No cached token to authenticate the HTTP probe; fall back to TCP.
		reachable = s.probeUpstreamTCP()
	}
	status := "unreachable"
	if reachable {
		status = "reachable"
	}
	return statusCheckResult{Status: status}
}

// probeUpstreamHTTP attempts an authenticated probe against the upstream. It
// returns (reachable, probed): probed is false only when there is no cached
// token to authenticate with (the caller may then fall back to a TCP probe).
// When probed is true, reachable reflects whether the upstream returned 2xx —
// an explicit non-2xx (e.g. 401/500) is honored as unreachable and must NOT be
// overridden by a TCP probe.
func (s *APIServer) probeUpstreamHTTP() (reachable, probed bool) {
	s.upstreamCacheMu.RLock()
	cached := s.upstreamCache
	s.upstreamCacheMu.RUnlock()

	if cached == nil || cached.token == "" {
		return false, false
	}

	baseURL := s.ensureScheme(s.cfg.BaseURL)
	profileURL := strings.TrimSuffix(baseURL, "/") + "/user/profile"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, profileURL, nil)
	if err != nil {
		return false, true
	}
	req.Header.Set("Authorization", "Bearer "+cached.token)

	httpClient := &http.Client{Timeout: 5 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return false, true
	}
	//nolint:errcheck
	_ = resp.Body.Close()
	// A non-2xx response means the upstream is misbehaving or rejecting the
	// cached token; treat it as not reachable. This was probed, so the caller
	// must NOT fall back to TCP and flip it back to reachable.
	return resp.StatusCode >= 200 && resp.StatusCode < 300, true
}

func (s *APIServer) probeUpstreamTCP() bool {
	baseURL := s.ensureScheme(s.cfg.BaseURL)

	u, err := url.Parse(baseURL)
	if err != nil {
		return false
	}

	host := u.Host
	if !strings.Contains(host, ":") {
		port := "80"
		if u.Scheme == "https" {
			port = "443"
		}
		host = net.JoinHostPort(host, port)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return false
	}
	//nolint:errcheck
	_ = conn.Close()
	return true
}

func (s *APIServer) ensureScheme(rawURL string) string {
	if !strings.HasPrefix(rawURL, "http") {
		return "https://" + rawURL
	}
	return rawURL
}

func firstQueryValue(values url.Values, keys ...string) string {
	for _, key := range keys {
		if value := values.Get(key); value != "" {
			return value
		}
	}
	return ""
}

func respondWithUpstreamFailure(w http.ResponseWriter, code, message, command string, err error) {
	if err != nil {
		// Scrub upstream errors before logging: they may carry tokens/URLs even
		// after client-level redaction (defense in depth against log leakage).
		log.Printf("%s failed for %s: %s", code, command, secrets.ScrubError(err))
	}
	respondWithError(w, http.StatusBadGateway, code, message, command, &retryHint{Retryable: true, RetryAfter: 30})
}

func (s *APIServer) checkFFmpegStatus() statusCheckResult {
	if _, err := exec.LookPath("ffmpeg"); err == nil {
		return statusCheckResult{Status: "available"}
	}
	return statusCheckResult{Status: "not_found"}
}

func (s *APIServer) coursesHandler(w http.ResponseWriter, r *http.Request) {
	apiClient, cfg, err := s.getOrRefreshUpstreamClient(r.Context())
	if err != nil {
		respondWithUpstreamFailure(w, "LOGIN_FAILED", upstreamLoginFailedMessage, "listCourses", err)
		return
	}

	courses, err := apiClient.GetCourses(r.Context(), cfg)
	if err != nil {
		respondWithUpstreamFailure(w, "COURSES_FETCH_FAILED", upstreamCoursesFailedMessage, "listCourses", err)
		return
	}

	respondWithEnvelope(w, http.StatusOK, "listCourses", courses)
}

func (s *APIServer) lecturesHandler(w http.ResponseWriter, r *http.Request) {
	subjectID := firstQueryValue(r.URL.Query(), lectureSubjectQueryParam, lectureSubjectLegacyParam)
	sessionID := firstQueryValue(r.URL.Query(), lectureSessionQueryParam, lectureSessionLegacyParam)

	if subjectID == "" || sessionID == "" {
		respondWithError(w, http.StatusBadRequest, "MISSING_PARAMETER", fmt.Sprintf("%s and %s query parameters required", lectureSubjectQueryParam, lectureSessionQueryParam), "listLectures", nil)
		return
	}

	subjectInt, err := strconv.Atoi(subjectID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "INVALID_REQUEST", "subjectId must be a valid integer", "listLectures", nil)
		return
	}
	sessionInt, err := strconv.Atoi(sessionID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "INVALID_REQUEST", "sessionId must be a valid integer", "listLectures", nil)
		return
	}

	apiClient, cfg, loginErr := s.getOrRefreshUpstreamClient(r.Context())
	if loginErr != nil {
		respondWithUpstreamFailure(w, "LOGIN_FAILED", upstreamLoginFailedMessage, "listLectures", loginErr)
		return
	}

	lectures, err := apiClient.GetLectures(r.Context(), cfg, client.Course{SubjectID: subjectInt, SessionID: sessionInt})
	if err != nil {
		respondWithUpstreamFailure(w, "LECTURES_FETCH_FAILED", upstreamLecturesFailedMessage, "listLectures", err)
		return
	}

	respondWithEnvelope(w, http.StatusOK, "listLectures", lectures)
}

func (s *APIServer) createJobHandler(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB limit
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
	if req.StartIndex < 1 {
		respondWithError(w, http.StatusBadRequest, "INVALID_REQUEST", "startIndex must be 1 or greater (1-based, matching CLI --start)", "createJob", nil)
		return
	}
	if req.EndIndex < req.StartIndex {
		respondWithError(w, http.StatusBadRequest, "INVALID_REQUEST", "endIndex must be greater than or equal to startIndex", "createJob", nil)
		return
	}

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

	job, created := s.jobStore.CreateJobWithKey(req.SubjectID, req.SessionID, req.StartIndex, req.EndIndex, mergedCfg, req.IdempotencyKey)

	if !created {
		respondWithEnvelope(w, http.StatusConflict, "createJob", createJobConflictResponse{
			Job:       job.copy(),
			Duplicate: true,
		})
		return
	}

	// Respond before starting the background goroutine to avoid a data race:
	// respondWithEnvelope marshals the job to JSON, which reads job fields
	// concurrently with executeJob writing to the same job via UpdateJob.
	respondWithEnvelope(w, http.StatusCreated, "createJob", job.copy())

	go func() {
		s.jobSem <- struct{}{}
		defer func() { <-s.jobSem }()
		s.executeJob(job.ID)
	}()
}

func (s *APIServer) listJobsHandler(w http.ResponseWriter, _ *http.Request) {
	snapshot := s.jobStore.ListJobCopies()
	respondWithEnvelope(w, http.StatusOK, "listJobs", snapshot)
}

func (s *APIServer) getJobHandler(w http.ResponseWriter, r *http.Request) {
	jobID := mux.Vars(r)["id"]
	if jobID == "" {
		respondWithError(w, http.StatusBadRequest, "MISSING_PARAMETER", "Job ID is required", "getJob", nil)
		return
	}

	job, ok := s.jobStore.CopyJob(jobID)
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
		if errors.Is(err, ErrJobNotFound) {
			respondWithError(w, http.StatusNotFound, "JOB_NOT_FOUND", "Job not found", "cancelJob", nil)
			return
		}
		var terminalErr *TerminalStatusError
		if errors.As(err, &terminalErr) {
			respondWithError(w, http.StatusBadRequest, "JOB_CANNOT_CANCEL", "Cannot cancel job in terminal state", "cancelJob", nil, map[string]string{"status": string(terminalErr.Status)})
			return
		}
		respondWithError(w, http.StatusInternalServerError, "CANCEL_FAILED", err.Error(), "cancelJob", &retryHint{Retryable: true, RetryAfter: 10})
		return
	}

	evt := newWSEvent(jobCancelledEventType, jobID)
	evt.Status = StatusCanceled
	evt.Progress = job.Progress
	broadcastEvent(s.wsHub, evt)

	respondWithSuccess(w, "cancelJob", cancelJobResponse{ID: jobID, Status: StatusCanceled})
}

func (s *APIServer) websocketHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer func() {
		_ = conn.Close() //nolint:errcheck
	}()

	if err := conn.SetReadDeadline(time.Now().Add(60 * time.Second)); err != nil {
		return
	}
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second)) //nolint:errcheck // reset deadline on pong
		return nil
	})

	s.wsHub.Register(conn)
	defer s.wsHub.Unregister(conn)

	// Start ping ticker to keep the connection alive and detect dead peers.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
					return
				}
			}
		}
	}()

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}
