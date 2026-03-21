package analytics

import (
	"os"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/buildinfo"
)

func TestDefaultConfig(t *testing.T) {
	// Clear any existing env vars
	os.Unsetenv("POSTHOG_API_KEY")
	os.Unsetenv("POSTHOG_ENDPOINT")
	os.Unsetenv("ANALYTICS_ENDPOINT")
	os.Unsetenv("ANALYTICS_API_KEY")
	os.Unsetenv("IMPARTUS_ANALYTICS_ENABLED")

	cfg := DefaultConfig()

	if cfg.PostHogAPIKey != "" {
		t.Errorf("expected empty PostHogAPIKey, got %s", cfg.PostHogAPIKey)
	}

	if cfg.PostHogEndpoint != "https://app.posthog.com" {
		t.Errorf("expected default endpoint, got %s", cfg.PostHogEndpoint)
	}

	if cfg.Enabled {
		t.Error("expected Enabled to be false without env var")
	}

	if cfg.BatchSize != 50 {
		t.Errorf("expected default batch size of 50, got %d", cfg.BatchSize)
	}

	if cfg.FlushInterval != 30*time.Second {
		t.Errorf("expected flush interval of 30s, got %v", cfg.FlushInterval)
	}
}

func TestConfigFromEnv(t *testing.T) {
	os.Setenv("POSTHOG_API_KEY", "test-key")
	os.Setenv("POSTHOG_ENDPOINT", "https://custom.posthog.com")
	os.Setenv("ANALYTICS_ENDPOINT", "https://custom.endpoint.com")
	os.Setenv("IMPARTUS_ANALYTICS_ENABLED", "true")
	defer func() {
		os.Unsetenv("POSTHOG_API_KEY")
		os.Unsetenv("POSTHOG_ENDPOINT")
		os.Unsetenv("ANALYTICS_ENDPOINT")
		os.Unsetenv("IMPARTUS_ANALYTICS_ENABLED")
	}()

	cfg := DefaultConfig()

	if cfg.PostHogAPIKey != "test-key" {
		t.Errorf("expected PostHogAPIKey 'test-key', got %s", cfg.PostHogAPIKey)
	}

	if cfg.PostHogEndpoint != "https://custom.posthog.com" {
		t.Errorf("expected custom endpoint, got %s", cfg.PostHogEndpoint)
	}

	if cfg.CustomEndpoint != "https://custom.endpoint.com" {
		t.Errorf("expected custom endpoint, got %s", cfg.CustomEndpoint)
	}

	if !cfg.Enabled {
		t.Error("expected Enabled to be true")
	}
}

func TestGetEnvOrDefault(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		value      string
		defaultVal string
		want       string
	}{
		{"returns value when set", "TEST_KEY", "custom-value", "default", "custom-value"},
		{"returns default when empty", "TEST_KEY_EMPTY", "", "default", "default"},
		{"returns default when unset", "UNSET_KEY", "", "default", "default"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value != "" {
				os.Setenv(tt.key, tt.value)
				defer os.Unsetenv(tt.key)
			} else if tt.key != "" {
				os.Unsetenv(tt.key)
			}

			got := getEnvOrDefault(tt.key, tt.defaultVal)
			if got != tt.want {
				t.Errorf("getEnvOrDefault() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMergeProperties(t *testing.T) {
	props := map[string]interface{}{
		"custom_field": "value",
		"count":        42,
	}

	result := mergeProperties(props)

	// Check that library fields are added
	if result["library"] != "impartus-cli" {
		t.Errorf("expected library field, got %v", result["library"])
	}

	if result["library_version"] != buildinfo.Version {
		t.Errorf("expected library_version field, got %v", result["library_version"])
	}

	if result["os"] == "" {
		t.Error("expected os field to be set")
	}

	if result["arch"] == "" {
		t.Error("expected arch field to be set")
	}

	// Check that custom fields are preserved
	if result["custom_field"] != "value" {
		t.Errorf("expected custom_field 'value', got %v", result["custom_field"])
	}

	if result["count"] != 42 {
		t.Errorf("expected count 42, got %v", result["count"])
	}
}

func TestGenerateDistinctID(t *testing.T) {
	id1 := generateDistinctID()
	id2 := generateDistinctID()

	if id1 == "" {
		t.Error("expected non-empty distinct ID")
	}

	// IDs should be different due to timestamp
	// (though theoretically could be same if called within same second)
	if id1 == id2 {
		t.Log("Note: IDs are same (expected if called in same second)")
	}
}

func TestTrackDisabled(t *testing.T) {
	os.Unsetenv("IMPARTUS_ANALYTICS_ENABLED")
	cfg := DefaultConfig()
	cfg.Enabled = false

	analytics := &Analytics{
		config: cfg,
		events: make([]Event, 0),
	}

	// Should not panic when tracking is disabled
	analytics.Track("test_event", map[string]interface{}{"key": "value"})

	if len(analytics.events) != 0 {
		t.Errorf("expected no events when disabled, got %d", len(analytics.events))
	}
}

func TestTrackFeatureUsage(t *testing.T) {
	os.Setenv("IMPARTUS_ANALYTICS_ENABLED", "true")
	defer os.Unsetenv("IMPARTUS_ANALYTICS_ENABLED")

	cfg := DefaultConfig()
	analytics := &Analytics{
		config:     cfg,
		events:     make([]Event, 0, 10),
		distinctID: "test-id",
	}

	analytics.TrackFeatureUsage("download", map[string]interface{}{"quality": "720"})

	if len(analytics.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(analytics.events))
	}

	event := analytics.events[0]
	if event.Event != "feature_used" {
		t.Errorf("expected event 'feature_used', got %s", event.Event)
	}

	if event.Properties["feature"] != "download" {
		t.Errorf("expected feature 'download', got %v", event.Properties["feature"])
	}

	if event.Properties["quality"] != "720" {
		t.Errorf("expected quality '720', got %v", event.Properties["quality"])
	}
}

func TestTrackCommandExecution(t *testing.T) {
	os.Setenv("IMPARTUS_ANALYTICS_ENABLED", "true")
	defer os.Unsetenv("IMPARTUS_ANALYTICS_ENABLED")

	cfg := DefaultConfig()
	analytics := &Analytics{
		config:     cfg,
		events:     make([]Event, 0, 10),
		distinctID: "test-id",
	}

	analytics.TrackCommandExecution("download", true, 5*time.Second)

	if len(analytics.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(analytics.events))
	}

	event := analytics.events[0]
	if event.Event != "command_executed" {
		t.Errorf("expected event 'command_executed', got %s", event.Event)
	}

	if event.Properties["command"] != "download" {
		t.Errorf("expected command 'download', got %v", event.Properties["command"])
	}

	if event.Properties["success"] != true {
		t.Errorf("expected success true, got %v", event.Properties["success"])
	}

	if event.Properties["duration_ms"] != int64(5000) {
		t.Errorf("expected duration_ms 5000, got %v", event.Properties["duration_ms"])
	}
}

func TestTrackDownload(t *testing.T) {
	os.Setenv("IMPARTUS_ANALYTICS_ENABLED", "true")
	defer os.Unsetenv("IMPARTUS_ANALYTICS_ENABLED")

	cfg := DefaultConfig()
	analytics := &Analytics{
		config:     cfg,
		events:     make([]Event, 0, 10),
		distinctID: "test-id",
	}

	analytics.TrackDownload("720p", "Lecture 1", 1024000, 30*time.Second, true)

	if len(analytics.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(analytics.events))
	}

	event := analytics.events[0]
	if event.Event != "download_completed" {
		t.Errorf("expected event 'download_completed', got %s", event.Event)
	}

	if event.Properties["quality"] != "720p" {
		t.Errorf("expected quality '720p', got %v", event.Properties["quality"])
	}

	if event.Properties["bytes"] != int64(1024000) {
		t.Errorf("expected bytes 1024000, got %v", event.Properties["bytes"])
	}

	if event.Properties["success"] != true {
		t.Errorf("expected success true, got %v", event.Properties["success"])
	}
}

func TestFlushWithNoEvents(t *testing.T) {
	cfg := DefaultConfig()
	analytics := &Analytics{
		config:     cfg,
		events:     make([]Event, 0, 10),
		distinctID: "test-id",
	}

	// Should not panic
	analytics.Flush()
}
