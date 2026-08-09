package config

import (
	"fmt"
	"strings"
	"time"
)

// WatchTarget identifies one Impartus course to poll. It deliberately carries
// no downstream-provider routing; completed artifacts are the integration
// boundary for external consumers.
type WatchTarget struct {
	SubjectID int    `json:"subjectId"`
	SessionID int    `json:"sessionId"`
	Label     string `json:"label,omitempty"`
}

// WatchConfig controls generic lecture discovery and durable auto-download.
type WatchConfig struct {
	Enabled             bool          `json:"enabled"`
	PollInterval        string        `json:"pollInterval,omitempty"`
	MaxLecturesPerCycle int           `json:"maxLecturesPerCycle,omitempty"`
	MaxRetries          int           `json:"maxRetries,omitempty"`
	Targets             []WatchTarget `json:"targets,omitempty"`
	Quality             string        `json:"quality,omitempty"`
	Views               string        `json:"views,omitempty"`
	AudioFormat         string        `json:"audioFormat,omitempty"`
}

func (c *Config) applyWatchDefaults() {
	if c.Watch.PollInterval == "" {
		c.Watch.PollInterval = "5m"
	}
	if c.Watch.MaxLecturesPerCycle == 0 {
		c.Watch.MaxLecturesPerCycle = 3
	}
	if c.Watch.MaxRetries == 0 {
		c.Watch.MaxRetries = 3
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
}

// ApplyWatchMediaDefaults applies the watcher's bandwidth-efficient media
// selection to the top-level downloader configuration.
func (c *Config) ApplyWatchMediaDefaults() {
	c.AudioOnly = true
	c.SkipNoAudio = true
	c.Quality = c.Watch.Quality
	c.Views = NormalizeViews(c.Watch.Views)
	c.AudioFormat = c.Watch.AudioFormat
}

func (c *Config) validateWatch() error {
	if !c.Watch.Enabled {
		return nil
	}
	interval, err := time.ParseDuration(c.Watch.PollInterval)
	if err != nil {
		return fmt.Errorf("invalid watch.pollInterval: %w", err)
	}
	if interval < 5*time.Minute || interval > 24*time.Hour {
		return fmt.Errorf("watch.pollInterval must be between 5m and 24h, got %v", interval)
	}
	if c.Watch.MaxLecturesPerCycle < 1 {
		return errorsWatch("maxLecturesPerCycle", "must be >= 1")
	}
	if c.Watch.MaxRetries < 1 {
		return errorsWatch("maxRetries", "must be >= 1")
	}
	if !OneOf(c.Watch.Quality, "144", "450", "720") {
		return errorsWatch("quality", "must be one of: 144, 450, 720")
	}
	if !OneOf(c.Watch.Views, "left", "right", "both") {
		return errorsWatch("views", "must be one of: left, right, both")
	}
	if !OneOf(c.Watch.AudioFormat, "mp3", "m4a", "aac", "opus") {
		return errorsWatch("audioFormat", "must be one of: mp3, m4a, aac, opus")
	}
	if len(c.Watch.Targets) == 0 {
		return fmt.Errorf("watch.targets is required when watch.enabled is true")
	}
	seen := make(map[string]struct{}, len(c.Watch.Targets))
	for index, target := range c.Watch.Targets {
		if target.SubjectID <= 0 || target.SessionID <= 0 {
			return fmt.Errorf("watch.targets[%d]: subjectId and sessionId are required", index)
		}
		key := fmt.Sprintf("%d:%d", target.SubjectID, target.SessionID)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("watch.targets contains duplicate subjectId/sessionId %s", key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func errorsWatch(field, message string) error {
	return fmt.Errorf("watch.%s %s", field, message)
}

func applyWatchEnvOverrides(c *Config) error {
	applyStringEnv("IMPARTUS_WATCH_POLL_INTERVAL", &c.Watch.PollInterval)
	applyStringEnv("IMPARTUS_WATCH_QUALITY", &c.Watch.Quality)
	applyStringEnv("IMPARTUS_WATCH_VIEWS", &c.Watch.Views)
	applyStringEnv("IMPARTUS_WATCH_AUDIO_FORMAT", &c.Watch.AudioFormat)
	for _, apply := range []func() error{
		func() error { return applyBoolEnv("IMPARTUS_WATCH_ENABLED", &c.Watch.Enabled) },
		func() error {
			return applyIntEnv("IMPARTUS_WATCH_MAX_LECTURES_PER_CYCLE", &c.Watch.MaxLecturesPerCycle)
		},
		func() error { return applyIntEnv("IMPARTUS_WATCH_MAX_RETRIES", &c.Watch.MaxRetries) },
	} {
		if err := apply(); err != nil {
			return err
		}
	}
	c.Watch.Views = NormalizeViews(c.Watch.Views)
	for index := range c.Watch.Targets {
		c.Watch.Targets[index].Label = strings.TrimSpace(c.Watch.Targets[index].Label)
	}
	return nil
}
