package notebooklm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/rabesss/impartus-cli/internal/secrets"
)

// UploadOutcome describes what happened at the provider boundary.
type UploadOutcome string

const (
	// UploadCreated means this call created a source.
	UploadCreated UploadOutcome = "created"
	// UploadFound means reconciliation found a previously created source.
	UploadFound UploadOutcome = "found"
	// UploadAmbiguous means an add may have succeeded and must be reconciled.
	UploadAmbiguous UploadOutcome = "ambiguous"
	// UploadRejected means the provider definitely rejected the add.
	UploadRejected UploadOutcome = "rejected"
)

// UploadResult is the typed outcome of adding or reconciling a source.
type UploadResult struct {
	Outcome    UploadOutcome `json:"outcome,omitempty"`
	SourceID   string        `json:"sourceId,omitempty"`
	Title      string        `json:"title,omitempty"`
	NotebookID string        `json:"notebookId,omitempty"`
	Status     string        `json:"-"`
	StatusID   int           `json:"-"`
}

type sourceInventory struct {
	Sources []UploadResult
	Count   int
}

// providerJSONValue is the single dynamic escape hatch for provider-owned JSON
// envelopes. Both supported CLIs have changed nesting and field aliases across
// releases, so decoding the small boundary here is safer than exposing an
// implicit map contract to the rest of the package.
type providerJSONValue = any
type providerJSONObject = map[string]providerJSONValue
type providerJSONArray = []providerJSONValue

type providerErrorEnvelope struct {
	Error   bool   `json:"error"`
	Code    string `json:"code"`
	Message string `json:"message"`
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

	var payload providerJSONObject
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return UploadResult{}, err
	}

	result := UploadResult{Outcome: UploadCreated, NotebookID: notebookID}
	result.SourceID = stringField(payload, "source_id", "sourceId", "id")
	result.Title = stringField(payload, "title", "name")
	if nested, ok := payload["source"].(providerJSONObject); ok {
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

func parseSourceInventory(stdout, notebookID string) (sourceInventory, error) {
	trimmed := strings.TrimSpace(stdout)
	if trimmed == "" {
		return sourceInventory{}, fmt.Errorf("empty source list response")
	}
	var payload providerJSONValue
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return sourceInventory{}, err
	}

	items, count, ok := sourceItems(payload)
	if !ok {
		return sourceInventory{}, fmt.Errorf("unrecognized source list JSON")
	}
	inventory := sourceInventory{Count: count}
	for _, item := range items {
		entry, ok := item.(providerJSONObject)
		if !ok {
			continue
		}
		inventory.Sources = append(inventory.Sources, sourceResult(entry, notebookID))
	}
	return inventory, nil
}

func sourceResult(entry providerJSONObject, notebookID string) UploadResult {
	var nested providerJSONObject
	if value, ok := entry["source"].(providerJSONObject); ok {
		nested = value
	}
	result := UploadResult{
		Outcome:    UploadFound,
		SourceID:   firstNonEmpty(stringField(nested, "source_id", "sourceId", "id"), stringField(entry, "source_id", "sourceId", "id")),
		Title:      firstNonEmpty(stringField(nested, "title", "name", "display_name", "displayName"), stringField(entry, "title", "name", "display_name", "displayName")),
		NotebookID: notebookID,
		Status:     normalizeSourceStatus(firstNonEmpty(stringField(nested, "status"), stringField(entry, "status"))),
		StatusID: firstNonZero(
			intField(nested, "status_id", "statusId", "status_code", "statusCode", "status"),
			intField(entry, "status_id", "statusId", "status_code", "statusCode", "status"),
		),
	}
	return result
}

func parseSourceWaitStatus(stdout string) (string, error) {
	trimmed := strings.TrimSpace(stdout)
	if trimmed == "" {
		return "", fmt.Errorf("empty source wait response")
	}
	var payload providerJSONObject
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return "", err
	}
	var nested providerJSONObject
	if value, ok := payload["source"].(providerJSONObject); ok {
		nested = value
	}
	status := normalizeSourceStatus(firstNonEmpty(stringField(nested, "status"), stringField(payload, "status")))
	if status == "" && firstNonZero(
		intField(nested, "status_code", "statusCode", "status_id", "statusId", "status"),
		intField(payload, "status_code", "statusCode", "status_id", "statusId", "status"),
	) == 2 {
		status = "ready"
	}
	if status == "" {
		return "", fmt.Errorf("source wait response did not include status")
	}
	return status, nil
}

func sourceItems(payload providerJSONValue) (providerJSONArray, int, bool) {
	switch typed := payload.(type) {
	case providerJSONArray:
		return typed, len(typed), true
	case providerJSONObject:
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

func stringField(m providerJSONObject, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			if typed, ok := v.(string); ok && typed != "" {
				return typed
			}
		}
	}
	return ""
}

func intField(m providerJSONObject, keys ...string) int {
	for _, key := range keys {
		value, ok := m[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case float64:
			return int(typed)
		case int:
			return typed
		case json.Number:
			if parsed, err := strconv.Atoi(typed.String()); err == nil {
				return parsed
			}
		case string:
			if parsed, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil {
				return parsed
			}
		}
	}
	return 0
}

func firstNonZero(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func normalizeSourceStatus(status string) string {
	return strings.ToLower(strings.TrimSpace(status))
}

// ClassifyError turns a notebooklm-py CLI failure into a typed error for retry
// decisions. Provider calls use classifyProviderError so optional nlm installs
// retain their legacy text contract.
func ClassifyError(err error, stdout, stderr string) error {
	return classifyProviderError(ProviderNotebookLMpy, err, stdout, stderr)
}

func classifyProviderError(provider Provider, err error, stdout, stderr string) error {
	if provider == ProviderNotebookLMpy {
		if envelope, ok := parseProviderErrorEnvelope(stdout); ok {
			return classifyProviderErrorEnvelope(envelope, err)
		}
	}
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

func parseProviderErrorEnvelope(stdout string) (providerErrorEnvelope, bool) {
	var envelope providerErrorEnvelope
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &envelope); err != nil {
		return providerErrorEnvelope{}, false
	}
	if !envelope.Error || strings.TrimSpace(envelope.Code) == "" {
		return providerErrorEnvelope{}, false
	}
	return envelope, true
}

func classifyProviderErrorEnvelope(envelope providerErrorEnvelope, err error) error {
	detail := trimForError(secrets.Scrub(firstNonEmpty(envelope.Message, envelope.Code)))
	var kind ErrorKind
	switch strings.ToUpper(strings.TrimSpace(envelope.Code)) {
	case "AUTH_ERROR":
		kind = ErrAuth
	case "RATE_LIMITED":
		kind = ErrRateLimit
	case "NETWORK_ERROR", "TIMEOUT", "POLL_TIMEOUT":
		kind = ErrTransient
	case "CANCELLED": //nolint:misspell // Exact notebooklm-py v0.8.0 error code.
		kind = ErrCancelled
	case "VALIDATION_ERROR", "CONFIG_ERROR", "NOTEBOOK_LIMIT", "NOT_FOUND":
		kind = ErrPermanent
	case "NOTEBOOKLM_ERROR", "UNEXPECTED_ERROR":
		kind = ErrPermanent
		// v0.8.0 can wrap an RPC code 16 response in the generic envelope
		// while retaining the only auth signal in its message.
		if containsAny(strings.ToLower(envelope.Message),
			"unauthenticated", "not authenticated", "authentication failed", "unauthorized") {
			kind = ErrAuth
		}
	default:
		kind = ErrPermanent
	}
	return &Error{Kind: kind, Message: detail, Err: err}
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
	// ErrCancelled indicates that the provider canceled the operation. It is
	// non-retryable because an idempotent add may already have mutated remotely.
	ErrCancelled
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
	return hasErrorKind(err, ErrAuth)
}

// IsAmbiguous reports whether err contains an upload whose remote mutation
// outcome is unknown. Callers must never retry the corresponding add.
func IsAmbiguous(err error) bool {
	return hasErrorKind(err, ErrAmbiguous)
}

func hasErrorKind(err error, kind ErrorKind) bool {
	if err == nil {
		return false
	}
	if typed, ok := err.(*Error); ok && typed.Kind == kind {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, nested := range joined.Unwrap() {
			if hasErrorKind(nested, kind) {
				return true
			}
		}
		return false
	}
	return hasErrorKind(errors.Unwrap(err), kind)
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
