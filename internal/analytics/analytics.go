package analytics

// Package analytics provides product analytics instrumentation for understanding
// feature usage and measuring the impact of changes.
//
// This package supports multiple analytics backends:
// - PostHog (self-hosted or cloud)
// - Custom HTTP endpoint
//
// Usage:
//
//	analytics.Track("download_completed", map[string]interface{}{
//	    "quality": "720",
//	    "duration_ms": 45000,
//	})

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/rabesss/impartus-cli/internal/buildinfo"
)

// Event represents an analytics event
type Event struct {
	Event      string                 `json:"event"`
	Properties map[string]interface{} `json:"properties"`
	Timestamp  time.Time              `json:"timestamp"`
}

// Config holds analytics configuration
type Config struct {
	// PostHog configuration
	PostHogAPIKey   string
	PostHogEndpoint string

	// Custom endpoint configuration
	CustomEndpoint string
	CustomAPIKey   string

	// Feature flag to enable/disable
	Enabled bool

	// Sample rate (0.0 to 1.0) for reducing volume
	SampleRate float64

	// Batch settings
	BatchSize     int
	FlushInterval time.Duration
}

// Analytics handles event tracking
type Analytics struct {
	config     Config
	client     *http.Client
	events     []Event
	mu         sync.Mutex
	distinctID string
}

var (
	instance *Analytics
	once     sync.Once
)

// DefaultConfig returns the default analytics configuration from environment
func DefaultConfig() Config {
	return Config{
		PostHogAPIKey:   os.Getenv("POSTHOG_API_KEY"),
		PostHogEndpoint: getEnvOrDefault("POSTHOG_ENDPOINT", "https://app.posthog.com"),
		CustomEndpoint:  os.Getenv("ANALYTICS_ENDPOINT"),
		CustomAPIKey:    os.Getenv("ANALYTICS_API_KEY"),
		Enabled:         os.Getenv("IMPARTUS_ANALYTICS_ENABLED") == "true",
		SampleRate:      1.0,
		BatchSize:       50,
		FlushInterval:   30 * time.Second,
	}
}

func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

// Init initializes the analytics client
func Init() error {
	var initErr error
	once.Do(func() {
		cfg := DefaultConfig()
		instance = &Analytics{
			config:     cfg,
			client:     &http.Client{Timeout: 10 * time.Second},
			events:     make([]Event, 0, cfg.BatchSize),
			distinctID: generateDistinctID(),
		}

		// Start background flusher if enabled
		if cfg.Enabled {
			go instance.startFlusher()
		}
	})
	return initErr
}

// Get returns the analytics instance
func Get() *Analytics {
	return instance
}

// IsEnabled returns whether analytics is enabled
func (a *Analytics) IsEnabled() bool {
	return a.config.Enabled
}

// Track sends an analytics event
func (a *Analytics) Track(eventName string, properties map[string]interface{}) {
	if !a.config.Enabled {
		return
	}

	// Apply sampling
	if a.config.SampleRate < 1.0 && a.config.SampleRate > 0 {
		if time.Now().UnixNano()%100 > int64(a.config.SampleRate*100) {
			return
		}
	}

	event := Event{
		Event:      eventName,
		Properties: mergeProperties(properties),
		Timestamp:  time.Now(),
	}

	a.mu.Lock()
	a.events = append(a.events, event)
	shouldFlush := len(a.events) >= a.config.BatchSize
	a.mu.Unlock()

	if shouldFlush {
		go a.Flush()
	}
}

// TrackFeatureUsage tracks a feature being used
func (a *Analytics) TrackFeatureUsage(featureName string, properties map[string]interface{}) {
	if properties == nil {
		properties = make(map[string]interface{})
	}
	properties["feature"] = featureName
	a.Track("feature_used", properties)
}

// TrackCommandExecution tracks CLI command execution
func (a *Analytics) TrackCommandExecution(command string, success bool, duration time.Duration) {
	properties := map[string]interface{}{
		"command":     command,
		"success":     success,
		"duration_ms": duration.Milliseconds(),
	}
	a.Track("command_executed", properties)
}

// TrackDownload tracks download statistics
func (a *Analytics) TrackDownload(quality, views string, bytes int64, duration time.Duration, success bool) {
	properties := map[string]interface{}{
		"quality":     quality,
		"views":       views,
		"bytes":       bytes,
		"duration_ms": duration.Milliseconds(),
		"success":     success,
	}
	a.Track("download_completed", properties)
}

// Flush sends all buffered events
func (a *Analytics) Flush() {
	a.mu.Lock()
	if len(a.events) == 0 {
		a.mu.Unlock()
		return
	}
	events := a.events
	a.events = make([]Event, 0, a.config.BatchSize)
	a.mu.Unlock()

	// Send to PostHog if configured
	if a.config.PostHogAPIKey != "" {
		a.sendToPostHog(events)
	}

	// Send to custom endpoint if configured
	if a.config.CustomEndpoint != "" {
		go a.sendToCustomEndpoint(events)
	}
}

// FlushWithContext sends all buffered events with context
func (a *Analytics) FlushWithContext(ctx context.Context) {
	a.mu.Lock()
	if len(a.events) == 0 {
		a.mu.Unlock()
		return
	}
	events := a.events
	a.events = make([]Event, 0, a.config.BatchSize)
	a.mu.Unlock()

	// Send to PostHog if configured
	if a.config.PostHogAPIKey != "" {
		a.sendToPostHogWithContext(ctx, events)
	}

	// Send to custom endpoint if configured
	if a.config.CustomEndpoint != "" {
		go a.sendToCustomEndpoint(events)
	}
}

func (a *Analytics) startFlusher() {
	ticker := time.NewTicker(a.config.FlushInterval)
	defer ticker.Stop()

	for range ticker.C {
		a.Flush()
	}
}

func (a *Analytics) sendToPostHog(events []Event) {
	a.sendToPostHogWithContext(context.Background(), events)
}

func (a *Analytics) sendToPostHogWithContext(ctx context.Context, events []Event) {
	payload := map[string]interface{}{
		"api_key": a.config.PostHogAPIKey,
		"batch":   a.formatPostHogEvents(events),
	}

	//nolint:errcheck // json.Marshal never fails for these simple structs
	body, _ := json.Marshal(payload)
	//nolint:errcheck // URL is constructed from validated config
	req, _ := http.NewRequestWithContext(ctx, "POST", a.config.PostHogEndpoint+"/batch", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[analytics] failed to send to PostHog: %v\n", err)
		return
	}
	defer resp.Body.Close()
}

func (a *Analytics) formatPostHogEvents(events []Event) []map[string]interface{} {
	result := make([]map[string]interface{}, len(events))
	for i, event := range events {
		result[i] = map[string]interface{}{
			"event":       event.Event,
			"timestamp":   event.Timestamp.Format(time.RFC3339),
			"distinct_id": a.distinctID,
			"properties":  event.Properties,
		}
	}
	return result
}

func (a *Analytics) sendToCustomEndpoint(events []Event) {
	payload := map[string]interface{}{
		"events": events,
		"source": "impartus-cli",
	}

	//nolint:errcheck // json.Marshal never fails for these simple structs
	body, _ := json.Marshal(payload)
	//nolint:errcheck // URL is constructed from validated config
	req, _ := http.NewRequestWithContext(context.Background(), "POST", a.config.CustomEndpoint, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	if a.config.CustomAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.config.CustomAPIKey)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[analytics] failed to send to custom endpoint: %v\n", err)
		return
	}
	defer resp.Body.Close()
}

func mergeProperties(props map[string]interface{}) map[string]interface{} {
	result := map[string]interface{}{
		"library":         "impartus-cli",
		"library_version": buildinfo.Version,
		"os":              runtime.GOOS,
		"arch":            runtime.GOARCH,
	}
	for k, v := range props {
		result[k] = v
	}
	return result
}

func generateDistinctID() string {
	hostname, _ := os.Hostname() //nolint:errcheck // hostname fallback is empty string
	return fmt.Sprintf("%s-%d", hostname, time.Now().Unix())
}
