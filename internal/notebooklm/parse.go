package notebooklm

import (
	"context"
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

type sourceInventory struct {
	Sources []UploadResult
	Count   int
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
	if result.SourceID == "" {
		return UploadResult{}, fmt.Errorf("response did not include a source id")
	}
	return result, nil
}

func parseSourceCount(stdout string) (int, error) {
	inventory, err := parseSourceInventory(stdout, "")
	if err != nil {
		return 0, err
	}
	return inventory.Count, nil
}

func parseSourceInventory(stdout, notebookID string) (sourceInventory, error) {
	trimmed := strings.TrimSpace(stdout)
	if trimmed == "" {
		return sourceInventory{}, fmt.Errorf("empty source list response")
	}
	var payload any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return sourceInventory{}, err
	}

	items, count, ok := sourceItems(payload)
	if !ok {
		return sourceInventory{}, fmt.Errorf("unrecognized source list JSON")
	}
	inventory := sourceInventory{Count: count}
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if nested, nestedOK := entry["source"].(map[string]any); nestedOK {
			entry = nested
		}
		inventory.Sources = append(inventory.Sources, UploadResult{
			SourceID:   stringField(entry, "source_id", "sourceId", "id"),
			Title:      stringField(entry, "title", "name", "display_name", "displayName"),
			NotebookID: notebookID,
		})
	}
	return inventory, nil
}

func sourceItems(payload any) ([]any, int, bool) {
	switch typed := payload.(type) {
	case []any:
		return typed, len(typed), true
	case map[string]any:
		for _, key := range []string{"sources", "items", "data"} {
			value, exists := typed[key]
			if !exists {
				continue
			}
			if items, count, ok := sourceItems(value); ok {
				return items, count, true
			}
		}
		if n, ok := typed["count"].(float64); ok {
			return nil, int(n), true
		}
	}
	return nil, 0, false
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
	detail := firstNonEmpty(secrets.Scrub(stderr), secrets.Scrub(stdout), secrets.ScrubError(err))
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{Kind: ErrTransient, Message: trimForError(detail), Err: err}
	}
	lower := strings.ToLower(detail)
	switch {
	case containsAny(lower,
		"authentication", "re-authenticate", "auth status", "unauthenticated",
		"unauthorized", "not authenticated", "invalid credential", "http 401",
	):
		return &Error{Kind: ErrAuth, Message: trimForError(detail), Err: err}
	case containsAny(lower, "rate limit", "rate-limit", "rate_limit", "429", "quota", "throttl"):
		return &Error{Kind: ErrRateLimit, Message: trimForError(detail), Err: err}
	case containsAny(lower, "timeout", "temporar", "connection reset"):
		return &Error{Kind: ErrTransient, Message: trimForError(detail), Err: err}
	default:
		return &Error{Kind: ErrPermanent, Message: trimForError(detail), Err: err}
	}
}

func containsAny(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if strings.Contains(value, fragment) {
			return true
		}
	}
	return false
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
	// ErrAmbiguous means an add may have succeeded remotely and must be
	// reconciled before another write is attempted.
	ErrAmbiguous
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

// IsAmbiguous reports whether an add may have succeeded remotely and must be
// reconciled without issuing another write.
func IsAmbiguous(err error) bool {
	var nlmErr *Error
	return errors.As(err, &nlmErr) && nlmErr.Kind == ErrAmbiguous
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
