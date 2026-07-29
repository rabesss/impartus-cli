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
	notebooks := []string{}
	if strings.TrimSpace(u.cfg.NotebookID) != "" {
		notebooks = append(notebooks, u.cfg.NotebookID)
	}
	return u.DoctorNotebooks(ctx, notebooks)
}

// DoctorNotebooks checks the provider once and validates every routed notebook.
func (u *Uploader) DoctorNotebooks(ctx context.Context, notebookIDs []string) error {
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
	seen := make(map[string]struct{}, len(notebookIDs))
	for _, notebookID := range notebookIDs {
		notebookID = strings.TrimSpace(notebookID)
		if notebookID == "" {
			continue
		}
		if _, ok := seen[notebookID]; ok {
			continue
		}
		seen[notebookID] = struct{}{}
		if err := u.checkSourceCap(ctx, notebookID); err != nil {
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
		if authOutputOK(stdout + stderr) {
			return nil
		}
		return fmt.Errorf("notebooklm auth check returned unreadable JSON: %w", parseErr)
	}
	if !authStatusOK(status) {
		return fmt.Errorf("notebooklm auth status is %q (run the provider's native login check)", status)
	}
	return nil
}

func authOutputOK(output string) bool {
	lower := strings.ToLower(output)
	if strings.Contains(lower, "not authenticated") ||
		strings.Contains(lower, "unauthenticated") ||
		strings.Contains(lower, "authentication failed") {
		return false
	}
	return strings.Contains(lower, "authentication valid") ||
		strings.Contains(lower, "successfully authenticated") ||
		strings.Contains(lower, "authenticated") ||
		strings.Contains(lower, "status: ok")
}

func authStatusOK(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "ok", "authenticated", "valid", "success":
		return true
	default:
		return false
	}
}

func (u *Uploader) checkSourceCap(ctx context.Context, notebookID string) error {
	if u.cfg.MaxSourcesPerNotebook <= 0 {
		return nil
	}
	inventory, err := u.listSources(ctx, notebookID)
	if err != nil {
		return err
	}
	if inventory.Count >= u.cfg.MaxSourcesPerNotebook {
		return fmt.Errorf("notebook %s already has %d sources (cap %d)", notebookID, inventory.Count, u.cfg.MaxSourcesPerNotebook)
	}
	return nil
}

func (u *Uploader) listSources(ctx context.Context, notebookID string) (sourceInventory, error) {
	listCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	args := BuildListSourcesArgs(u.cfg, notebookID)
	stdout, stderr, err := u.runner.Run(listCtx, u.cfg.CLIPath, args, os.Environ())
	if err != nil {
		detail := firstNonEmpty(secrets.Scrub(stderr), secrets.Scrub(stdout), secrets.ScrubError(err))
		return sourceInventory{}, fmt.Errorf("check notebook sources: %s", trimForError(detail))
	}
	inventory, parseErr := parseSourceInventory(stdout, notebookID)
	if parseErr != nil {
		return sourceInventory{}, fmt.Errorf("parse notebook sources: %w", parseErr)
	}
	return inventory, nil
}
