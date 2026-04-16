package alerts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAlertStruct(t *testing.T) {
	alert := Alert{
		Severity:    SeverityCritical,
		Title:       "Test Alert",
		Message:     "This is a test message",
		Source:      "test",
		Environment: "test",
	}

	if alert.Severity != SeverityCritical {
		t.Errorf("expected SeverityCritical, got %v", alert.Severity)
	}
	if alert.Title != "Test Alert" {
		t.Errorf("expected 'Test Alert', got %v", alert.Title)
	}
	if alert.Message != "This is a test message" {
		t.Errorf("expected 'This is a test message', got %v", alert.Message)
	}
	if alert.Source != "test" {
		t.Errorf("expected 'test', got %v", alert.Source)
	}
	if alert.Environment != "test" {
		t.Errorf("expected 'test', got %v", alert.Environment)
	}
}

func TestSeverityConstants(t *testing.T) {
	if SeverityInfo != "info" {
		t.Errorf("expected SeverityInfo to be 'info', got %v", SeverityInfo)
	}
	if SeverityWarning != "warning" {
		t.Errorf("expected SeverityWarning to be 'warning', got %v", SeverityWarning)
	}
	if SeverityCritical != "critical" {
		t.Errorf("expected SeverityCritical to be 'critical', got %v", SeverityCritical)
	}
}

func TestConfig(t *testing.T) {
	cfg := Config{
		WebhookURL:  "https://example.com/webhook",
		Enabled:     true,
		Environment: "test",
	}

	if cfg.WebhookURL != "https://example.com/webhook" {
		t.Errorf("expected 'https://example.com/webhook', got %v", cfg.WebhookURL)
	}
	if !cfg.Enabled {
		t.Error("expected Enabled to be true")
	}
	if cfg.Environment != "test" {
		t.Errorf("expected Environment to be 'test', got %v", cfg.Environment)
	}
}

func TestAlerterStruct(t *testing.T) {
	alerter := &Alerter{
		config: Config{
			WebhookURL:  "https://example.com/webhook",
			Enabled:     true,
			Environment: "test",
		},
		lastAlert: make(map[string]time.Time),
	}

	if alerter.config.WebhookURL != "https://example.com/webhook" {
		t.Errorf("unexpected WebhookURL: %v", alerter.config.WebhookURL)
	}
	if alerter.config.Enabled != true {
		t.Error("expected Enabled to be true")
	}
	if alerter.lastAlert == nil {
		t.Error("expected lastAlert to be initialized")
	}
}

func TestSend_Disabled(t *testing.T) {
	alerter := &Alerter{
		config:    Config{Enabled: false, Environment: "test"},
		lastAlert: make(map[string]time.Time),
	}
	err := alerter.Send(context.Background(), Alert{
		Severity: SeverityInfo,
		Title:    "test",
		Message:  "disabled test",
	})
	if err != nil {
		t.Fatalf("disabled Send should not error: %v", err)
	}
}

func TestSend_GenericWebhook(t *testing.T) {
	var receivedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		receivedBody = string(buf[:n])
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	alerter := &Alerter{
		config:    Config{WebhookURL: server.URL, Enabled: true, Environment: "test"},
		client:    server.Client(),
		lastAlert: make(map[string]time.Time),
	}
	err := alerter.Send(context.Background(), Alert{
		Severity: SeverityWarning,
		Title:    "webhook test",
		Message:  "test message",
	})
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if receivedBody == "" {
		t.Fatal("expected webhook to receive body")
	}
}

func TestSend_SlackWebhook(t *testing.T) {
	var receivedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		receivedBody = string(buf[:n])
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	alerter := &Alerter{
		config:    Config{WebhookURL: "https://hooks.slack.com/services/xxx", Enabled: true, Environment: "test"},
		client:    server.Client(),
		lastAlert: make(map[string]time.Time),
	}
	// Override URL after formatPayload picks slack format
	alerter.config.WebhookURL = server.URL

	err := alerter.Send(context.Background(), Alert{
		Severity: SeverityCritical,
		Title:    "slack test",
		Message:  "slack message",
	})
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if receivedBody == "" {
		t.Fatal("expected slack webhook to receive body")
	}
}

func TestSend_RateLimiting(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	alerter := &Alerter{
		config:    Config{WebhookURL: server.URL, Enabled: true, Environment: "test", RateLimit: time.Hour},
		client:    server.Client(),
		lastAlert: make(map[string]time.Time),
	}

	alert := Alert{Severity: SeverityInfo, Title: "rate-test", Message: "msg"}
	_ = alerter.Send(context.Background(), alert)
	_ = alerter.Send(context.Background(), alert)
	_ = alerter.Send(context.Background(), alert)

	if callCount != 1 {
		t.Fatalf("expected 1 webhook call (rate limited), got %d", callCount)
	}
}

func TestSend_WebhookError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	alerter := &Alerter{
		config:    Config{WebhookURL: server.URL, Enabled: true, Environment: "test"},
		client:    server.Client(),
		lastAlert: make(map[string]time.Time),
	}
	err := alerter.Send(context.Background(), Alert{
		Severity: SeverityInfo,
		Title:    "error test",
		Message:  "should fail",
	})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestInitAndGet(t *testing.T) {
	Reset()
	err := Init()
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	a := Get()
	if a == nil {
		t.Fatal("expected non-nil alerter after Init")
	}
}

func TestSendAlertHelpersDisabled(t *testing.T) {
	// Create an alerter with alerts disabled (no webhook)
	alerter := &Alerter{
		config:    Config{Enabled: false, Environment: "test"},
		client:    &http.Client{Timeout: time.Second},
		lastAlert: make(map[string]time.Time),
	}

	err := alerter.Send(context.Background(), Alert{Severity: SeverityInfo, Title: "info", Message: "msg"})
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
}
