package config

import (
	"os"
	"strings"
)

// FeatureFlags defines the available feature flags for the application.
// Feature flags allow agents to safely ship changes behind toggles,
// reducing risk of agent-authored code affecting all users immediately.
type FeatureFlags struct {
	// EnableVerboseLogging enables verbose logging for debugging.
	EnableVerboseLogging bool `json:"enableVerboseLogging"`

	// EnableExperimentalFeatures enables experimental features that are not yet stable.
	EnableExperimentalFeatures bool `json:"enableExperimentalFeatures"`

	// EnableAdvancedRateLimiting enables advanced rate limiting with adaptive throttling.
	EnableAdvancedRateLimiting bool `json:"enableAdvancedRateLimiting"`

	// EnableRetryWithBackoff enables exponential backoff for retries.
	EnableRetryWithBackoff bool `json:"enableRetryWithBackoff"`

	// EnableParallelChunkDownloads enables parallel chunk downloads.
	EnableParallelChunkDownloads bool `json:"enableParallelChunkDownloads"`

	// EnableDetailedProgress enables detailed progress reporting with more metrics.
	EnableDetailedProgress bool `json:"enableDetailedProgress"`

	// EnableMetricsExport enables OpenTelemetry metrics export.
	EnableMetricsExport bool `json:"enableMetricsExport"`

	// EnableAlerting enables webhook alerting functionality.
	EnableAlerting bool `json:"enableAlerting"`

	// EnableSentryErrors enables Sentry error tracking.
	EnableSentryErrors bool `json:"enableSentryErrors"`

	// EnableWebSocketCompression enables WebSocket compression.
	EnableWebSocketCompression bool `json:"enableWebSocketCompression"`

	// EnableStreamingResponse enables streaming responses for large data.
	EnableStreamingResponse bool `json:"enableStreamingResponse"`

	// EnableMetricsEndpoint enables the /metrics endpoint for Prometheus scraping.
	EnableMetricsEndpoint bool `json:"enableMetricsEndpoint"`
}

// DefaultFeatureFlags returns the default feature flag configuration.
// These flags are conservative by default to minimize risk.
func DefaultFeatureFlags() *FeatureFlags {
	return &FeatureFlags{
		EnableVerboseLogging:          false,
		EnableExperimentalFeatures:   false,
		EnableAdvancedRateLimiting:   false,
		EnableRetryWithBackoff:       true,  // Safe default - helps with transient failures
		EnableParallelChunkDownloads: true,  // Safe default - improves download speed
		EnableDetailedProgress:       true,  // Safe default - better UX
		EnableMetricsExport:          false, // Disabled by default - requires OTEL endpoint
		EnableAlerting:               false, // Disabled by default - requires webhook URL
		EnableSentryErrors:           false, // Disabled by default - requires DSN
		EnableWebSocketCompression:   true,  // Safe default - reduces bandwidth
		EnableStreamingResponse:      false, // Disabled by default - may affect compatibility
		EnableMetricsEndpoint:        false, // Disabled by default - security consideration
	}
}

// ApplyFeatureFlagEnvOverrides applies feature flag values from environment variables.
// Environment variables take precedence over config file values.
// Format: IMPARTUS_FLAG_<FEATURE_NAME>=true|false
func (c *Config) ApplyFeatureFlagEnvOverrides() {
	// Verbose logging
	if val := os.Getenv("IMPARTUS_FLAG_VERBOSE_LOGGING"); val != "" {
		c.FeatureFlags.EnableVerboseLogging = strings.ToLower(val) == "true"
	}

	// Experimental features
	if val := os.Getenv("IMPARTUS_FLAG_EXPERIMENTAL_FEATURES"); val != "" {
		c.FeatureFlags.EnableExperimentalFeatures = strings.ToLower(val) == "true"
	}

	// Advanced rate limiting
	if val := os.Getenv("IMPARTUS_FLAG_ADVANCED_RATE_LIMITING"); val != "" {
		c.FeatureFlags.EnableAdvancedRateLimiting = strings.ToLower(val) == "true"
	}

	// Retry with backoff
	if val := os.Getenv("IMPARTUS_FLAG_RETRY_WITH_BACKOFF"); val != "" {
		c.FeatureFlags.EnableRetryWithBackoff = strings.ToLower(val) == "true"
	}

	// Parallel chunk downloads
	if val := os.Getenv("IMPARTUS_FLAG_PARALLEL_CHUNKS"); val != "" {
		c.FeatureFlags.EnableParallelChunkDownloads = strings.ToLower(val) == "true"
	}

	// Detailed progress
	if val := os.Getenv("IMPARTUS_FLAG_DETAILED_PROGRESS"); val != "" {
		c.FeatureFlags.EnableDetailedProgress = strings.ToLower(val) == "true"
	}

	// Metrics export
	if val := os.Getenv("IMPARTUS_FLAG_METRICS_EXPORT"); val != "" {
		c.FeatureFlags.EnableMetricsExport = strings.ToLower(val) == "true"
	}

	// Alerting
	if val := os.Getenv("IMPARTUS_FLAG_ALERTING"); val != "" {
		c.FeatureFlags.EnableAlerting = strings.ToLower(val) == "true"
	}

	// Sentry errors
	if val := os.Getenv("IMPARTUS_FLAG_SENTRY_ERRORS"); val != "" {
		c.FeatureFlags.EnableSentryErrors = strings.ToLower(val) == "true"
	}

	// WebSocket compression
	if val := os.Getenv("IMPARTUS_FLAG_WS_COMPRESSION"); val != "" {
		c.FeatureFlags.EnableWebSocketCompression = strings.ToLower(val) == "true"
	}

	// Streaming response
	if val := os.Getenv("IMPARTUS_FLAG_STREAMING_RESPONSE"); val != "" {
		c.FeatureFlags.EnableStreamingResponse = strings.ToLower(val) == "true"
	}

	// Metrics endpoint
	if val := os.Getenv("IMPARTUS_FLAG_METRICS_ENDPOINT"); val != "" {
		c.FeatureFlags.EnableMetricsEndpoint = strings.ToLower(val) == "true"
	}
}

// IsFeatureEnabled checks if a specific feature flag is enabled.
// This provides a convenient way to check flags in the codebase.
func (ff *FeatureFlags) IsFeatureEnabled(featureName string) bool {
	switch strings.ToLower(featureName) {
	case "verbose_logging", "verbose":
		return ff.EnableVerboseLogging
	case "experimental_features", "experimental":
		return ff.EnableExperimentalFeatures
	case "advanced_rate_limiting", "advanced_ratelimit":
		return ff.EnableAdvancedRateLimiting
	case "retry_with_backoff", "retry":
		return ff.EnableRetryWithBackoff
	case "parallel_chunk_downloads", "parallel_chunks":
		return ff.EnableParallelChunkDownloads
	case "detailed_progress", "progress":
		return ff.EnableDetailedProgress
	case "metrics_export", "metrics":
		return ff.EnableMetricsExport
	case "alerting", "alerts":
		return ff.EnableAlerting
	case "sentry_errors", "sentry":
		return ff.EnableSentryErrors
	case "websocket_compression", "ws_compression":
		return ff.EnableWebSocketCompression
	case "streaming_response", "streaming":
		return ff.EnableStreamingResponse
	case "metrics_endpoint":
		return ff.EnableMetricsEndpoint
	default:
		return false
	}
}

// ListEnabledFeatures returns a list of all currently enabled feature flags.
// Useful for debugging and logging purposes.
func (ff *FeatureFlags) ListEnabledFeatures() []string {
	var enabled []string

	if ff.EnableVerboseLogging {
		enabled = append(enabled, "verbose_logging")
	}
	if ff.EnableExperimentalFeatures {
		enabled = append(enabled, "experimental_features")
	}
	if ff.EnableAdvancedRateLimiting {
		enabled = append(enabled, "advanced_rate_limiting")
	}
	if ff.EnableRetryWithBackoff {
		enabled = append(enabled, "retry_with_backoff")
	}
	if ff.EnableParallelChunkDownloads {
		enabled = append(enabled, "parallel_chunk_downloads")
	}
	if ff.EnableDetailedProgress {
		enabled = append(enabled, "detailed_progress")
	}
	if ff.EnableMetricsExport {
		enabled = append(enabled, "metrics_export")
	}
	if ff.EnableAlerting {
		enabled = append(enabled, "alerting")
	}
	if ff.EnableSentryErrors {
		enabled = append(enabled, "sentry_errors")
	}
	if ff.EnableWebSocketCompression {
		enabled = append(enabled, "websocket_compression")
	}
	if ff.EnableStreamingResponse {
		enabled = append(enabled, "streaming_response")
	}
	if ff.EnableMetricsEndpoint {
		enabled = append(enabled, "metrics_endpoint")
	}

	return enabled
}
