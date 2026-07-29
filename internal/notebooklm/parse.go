package notebooklm

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/rabesss/impartus-cli/internal/secrets"
)

// UploadResult is the successful outcome of adding a source.
type UploadResult struct {
	SourceID   string `json:"sourceId,omitempty"`
	Title      string `json:"title,omitempty"`
	NotebookID string `json:"notebookId,omitempty"`
	Raw        string `json:"-"`
}

func parseAuthStatus(stdout string) (string, error) {
	var payload struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &payload); err != nil {
		return "", err
	}
	return payload.Status, nil
}

func parseUploadResult(stdout, notebookID string) (UploadResult, error) {
	trimmed := strings.TrimSpace(stdout)
	if trimmed == "" {
		return UploadResult{}, fmt.Errorf("empty response")
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return UploadResult{}, err
	}

	result := UploadResult{NotebookID: notebookID}
	result.SourceID = stringField(payload, "source_id", "sourceId", "id")
	result.Title = stringField(payload, "title", "name")
	if nested, ok := payload["source"].(map[string]any); ok {
		if result.SourceID == "" {
			result.SourceID = stringField(nested, "source_id", "sourceId", "id")
		}
		if result.Title == "" {
			result.Title = stringField(nested, "title", "name")
		}
	}
	return result, nil
}

func parseSourceCount(stdout string) (int, error) {
	trimmed := strings.TrimSpace(stdout)
	if trimmed == "" {
		return 0, fmt.Errorf("empty source list response")
	}
	var asArray []any
	if err := json.Unmarshal([]byte(trimmed), &asArray); err == nil {
		return len(asArray), nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return 0, err
	}
	for _, key := range []string{"sources", "items", "data"} {
		if arr, ok := payload[key].([]any); ok {
			return len(arr), nil
		}
	}
	if n, ok := payload["count"].(float64); ok {
		return int(n), nil
	}
	return 0, fmt.Errorf("unrecognized source list JSON")
}

func stringField(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			if typed, ok := v.(string); ok && typed != "" {
				return typed
			}
		}
	}
	return ""
}

// ClassifyError turns a CLI failure into a typed error for retry decisions.
func ClassifyError(err error, stdout, stderr string) error {
	detail := firstNonEmpty(stderr, stdout, secrets.ScrubError(err))
	lower := strings.ToLower(detail)
	switch {
	case strings.Contains(lower, "authentication") || strings.Contains(lower, "re-authenticate") || strings.Contains(lower, "auth status") || strings.Contains(lower, "unauthenticated"):
		return &Error{Kind: ErrAuth, Message: trimForError(detail), Err: err}
	case strings.Contains(lower, "rate") || strings.Contains(lower, "429") || strings.Contains(lower, "quota"):
		return &Error{Kind: ErrRateLimit, Message: trimForError(detail), Err: err}
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "temporar") || strings.Contains(lower, "connection reset"):
		return &Error{Kind: ErrTransient, Message: trimForError(detail), Err: err}
	default:
		return &Error{Kind: ErrPermanent, Message: trimForError(detail), Err: err}
	}
}

// ErrorKind classifies NotebookLM failures for the watcher's retry policy.
type ErrorKind int

const (
	// ErrPermanent is a non-retryable NotebookLM failure (bad args, missing file, etc.).
	ErrPermanent ErrorKind = iota
	// ErrTransient is a temporary failure that may succeed on a later attempt.
	ErrTransient
	// ErrAuth indicates the NotebookLM session/credential is unusable.
	ErrAuth
	// ErrRateLimit indicates quota or HTTP 429 throttling; retryable with backoff.
	ErrRateLimit
)

// Error is a classified NotebookLM failure.
type Error struct {
	Kind    ErrorKind
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "notebooklm error"
}

func (e *Error) Unwrap() error { return e.Err }

// Retryable reports whether the watcher should retry the operation.
func (e *Error) Retryable() bool {
	return e != nil && (e.Kind == ErrTransient || e.Kind == ErrRateLimit)
}

// IsAuth reports whether err is an authentication failure.
func IsAuth(err error) bool {
	var nlmErr *Error
	return errors.As(err, &nlmErr) && nlmErr.Kind == ErrAuth
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func trimForError(s string) string {
	s = strings.TrimSpace(s)
	lines := strings.Split(s, "\n")
	if len(lines) > 8 {
		lines = lines[len(lines)-8:]
	}
	return strings.Join(lines, "\n")
}
