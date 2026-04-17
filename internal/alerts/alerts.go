// Package alerts provides configurable alerting for the Impartus CLI.
// Supports webhook-based alerts to Slack, PagerDuty, or custom endpoints.
package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Severity levels for alerts
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// Alert represents an alert to be sent
type Alert struct {
	Timestamp   time.Time              `json:"timestamp"`
	Severity    Severity               `json:"severity"`
	Title       string                 `json:"title"`
	Message     string                 `json:"message"`
	Source      string                 `json:"source"`
	RequestID   string                 `json:"request_id,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	Environment string                 `json:"environment"`
}

// Config holds alerting configuration
type Config struct {
	WebhookURL  string
	Enabled     bool
	Environment string
	RateLimit   time.Duration // Minimum time between alerts of same type
}

// Alerter handles sending alerts
type Alerter struct {
	config    Config
	client    *http.Client
	lastAlert map[string]time.Time
	mu        sync.Mutex
}

var (
	defaultAlerter *Alerter
	once           sync.Once
)

// initAlerter initializes the default alerter from environment (internal, runs once)
func initAlerter() {
	once.Do(func() {
		webhookURL := os.Getenv("ALERT_WEBHOOK_URL")
		enabled := webhookURL != "" && os.Getenv("ALERT_ON_ERRORS") == "true"
		environment := os.Getenv("SENTRY_ENVIRONMENT")
		if environment == "" {
			environment = "development"
		}

		defaultAlerter = &Alerter{
			config: Config{
				WebhookURL:  webhookURL,
				Enabled:     enabled,
				Environment: environment,
				RateLimit:   time.Minute, // Don't spam alerts
			},
			client:    &http.Client{Timeout: 10 * time.Second},
			lastAlert: make(map[string]time.Time),
		}
	})
}

// Init initializes the default alerter from environment
func Init() error {
	initAlerter() // Initialization is idempotent due to sync.Once
	return nil    // Init always succeeds since it only sets up from env vars
}

// Get returns the default alerter
func Get() *Alerter {
	if defaultAlerter == nil {
		initAlerter()
	}
	return defaultAlerter
}

// Send sends an alert through the configured webhook
func (a *Alerter) Send(ctx context.Context, alert Alert) error {
	if !a.config.Enabled {
		log.Printf("[ALERT] %s: %s - %s", alert.Severity, alert.Title, alert.Message)
		return nil
	}

	// Rate limiting
	alertKey := fmt.Sprintf("%s:%s", alert.Severity, alert.Title)
	a.mu.Lock()
	if last, ok := a.lastAlert[alertKey]; ok && time.Since(last) < a.config.RateLimit {
		a.mu.Unlock()
		return nil // Skip, rate limited
	}
	a.lastAlert[alertKey] = time.Now()
	a.mu.Unlock()

	// Set defaults
	if alert.Timestamp.IsZero() {
		alert.Timestamp = time.Now()
	}
	if alert.Environment == "" {
		alert.Environment = a.config.Environment
	}
	if alert.Source == "" {
		alert.Source = "impartus-cli"
	}

	// Detect webhook type and format accordingly
	payload := a.formatPayload(alert)

	return a.sendWebhook(ctx, payload)
}

func (a *Alerter) formatPayload(alert Alert) any {
	// Check if it's a Slack webhook (contains "slack.com")
	if len(a.config.WebhookURL) > 0 && strings.Contains(a.config.WebhookURL, "slack.com") {
		return a.formatSlack(alert)
	}
	// Check if it's PagerDuty
	if len(a.config.WebhookURL) > 0 && strings.Contains(a.config.WebhookURL, "pagerduty.com") {
		return a.formatPagerDuty(alert)
	}
	// Generic JSON payload
	return alert
}

func (a *Alerter) formatSlack(alert Alert) map[string]any {
	color := "#36a64f" // green for info
	if alert.Severity == SeverityWarning {
		color = "#ff9900" // orange
	} else if alert.Severity == SeverityCritical {
		color = "#ff0000" // red
	}

	return map[string]any{
		"attachments": []map[string]any{
			{
				"color":     color,
				"title":     alert.Title,
				"text":      alert.Message,
				"timestamp": alert.Timestamp.Unix(),
				"fields": []map[string]any{
					{"title": "Severity", "value": string(alert.Severity), "short": true},
					{"title": "Environment", "value": alert.Environment, "short": true},
					{"title": "Source", "value": alert.Source, "short": true},
				},
			},
		},
	}
}

func (a *Alerter) formatPagerDuty(alert Alert) map[string]any {
	return map[string]any{
		"routing_key":  os.Getenv("PAGERDUTY_ROUTING_KEY"),
		"event_action": "trigger",
		"dedup_key":    alert.Title,
		"payload": map[string]any{
			"summary":   fmt.Sprintf("%s: %s", alert.Severity, alert.Title),
			"severity":  string(alert.Severity),
			"source":    alert.Source,
			"timestamp": alert.Timestamp.Format(time.RFC3339),
			"details":   alert.Metadata,
		},
	}
}

func (a *Alerter) sendWebhook(ctx context.Context, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal alert payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.config.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create webhook request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}

// Reset clears the default alerter and resets the init guard,
// allowing it to be reinitialized from current environment.
// This is useful for tests and alternate boot flows that need to pick up new environment
// variables without being coupled to first-call order.
func Reset() {
	defaultAlerter = nil
	once = sync.Once{}
}

// SendAlert sends an alert using the default alerter
func SendAlert(ctx context.Context, severity Severity, title, message string, metadata map[string]any) error {
	return Get().Send(ctx, Alert{
		Severity: severity,
		Title:    title,
		Message:  message,
		Metadata: metadata,
	})
}

// SendInfo sends an info-level alert
func SendInfo(ctx context.Context, title, message string) error {
	return SendAlert(ctx, SeverityInfo, title, message, nil)
}

// SendWarning sends a warning-level alert
func SendWarning(ctx context.Context, title, message string) error {
	return SendAlert(ctx, SeverityWarning, title, message, nil)
}

// SendCritical sends a critical-level alert
func SendCritical(ctx context.Context, title, message string) error {
	return SendAlert(ctx, SeverityCritical, title, message, nil)
}

// SendAlertWithRequestID sends an alert with request context
func SendAlertWithRequestID(ctx context.Context, severity Severity, title, message, requestID string) error {
	return Get().Send(ctx, Alert{
		Severity:  severity,
		Title:     title,
		Message:   message,
		RequestID: requestID,
	})
}
