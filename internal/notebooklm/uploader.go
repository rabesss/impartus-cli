// Package notebooklm shells out to the notebooklm-py CLI to upload lecture audio.
package notebooklm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Config is the subset of NotebookLM settings the uploader needs.
type Config struct {
	NotebookID  string
	CLIPath     string
	AuthProfile string
}

// UploadResult is the successful outcome of adding a source.
type UploadResult struct {
	SourceID   string `json:"sourceId,omitempty"`
	Title      string `json:"title,omitempty"`
	NotebookID string `json:"notebookId,omitempty"`
	Raw        string `json:"-"`
}

// Uploader adds local audio files to a NotebookLM notebook.
type Uploader struct {
	cfg    Config
	runner CommandRunner
}

// CommandRunner runs an external command. Tests inject a fake.
type CommandRunner interface {
	Run(ctx context.Context, name string, args []string, env []string) (stdout, stderr string, err error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args []string, env []string) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- CLI path is operator-configured
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// New returns an Uploader that shells out to the notebooklm CLI.
func New(cfg Config) *Uploader {
	if cfg.CLIPath == "" {
		cfg.CLIPath = "notebooklm"
	}
	return &Uploader{cfg: cfg, runner: execRunner{}}
}

// NewWithRunner returns an Uploader with an injected command runner.
func NewWithRunner(cfg Config, runner CommandRunner) *Uploader {
	u := New(cfg)
	u.runner = runner
	return u
}

// Doctor checks that the CLI is present and authentication looks usable.
func (u *Uploader) Doctor(ctx context.Context) error {
	if _, err := exec.LookPath(u.cfg.CLIPath); err != nil {
		return fmt.Errorf("notebooklm CLI %q not found on PATH: %w (install with: pip install --pre 'notebooklm-py[headless]==0.8.0rc1')", u.cfg.CLIPath, err)
	}
	if strings.TrimSpace(u.cfg.NotebookID) == "" {
		return errors.New("notebooklm notebook ID is required")
	}
	args := []string{"auth", "check", "--json"}
	if u.cfg.AuthProfile != "" {
		args = append([]string{"--profile", u.cfg.AuthProfile}, args...)
	}
	// Auth check is a short RPC; bound it so a hung CLI cannot block watch forever.
	doctorCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	stdout, stderr, err := u.runner.Run(doctorCtx, u.cfg.CLIPath, args, os.Environ())
	if err != nil {
		detail := firstNonEmpty(stderr, stdout, err.Error())
		return fmt.Errorf("notebooklm auth check failed: %s", trimForError(detail))
	}
	status, parseErr := parseAuthStatus(stdout)
	if parseErr != nil {
		return fmt.Errorf("notebooklm auth check returned unreadable JSON: %w", parseErr)
	}
	if status != "ok" {
		return fmt.Errorf("notebooklm auth status is %q (run scripts/notebooklm-auth/nlm_auth.py verify)", status)
	}
	return nil
}

// UploadFile adds a local audio file as a NotebookLM source.
func (u *Uploader) UploadFile(ctx context.Context, filePath, title string) (UploadResult, error) {
	if strings.TrimSpace(u.cfg.NotebookID) == "" {
		return UploadResult{}, errors.New("notebooklm notebook ID is required")
	}
	if strings.TrimSpace(filePath) == "" {
		return UploadResult{}, errors.New("upload file path is required")
	}
	if _, err := os.Stat(filePath); err != nil {
		return UploadResult{}, fmt.Errorf("upload file: %w", err)
	}

	args := []string{
		"source", "add",
		"--notebook", u.cfg.NotebookID,
		"--type", "file",
		"--json",
		filePath,
	}
	if title != "" {
		args = append(args, "--title", title)
	}
	if u.cfg.AuthProfile != "" {
		args = append([]string{"--profile", u.cfg.AuthProfile}, args...)
	}

	// Large audio uploads can exceed the library's default HTTP timeout.
	uploadCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	stdout, stderr, err := u.runner.Run(uploadCtx, u.cfg.CLIPath, args, os.Environ())
	if err != nil {
		classified := ClassifyError(err, stdout, stderr)
		return UploadResult{}, classified
	}
	result, parseErr := parseUploadResult(stdout, u.cfg.NotebookID)
	if parseErr != nil {
		return UploadResult{Raw: stdout}, fmt.Errorf("parse notebooklm upload response: %w", parseErr)
	}
	result.Raw = stdout
	if title != "" && result.Title == "" {
		result.Title = title
	}
	return result, nil
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
		return UploadResult{}, errors.New("empty response")
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

func stringField(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			switch typed := v.(type) {
			case string:
				if typed != "" {
					return typed
				}
			}
		}
	}
	return ""
}

// ClassifyError turns a CLI failure into a typed error for retry decisions.
func ClassifyError(err error, stdout, stderr string) error {
	detail := firstNonEmpty(stderr, stdout, err.Error())
	lower := strings.ToLower(detail)
	switch {
	case strings.Contains(lower, "authentication") || strings.Contains(lower, "re-authenticate") || strings.Contains(lower, "auth status"):
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
