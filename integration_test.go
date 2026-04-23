package main

import (
	"testing"

	"github.com/rabesss/impartus-cli/internal/buildinfo"
	iconfig "github.com/rabesss/impartus-cli/internal/config"
)

func TestVersionFlagVariable(t *testing.T) {
	oldVersion := buildinfo.Version
	buildinfo.Version = "test-version"
	defer func() { buildinfo.Version = oldVersion }()

	if buildinfo.Version != "test-version" {
		t.Fatalf("expected version to be updated")
	}
}

func TestInternalConfigDefaults(t *testing.T) {
	cfg := &iconfig.Config{
		Username: "user",
		Password: "pass",
		BaseURL:  "https://example.com",
		Quality:  "720",
		Views:    "both",
	}
	cfg.ApplyDefaults()

	if cfg.NumWorkers != 5 {
		t.Fatalf("expected numWorkers default 5, got %d", cfg.NumWorkers)
	}
	if cfg.AudioFormat != "mp3" {
		t.Fatalf("expected audioFormat default mp3, got %q", cfg.AudioFormat)
	}
	if cfg.RateLimit != 10 || cfg.APIRateLimit != 2 {
		t.Fatalf("expected default rate limits 10/2, got %.1f/%.1f", cfg.RateLimit, cfg.APIRateLimit)
	}
	if cfg.HTTPTimeout != "10m" {
		t.Fatalf("expected default httpTimeout 10m, got %q", cfg.HTTPTimeout)
	}
}

func TestInternalConfigValidateMinimal(t *testing.T) {
	cfg := &iconfig.Config{
		Username: "user",
		Password: "pass",
		BaseURL:  "https://example.com",
		Quality:  "450",
		Views:    "both",
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected minimal config to validate, got %v", err)
	}
}
