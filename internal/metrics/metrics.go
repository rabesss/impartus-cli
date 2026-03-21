// Package metrics provides OpenTelemetry-based metrics instrumentation for the Impartus CLI.
// This package implements counters, gauges, and histograms for monitoring application performance.
package metrics

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

const (
	serviceName    = "impartus-cli"
	serviceVersion = "1.0.0"
)

// Metrics holds all application metrics
type Metrics struct {
	// Download metrics
	DownloadsTotal   metric.Int64Counter
	DownloadDuration metric.Float64Histogram
	DownloadErrors   metric.Int64Counter
	DownloadBytes    metric.Int64Counter
	ActiveDownloads  metric.Int64UpDownCounter

	// API metrics
	APIRequestsTotal   metric.Int64Counter
	APIRequestDuration metric.Float64Histogram
	APIErrors          metric.Int64Counter

	// Job metrics
	ActiveJobs    metric.Int64UpDownCounter
	JobsCompleted metric.Int64Counter
	JobsFailed    metric.Int64Counter

	// Provider and meter
	provider *sdkmetric.MeterProvider
	meter    metric.Meter
	shutdown func(context.Context) error
}

var (
	instance *Metrics
	once     sync.Once
)

// Init initializes the metrics system with OpenTelemetry
func Init() error {
	var initErr error
	once.Do(func() {
		instance, initErr = initMetrics(context.Background())
	})
	return initErr
}

// Get returns the global metrics instance
func Get() *Metrics {
	return instance
}

// Shutdown gracefully shuts down the metrics provider
func (m *Metrics) Shutdown(ctx context.Context) error {
	if m.shutdown != nil {
		return m.shutdown(ctx)
	}
	return nil
}

func initMetrics(ctx context.Context) (*Metrics, error) {
	m := &Metrics{}

	// Create resource with service info
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(serviceVersion),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Check for OTLP endpoint
	otlpEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")

	var opts []sdkmetric.Option
	opts = append(opts, sdkmetric.WithResource(res))

	if otlpEndpoint != "" {
		// Use OTLP exporter for production
		exporter, err := otlpmetrichttp.New(ctx,
			otlpmetrichttp.WithEndpoint(otlpEndpoint),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
		}
		opts = append(opts, sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter)))
	} else {
		// Use manual reader for development (metrics accessible via /metrics endpoint)
		reader := sdkmetric.NewManualReader()
		opts = append(opts, sdkmetric.WithReader(reader))
	}

	m.provider = sdkmetric.NewMeterProvider(opts...)
	m.shutdown = m.provider.Shutdown
	otel.SetMeterProvider(m.provider)

	meter := m.provider.Meter(serviceName)
	m.meter = meter

	// Initialize counters and histograms
	if err := m.initDownloadMetrics(meter); err != nil {
		return nil, err
	}
	if err := m.initAPIMetrics(meter); err != nil {
		return nil, err
	}
	if err := m.initJobMetrics(meter); err != nil {
		return nil, err
	}

	return m, nil
}

func (m *Metrics) initDownloadMetrics(meter metric.Meter) error {
	var err error

	m.DownloadsTotal, err = meter.Int64Counter(
		"impartus_downloads_total",
		metric.WithDescription("Total number of downloads"),
		metric.WithUnit("{download}"),
	)
	if err != nil {
		return fmt.Errorf("failed to create downloads_total counter: %w", err)
	}

	m.DownloadDuration, err = meter.Float64Histogram(
		"impartus_download_duration_seconds",
		metric.WithDescription("Download duration in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(1, 5, 10, 30, 60, 120, 300, 600, 1200),
	)
	if err != nil {
		return fmt.Errorf("failed to create download_duration histogram: %w", err)
	}

	m.DownloadErrors, err = meter.Int64Counter(
		"impartus_download_errors_total",
		metric.WithDescription("Total number of download errors"),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		return fmt.Errorf("failed to create download_errors counter: %w", err)
	}

	m.DownloadBytes, err = meter.Int64Counter(
		"impartus_download_bytes_total",
		metric.WithDescription("Total bytes downloaded"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return fmt.Errorf("failed to create download_bytes counter: %w", err)
	}

	m.ActiveDownloads, err = meter.Int64UpDownCounter(
		"impartus_active_downloads",
		metric.WithDescription("Number of currently active downloads"),
		metric.WithUnit("{download}"),
	)
	if err != nil {
		return fmt.Errorf("failed to create active_downloads counter: %w", err)
	}

	return nil
}

func (m *Metrics) initAPIMetrics(meter metric.Meter) error {
	var err error

	m.APIRequestsTotal, err = meter.Int64Counter(
		"impartus_api_requests_total",
		metric.WithDescription("Total number of API requests"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return fmt.Errorf("failed to create api_requests_total counter: %w", err)
	}

	m.APIRequestDuration, err = meter.Float64Histogram(
		"impartus_api_request_duration_seconds",
		metric.WithDescription("API request duration in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10),
	)
	if err != nil {
		return fmt.Errorf("failed to create api_request_duration histogram: %w", err)
	}

	m.APIErrors, err = meter.Int64Counter(
		"impartus_api_errors_total",
		metric.WithDescription("Total number of API errors"),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		return fmt.Errorf("failed to create api_errors counter: %w", err)
	}

	return nil
}

func (m *Metrics) initJobMetrics(meter metric.Meter) error {
	var err error

	m.ActiveJobs, err = meter.Int64UpDownCounter(
		"impartus_active_jobs",
		metric.WithDescription("Number of currently active jobs"),
		metric.WithUnit("{job}"),
	)
	if err != nil {
		return fmt.Errorf("failed to create active_jobs counter: %w", err)
	}

	m.JobsCompleted, err = meter.Int64Counter(
		"impartus_jobs_completed_total",
		metric.WithDescription("Total number of completed jobs"),
		metric.WithUnit("{job}"),
	)
	if err != nil {
		return fmt.Errorf("failed to create jobs_completed counter: %w", err)
	}

	m.JobsFailed, err = meter.Int64Counter(
		"impartus_jobs_failed_total",
		metric.WithDescription("Total number of failed jobs"),
		metric.WithUnit("{job}"),
	)
	if err != nil {
		return fmt.Errorf("failed to create jobs_failed counter: %w", err)
	}

	return nil
}

// RecordDownload records a completed download
func (m *Metrics) RecordDownload(ctx context.Context, duration time.Duration, bytes int64, success bool) {
	if m == nil {
		return
	}

	attr := metric.WithAttributes()
	if success {
		m.DownloadsTotal.Add(ctx, 1, attr)
		m.DownloadBytes.Add(ctx, bytes, attr)
	} else {
		m.DownloadErrors.Add(ctx, 1, attr)
	}
	m.DownloadDuration.Record(ctx, duration.Seconds(), attr)
	m.ActiveDownloads.Add(ctx, -1, attr)
}

// StartDownload increments active download counter
func (m *Metrics) StartDownload(ctx context.Context) {
	if m == nil {
		return
	}
	m.ActiveDownloads.Add(ctx, 1)
}

// RecordAPIRequest records an API request
func (m *Metrics) RecordAPIRequest(ctx context.Context, method, path string, duration time.Duration, statusCode int) {
	if m == nil {
		return
	}

	attr := metric.WithAttributes(
	// Method and path as string attributes
	)

	m.APIRequestsTotal.Add(ctx, 1, attr)
	m.APIRequestDuration.Record(ctx, duration.Seconds(), attr)

	if statusCode >= 400 {
		m.APIErrors.Add(ctx, 1, attr)
	}
}

// RecordJobStart increments active job counter
func (m *Metrics) RecordJobStart(ctx context.Context) {
	if m == nil {
		return
	}
	m.ActiveJobs.Add(ctx, 1)
}

// RecordJobComplete records job completion
func (m *Metrics) RecordJobComplete(ctx context.Context, success bool) {
	if m == nil {
		return
	}

	m.ActiveJobs.Add(ctx, -1)
	if success {
		m.JobsCompleted.Add(ctx, 1)
	} else {
		m.JobsFailed.Add(ctx, 1)
	}
}

// MetricsHandler returns an HTTP handler for exposing metrics
func (m *Metrics) MetricsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// In production, this would use promhttp.Handler()
		// For now, return a simple status
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "# impartus-cli metrics\n")
		fmt.Fprintf(w, "# Use OTEL_EXPORTER_OTLP_ENDPOINT to export to OpenTelemetry collector\n")
	}
}
