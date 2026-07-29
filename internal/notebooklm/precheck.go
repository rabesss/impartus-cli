package notebooklm

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
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
		detail := firstNonEmpty(stderr, stdout, err.Error())
		return fmt.Errorf("notebooklm auth check failed: %s", trimForError(detail))
	}
	status, parseErr := parseAuthStatus(stdout)
	if parseErr != nil {
		// nlm may return a different shape; treat missing status as soft-ok when exit 0.
		if strings.TrimSpace(stdout) == "" {
			return nil
		}
		lower := strings.ToLower(stdout + stderr)
		if strings.Contains(lower, "ok") || strings.Contains(lower, "authenticated") {
			return nil
		}
		return fmt.Errorf("notebooklm auth check returned unreadable JSON: %w", parseErr)
	}
	if status != "" && status != "ok" {
		return fmt.Errorf("notebooklm auth status is %q (run scripts/notebooklm-auth/nlm_auth.py verify)", status)
	}
	return nil
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
		// Listing may be unavailable on older CLIs; do not hard-fail doctor.
		_ = stderr
		return nil
	}
	count, parseErr := parseSourceCount(stdout)
	if parseErr != nil {
		return nil
	}
	if count >= u.cfg.MaxSourcesPerNotebook {
		return fmt.Errorf("notebook %s already has %d sources (cap %d)", notebookID, count, u.cfg.MaxSourcesPerNotebook)
	}
	return nil
}
