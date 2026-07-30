package config

import (
	"fmt"
	"strings"
	"time"
)

// WatchTarget maps one Impartus course to a NotebookLM notebook.
type WatchTarget struct {
	SubjectID  int    `json:"subjectId"`
	SessionID  int    `json:"sessionId"`
	NotebookID string `json:"notebookId"`
	Label      string `json:"label,omitempty"`
}

// WatchConfig controls the automated lecture poll, download, and upload loop.
type WatchConfig struct {
	Enabled                bool             `json:"enabled"`
	PollInterval           string           `json:"pollInterval,omitempty"`
	StateFile              string           `json:"stateFile,omitempty"`
	MaxLecturesPerCycle    int              `json:"maxLecturesPerCycle,omitempty"`
	MaxUploadRetries       int              `json:"maxUploadRetries,omitempty"`
	DeleteAudioAfterUpload bool             `json:"deleteAudioAfterUpload"`
	Targets                []WatchTarget    `json:"targets,omitempty"`
	NotebookLM             NotebookLMConfig `json:"notebooklm,omitempty"`
	Quality                string           `json:"quality,omitempty"`
	Views                  string           `json:"views,omitempty"`
	AudioFormat            string           `json:"audioFormat,omitempty"`
	Upload                 bool             `json:"upload"`
}

// NotebookLMConfig configures the provider CLI used by watch.
type NotebookLMConfig struct {
	Provider              string `json:"provider,omitempty"`
	Command               string `json:"command,omitempty"`
	Profile               string `json:"profile,omitempty"`
	UploadTimeout         string `json:"uploadTimeout,omitempty"`
	MaxSourcesPerNotebook int    `json:"maxSourcesPerNotebook,omitempty"`
	NotebookID            string `json:"notebookId,omitempty"`
}

func (c *Config) applyWatchDefaults() {
	if c.Watch.PollInterval == "" {
		c.Watch.PollInterval = "5m"
	}
	if c.Watch.StateFile == "" {
		c.Watch.StateFile = "./.watch-state.json"
	}
	if c.Watch.MaxLecturesPerCycle == 0 {
		c.Watch.MaxLecturesPerCycle = 3
	}
	if c.Watch.MaxUploadRetries == 0 {
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
	c.applyNotebookLMDefaults()
}

func (c *Config) applyNotebookLMDefaults() {
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
	if nlm.UploadTimeout == "" {
		nlm.UploadTimeout = "30m"
	}
	if nlm.MaxSourcesPerNotebook == 0 {
		nlm.MaxSourcesPerNotebook = 300
	}
}

// ResolvedTargets returns a copy of the canonical target list with the optional
// notebook default applied.
func (c *Config) ResolvedTargets() []WatchTarget {
	targets := make([]WatchTarget, len(c.Watch.Targets))
	copy(targets, c.Watch.Targets)
	for i := range targets {
		if strings.TrimSpace(targets[i].NotebookID) == "" {
			targets[i].NotebookID = strings.TrimSpace(c.Watch.NotebookLM.NotebookID)
		}
	}
	return targets
}

// ApplyWatchMediaDefaults forces the bandwidth-efficient audio settings used by
// the watch loop onto the top-level media fields read by the downloader.
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

func (c *Config) validateWatch() error {
	if err := c.validateWatchShape(); err != nil {
		return err
	}
	if !c.Watch.Enabled {
		return nil
	}
	targets := c.ResolvedTargets()
	if len(targets) == 0 {
		return fmt.Errorf("watch.targets is required when watch.enabled is true")
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
	}
	return nil
}

func (c *Config) validateWatchShape() error {
	if c.Watch.PollInterval != "" {
		interval, err := time.ParseDuration(c.Watch.PollInterval)
		if err != nil {
			return fmt.Errorf("invalid watch.pollInterval: %w", err)
		}
		if interval < 5*time.Minute || interval > 24*time.Hour {
			return fmt.Errorf("watch.pollInterval must be between 5m and 24h, got %v", interval)
		}
	}
	if c.Watch.MaxLecturesPerCycle < 1 {
		return fmt.Errorf("watch.maxLecturesPerCycle must be >= 1")
	}
	if c.Watch.MaxUploadRetries < 1 {
		return fmt.Errorf("watch.maxUploadRetries must be >= 1")
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
	if !OneOf(nlm.Provider, "notebooklm-py", "nlm") {
		return fmt.Errorf("watch.notebooklm.provider must be one of: notebooklm-py, nlm")
	}
	if nlm.UploadTimeout != "" {
		timeout, err := time.ParseDuration(nlm.UploadTimeout)
		if err != nil {
			return fmt.Errorf("invalid watch.notebooklm.uploadTimeout: %w", err)
		}
		if timeout < time.Minute || timeout > 2*time.Hour {
			return fmt.Errorf("watch.notebooklm.uploadTimeout must be between 1m and 2h, got %v", timeout)
		}
	}
	if nlm.MaxSourcesPerNotebook < 1 {
		return fmt.Errorf("watch.notebooklm.maxSourcesPerNotebook must be >= 1")
	}
	if !c.Watch.Upload {
		return nil
	}
	targets := c.ResolvedTargets()
	for i, target := range targets {
		if strings.TrimSpace(target.NotebookID) == "" {
			return fmt.Errorf("watch.targets[%d].notebookId (or watch.notebooklm.notebookId) is required when watch.upload is true", i)
		}
	}
	if len(targets) == 0 && strings.TrimSpace(nlm.NotebookID) == "" {
		return fmt.Errorf("watch.notebooklm.notebookId is required when watch.upload is true")
	}
	return nil
}

func applyWatchEnvOverrides(cfg *Config) error {
	applyStringEnv("IMPARTUS_WATCH_POLL_INTERVAL", &cfg.Watch.PollInterval)
	applyStringEnv("IMPARTUS_WATCH_STATE_FILE", &cfg.Watch.StateFile)
	applyStringEnv("IMPARTUS_WATCH_QUALITY", &cfg.Watch.Quality)
	applyStringEnv("IMPARTUS_WATCH_VIEWS", &cfg.Watch.Views)
	applyStringEnv("IMPARTUS_WATCH_AUDIO_FORMAT", &cfg.Watch.AudioFormat)
	applyStringEnv("IMPARTUS_NOTEBOOKLM_NOTEBOOK_ID", &cfg.Watch.NotebookLM.NotebookID)
	applyStringEnv("IMPARTUS_NOTEBOOKLM_COMMAND", &cfg.Watch.NotebookLM.Command)
	applyStringEnv("IMPARTUS_NOTEBOOKLM_PROFILE", &cfg.Watch.NotebookLM.Profile)
	applyStringEnv("IMPARTUS_NOTEBOOKLM_PROVIDER", &cfg.Watch.NotebookLM.Provider)
	applyStringEnv("IMPARTUS_NOTEBOOKLM_UPLOAD_TIMEOUT", &cfg.Watch.NotebookLM.UploadTimeout)
	for _, apply := range []func() error{
		func() error { return applyBoolEnv("IMPARTUS_WATCH_ENABLED", &cfg.Watch.Enabled) },
		func() error { return applyBoolEnv("IMPARTUS_WATCH_UPLOAD", &cfg.Watch.Upload) },
		func() error {
			return applyBoolEnv("IMPARTUS_WATCH_DELETE_AUDIO_AFTER_UPLOAD", &cfg.Watch.DeleteAudioAfterUpload)
		},
		func() error {
			return applyIntEnv("IMPARTUS_WATCH_MAX_LECTURES_PER_CYCLE", &cfg.Watch.MaxLecturesPerCycle)
		},
		func() error { return applyIntEnv("IMPARTUS_WATCH_MAX_UPLOAD_RETRIES", &cfg.Watch.MaxUploadRetries) },
		func() error {
			return applyIntEnv("IMPARTUS_NOTEBOOKLM_MAX_SOURCES", &cfg.Watch.NotebookLM.MaxSourcesPerNotebook)
		},
	} {
		if err := apply(); err != nil {
			return err
		}
	}
	return nil
}
