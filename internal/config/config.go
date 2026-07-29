// Package config handles loading, validating, and defaulting application configuration.
package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// ConfigLocation is the default path to the configuration file.
const ConfigLocation = "./config.json"

// ProgressConfig controls progress bar behavior during downloads.
type ProgressConfig struct {
	Enabled         bool   `json:"enabled"`
	ShowSpeed       bool   `json:"showSpeed"`
	ShowETA         bool   `json:"showETA"`
	UpdateInterval  string `json:"updateInterval"`
	SpeedWindowSize int    `json:"speedWindowSize"`
}

// WatchTarget is one Impartus course that maps to a NotebookLM notebook.
type WatchTarget struct {
	SubjectID  int    `json:"subjectId"`
	SessionID  int    `json:"sessionId"`
	NotebookID string `json:"notebookId"`
	Label      string `json:"label,omitempty"`
}

// WatchConfig controls the automated lecture poll / download / upload loop.
// Media fields default to bandwidth-efficient audio (quality 144, left view)
// when watch runs; leave them empty to keep those defaults.
type WatchConfig struct {
	Enabled                bool             `json:"enabled"`
	PollInterval           string           `json:"pollInterval,omitempty"`
	Interval               string           `json:"interval,omitempty"` // alias for pollInterval
	StateFile              string           `json:"stateFile,omitempty"`
	StatePath              string           `json:"statePath,omitempty"` // alias for stateFile
	MaxLecturesPerCycle    int              `json:"maxLecturesPerCycle,omitempty"`
	MaxUploadRetries       int              `json:"maxUploadRetries,omitempty"`
	DeleteAudioAfterUpload bool             `json:"deleteAudioAfterUpload"`
	Targets                []WatchTarget    `json:"targets,omitempty"`
	NotebookLM             NotebookLMConfig `json:"notebooklm,omitempty"`
	// Legacy single-target fields — normalized into Targets during ApplyDefaults.
	SubjectID   int    `json:"subjectId,omitempty"`
	SessionID   int    `json:"sessionId,omitempty"`
	Quality     string `json:"quality,omitempty"`
	Views       string `json:"views,omitempty"`
	AudioFormat string `json:"audioFormat,omitempty"`
	Upload      bool   `json:"upload"`
}

// NotebookLMConfig configures the NotebookLM upload step used by watch.
// Authentication secrets stay out of this file — use NOTEBOOKLM_* env vars
// (see docs/notebooklm-auth.md).
type NotebookLMConfig struct {
	Provider              string `json:"provider,omitempty"` // notebooklm-py | nlm
	Command               string `json:"command,omitempty"`
	CLIPath               string `json:"cliPath,omitempty"` // alias for command
	Profile               string `json:"profile,omitempty"`
	AuthProfile           string `json:"authProfile,omitempty"` // alias for profile
	UploadTimeout         string `json:"uploadTimeout,omitempty"`
	MaxSourcesPerNotebook int    `json:"maxSourcesPerNotebook,omitempty"`
	NotebookID            string `json:"notebookId,omitempty"` // legacy single-notebook default
}

// Config holds all application configuration values.
type Config struct {
	Username         string  `json:"username"`
	Password         string  `json:"password"`
	BaseURL          string  `json:"baseUrl"`
	Quality          string  `json:"quality"`
	Views            string  `json:"views"`
	DownloadLocation string  `json:"downloadLocation"`
	Token            string  `json:"token"`
	TempDirLocation  string  `json:"tempDirLocation"`
	NumWorkers       int     `json:"numWorkers"`
	Slides           bool    `json:"slides"`
	AudioOnly        bool    `json:"audioOnly"`
	AudioFormat      string  `json:"audioFormat"`
	RateLimit        float64 `json:"rateLimit"`
	APIRateLimit     float64 `json:"apiRateLimit"`
	EnableJitter     bool    `json:"enableJitter"`
	SkipNoAudio      bool    `json:"skipNoAudio"`

	EnablePipeline            bool           `json:"enablePipeline"`
	DownloadWorkersPerLecture int            `json:"downloadWorkersPerLecture"`
	DecryptWorkersPerLecture  int            `json:"decryptWorkersPerLecture"`
	ProgressTracking          ProgressConfig `json:"progressTracking"`
	Watch                     WatchConfig    `json:"watch,omitempty"`
	// NotebookLM is a legacy top-level alias merged into Watch.NotebookLM.
	NotebookLM  NotebookLMConfig `json:"notebooklm,omitempty"`
	HTTPTimeout string           `json:"httpTimeout"`
	ListenAddr  string           `json:"listenAddr,omitempty"`
	// AllowRemoteAccess must be explicitly enabled to bind a non-loopback
	// ListenAddr (e.g. 0.0.0.0). Defaults to false for safety.
	AllowRemoteAccess bool `json:"allowRemoteAccess,omitempty"`
}

// ApplyDefaults fills in zero-valued fields with sensible defaults.
func (c *Config) ApplyDefaults() {
	c.applyPathDefaults()
	c.applyWorkerDefaults()
	c.applyRateLimitDefaults()
	c.applyProgressDefaults()
	c.applyListenDefaults()
	if c.Quality == "" {
		c.Quality = "720"
	}
}

func (c *Config) applyPathDefaults() {
	if c.TempDirLocation == "" {
		c.TempDirLocation = "./temp"
	}
	if c.DownloadLocation == "" {
		c.DownloadLocation = "./downloads"
	}
}

func (c *Config) applyWorkerDefaults() {
	if c.NumWorkers == 0 {
		c.NumWorkers = 5
	}
	if c.DownloadWorkersPerLecture == 0 {
		c.DownloadWorkersPerLecture = 12
	}
	if c.DecryptWorkersPerLecture == 0 {
		c.DecryptWorkersPerLecture = 4
	}
}

func (c *Config) applyRateLimitDefaults() {
	if c.RateLimit == 0 {
		c.RateLimit = 100
	}
	if c.APIRateLimit == 0 {
		c.APIRateLimit = 2
	}
	// EnableJitter is intentionally NOT forced here: it defaults ON at load time
	// (Parse/LoadResolved) so an explicit false in config/env is honored instead
	// of silently overridden on every ApplyDefaults() call.
}

func (c *Config) applyProgressDefaults() {
	if c.AudioFormat == "" {
		c.AudioFormat = "mp3"
	}
	if c.ProgressTracking.UpdateInterval == "" {
		c.ProgressTracking.UpdateInterval = "2s"
	}
	if c.ProgressTracking.SpeedWindowSize == 0 {
		c.ProgressTracking.SpeedWindowSize = 10
	}
	if c.HTTPTimeout == "" {
		c.HTTPTimeout = "10m"
	}
	if c.Views == "" {
		c.Views = "both"
	} else {
		c.Views = NormalizeViews(c.Views)
	}
	c.applyWatchDefaults()
}

func (c *Config) applyWatchDefaults() {
	c.normalizeWatchAliases()
	if c.Watch.PollInterval == "" {
		c.Watch.PollInterval = "5m"
	}
	c.Watch.Interval = c.Watch.PollInterval
	if c.Watch.StateFile == "" {
		c.Watch.StateFile = "./.watch-state.json"
	}
	c.Watch.StatePath = c.Watch.StateFile
	if c.Watch.MaxLecturesPerCycle <= 0 {
		c.Watch.MaxLecturesPerCycle = 3
	}
	if c.Watch.MaxUploadRetries <= 0 {
		c.Watch.MaxUploadRetries = 3
	}
	if c.Watch.Quality == "" {
		c.Watch.Quality = "144"
	}
	if c.Watch.Views == "" {
		c.Watch.Views = "left"
	} else {
		c.Watch.Views = NormalizeViews(c.Watch.Views)
	}
	if c.Watch.AudioFormat == "" {
		c.Watch.AudioFormat = "mp3"
	}
	nlm := &c.Watch.NotebookLM
	if nlm.Provider == "" {
		nlm.Provider = "notebooklm-py"
	}
	if nlm.Command == "" {
		nlm.Command = "notebooklm"
		if nlm.Provider == "nlm" {
			nlm.Command = "nlm"
		}
	}
	nlm.CLIPath = nlm.Command
	if nlm.Profile == "" && nlm.AuthProfile != "" {
		nlm.Profile = nlm.AuthProfile
	}
	nlm.AuthProfile = nlm.Profile
	if nlm.UploadTimeout == "" {
		nlm.UploadTimeout = "30m"
	}
	if nlm.MaxSourcesPerNotebook <= 0 {
		nlm.MaxSourcesPerNotebook = 300
	}
	c.NotebookLM = *nlm
	c.synthesizeLegacyTarget()
}

// normalizeWatchAliases copies legacy/alternate field names onto the plan names.
func (c *Config) normalizeWatchAliases() {
	if c.Watch.PollInterval == "" && c.Watch.Interval != "" {
		c.Watch.PollInterval = c.Watch.Interval
	}
	if c.Watch.StateFile == "" && c.Watch.StatePath != "" {
		c.Watch.StateFile = c.Watch.StatePath
	}
	// Top-level notebooklm merges into watch.notebooklm when nested fields are empty.
	if c.Watch.NotebookLM.NotebookID == "" {
		c.Watch.NotebookLM.NotebookID = c.NotebookLM.NotebookID
	}
	if c.Watch.NotebookLM.Command == "" {
		c.Watch.NotebookLM.Command = firstNonEmpty(c.NotebookLM.Command, c.NotebookLM.CLIPath)
	}
	if c.Watch.NotebookLM.CLIPath == "" {
		c.Watch.NotebookLM.CLIPath = c.NotebookLM.CLIPath
	}
	if c.Watch.NotebookLM.Profile == "" {
		c.Watch.NotebookLM.Profile = firstNonEmpty(c.NotebookLM.Profile, c.NotebookLM.AuthProfile)
	}
	if c.Watch.NotebookLM.AuthProfile == "" {
		c.Watch.NotebookLM.AuthProfile = c.NotebookLM.AuthProfile
	}
	if c.Watch.NotebookLM.Provider == "" {
		c.Watch.NotebookLM.Provider = c.NotebookLM.Provider
	}
	if c.Watch.NotebookLM.UploadTimeout == "" {
		c.Watch.NotebookLM.UploadTimeout = c.NotebookLM.UploadTimeout
	}
	if c.Watch.NotebookLM.MaxSourcesPerNotebook == 0 {
		c.Watch.NotebookLM.MaxSourcesPerNotebook = c.NotebookLM.MaxSourcesPerNotebook
	}
}

func (c *Config) synthesizeLegacyTarget() {
	if len(c.Watch.Targets) > 0 {
		return
	}
	if c.Watch.SubjectID <= 0 || c.Watch.SessionID <= 0 {
		return
	}
	c.Watch.Targets = []WatchTarget{{
		SubjectID:  c.Watch.SubjectID,
		SessionID:  c.Watch.SessionID,
		NotebookID: c.Watch.NotebookLM.NotebookID,
	}}
}

// ResolvedTargets returns watch targets after legacy single-target synthesis.
func (c *Config) ResolvedTargets() []WatchTarget {
	if len(c.Watch.Targets) > 0 {
		return c.Watch.Targets
	}
	if c.Watch.SubjectID > 0 && c.Watch.SessionID > 0 {
		return []WatchTarget{{
			SubjectID:  c.Watch.SubjectID,
			SessionID:  c.Watch.SessionID,
			NotebookID: firstNonEmpty(c.Watch.NotebookLM.NotebookID, c.NotebookLM.NotebookID),
		}}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ApplyWatchMediaDefaults forces the bandwidth-efficient audio settings used by
// the watch loop onto the top-level media fields the downloader reads.
func (c *Config) ApplyWatchMediaDefaults() {
	c.AudioOnly = true
	c.SkipNoAudio = true
	c.Quality = c.Watch.Quality
	if c.Quality == "" {
		c.Quality = "144"
	}
	c.Views = NormalizeViews(c.Watch.Views)
	if c.Views == "" {
		c.Views = "left"
	}
	c.AudioFormat = c.Watch.AudioFormat
	if c.AudioFormat == "" {
		c.AudioFormat = "mp3"
	}
}

func (c *Config) applyListenDefaults() {
	if c.ListenAddr == "" {
		c.ListenAddr = "127.0.0.1"
	}
}

// NormalizeViews maps view aliases to canonical downloader names.
// "first" → "left", "second" → "right", others pass through lowercased.
func NormalizeViews(views string) string {
	switch strings.ToLower(strings.TrimSpace(views)) {
	case "first":
		return "left"
	case "second":
		return "right"
	default:
		return strings.ToLower(strings.TrimSpace(views))
	}
}

// IncludesLeft reports whether the configured view set includes the left
// (first) camera view. Assumes Views is normalized ("both" | "left" | "right").
func (c *Config) IncludesLeft() bool { return c.Views != "right" }

// IncludesRight reports whether the configured view set includes the right
// (second) camera view. Assumes Views is normalized ("both" | "left" | "right").
func (c *Config) IncludesRight() bool { return c.Views != "left" }

// HasBothViews reports whether both camera views are configured.
func (c *Config) HasBothViews() bool { return c.Views == "both" }

// Validate checks the configuration for errors and returns the first one found.
func (c *Config) Validate() error {
	if err := c.validateCore(); err != nil {
		return err
	}
	if err := c.validateProgressTracking(); err != nil {
		return err
	}
	if err := c.validatePipeline(); err != nil {
		return err
	}
	if err := c.validateWatch(); err != nil {
		return err
	}
	if err := c.validateNotebookLM(); err != nil {
		return err
	}
	return c.validateTimeout()
}

func (c *Config) validateCore() error {
	if c.Username == "" || c.Password == "" {
		return fmt.Errorf("username and password are required")
	}
	if err := c.validateBaseURL(); err != nil {
		return err
	}
	if c.NumWorkers < 1 || c.NumWorkers > 50 {
		return fmt.Errorf("numWorkers must be between 1 and 50, got %d", c.NumWorkers)
	}
	if err := c.validateMediaSettings(); err != nil {
		return err
	}
	return c.validateRateLimits()
}

func (c *Config) validateBaseURL() error {
	if c.BaseURL == "" {
		return fmt.Errorf("baseUrl is required")
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("baseUrl must be a valid HTTP(S) URL")
	}
	return nil
}

func (c *Config) validateMediaSettings() error {
	if !OneOf(c.Quality, "144", "450", "720") {
		return fmt.Errorf("quality must be one of: 144, 450, 720")
	}
	if !OneOf(c.Views, "first", "second", "both", "left", "right") {
		return fmt.Errorf("views must be one of: first, second, both, left, right")
	}
	if c.AudioOnly && !OneOf(c.AudioFormat, "mp3", "m4a", "aac", "opus") {
		return fmt.Errorf("audioFormat must be one of: mp3, m4a, aac, opus")
	}
	return nil
}

func (c *Config) validateRateLimits() error {
	if c.RateLimit < 0.1 || c.RateLimit > 100 {
		return fmt.Errorf("rateLimit must be between 0.1 and 100 requests per second, got %.2f", c.RateLimit)
	}
	if c.APIRateLimit < 0.1 || c.APIRateLimit > 20 {
		return fmt.Errorf("apiRateLimit must be between 0.1 and 20 requests per second, got %.2f", c.APIRateLimit)
	}
	return nil
}

func (c *Config) validateProgressTracking() error {
	if !c.ProgressTracking.Enabled {
		return nil
	}
	if c.ProgressTracking.UpdateInterval != "" {
		duration, err := time.ParseDuration(c.ProgressTracking.UpdateInterval)
		if err != nil {
			return fmt.Errorf("invalid progressTracking.updateInterval: %w", err)
		}
		if duration < 500*time.Millisecond || duration > 10*time.Second {
			return fmt.Errorf("progressTracking.updateInterval must be between 500ms and 10s, got %v", duration)
		}
	}
	if c.ProgressTracking.SpeedWindowSize < 3 || c.ProgressTracking.SpeedWindowSize > 30 {
		return fmt.Errorf("progressTracking.speedWindowSize must be between 3 and 30, got %d", c.ProgressTracking.SpeedWindowSize)
	}
	return nil
}

func (c *Config) validatePipeline() error {
	// Always validate worker count ranges regardless of pipeline enablement
	// since these values can be set via API and should always be valid
	if c.DownloadWorkersPerLecture < 1 || c.DownloadWorkersPerLecture > 12 {
		return fmt.Errorf("downloadWorkersPerLecture must be between 1 and 12, got %d", c.DownloadWorkersPerLecture)
	}
	if c.DecryptWorkersPerLecture < 1 || c.DecryptWorkersPerLecture > 10 {
		return fmt.Errorf("decryptWorkersPerLecture must be between 1 and 10, got %d", c.DecryptWorkersPerLecture)
	}

	return nil
}

func (c *Config) validateWatch() error {
	targets := c.ResolvedTargets()
	if !c.Watch.Enabled && len(targets) == 0 {
		return c.validateWatchShape()
	}
	if err := c.validateWatchShape(); err != nil {
		return err
	}
	if !c.Watch.Enabled {
		return nil
	}
	if len(targets) == 0 {
		return fmt.Errorf("watch.targets is required when watch.enabled is true (or set watch.subjectId/sessionId)")
	}
	seen := map[string]struct{}{}
	for i, target := range targets {
		if target.SubjectID <= 0 || target.SessionID <= 0 {
			return fmt.Errorf("watch.targets[%d]: subjectId and sessionId are required", i)
		}
		key := fmt.Sprintf("%d:%d", target.SubjectID, target.SessionID)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("watch.targets: duplicate subjectId/sessionId %s", key)
		}
		seen[key] = struct{}{}
		if c.Watch.Upload && strings.TrimSpace(target.NotebookID) == "" && strings.TrimSpace(c.Watch.NotebookLM.NotebookID) == "" {
			return fmt.Errorf("watch.targets[%d]: notebookId is required when watch.upload is true", i)
		}
	}
	return nil
}

func (c *Config) validateWatchShape() error {
	intervalStr := firstNonEmpty(c.Watch.PollInterval, c.Watch.Interval)
	if intervalStr != "" {
		interval, err := time.ParseDuration(intervalStr)
		if err != nil {
			return fmt.Errorf("invalid watch.pollInterval: %w", err)
		}
		if interval < 5*time.Minute || interval > 24*time.Hour {
			return fmt.Errorf("watch.pollInterval must be between 5m and 24h, got %v", interval)
		}
	}
	if c.Watch.MaxLecturesPerCycle < 0 {
		return fmt.Errorf("watch.maxLecturesPerCycle must be >= 0")
	}
	if c.Watch.MaxUploadRetries < 0 {
		return fmt.Errorf("watch.maxUploadRetries must be >= 0")
	}
	if c.Watch.Quality != "" && !OneOf(c.Watch.Quality, "144", "450", "720") {
		return fmt.Errorf("watch.quality must be one of: 144, 450, 720")
	}
	if c.Watch.Views != "" && !OneOf(c.Watch.Views, "first", "second", "both", "left", "right") {
		return fmt.Errorf("watch.views must be one of: first, second, both, left, right")
	}
	if c.Watch.AudioFormat != "" && !OneOf(c.Watch.AudioFormat, "mp3", "m4a", "aac", "opus") {
		return fmt.Errorf("watch.audioFormat must be one of: mp3, m4a, aac, opus")
	}
	return nil
}

func (c *Config) validateNotebookLM() error {
	nlm := c.Watch.NotebookLM
	if nlm.Provider == "" && c.NotebookLM.Provider == "" {
		nlm = c.NotebookLM
	}
	provider := firstNonEmpty(nlm.Provider, c.Watch.NotebookLM.Provider, "notebooklm-py")
	if provider != "" && !OneOf(provider, "notebooklm-py", "nlm") {
		return fmt.Errorf("watch.notebooklm.provider must be one of: notebooklm-py, nlm")
	}
	if timeout := firstNonEmpty(nlm.UploadTimeout, c.Watch.NotebookLM.UploadTimeout); timeout != "" {
		d, err := time.ParseDuration(timeout)
		if err != nil {
			return fmt.Errorf("invalid watch.notebooklm.uploadTimeout: %w", err)
		}
		if d < time.Minute || d > 2*time.Hour {
			return fmt.Errorf("watch.notebooklm.uploadTimeout must be between 1m and 2h, got %v", d)
		}
	}
	if !c.Watch.Upload {
		return nil
	}
	targets := c.ResolvedTargets()
	for i, target := range targets {
		nb := firstNonEmpty(target.NotebookID, c.Watch.NotebookLM.NotebookID, c.NotebookLM.NotebookID)
		if nb == "" {
			return fmt.Errorf("watch.targets[%d].notebookId (or watch.notebooklm.notebookId) is required when watch.upload is true", i)
		}
	}
	if len(targets) == 0 {
		if strings.TrimSpace(firstNonEmpty(c.Watch.NotebookLM.NotebookID, c.NotebookLM.NotebookID)) == "" {
			return fmt.Errorf("watch.notebooklm.notebookId is required when watch.upload is true")
		}
	}
	return nil
}

func (c *Config) validateTimeout() error {
	if c.HTTPTimeout == "" {
		return nil
	}
	timeout, err := time.ParseDuration(c.HTTPTimeout)
	if err != nil {
		return fmt.Errorf("invalid httpTimeout: %w", err)
	}
	if timeout < 30*time.Second || timeout > 60*time.Minute {
		return fmt.Errorf("httpTimeout must be between 30s and 60m, got %v", timeout)
	}
	return nil
}

// Parse reads and unmarshals the configuration file at the given path.
func Parse(path string) (*Config, error) {
	if path == "" {
		path = ConfigLocation
	}

	// G304: config path is user-provided by design
	// #nosec G304
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not open config file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(contents, &cfg); err != nil {
		return nil, fmt.Errorf("could not parse config json: %w", err)
	}
	// Default API jitter ON unless the config file explicitly disables it; an
	// explicit "enableJitter": false must be honored rather than overridden.
	if !jsonKeyPresent(contents, "enableJitter") {
		cfg.EnableJitter = true
	}

	return &cfg, nil
}

// LoadResolved loads config from the given path (or default), applies env overrides, defaults, and validation.
func LoadResolved(path string) (*Config, error) {
	var cfg *Config
	var fileLoaded bool
	var err error

	if path != "" {
		cfg, err = Parse(path)
		if err != nil {
			return nil, err
		}
		fileLoaded = true
	} else {
		cfg = &Config{}
		if _, statErr := os.Stat(ConfigLocation); statErr == nil {
			cfg, err = Parse(ConfigLocation)
			if err != nil {
				return nil, err
			}
			fileLoaded = true
		} else if !os.IsNotExist(statErr) {
			return nil, fmt.Errorf("could not open config file: %w", statErr)
		}
	}

	if err := applyEnvOverrides(cfg); err != nil {
		return nil, err
	}
	// For configs not loaded from a file, default API jitter ON unless the env
	// explicitly disabled it. File-loaded configs already resolved this in Parse.
	if !fileLoaded {
		if _, envSet := os.LookupEnv("IMPARTUS_ENABLE_JITTER"); !envSet {
			cfg.EnableJitter = true
		}
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func applyEnvOverrides(cfg *Config) error {
	applyStringEnv("IMPARTUS_USERNAME", &cfg.Username)
	applyStringEnv("IMPARTUS_PASSWORD", &cfg.Password)
	applyStringEnv("IMPARTUS_BASE_URL", &cfg.BaseURL)
	applyStringEnv("IMPARTUS_QUALITY", &cfg.Quality)
	applyStringEnv("IMPARTUS_VIEWS", &cfg.Views)
	applyStringEnv("IMPARTUS_DOWNLOAD_LOCATION", &cfg.DownloadLocation)
	applyStringEnv("IMPARTUS_TEMP_DIR", &cfg.TempDirLocation)
	applyStringEnv("IMPARTUS_TEMP_DIR_LOCATION", &cfg.TempDirLocation)
	applyStringEnv("IMPARTUS_AUDIO_FORMAT", &cfg.AudioFormat)
	applyStringEnv("IMPARTUS_HTTP_TIMEOUT", &cfg.HTTPTimeout)
	applyStringEnv("IMPARTUS_LISTEN_ADDR", &cfg.ListenAddr)
	applyStringEnv("IMPARTUS_WATCH_INTERVAL", &cfg.Watch.Interval)
	applyStringEnv("IMPARTUS_WATCH_POLL_INTERVAL", &cfg.Watch.PollInterval)
	applyStringEnv("IMPARTUS_WATCH_STATE_PATH", &cfg.Watch.StatePath)
	applyStringEnv("IMPARTUS_WATCH_STATE_FILE", &cfg.Watch.StateFile)
	applyStringEnv("IMPARTUS_WATCH_QUALITY", &cfg.Watch.Quality)
	applyStringEnv("IMPARTUS_WATCH_VIEWS", &cfg.Watch.Views)
	applyStringEnv("IMPARTUS_WATCH_AUDIO_FORMAT", &cfg.Watch.AudioFormat)
	applyStringEnv("IMPARTUS_NOTEBOOKLM_NOTEBOOK_ID", &cfg.Watch.NotebookLM.NotebookID)
	applyStringEnv("IMPARTUS_NOTEBOOKLM_CLI_PATH", &cfg.Watch.NotebookLM.CLIPath)
	applyStringEnv("IMPARTUS_NOTEBOOKLM_COMMAND", &cfg.Watch.NotebookLM.Command)
	applyStringEnv("IMPARTUS_NOTEBOOKLM_AUTH_PROFILE", &cfg.Watch.NotebookLM.AuthProfile)
	applyStringEnv("IMPARTUS_NOTEBOOKLM_PROFILE", &cfg.Watch.NotebookLM.Profile)
	applyStringEnv("IMPARTUS_NOTEBOOKLM_PROVIDER", &cfg.Watch.NotebookLM.Provider)
	applyStringEnv("IMPARTUS_NOTEBOOKLM_UPLOAD_TIMEOUT", &cfg.Watch.NotebookLM.UploadTimeout)
	for _, apply := range []func() error{
		func() error { return applyBoolEnv("IMPARTUS_AUDIO_ONLY", &cfg.AudioOnly) },
		func() error { return applyBoolEnv("IMPARTUS_SLIDES", &cfg.Slides) },
		func() error { return applyBoolEnv("IMPARTUS_SKIP_NO_AUDIO", &cfg.SkipNoAudio) },
		func() error { return applyBoolEnv("IMPARTUS_ALLOW_REMOTE_ACCESS", &cfg.AllowRemoteAccess) },
		func() error { return applyBoolEnv("IMPARTUS_ENABLE_JITTER", &cfg.EnableJitter) },
		func() error { return applyBoolEnv("IMPARTUS_PROGRESS_TRACKING_ENABLED", &cfg.ProgressTracking.Enabled) },
		func() error { return applyBoolEnv("IMPARTUS_WATCH_ENABLED", &cfg.Watch.Enabled) },
		func() error { return applyBoolEnv("IMPARTUS_WATCH_UPLOAD", &cfg.Watch.Upload) },
		func() error {
			return applyBoolEnv("IMPARTUS_WATCH_DELETE_AUDIO_AFTER_UPLOAD", &cfg.Watch.DeleteAudioAfterUpload)
		},
		func() error { return applyIntEnv("IMPARTUS_NUM_WORKERS", &cfg.NumWorkers) },
		func() error { return applyIntEnv("IMPARTUS_WATCH_SUBJECT_ID", &cfg.Watch.SubjectID) },
		func() error { return applyIntEnv("IMPARTUS_WATCH_SESSION_ID", &cfg.Watch.SessionID) },
		func() error {
			return applyIntEnv("IMPARTUS_WATCH_MAX_LECTURES_PER_CYCLE", &cfg.Watch.MaxLecturesPerCycle)
		},
		func() error { return applyIntEnv("IMPARTUS_WATCH_MAX_UPLOAD_RETRIES", &cfg.Watch.MaxUploadRetries) },
		func() error {
			return applyIntEnv("IMPARTUS_NOTEBOOKLM_MAX_SOURCES", &cfg.Watch.NotebookLM.MaxSourcesPerNotebook)
		},
		func() error { return applyFloatEnv("IMPARTUS_RATE_LIMIT", &cfg.RateLimit) },
		func() error { return applyFloatEnv("IMPARTUS_API_RATE_LIMIT", &cfg.APIRateLimit) },
	} {
		if err := apply(); err != nil {
			return err
		}
	}

	applyCanonicalFields(cfg)
	return nil
}

func applyCanonicalFields(cfg *Config) {
	if cfg.Views != "" {
		cfg.Views = strings.ToLower(strings.TrimSpace(cfg.Views))
	}
}

// jsonKeyPresent reports whether the given top-level key is present in the raw
// JSON config bytes. It is used to distinguish an explicit value from an
// omitted one for fields (like enableJitter) whose zero value is meaningful.
func jsonKeyPresent(data []byte, key string) bool {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return false
	}
	target := strings.ToLower(key)
	for k := range m {
		if strings.ToLower(k) == target {
			return true
		}
	}
	return false
}

func applyStringEnv(key string, target *string) {
	if value, ok := os.LookupEnv(key); ok {
		*target = value
	}
}

func applyBoolEnv(key string, target *bool) error {
	value, ok := os.LookupEnv(key)
	if !ok {
		return nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fmt.Errorf("invalid %s: %w", key, err)
	}
	*target = parsed
	return nil
}

func applyIntEnv(key string, target *int) error {
	value, ok := os.LookupEnv(key)
	if !ok {
		return nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("invalid %s: %w", key, err)
	}
	*target = parsed
	return nil
}

func applyFloatEnv(key string, target *float64) error {
	value, ok := os.LookupEnv(key)
	if !ok {
		return nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fmt.Errorf("invalid %s: %w", key, err)
	}
	*target = parsed
	return nil
}

// OneOf checks if a value is in the allowed set.
func OneOf(val string, allowed ...string) bool {
	for _, a := range allowed {
		if val == a {
			return true
		}
	}
	return false
}
