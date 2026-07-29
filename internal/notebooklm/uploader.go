// Package notebooklm shells out to a NotebookLM provider CLI to upload lecture audio.
package notebooklm

import (
	"bytes"
	"context"
	"errors"
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
		existing, found, prepErr := u.prepareIdempotentUpload(ctx, notebookID, req.Title, req.IdempotencyKey)
		if prepErr != nil {
			return UploadResult{}, prepErr
		}
		if found {
			return existing, nil
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

	stdout, stderr, runErr := u.runner.Run(uploadCtx, u.cfg.CLIPath, args, os.Environ())
	if runErr != nil {
		classifyErr := runErr
		if uploadCtx.Err() != nil {
			classifyErr = uploadCtx.Err()
		}
		classified := ClassifyError(classifyErr, stdout, stderr)
		if req.IdempotencyKey == "" || IsAuth(classified) || !isTypedRetryable(classified) {
			return UploadResult{}, classified
		}
		return u.reconcileAmbiguousUpload(ctx, notebookID, req.Title, classified)
	}
	result, parseErr := parseUploadResult(stdout, notebookID)
	if parseErr != nil {
		if req.IdempotencyKey != "" {
			return u.reconcileAmbiguousUpload(
				ctx,
				notebookID,
				req.Title,
				fmt.Errorf("parse notebooklm upload response: %w", parseErr),
			)
		}
		return UploadResult{Raw: stdout}, fmt.Errorf("parse notebooklm upload response: %w", parseErr)
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

func (u *Uploader) prepareIdempotentUpload(
	ctx context.Context,
	notebookID, title, idempotencyKey string,
) (UploadResult, bool, error) {
	if !strings.Contains(title, idempotencyKey) {
		return UploadResult{}, false, fmt.Errorf("upload title must contain idempotency key")
	}
	inventory, err := u.listSources(ctx, notebookID)
	if err != nil {
		return UploadResult{}, false, fmt.Errorf("reconcile notebook before upload: %w", err)
	}
	if existing, ok := findSourceByTitle(inventory.Sources, title); ok {
		return existing, true, nil
	}
	if u.cfg.MaxSourcesPerNotebook > 0 && inventory.Count >= u.cfg.MaxSourcesPerNotebook {
		return UploadResult{}, false, fmt.Errorf(
			"notebook %s already has %d sources (cap %d)",
			notebookID,
			inventory.Count,
			u.cfg.MaxSourcesPerNotebook,
		)
	}
	return UploadResult{}, false, nil
}

func (u *Uploader) reconcileAmbiguousUpload(ctx context.Context, notebookID, title string, cause error) (UploadResult, error) {
	inventory, err := u.listSources(ctx, notebookID)
	if err == nil {
		if existing, ok := findSourceByTitle(inventory.Sources, title); ok {
			return existing, nil
		}
	}
	message := "upload outcome is ambiguous; the next watch cycle will reconcile before another add"
	if err != nil {
		message += ": " + trimForError(err.Error())
	}
	return UploadResult{}, &Error{Kind: ErrAmbiguous, Message: message, Err: cause}
}

func findSourceByTitle(sources []UploadResult, title string) (UploadResult, bool) {
	title = strings.TrimSpace(title)
	if title == "" {
		return UploadResult{}, false
	}
	for _, source := range sources {
		if strings.TrimSpace(source.Title) == title && strings.TrimSpace(source.SourceID) != "" {
			return source, true
		}
	}
	return UploadResult{}, false
}

func isTypedRetryable(err error) bool {
	var typed *Error
	return errors.As(err, &typed) && typed.Retryable()
}
