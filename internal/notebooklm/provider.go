package notebooklm

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Provider identifies a NotebookLM CLI implementation.
type Provider string

const (
	// ProviderNotebookLMpy is the teng-lin/notebooklm-py CLI.
	ProviderNotebookLMpy Provider = "notebooklm-py"
	// ProviderNLM is the jacob-bd/notebooklm-mcp-cli (`nlm`) binary.
	ProviderNLM Provider = "nlm"
)

// UploadRequest is the internal input to a provider upload.
type UploadRequest struct {
	FilePath       string
	Title          string
	NotebookID     string
	IdempotencyKey string
}

// Config is the subset of NotebookLM settings the uploader needs.
type Config struct {
	Provider              Provider
	NotebookID            string
	CLIPath               string
	AuthProfile           string
	UploadTimeout         time.Duration
	MaxSourcesPerNotebook int
}

// Normalize fills defaults and aliases.
func (c *Config) Normalize() {
	if c.Provider == "" {
		c.Provider = ProviderNotebookLMpy
	}
	if c.CLIPath == "" {
		if c.Provider == ProviderNLM {
			c.CLIPath = "nlm"
		} else {
			c.CLIPath = "notebooklm"
		}
	}
	if c.UploadTimeout <= 0 {
		c.UploadTimeout = 30 * time.Minute
	}
	if c.MaxSourcesPerNotebook <= 0 {
		c.MaxSourcesPerNotebook = 300
	}
}

// BuildUploadArgs returns the argv for adding a local file as a source.
func BuildUploadArgs(cfg Config, req UploadRequest) ([]string, error) {
	cfg.Normalize()
	notebookID := firstNonEmpty(req.NotebookID, cfg.NotebookID)
	if notebookID == "" {
		return nil, fmt.Errorf("notebook id is required")
	}
	if strings.TrimSpace(req.FilePath) == "" {
		return nil, fmt.Errorf("upload file path is required")
	}
	switch cfg.Provider {
	case ProviderNLM:
		return buildNLMUploadArgs(cfg, req, notebookID), nil
	case ProviderNotebookLMpy:
		return buildNotebookLMpyUploadArgs(cfg, req, notebookID), nil
	default:
		return nil, fmt.Errorf("unsupported notebooklm provider %q", cfg.Provider)
	}
}

func buildNotebookLMpyUploadArgs(cfg Config, req UploadRequest, notebookID string) []string {
	args := []string{
		"source", "add",
		"--notebook", notebookID,
		"--type", "file",
		"--json",
		req.FilePath,
	}
	if req.Title != "" {
		args = append(args, "--title", req.Title)
	}
	if cfg.AuthProfile != "" {
		args = append([]string{"--profile", cfg.AuthProfile}, args...)
	}
	return args
}

func buildNLMUploadArgs(cfg Config, req UploadRequest, notebookID string) []string {
	args := []string{
		"source", "add", notebookID,
		"--file", req.FilePath,
		"--wait",
		"--wait-timeout", strconv.FormatInt(timeoutSeconds(cfg.UploadTimeout), 10),
		"--json",
	}
	if req.Title != "" {
		args = append(args, "--title", req.Title)
	}
	if cfg.AuthProfile != "" {
		args = append([]string{"--profile", cfg.AuthProfile}, args...)
	}
	return args
}

func buildNotebookLMpyWaitArgs(cfg Config, notebookID, sourceID string) []string {
	args := []string{
		"source", "wait", sourceID,
		"--notebook", notebookID,
		"--timeout", strconv.FormatInt(timeoutSeconds(cfg.UploadTimeout), 10),
		"--interval", "1",
		"--json",
	}
	if cfg.AuthProfile != "" {
		args = append([]string{"--profile", cfg.AuthProfile}, args...)
	}
	return args
}

func timeoutSeconds(timeout time.Duration) int64 {
	seconds := int64((timeout + time.Second - 1) / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}

// BuildAuthCheckArgs returns argv for an auth health check.
func BuildAuthCheckArgs(cfg Config) []string {
	cfg.Normalize()
	if cfg.Provider == ProviderNLM {
		args := []string{"login", "--check"}
		if cfg.AuthProfile != "" {
			args = append(args, "--profile", cfg.AuthProfile)
		}
		return args
	}
	args := []string{"auth", "check", "--test", "--json"}
	if cfg.AuthProfile != "" {
		args = append([]string{"--profile", cfg.AuthProfile}, args...)
	}
	return args
}

// BuildListSourcesArgs returns argv for listing notebook sources (count guard).
func BuildListSourcesArgs(cfg Config, notebookID string) []string {
	cfg.Normalize()
	nb := firstNonEmpty(notebookID, cfg.NotebookID)
	if cfg.Provider == ProviderNLM {
		args := []string{"source", "list", nb, "--json"}
		if cfg.AuthProfile != "" {
			args = append([]string{"--profile", cfg.AuthProfile}, args...)
		}
		return args
	}
	args := []string{"source", "list", "--notebook", nb, "--json"}
	if cfg.AuthProfile != "" {
		args = append([]string{"--profile", cfg.AuthProfile}, args...)
	}
	return args
}
