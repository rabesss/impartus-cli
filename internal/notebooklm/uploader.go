// Package notebooklm shells out to a NotebookLM provider CLI to upload lecture audio.
package notebooklm

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
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
	return u.UploadFile(ctx, req.FilePath, req.Title)
}

// UploadFile adds a local audio file as a NotebookLM source (legacy helper).
func (u *Uploader) UploadFile(ctx context.Context, filePath, title string) (UploadResult, error) {
	req := UploadRequest{FilePath: filePath, Title: title, NotebookID: u.cfg.NotebookID}
	args, err := BuildUploadArgs(u.cfg, req)
	if err != nil {
		return UploadResult{}, err
	}
	if _, err := os.Stat(req.FilePath); err != nil {
		return UploadResult{}, fmt.Errorf("upload file: %w", err)
	}

	uploadCtx, cancel := context.WithTimeout(ctx, u.cfg.UploadTimeout)
	defer cancel()

	stdout, stderr, runErr := u.runner.Run(uploadCtx, u.cfg.CLIPath, args, os.Environ())
	if runErr != nil {
		return UploadResult{}, ClassifyError(runErr, stdout, stderr)
	}
	notebookID := firstNonEmpty(req.NotebookID, u.cfg.NotebookID)
	result, parseErr := parseUploadResult(stdout, notebookID)
	if parseErr != nil {
		return UploadResult{Raw: stdout}, fmt.Errorf("parse notebooklm upload response: %w", parseErr)
	}
	result.Raw = stdout
	if title != "" && result.Title == "" {
		result.Title = title
	}
	return result, nil
}

// UploadToNotebook uploads to an explicit notebook id (multi-target watch).
func (u *Uploader) UploadToNotebook(ctx context.Context, notebookID, filePath, title string) (UploadResult, error) {
	cfg := u.cfg
	cfg.NotebookID = firstNonEmpty(notebookID, cfg.NotebookID)
	tmp := *u
	tmp.cfg = cfg
	return tmp.UploadFile(ctx, filePath, title)
}
