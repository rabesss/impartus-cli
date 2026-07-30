// Package notebooklm shells out to a NotebookLM provider CLI to upload lecture audio.
package notebooklm

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Uploader adds local audio files to a NotebookLM notebook via a provider CLI.
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

// New returns an Uploader that shells out to the configured provider CLI.
func New(cfg Config) *Uploader {
	cfg.Normalize()
	return &Uploader{cfg: cfg, runner: execRunner{}}
}

// NewWithRunner returns an Uploader with an injected command runner.
func NewWithRunner(cfg Config, runner CommandRunner) *Uploader {
	u := New(cfg)
	u.runner = runner
	return u
}

// Upload adds a local audio file as a NotebookLM source.
func (u *Uploader) Upload(ctx context.Context, req UploadRequest) (UploadResult, error) {
	return u.upload(ctx, req)
}

// UploadFile adds a local audio file as a NotebookLM source (legacy helper).
func (u *Uploader) UploadFile(ctx context.Context, filePath, title string) (UploadResult, error) {
	return u.upload(ctx, UploadRequest{FilePath: filePath, Title: title})
}

func (u *Uploader) upload(ctx context.Context, req UploadRequest) (UploadResult, error) {
	args, err := BuildUploadArgs(u.cfg, req)
	if err != nil {
		return UploadResult{}, err
	}
	if _, err := os.Stat(req.FilePath); err != nil {
		return UploadResult{}, fmt.Errorf("upload file: %w", err)
	}
	notebookID := firstNonEmpty(req.NotebookID, u.cfg.NotebookID)
	if req.IdempotencyKey != "" {
		token := idempotencyToken(req.IdempotencyKey)
		if token == "" || !strings.Contains(req.Title, token) {
			return UploadResult{Outcome: UploadRejected}, fmt.Errorf("upload title must contain idempotency key")
		}
	}
	return u.executeUpload(ctx, args, req, notebookID)
}

func (u *Uploader) executeUpload(
	ctx context.Context,
	args []string,
	req UploadRequest,
	notebookID string,
) (UploadResult, error) {
	uploadCtx, cancel := context.WithTimeout(ctx, u.cfg.UploadTimeout)
	defer cancel()

	stdout, stderr, runErr := u.runner.Run(uploadCtx, u.cfg.CLIPath, args, providerEnvironment())
	if runErr != nil {
		classifyErr := runErr
		if uploadCtx.Err() != nil {
			classifyErr = uploadCtx.Err()
		}
		classified := ClassifyError(classifyErr, stdout, stderr)
		outcome := classifyUploadOutcome(u.cfg.Provider, classified)
		if req.IdempotencyKey == "" || outcome == UploadRejected {
			return UploadResult{Outcome: UploadRejected}, classified
		}
		return UploadResult{Outcome: UploadAmbiguous}, &Error{
			Kind:    ErrAmbiguous,
			Message: "upload outcome is ambiguous; later watch cycles must reconcile without another add",
			Err:     classified,
		}
	}
	result, parseErr := parseUploadResult(stdout, notebookID)
	if parseErr != nil {
		if req.IdempotencyKey != "" {
			return UploadResult{Outcome: UploadAmbiguous}, &Error{
				Kind:    ErrAmbiguous,
				Message: "upload response was ambiguous; later watch cycles must reconcile without another add",
				Err:     fmt.Errorf("parse notebooklm upload response: %w", parseErr),
			}
		}
		return UploadResult{Outcome: UploadRejected, Raw: stdout}, fmt.Errorf("parse notebooklm upload response: %w", parseErr)
	}
	result.Raw = stdout
	if req.Title != "" && result.Title == "" {
		result.Title = req.Title
	}
	return result, nil
}

// UploadToNotebook uploads to an explicit notebook id (multi-target watch).
func (u *Uploader) UploadToNotebook(ctx context.Context, notebookID, filePath, title, idempotencyKey string) (UploadResult, error) {
	return u.upload(ctx, UploadRequest{
		NotebookID:     notebookID,
		FilePath:       filePath,
		Title:          title,
		IdempotencyKey: idempotencyKey,
	})
}

// ReconcileUpload looks up an idempotent source without issuing another add.
func (u *Uploader) ReconcileUpload(
	ctx context.Context,
	notebookID, title, idempotencyKey string,
) (UploadResult, bool, error) {
	token := idempotencyToken(idempotencyKey)
	if token == "" || !strings.Contains(title, token) {
		return UploadResult{}, false, fmt.Errorf("upload title must contain idempotency key")
	}
	inventory, err := u.listSources(ctx, notebookID)
	if err != nil {
		return UploadResult{}, false, fmt.Errorf("reconcile notebook after ambiguous upload: %w", err)
	}
	result, found := findSourceByTitle(inventory.Sources, title, idempotencyKey)
	if found {
		result.Outcome = UploadFound
	}
	return result, found, nil
}

func findSourceByTitle(sources []UploadResult, title, idempotencyKey string) (UploadResult, bool) {
	title = strings.TrimSpace(title)
	if title == "" {
		return UploadResult{}, false
	}
	token := idempotencyToken(idempotencyKey)
	for _, source := range sources {
		sourceTitle := strings.TrimSpace(source.Title)
		if strings.TrimSpace(source.SourceID) != "" &&
			(sourceTitle == title || (token != "" && strings.Contains(sourceTitle, token))) {
			return source, true
		}
	}
	return UploadResult{}, false
}

func idempotencyToken(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	return "[" + key + "]"
}
