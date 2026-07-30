package config

import (
	"strings"
	"testing"
)

func TestApplyWatchMediaDefaultsForcesEfficientAudio(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.ApplyDefaults()
	cfg.Quality = "720"
	cfg.Views = "both"
	cfg.AudioOnly = false
	cfg.ApplyWatchMediaDefaults()

	if !cfg.AudioOnly || !cfg.SkipNoAudio {
		t.Fatalf("expected audio-only + skip-no-audio, got %+v", cfg)
	}
	if cfg.Quality != "144" || cfg.Views != "left" || cfg.AudioFormat != "mp3" {
		t.Fatalf("expected 144/left/mp3, got quality=%s views=%s format=%s", cfg.Quality, cfg.Views, cfg.AudioFormat)
	}
}

func TestValidateWatchRequiresTargetsWhenEnabled(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.ApplyDefaults()
	cfg.Watch.Enabled = true
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "watch.targets") {
		t.Fatalf("expected targets error, got %v", err)
	}

	cfg.Watch.Targets = []WatchTarget{{SubjectID: 1, SessionID: 2, NotebookID: "nb1"}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid watch config, got %v", err)
	}
}

func TestValidateWatchLegacySubjectSession(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.ApplyDefaults()
	cfg.Watch.Enabled = true
	cfg.Watch.SubjectID = 1
	cfg.Watch.SessionID = 2
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected legacy subject/session to synthesize target, got %v", err)
	}
	if len(cfg.ResolvedTargets()) != 1 {
		t.Fatalf("expected 1 target, got %d", len(cfg.ResolvedTargets()))
	}
}

func TestValidateWatchRejectsDuplicateTargets(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.ApplyDefaults()
	cfg.Watch.Enabled = true
	cfg.Watch.Targets = []WatchTarget{
		{SubjectID: 1, SessionID: 2, NotebookID: "a"},
		{SubjectID: 1, SessionID: 2, NotebookID: "b"},
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestValidateNotebookLMRequiredWhenUploadEnabled(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.ApplyDefaults()
	cfg.Watch.Upload = true
	cfg.Watch.Targets = []WatchTarget{{SubjectID: 1, SessionID: 2}}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "notebookId") {
		t.Fatalf("expected notebook id error, got %v", err)
	}
	cfg.Watch.Targets[0].NotebookID = "nb1"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}
}

func TestValidateWatchIntervalBounds(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.ApplyDefaults()
	cfg.Watch.PollInterval = "1m"
	cfg.Watch.Interval = "1m"
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "watch.pollInterval") {
		t.Fatalf("expected interval error, got %v", err)
	}
}

func TestWatchCountDefaultsAndNegativeValidationAgree(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.ApplyDefaults()
	if cfg.Watch.MaxLecturesPerCycle != 3 || cfg.Watch.MaxUploadRetries != 3 ||
		cfg.Watch.NotebookLM.MaxSourcesPerNotebook != 300 {
		t.Fatalf("watch count defaults changed: %+v", cfg.Watch)
	}

	cfg.Watch.MaxLecturesPerCycle = -1
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "maxLecturesPerCycle") {
		t.Fatalf("expected negative lecture limit rejection, got %v", err)
	}
	cfg.Watch.MaxLecturesPerCycle = 3
	cfg.Watch.MaxUploadRetries = -1
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "maxUploadRetries") {
		t.Fatalf("expected negative retry rejection, got %v", err)
	}
	cfg.Watch.MaxUploadRetries = 3
	cfg.Watch.NotebookLM.MaxSourcesPerNotebook = -1
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "maxSourcesPerNotebook") {
		t.Fatalf("expected negative source cap rejection, got %v", err)
	}
}

func TestValidateNotebookLMProvider(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.ApplyDefaults()
	cfg.Watch.NotebookLM.Provider = "other"
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "provider") {
		t.Fatalf("expected provider error, got %v", err)
	}
}

func TestValidateLegacyNotebookLMUploadTimeout(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.ApplyDefaults()
	cfg.Watch.NotebookLM.UploadTimeout = ""
	cfg.NotebookLM.UploadTimeout = "not-a-duration"
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "uploadTimeout") {
		t.Fatalf("expected legacy upload timeout validation error, got %v", err)
	}
}

func TestApplyDefaultsPreservesLegacyNotebookLMUploadTimeout(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.NotebookLM.UploadTimeout = "45m"
	cfg.ApplyDefaults()
	if cfg.Watch.NotebookLM.UploadTimeout != "45m" || cfg.NotebookLM.UploadTimeout != "45m" {
		t.Fatalf("legacy upload timeout was not normalized before defaults: %+v", cfg.Watch.NotebookLM)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("preserved legacy upload timeout did not validate: %v", err)
	}
}

func TestWatchNotebookLMCLIPathAliasIsPreserved(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Watch.NotebookLM.CLIPath = "/opt/tools/notebooklm"
	cfg.ApplyDefaults()
	if cfg.Watch.NotebookLM.Command != "/opt/tools/notebooklm" ||
		cfg.Watch.NotebookLM.CLIPath != "/opt/tools/notebooklm" {
		t.Fatalf("cliPath alias was overwritten: %+v", cfg.Watch.NotebookLM)
	}
}

func TestWatchEnvOverrides(t *testing.T) {
	t.Setenv("IMPARTUS_USERNAME", "u")
	t.Setenv("IMPARTUS_PASSWORD", "p")
	t.Setenv("IMPARTUS_BASE_URL", "https://example.com")
	t.Setenv("IMPARTUS_WATCH_ENABLED", "true")
	t.Setenv("IMPARTUS_WATCH_SUBJECT_ID", "9")
	t.Setenv("IMPARTUS_WATCH_SESSION_ID", "8")
	t.Setenv("IMPARTUS_WATCH_UPLOAD", "true")
	t.Setenv("IMPARTUS_NOTEBOOKLM_NOTEBOOK_ID", "nb-env")
	t.Setenv("IMPARTUS_WATCH_INTERVAL", "10m")
	t.Setenv("IMPARTUS_NOTEBOOKLM_PROVIDER", "notebooklm-py")
	t.Setenv("IMPARTUS_NOTEBOOKLM_COMMAND", "notebooklm")

	cfg, err := LoadResolved("")
	if err != nil {
		t.Fatalf("LoadResolved: %v", err)
	}
	if !cfg.Watch.Enabled || cfg.Watch.SubjectID != 9 || cfg.Watch.SessionID != 8 {
		t.Fatalf("watch env not applied: %+v", cfg.Watch)
	}
	if !cfg.Watch.Upload || cfg.Watch.NotebookLM.NotebookID != "nb-env" || cfg.Watch.PollInterval != "10m" {
		t.Fatalf("upload/notebook env not applied: watch=%+v", cfg.Watch)
	}
	if len(cfg.ResolvedTargets()) != 1 || cfg.ResolvedTargets()[0].NotebookID != "nb-env" {
		t.Fatalf("expected synthesized target with notebook, got %+v", cfg.ResolvedTargets())
	}
}

func TestWatchCLIPathEnvOverrideIsPreserved(t *testing.T) {
	t.Setenv("IMPARTUS_USERNAME", "u")
	t.Setenv("IMPARTUS_PASSWORD", "p")
	t.Setenv("IMPARTUS_BASE_URL", "https://example.com")
	t.Setenv("IMPARTUS_NOTEBOOKLM_CLI_PATH", "/opt/tools/notebooklm")

	cfg, err := LoadResolved("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Watch.NotebookLM.Command != "/opt/tools/notebooklm" {
		t.Fatalf("CLI path env override discarded: %+v", cfg.Watch.NotebookLM)
	}
}
