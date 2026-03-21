// Package sentryhook provides Sentry integration for error tracking.
// This package configures Sentry SDK for automatic error reporting with context.
package sentryhook

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
)

// Config holds Sentry configuration
type Config struct {
	DSN         string
	Environment string
	Release     string
	Debug       bool
}

// Init initializes Sentry SDK with configuration from environment
func Init() error {
	dsn := os.Getenv("SENTRY_DSN")
	if dsn == "" {
		// Sentry not configured, skip initialization
		return nil
	}

	environment := os.Getenv("SENTRY_ENVIRONMENT")
	if environment == "" {
		environment = "development"
	}

	release := os.Getenv("SENTRY_RELEASE")
	if release == "" {
		release = "impartus-cli@1.0.0"
	}

	debug := os.Getenv("SENTRY_DEBUG") == "true"

	err := sentry.Init(sentry.ClientOptions{
		Dsn:              dsn,
		Environment:      environment,
		Release:          release,
		Debug:            debug,
		AttachStacktrace: true,
		EnableTracing:    false, // Disable tracing for now, just error reporting
		BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			// Add custom tags
			event.Tags["go_version"] = runtime.Version()
			event.Tags["os"] = runtime.GOOS
			event.Tags["arch"] = runtime.GOARCH
			return event
		},
	})

	if err != nil {
		return fmt.Errorf("failed to initialize Sentry: %w", err)
	}

	return nil
}

// Flush sends any buffered events to Sentry
func Flush(timeout time.Duration) {
	sentry.Flush(timeout)
}

// SetUser sets the user context for subsequent events
func SetUser(userID, email string) {
	sentry.ConfigureScope(func(scope *sentry.Scope) {
		scope.SetUser(sentry.User{
			ID:    userID,
			Email: email,
		})
	})
}

// SetRequestID sets the request ID for correlation
func SetRequestID(requestID string) {
	sentry.ConfigureScope(func(scope *sentry.Scope) {
		scope.SetTag("request_id", requestID)
	})
}

// SetTag sets a custom tag
func SetTag(key, value string) {
	sentry.ConfigureScope(func(scope *sentry.Scope) {
		scope.SetTag(key, value)
	})
}

// SetContext sets additional context data
func SetContext(name string, data map[string]interface{}) {
	sentry.ConfigureScope(func(scope *sentry.Scope) {
		scope.SetContext(name, data)
	})
}

// CaptureError reports an error to Sentry
func CaptureError(err error) *sentry.EventID {
	if !IsEnabled() {
		return nil
	}
	return sentry.CaptureException(err)
}

// CaptureErrorWithContext reports an error with additional context
func CaptureErrorWithContext(err error, tags map[string]string, contextData map[string]interface{}) *sentry.EventID {
	if !IsEnabled() {
		return nil
	}

	sentry.WithScope(func(scope *sentry.Scope) {
		for k, v := range tags {
			scope.SetTag(k, v)
		}
		scope.SetContext("additional", contextData)
		sentry.CaptureException(err)
	})

	return nil
}

// CaptureMessage reports a message to Sentry
func CaptureMessage(message string) *sentry.EventID {
	if !IsEnabled() {
		return nil
	}
	return sentry.CaptureMessage(message)
}

// IsEnabled checks if Sentry is configured and enabled
func IsEnabled() bool {
	return sentry.CurrentHub().Client() != nil
}

// WithRecovery wraps a function with Sentry error recovery
func WithRecovery(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			var err error
			switch v := r.(type) {
			case error:
				err = v
			case string:
				err = fmt.Errorf("%s", v)
			default:
				err = fmt.Errorf("%v", v)
			}
			sentry.CurrentHub().Recover(err)
			sentry.Flush(time.Second * 5)
		}
	}()
	fn()
}

// Middleware returns an HTTP middleware that captures panics and reports to Sentry
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get request ID from header or context
		requestID := r.Header.Get("X-Request-ID")

		sentry.WithScope(func(scope *sentry.Scope) {
			scope.SetTag("request_id", requestID)
			scope.SetTag("http_method", r.Method)
			scope.SetTag("http_path", r.URL.Path)
			scope.SetContext("request", map[string]interface{}{
				"method":  r.Method,
				"path":    r.URL.Path,
				"query":   r.URL.Query().Encode(),
				"headers": sanitizeHeaders(r.Header),
			})

			// Set user context if available
			if userID := r.Header.Get("X-User-ID"); userID != "" {
				scope.SetUser(sentry.User{ID: userID})
			}
		})

		// Use sentry http handler for panic recovery
		hub := sentry.GetHubFromContext(r.Context())
		if hub == nil {
			hub = sentry.CurrentHub().Clone()
		}

		defer func() {
			if r := recover(); r != nil {
				var err error
				switch v := r.(type) {
				case error:
					err = v
				case string:
					err = fmt.Errorf("%s", v)
				default:
					err = fmt.Errorf("%v", v)
				}
				hub.CaptureException(err)
				sentry.Flush(time.Second * 5)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()

		next.ServeHTTP(w, r)
	})
}

// sanitizeHeaders removes sensitive headers from logging
func sanitizeHeaders(headers http.Header) map[string]string {
	result := make(map[string]string)
	sensitive := map[string]bool{
		"authorization": true,
		"cookie":        true,
		"set-cookie":    true,
		"x-api-key":     true,
	}

	for k, v := range headers {
		lowerKey := strings.ToLower(k)
		if sensitive[lowerKey] {
			result[k] = "***REDACTED***"
		} else if len(v) > 0 {
			result[k] = v[0]
		}
	}
	return result
}

// ContextWithSentry returns a context with Sentry hub attached
func ContextWithSentry(ctx context.Context) context.Context {
	hub := sentry.CurrentHub().Clone()
	return sentry.SetHubOnContext(ctx, hub)
}
