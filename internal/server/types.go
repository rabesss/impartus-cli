package server

import (
	"context"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

const (
	statusPending   = "pending"
	statusRunning   = "running"
	statusCompleted = "completed"
	statusFailed    = "failed"
	statusCanceled  = "canceled"
)

const maxIdempotencyKeyLength = 256

type wsEvent struct {
	Type      string  `json:"type"`
	JobID     string  `json:"jobId,omitempty"`
	Status    string  `json:"status,omitempty"`
	Progress  float64 `json:"progress,omitempty"`
	Phase     string  `json:"phase,omitempty"`
	Timestamp int64   `json:"timestamp"`
	Details   any     `json:"details,omitempty"`
	Error     string  `json:"error,omitempty"`
	Outputs   any     `json:"outputs,omitempty"`
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

type upstreamCacheEntry struct {
	client    *client.Client
	cfg       *config.Config
	token     string
	expiresAt time.Time
}

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
