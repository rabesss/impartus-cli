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
	Status string `json:"status"`
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
		// Legacy flat fields (accepted for backward compatibility)
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
	default:
		r.JobConfig = mergeLegacyFields(raw.Quality, raw.Views, raw.AudioOnly, raw.AudioFormat,
			raw.OutputPath, raw.EnablePipeline, raw.NumWorkers,
			raw.DownloadWorkersPerLecture, raw.DecryptWorkersPerLecture, raw.SkipNoAudio)
	}

	return nil
}

// mergeLegacyFields constructs a JobConfigOptions from individual flat fields.
// Returns nil if all fields are nil (no legacy data present).
func mergeLegacyFields(quality, views *string, audioOnly *bool, audioFormat, outputPath *string,
	enablePipeline *bool, numWorkers, downloadWorkers, decryptWorkers *int, skipNoAudio *bool) *JobConfigOptions {
	if quality == nil && views == nil && audioOnly == nil && audioFormat == nil &&
		outputPath == nil && enablePipeline == nil && numWorkers == nil &&
		downloadWorkers == nil && decryptWorkers == nil && skipNoAudio == nil {
		return nil
	}
	var normalizedViews *string
	if views != nil {
		v := config.NormalizeViews(*views)
		normalizedViews = &v
	}
	return &JobConfigOptions{
		Quality:                   quality,
		Views:                     normalizedViews,
		AudioOnly:                 audioOnly,
		AudioFormat:               audioFormat,
		OutputPath:                outputPath,
		EnablePipeline:            enablePipeline,
		NumWorkers:                numWorkers,
		DownloadWorkersPerLecture: downloadWorkers,
		DecryptWorkersPerLecture:  decryptWorkers,
		SkipNoAudio:               skipNoAudio,
	}
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

// copy returns a deep copy of the Job's exported fields (not ctx/cancel/cfg).
func (j *Job) copy() *Job {
	if j == nil {
		return nil
	}
	cp := &Job{
		ID:                j.ID,
		SubjectID:         j.SubjectID,
		SessionID:         j.SessionID,
		StartIndex:        j.StartIndex,
		EndIndex:          j.EndIndex,
		Status:            j.Status,
		Progress:          j.Progress,
		Error:             j.Error,
		TotalLectures:     j.TotalLectures,
		CompletedLectures: j.CompletedLectures,
		FilteredLectures:  j.FilteredLectures,
		Config:            j.Config,
		IdempotencyKey:    j.IdempotencyKey,
		CreatedAt:         j.CreatedAt,
		UpdatedAt:         j.UpdatedAt,
	}
	if j.Outputs != nil {
		cp.Outputs = make([]string, len(j.Outputs))
		copy(cp.Outputs, j.Outputs)
	}
	return cp
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
	stopLoginLimiter func()
	upgrader         websocket.Upgrader
	router           *mux.Router
	port             string
	upstreamCache    *upstreamCacheEntry
	upstreamCacheMu  sync.RWMutex
	upstreamLogin    UpstreamLoginFunc
	loginLimiter     *loginRateLimiter
	// loopback reports whether the server binds a loopback address. When false
	// (e.g. ListenAddr=0.0.0.0), CORS and WebSocket origin checks are tightened.
	loopback bool
}
