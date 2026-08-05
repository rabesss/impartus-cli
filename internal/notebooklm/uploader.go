// Package notebooklm shells out to a NotebookLM provider CLI to upload lecture audio.
package notebooklm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	if _, statErr := os.Stat(req.FilePath); statErr != nil {
		return UploadResult{}, fmt.Errorf("upload file: %w", statErr)
	}
	notebookID := firstNonEmpty(req.NotebookID, u.cfg.NotebookID)
	if req.IdempotencyKey != "" {
		token := idempotencyToken(req.IdempotencyKey)
		if token == "" || !strings.Contains(req.Title, token) {
			return UploadResult{Outcome: UploadRejected}, fmt.Errorf("upload title must contain idempotency key")
		}
		prepared, cleanup, prepareErr := prepareIdempotentUploadFile(req)
		if prepareErr != nil {
			return UploadResult{Outcome: UploadRejected}, prepareErr
		}
		defer cleanup()
		req = prepared
		args, err = BuildUploadArgs(u.cfg, req)
		if err != nil {
			return UploadResult{Outcome: UploadRejected}, err
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
	tokens := idempotencyMatchTokens(idempotencyKey)
	for _, source := range sources {
		sourceTitle := strings.TrimSpace(source.Title)
		if strings.TrimSpace(source.SourceID) != "" && (sourceTitle == title || containsToken(sourceTitle, tokens)) {
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

func idempotencyFilenameToken(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(key))
	return fmt.Sprintf("[impartus-%x]", sum[:8])
}

func idempotencyMatchTokens(key string) []string {
	if strings.TrimSpace(key) == "" {
		return nil
	}
	return []string{idempotencyToken(key), idempotencyFilenameToken(key)}
}

func containsToken(title string, tokens []string) bool {
	for _, token := range tokens {
		if token != "" && strings.Contains(title, token) {
			return true
		}
	}
	return false
}

func prepareIdempotentUploadFile(req UploadRequest) (UploadRequest, func(), error) {
	token := idempotencyFilenameToken(req.IdempotencyKey)
	if token == "" || strings.Contains(filepath.Base(req.FilePath), token) {
		return req, func() {}, nil
	}

	source, err := os.Open(req.FilePath) // #nosec G304 -- caller-selected upload path
	if err != nil {
		return req, nil, fmt.Errorf("open upload file for stable naming: %w", err)
	}
	extension := filepath.Ext(req.FilePath)
	alias, err := os.CreateTemp(filepath.Dir(req.FilePath), token+"-*"+extension) // #nosec G304 -- same directory as validated upload file
	if err != nil {
		_ = source.Close() //nolint:errcheck // preserving the primary create error
		return req, nil, fmt.Errorf("create stable upload filename: %w", err)
	}
	aliasPath := alias.Name()
	cleanup := func() {
		_ = os.Remove(aliasPath) //nolint:errcheck // best-effort cleanup after provider consumed the file
	}
	if _, err := io.Copy(alias, source); err != nil {
		_ = source.Close() //nolint:errcheck // preserving the primary copy error
		_ = alias.Close()  //nolint:errcheck // preserving the primary copy error
		cleanup()
		return req, nil, fmt.Errorf("copy upload file for stable naming: %w", err)
	}
	if err := source.Close(); err != nil {
		_ = alias.Close() //nolint:errcheck // preserving the primary close error
		cleanup()
		return req, nil, fmt.Errorf("close source upload file: %w", err)
	}
	if err := alias.Close(); err != nil {
		cleanup()
		return req, nil, fmt.Errorf("close stable upload file: %w", err)
	}
	req.FilePath = aliasPath
	return req, cleanup, nil
}
