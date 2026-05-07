package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

// Typed sentinel errors for job operations.
var (
	ErrJobNotFound   = errors.New("job not found")
	ErrJobTerminated = errors.New("job in terminal state")
)

// TerminalStatusError wraps ErrJobTerminated with the specific terminal status.
type TerminalStatusError struct {
	Status JobStatus
}

func (e *TerminalStatusError) Error() string {
	return fmt.Sprintf("job in terminal state: %s", e.Status)
}

func (e *TerminalStatusError) Unwrap() error {
	return ErrJobTerminated
}

// JobStatus represents the current state of a download job.
type JobStatus string

const (
	// StatusPending indicates a job has been created but not yet started.
	StatusPending JobStatus = "pending"
	// StatusRunning indicates a job is actively downloading.
	StatusRunning JobStatus = "running"
	// StatusCompleted indicates a job finished successfully.
	StatusCompleted JobStatus = "completed"
	// StatusFailed indicates a job terminated with an error.
	StatusFailed JobStatus = "failed"
	// StatusCanceled indicates a job was manually canceled.
	StatusCanceled JobStatus = "canceled"
)

const maxIdempotencyKeyLength = 256

type wsEvent struct {
	Type      string    `json:"type"`
	JobID     string    `json:"jobId,omitempty"`
	Status    JobStatus `json:"status,omitempty"`
	Progress  float64   `json:"progress,omitempty"`
	Phase     string    `json:"phase,omitempty"`
	Timestamp int64     `json:"timestamp"`
	Details   any       `json:"details,omitempty"`
	Error     string    `json:"error,omitempty"`
	Outputs   []string  `json:"outputs,omitempty"`
}

func newWSEvent(eventType, jobID string) wsEvent {
	return wsEvent{
		Type:      eventType,
		JobID:     jobID,
		Timestamp: time.Now().Unix(),
	}
}

type healthResponse struct {
	Status   string            `json:"status"`
	Config   configCheckResult `json:"config"`
	Upstream statusCheckResult `json:"upstream"`
	FFmpeg   statusCheckResult `json:"ffmpeg"`
}

type configCheckResult struct {
	Status   string `json:"status"`
	Username string `json:"username"`
	Password string `json:"password"`
	BaseURL  string `json:"baseUrl"`
}

type statusCheckResult struct {
	Status string `json:"status"`
}

// JobConfigOptions contains optional per-job configuration overrides for download settings.
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

type legacyJobConfigOptions struct {
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

func (o legacyJobConfigOptions) hasValues() bool {
	return o.Quality != nil ||
		o.Views != nil ||
		o.AudioOnly != nil ||
		o.AudioFormat != nil ||
		o.OutputPath != nil ||
		o.EnablePipeline != nil ||
		o.NumWorkers != nil ||
		o.DownloadWorkersPerLecture != nil ||
		o.DecryptWorkersPerLecture != nil ||
		o.SkipNoAudio != nil
}

func (o legacyJobConfigOptions) toJobConfigOptions() *JobConfigOptions {
	if !o.hasValues() {
		return nil
	}
	var views *string
	if o.Views != nil {
		normalized := config.NormalizeViews(*o.Views)
		views = &normalized
	}
	return &JobConfigOptions{
		Quality:                   o.Quality,
		Views:                     views,
		AudioOnly:                 o.AudioOnly,
		AudioFormat:               o.AudioFormat,
		OutputPath:                o.OutputPath,
		EnablePipeline:            o.EnablePipeline,
		NumWorkers:                o.NumWorkers,
		DownloadWorkersPerLecture: o.DownloadWorkersPerLecture,
		DecryptWorkersPerLecture:  o.DecryptWorkersPerLecture,
		SkipNoAudio:               o.SkipNoAudio,
	}
}

// createJobRequest keeps the canonical request shape in the runtime model.
// Legacy flat config keys are accepted only during JSON decoding and normalized
// into JobConfig at the boundary.
type createJobRequest struct {
	SubjectID      int    `json:"subjectId"`
	SessionID      int    `json:"sessionId"`
	StartIndex     int    `json:"startIndex"`
	EndIndex       int    `json:"endIndex"`
	IdempotencyKey string `json:"idempotencyKey,omitempty"`

	JobConfig *JobConfigOptions `json:"jobConfig,omitempty"`
}

func (r createJobRequest) effectiveJobConfig() *JobConfigOptions {
	if r.JobConfig == nil {
		return nil
	}
	cp := *r.JobConfig
	return &cp
}

func (r *createJobRequest) UnmarshalJSON(data []byte) error {
	type rawCreateJobRequest struct {
		SubjectID      int               `json:"subjectId"`
		SessionID      int               `json:"sessionId"`
		StartIndex     int               `json:"startIndex"`
		EndIndex       int               `json:"endIndex"`
		IdempotencyKey string            `json:"idempotencyKey,omitempty"`
		JobConfig      *JobConfigOptions `json:"jobConfig,omitempty"`
		legacyJobConfigOptions
	}

	var raw rawCreateJobRequest
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	*r = createJobRequest{
		SubjectID:      raw.SubjectID,
		SessionID:      raw.SessionID,
		StartIndex:     raw.StartIndex,
		EndIndex:       raw.EndIndex,
		IdempotencyKey: raw.IdempotencyKey,
	}

	switch {
	case raw.JobConfig != nil:
		cp := *raw.JobConfig
		if cp.Views != nil {
			normalized := config.NormalizeViews(*cp.Views)
			cp.Views = &normalized
		}
		r.JobConfig = &cp
	case raw.hasValues():
		r.JobConfig = raw.toJobConfigOptions()
	}

	return nil
}

// JobRuntimeConfig holds the resolved configuration used during job execution.
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

// Job represents a download job with its metadata, status, and runtime configuration.
type Job struct {
	ID                string           `json:"id"`
	SubjectID         int              `json:"subjectId"`
	SessionID         int              `json:"sessionId"`
	StartIndex        int              `json:"startIndex"`
	EndIndex          int              `json:"endIndex"`
	Status            JobStatus        `json:"status"`
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

type upstreamCacheEntry struct {
	client    *client.Client
	cfg       *config.Config
	token     string
	expiresAt time.Time
}

// UpstreamLoginFunc is a function that authenticates with the upstream Impartus API
// and returns an initialized client and config with a valid token.
type UpstreamLoginFunc func(ctx context.Context, cfg *config.Config) (*client.Client, *config.Config, error)

type loginResponse struct {
	Token   string    `json:"token"`
	Expires time.Time `json:"expires"`
}

type cancelJobResponse struct {
	ID     string    `json:"id"`
	Status JobStatus `json:"status"`
}

type createJobConflictResponse struct {
	Job       *Job `json:"job"`
	Duplicate bool `json:"duplicate"`
}

// APIServer is the main REST API and WebSocket server for managing download jobs.
type APIServer struct {
	cfg              *config.Config
	jobStore         *JobStore
	wsHub            *WSHub
	tokenStore       *TokenStore
	stopTokenCleanup func()
	upgrader         websocket.Upgrader
	router           *mux.Router
	port             string
	upstreamCache    *upstreamCacheEntry
	upstreamCacheMu  sync.RWMutex
	upstreamLogin    UpstreamLoginFunc
}
