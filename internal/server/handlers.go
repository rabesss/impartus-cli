package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gorilla/mux"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/secrets"
)

const (
	internalJobExecutionError           = "internal job execution error"
	jobCancelledEventType               = "job.cancelled"
	websocketReadLimit            int64 = 4096
	lectureSubjectQueryParam            = "subjectId"
	lectureSubjectLegacyParam           = "subject_id"
	lectureSessionQueryParam            = "sessionId"
	lectureSessionLegacyParam           = "session_id"
	upstreamLoginFailedMessage          = "Failed to authenticate with Impartus API"
	upstreamCoursesFailedMessage        = "Failed to fetch courses from Impartus"
	upstreamLecturesFailedMessage       = "Failed to fetch lectures from Impartus"
)

func (s *APIServer) registerRoutes() {
	s.router.Use(requestIDMiddleware)
	s.router.Use(s.corsMiddleware)

	api := s.router.PathPrefix("/api/v1").Subrouter()
	api.HandleFunc("/health", s.healthHandler).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/health/live", s.livenessHandler).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/health/ready", s.healthHandler).Methods(http.MethodGet, http.MethodOptions)
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

	job, created, persistErr := s.jobStore.createJobWithKeyDurable(req.SubjectID, req.SessionID, req.StartIndex, req.EndIndex, mergedCfg, req.IdempotencyKey)
	if persistErr != nil {
		log.Printf("failed to persist created job: %v", persistErr)
		respondWithError(w, http.StatusInternalServerError, "JOB_PERSISTENCE_FAILED", "Job could not be durably created", "createJob", &retryHint{Retryable: true, RetryAfter: 10})
		return
	}

	if !created {
		respondWithEnvelope(w, http.StatusConflict, "createJob", createJobConflictResponse{
			Job:       job,
			Duplicate: true,
		})
		return
	}

	// Refresh the detached creation result from committed store state before
	// returning it; the executor may begin advancing the live job immediately.
	jobCopy, ok := s.jobStore.CopyJob(job.ID)
	if !ok {
		respondWithError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "job disappeared after creation", "createJob", nil)
		return
	}
	respondWithEnvelope(w, http.StatusCreated, "createJob", jobCopy)

	go s.runJob(job.ID, func() {
		s.executeJob(job.ID)
	})
}

func (s *APIServer) runJob(jobID string, execute func()) {
	s.jobSem <- struct{}{}
	defer func() { <-s.jobSem }()
	defer func() {
		if recovered := recover(); recovered != nil {
			// The panic type is sufficient to categorize private diagnostics. The
			// value may contain credentials, URLs, paths, or other sensitive data.
			log.Printf("panic in job executor for job %s (type %T)", jobID, recovered)
			s.failJob(jobID, internalJobExecutionError)
		}
	}()
	execute()
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

	err := s.cancelJob(jobID)
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
		log.Printf("failed to durably cancel job: %v", err)
		respondWithError(w, http.StatusInternalServerError, "CANCEL_FAILED", "Job cancellation could not be persisted", "cancelJob", &retryHint{Retryable: true, RetryAfter: 10})
		return
	}

	respondWithSuccess(w, "cancelJob", cancelJobResponse{ID: jobID, Status: StatusCanceled})
}

func (s *APIServer) cancelJob(jobID string) error {
	s.jobEventMu.Lock()
	defer s.jobEventMu.Unlock()
	job, err := s.jobStore.CancelJob(jobID)
	if err != nil {
		return err
	}
	evt := newWSEvent(jobCancelledEventType, jobID)
	evt.Status = StatusCanceled
	evt.Progress = job.Progress
	broadcastEvent(s.wsHub, evt)
	return nil
}

func (s *APIServer) websocketHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	conn.SetReadLimit(websocketReadLimit)
	if err := conn.SetReadDeadline(time.Now().Add(60 * time.Second)); err != nil {
		_ = conn.Close() //nolint:errcheck
		return
	}
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second)) //nolint:errcheck // reset deadline on pong
		return nil
	})

	client := s.wsHub.Register(conn)
	if client == nil {
		_ = conn.Close() //nolint:errcheck
		return
	}
	defer func() {
		s.wsHub.Unregister(client)
		<-client.writeDone
	}()

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}
