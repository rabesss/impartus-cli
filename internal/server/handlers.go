package server

import (
	"context"
	"encoding/json"
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

	"github.com/rabesss/impartus-cli/internal/client"
)

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

func (s *APIServer) healthHandler(w http.ResponseWriter, _ *http.Request) {
	configStatus := s.checkConfigStatus()
	upstreamStatus := s.checkUpstreamStatus()
	ffmpegStatus := s.checkFFmpegStatus()

	overallStatus := "ok"
	if configStatus.Status != "ok" || upstreamStatus.Status != "reachable" || ffmpegStatus.Status != "available" {
		overallStatus = "degraded"
	}

	respondWithEnvelope(w, http.StatusOK, "health", healthResponse{
		Status:   overallStatus,
		Config:   configStatus,
		Upstream: upstreamStatus,
		FFmpeg:   ffmpegStatus,
	})
}

func (s *APIServer) checkConfigStatus() configCheckResult {
	if s.cfg == nil {
		return configCheckResult{
			Status:   "misconfigured",
			Username: "missing",
			Password: "missing",
			BaseURL:  "missing",
		}
	}

	result := configCheckResult{Status: "ok"}
	checks := []struct {
		value string
		field *string
	}{
		{s.cfg.Username, &result.Username},
		{s.cfg.Password, &result.Password},
		{s.cfg.BaseURL, &result.BaseURL},
	}
	for _, check := range checks {
		if check.value == "" {
			*check.field = "missing"
			result.Status = "misconfigured"
		} else {
			*check.field = "ok"
		}
	}

	return result
}

func (s *APIServer) checkUpstreamStatus() statusCheckResult {
	if s.cfg == nil || s.cfg.BaseURL == "" {
		return statusCheckResult{Status: "not_configured"}
	}

	status := "unreachable"
	if s.probeUpstreamHTTP() || s.probeUpstreamTCP() {
		status = "reachable"
	}

	return statusCheckResult{Status: status}
}

func (s *APIServer) probeUpstreamHTTP() bool {
	s.upstreamCacheMu.RLock()
	cached := s.upstreamCache
	s.upstreamCacheMu.RUnlock()

	if cached == nil || cached.token == "" {
		return false
	}

	baseURL := s.ensureScheme(s.cfg.BaseURL)
	profileURL := strings.TrimSuffix(baseURL, "/") + "/user/profile"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, profileURL, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+cached.token)

	httpClient := &http.Client{Timeout: 5 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
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
	conn.Close()
	return true
}

func (s *APIServer) ensureScheme(rawURL string) string {
	if !strings.HasPrefix(rawURL, "http") {
		return "https://" + rawURL
	}
	return rawURL
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

	evt := newWSEvent("job.cancelled", jobID)
	evt.Status = statusCanceled
	evt.Progress = job.Progress
	broadcastEvent(s.wsHub, evt)

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
