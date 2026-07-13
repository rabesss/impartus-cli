// Package main contains integration tests for the impartus CLI.
package main

import (
	"testing"

	"github.com/rabesss/impartus-cli/internal/buildinfo"
	iconfig "github.com/rabesss/impartus-cli/internal/config"
)

func TestSampleConfigIsValidJSONAndConfig(t *testing.T) {
	cfg, err := iconfig.Parse("sample.config.json")
	if err != nil {
		t.Fatalf("sample config could not be parsed: %v", err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("sample config did not validate: %v", err)
	}

	if cfg.Quality != "720" {
		t.Errorf("quality = %q, want %q", cfg.Quality, "720")
	}
	if cfg.TempDirLocation != "./temp" {
		t.Errorf("tempDirLocation = %q, want %q", cfg.TempDirLocation, "./temp")
	}
	if cfg.HTTPTimeout != "10m" {
		t.Errorf("httpTimeout = %q, want %q", cfg.HTTPTimeout, "10m")
	}
	if cfg.NumWorkers != 5 || cfg.DownloadWorkersPerLecture != 12 || cfg.DecryptWorkersPerLecture != 4 {
		t.Errorf("worker defaults = (%d, %d, %d), want (5, 12, 4)", cfg.NumWorkers, cfg.DownloadWorkersPerLecture, cfg.DecryptWorkersPerLecture)
	}
	if cfg.ProgressTracking.Enabled || cfg.ProgressTracking.ShowSpeed || cfg.ProgressTracking.ShowETA {
		t.Error("progress presentation defaults must be disabled")
	}
	if cfg.ProgressTracking.UpdateInterval != "2s" || cfg.ProgressTracking.SpeedWindowSize != 10 {
		t.Errorf("progress sampling defaults = (%q, %d), want (%q, 10)", cfg.ProgressTracking.UpdateInterval, cfg.ProgressTracking.SpeedWindowSize, "2s")
	}
}

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
	if cfg.RateLimit != 100 || cfg.APIRateLimit != 2 {
		t.Fatalf("expected default rate limits 100/2, got %.1f/%.1f", cfg.RateLimit, cfg.APIRateLimit)
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
