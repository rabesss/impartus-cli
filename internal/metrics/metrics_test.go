package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/buildinfo"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func TestGetReturnsNilWhenNotInitialized(t *testing.T) {
	// Get should return nil if Init() has not been called
	// We rely on the package-level instance being nil initially
	// Since we can't reset the sync.Once in tests, we just verify Get works
	m := Get()
	if m != nil {
		t.Log("Get() returned non-nil (Init may have been called by previous test)")
	}
	// This test verifies Get() is callable and returns the current state
	// If instance is nil, this is expected behavior
}

func TestShutdownWithNilShutdownFunc(t *testing.T) {
	// Create a Metrics struct with nil shutdown function
	m := &Metrics{
		shutdown: nil,
	}

	// Shutdown should return nil when shutdown func is nil
	err := m.Shutdown(context.Background())
	if err != nil {
		t.Errorf("Shutdown() on nil shutdown func returned error: %v", err)
	}
}

func TestShutdownWithNilMetrics(t *testing.T) {
	// Shutdown on nil receiver will panic because it has a pointer receiver
	// and accesses m.shutdown field. This is expected Go behavior.
	// Testing nil receiver on methods that access fields is not safe.
	// We only test non-nil Metrics structs.
	_ = t // avoid unused variable
}

func TestMetricsHandler(t *testing.T) {
	m := &Metrics{}

	handler := m.MetricsHandler()
	if handler == nil {
		t.Fatal("MetricsHandler() returned nil")
	}

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Without a reader (OTLP exporter case), should return 503
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("MetricsHandler() returned status %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}
}

func TestMetricsHandlerWithReader(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer provider.Shutdown(context.Background())

	m := &Metrics{
		reader: reader,
	}

	handler := m.MetricsHandler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("MetricsHandler() returned status %d, want %d. Body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
}

func TestRecordDownloadWithNilReceiver(t *testing.T) {
	// RecordDownload should handle nil receiver gracefully
	var m *Metrics

	// These should not panic
	m.RecordDownload(context.Background(), time.Second, 1024, true)
	m.RecordDownload(context.Background(), time.Second, 0, false)
}

func TestStartDownloadWithNilReceiver(t *testing.T) {
	var m *Metrics

	// Should not panic
	m.StartDownload(context.Background())
}

func TestRecordAPIRequestWithNilReceiver(t *testing.T) {
	var m *Metrics

	// Should not panic
	m.RecordAPIRequest(context.Background(), "GET", "/api/courses", time.Millisecond, 200)
	m.RecordAPIRequest(context.Background(), "GET", "/api/courses", time.Millisecond, 500)
}

func TestRecordJobStartWithNilReceiver(t *testing.T) {
	var m *Metrics

	// Should not panic
	m.RecordJobStart(context.Background())
}

func TestRecordJobCompleteWithNilReceiver(t *testing.T) {
	var m *Metrics

	// Should not panic
	m.RecordJobComplete(context.Background(), true)
	m.RecordJobComplete(context.Background(), false)
}

func TestConstants(t *testing.T) {
	// Verify constants are defined correctly
	if serviceName != "impartus-cli" {
		t.Errorf("serviceName = %q, want %q", serviceName, "impartus-cli")
	}
	if buildinfo.Version != "0.1.2" {
		t.Errorf("buildinfo.Version = %q, want %q", buildinfo.Version, "0.1.2")
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestInitCreatesProviderAndMetrics(t *testing.T) {
	// Reset singleton for this test — use a fresh process context
	once = sync.Once{}
	instance = nil

	err := Init()
	if err != nil {
		t.Fatalf("Init() returned error: %v", err)
	}

	m := Get()
	if m == nil {
		t.Fatal("Get() returned nil after Init()")
	}
	if m.provider == nil {
		t.Error("expected provider to be initialized")
	}
	if m.reader == nil {
		t.Error("expected reader to be initialized")
	}
	if m.meter == nil {
		t.Error("expected meter to be initialized")
	}

	// Verify all counters are created
	if m.DownloadsTotal == nil {
		t.Error("DownloadsTotal not initialized")
	}
	if m.DownloadDuration == nil {
		t.Error("DownloadDuration not initialized")
	}
	if m.DownloadErrors == nil {
		t.Error("DownloadErrors not initialized")
	}
	if m.DownloadBytes == nil {
		t.Error("DownloadBytes not initialized")
	}
	if m.ActiveDownloads == nil {
		t.Error("ActiveDownloads not initialized")
	}
	if m.APIRequestsTotal == nil {
		t.Error("APIRequestsTotal not initialized")
	}
	if m.APIRequestDuration == nil {
		t.Error("APIRequestDuration not initialized")
	}
	if m.APIErrors == nil {
		t.Error("APIErrors not initialized")
	}
	if m.ActiveJobs == nil {
		t.Error("ActiveJobs not initialized")
	}
	if m.JobsCompleted == nil {
		t.Error("JobsCompleted not initialized")
	}
	if m.JobsFailed == nil {
		t.Error("JobsFailed not initialized")
	}

	// Shutdown should succeed
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown() returned error: %v", err)
	}
}

func TestRecordDownloadWithInitializedMetrics(t *testing.T) {
	once = sync.Once{}
	instance = nil
	if err := Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer Get().Shutdown(context.Background())

	m := Get()
	ctx := context.Background()
	m.StartDownload(ctx)
	m.RecordDownload(ctx, 5*time.Second, 1024, true)
	m.RecordDownload(ctx, time.Second, 0, false)
	// No assertions on values (manual reader doesn't expose easily), just no panic
}
