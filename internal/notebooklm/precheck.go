package notebooklm

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/rabesss/impartus-cli/internal/secrets"
)

// Doctor checks that the CLI is present, authentication looks usable, and the
// notebook is under the configured source-count cap when a notebook id is set.
func (u *Uploader) Doctor(ctx context.Context) error {
	u.cfg.Normalize()
	if _, err := exec.LookPath(u.cfg.CLIPath); err != nil {
		hint := "pip install --pre 'notebooklm-py[headless]==0.8.0rc1'"
		if u.cfg.Provider == ProviderNLM {
			hint = "pip install notebooklm-mcp-cli"
		}
		return fmt.Errorf("notebooklm CLI %q not found on PATH: %w (install with: %s)", u.cfg.CLIPath, err, hint)
	}
	if err := u.checkAuth(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(u.cfg.NotebookID) != "" {
		if err := u.checkSourceCap(ctx, u.cfg.NotebookID); err != nil {
			return err
		}
	}
	return nil
}

func (u *Uploader) checkAuth(ctx context.Context) error {
	doctorCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	args := BuildAuthCheckArgs(u.cfg)
	stdout, stderr, err := u.runner.Run(doctorCtx, u.cfg.CLIPath, args, os.Environ())
	if err != nil {
		detail := firstNonEmpty(secrets.Scrub(stderr), secrets.Scrub(stdout), secrets.ScrubError(err))
		return fmt.Errorf("notebooklm auth check failed: %s", trimForError(detail))
	}
	status, parseErr := parseAuthStatus(stdout)
	if parseErr != nil {
		// nlm may return a different shape; treat missing status as soft-ok when exit 0.
		if strings.TrimSpace(stdout) == "" {
			return nil
		}
		if authOutputOK(stdout + stderr) {
			return nil
		}
		return fmt.Errorf("notebooklm auth check returned unreadable JSON: %w", parseErr)
	}
	if status != "" && !authStatusOK(status) {
		return fmt.Errorf("notebooklm auth status is %q (run the provider's native login check)", status)
	}
	return nil
}

func authOutputOK(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "authentication valid") ||
		strings.Contains(lower, "successfully authenticated") ||
		strings.Contains(lower, "authenticated") ||
		strings.Contains(lower, "status: ok")
}

func authStatusOK(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "ok", "authenticated", "valid", "success":
		return true
	default:
		return false
	}
}

func (u *Uploader) checkSourceCap(ctx context.Context, notebookID string) error {
	if u.cfg.MaxSourcesPerNotebook <= 0 {
		return nil
	}
	listCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	args := BuildListSourcesArgs(u.cfg, notebookID)
	stdout, stderr, err := u.runner.Run(listCtx, u.cfg.CLIPath, args, os.Environ())
	if err != nil {
		detail := firstNonEmpty(secrets.Scrub(stderr), secrets.Scrub(stdout), secrets.ScrubError(err))
		return fmt.Errorf("check notebook source count: %s", trimForError(detail))
	}
	count, parseErr := parseSourceCount(stdout)
	if parseErr != nil {
		return fmt.Errorf("parse notebook source count: %w", parseErr)
	}
	if count >= u.cfg.MaxSourcesPerNotebook {
		return fmt.Errorf("notebook %s already has %d sources (cap %d)", notebookID, count, u.cfg.MaxSourcesPerNotebook)
	}
	return nil
}
