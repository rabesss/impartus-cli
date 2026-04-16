package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const ConfigLocation = "./config.json"

type ProgressConfig struct {
	Enabled         bool   `json:"enabled"`
	ShowSpeed       bool   `json:"showSpeed"`
	ShowETA         bool   `json:"showETA"`
	UpdateInterval  string `json:"updateInterval"`
	SpeedWindowSize int    `json:"speedWindowSize"`
}

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
	HTTPTimeout               string         `json:"httpTimeout"`
}

var (
	loadedConfig Config
	loadOnce     sync.Once
	loadErr      error
)

func (c *Config) ApplyDefaults() {
	c.applyPathDefaults()
	c.applyWorkerDefaults()
	c.applyRateLimitDefaults()
	c.applyProgressDefaults()
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
		c.DownloadWorkersPerLecture = 3
	}
	if c.DecryptWorkersPerLecture == 0 {
		c.DecryptWorkersPerLecture = 2
	}
}

func (c *Config) applyRateLimitDefaults() {
	if c.RateLimit == 0 {
		c.RateLimit = 10
	}
	if c.APIRateLimit == 0 {
		c.APIRateLimit = 2
	}
	c.EnableJitter = true
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
	return c.validateTimeout()
}

func (c *Config) validateCore() error {
	if c.Username == "" || c.Password == "" {
		return fmt.Errorf("username and password are required")
	}
	if c.BaseURL == "" {
		return fmt.Errorf("baseUrl is required")
	}
	if c.NumWorkers < 1 || c.NumWorkers > 50 {
		return fmt.Errorf("numWorkers must be between 1 and 50, got %d", c.NumWorkers)
	}
	if !oneOf(c.Quality, "144", "450", "720") {
		return fmt.Errorf("quality must be one of: 144, 450, 720")
	}
	if !oneOf(c.Views, "first", "second", "both", "left", "right") {
		return fmt.Errorf("views must be one of: first, second, both, left, right")
	}
	if c.AudioOnly && !oneOf(c.AudioFormat, "mp3", "m4a", "aac", "opus") {
		return fmt.Errorf("audioFormat must be one of: mp3, m4a, aac, opus")
	}
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
	if c.DownloadWorkersPerLecture < 1 || c.DownloadWorkersPerLecture > 10 {
		return fmt.Errorf("downloadWorkersPerLecture must be between 1 and 10, got %d", c.DownloadWorkersPerLecture)
	}
	if c.DecryptWorkersPerLecture < 1 || c.DecryptWorkersPerLecture > 10 {
		return fmt.Errorf("decryptWorkersPerLecture must be between 1 and 10, got %d", c.DecryptWorkersPerLecture)
	}

	// Only check efficiency warning when pipeline is actually enabled
	if c.EnablePipeline && c.DecryptWorkersPerLecture > c.DownloadWorkersPerLecture {
		fmt.Printf("[WARNING] decryptWorkersPerLecture (%d) > downloadWorkersPerLecture (%d) may be inefficient\n",
			c.DecryptWorkersPerLecture, c.DownloadWorkersPerLecture)
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

func Parse(path string) (*Config, error) {
	if path == "" {
		path = ConfigLocation
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not open config file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(contents, &cfg); err != nil {
		return nil, fmt.Errorf("could not parse config json: %w", err)
	}

	return &cfg, nil
}

func LoadResolved(path string) (*Config, error) {
	var cfg *Config
	var err error

	if path != "" {
		cfg, err = Parse(path)
		if err != nil {
			return nil, err
		}
	} else {
		cfg = &Config{}
		if _, statErr := os.Stat(ConfigLocation); statErr == nil {
			cfg, err = Parse(ConfigLocation)
			if err != nil {
				return nil, err
			}
		} else if !os.IsNotExist(statErr) {
			return nil, fmt.Errorf("could not open config file: %w", statErr)
		}
	}

	applyEnvOverrides(cfg)
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func applyEnvOverrides(cfg *Config) {
	applyStringEnv("IMPARTUS_USERNAME", &cfg.Username)
	applyStringEnv("IMPARTUS_PASSWORD", &cfg.Password)
	applyStringEnv("IMPARTUS_BASE_URL", &cfg.BaseURL)
	applyStringEnv("IMPARTUS_QUALITY", &cfg.Quality)
	applyStringEnv("IMPARTUS_VIEWS", &cfg.Views)
	applyStringEnv("IMPARTUS_DOWNLOAD_LOCATION", &cfg.DownloadLocation)
	applyStringEnv("IMPARTUS_TEMP_DIR", &cfg.TempDirLocation)
	applyStringEnv("IMPARTUS_AUDIO_FORMAT", &cfg.AudioFormat)
	applyStringEnv("IMPARTUS_HTTP_TIMEOUT", &cfg.HTTPTimeout)
	applyBoolEnv("IMPARTUS_AUDIO_ONLY", &cfg.AudioOnly)
	applyBoolEnv("IMPARTUS_SKIP_NO_AUDIO", &cfg.SkipNoAudio)
	applyIntEnv("IMPARTUS_NUM_WORKERS", &cfg.NumWorkers)
	applyFloatEnv("IMPARTUS_RATE_LIMIT", &cfg.RateLimit)
	applyFloatEnv("IMPARTUS_API_RATE_LIMIT", &cfg.APIRateLimit)

	applyCanonicalFields(cfg)
}

func applyCanonicalFields(cfg *Config) {
	if cfg.Views != "" {
		cfg.Views = strings.ToLower(strings.TrimSpace(cfg.Views))
	}
}

func applyStringEnv(key string, target *string) {
	if value, ok := os.LookupEnv(key); ok {
		*target = value
	}
}

func applyBoolEnv(key string, target *bool) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return
	}
	parsed, err := strconv.ParseBool(value)
	if err == nil {
		*target = parsed
	}
}

func applyIntEnv(key string, target *int) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return
	}
	parsed, err := strconv.Atoi(value)
	if err == nil {
		*target = parsed
	}
}

func applyFloatEnv(key string, target *float64) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err == nil {
		*target = parsed
	}
}

func Load(path string) (*Config, error) {
	cfg, err := Parse(path)
	if err != nil {
		return nil, err
	}

	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func Get() *Config {
	loadOnce.Do(func() {
		cfg, err := Load(ConfigLocation)
		if err != nil {
			loadErr = err
			return
		}
		loadedConfig = *cfg
	})

	if loadErr != nil {
		panic(loadErr)
	}

	return &loadedConfig
}

// oneOf checks if a value is in the allowed set.
func oneOf(val string, allowed ...string) bool {
	for _, a := range allowed {
		if val == a {
			return true
		}
	}
	return false
}
