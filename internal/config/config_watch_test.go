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

func TestValidateWatchRequiresCourseWhenEnabled(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.ApplyDefaults()
	cfg.Watch.Enabled = true
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "watch.subjectId") {
		t.Fatalf("expected subject/session error, got %v", err)
	}

	cfg.Watch.SubjectID = 1
	cfg.Watch.SessionID = 2
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid watch config, got %v", err)
	}
}

func TestValidateNotebookLMRequiredWhenUploadEnabled(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.ApplyDefaults()
	cfg.Watch.Upload = true
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "notebooklm.notebookId") {
		t.Fatalf("expected notebook id error, got %v", err)
	}
	cfg.NotebookLM.NotebookID = "nb1"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}
}

func TestValidateWatchIntervalBounds(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.ApplyDefaults()
	cfg.Watch.Interval = "1s"
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "watch.interval") {
		t.Fatalf("expected interval error, got %v", err)
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
	t.Setenv("IMPARTUS_WATCH_INTERVAL", "2m")

	cfg, err := LoadResolved("")
	if err != nil {
		t.Fatalf("LoadResolved: %v", err)
	}
	if !cfg.Watch.Enabled || cfg.Watch.SubjectID != 9 || cfg.Watch.SessionID != 8 {
		t.Fatalf("watch env not applied: %+v", cfg.Watch)
	}
	if !cfg.Watch.Upload || cfg.NotebookLM.NotebookID != "nb-env" || cfg.Watch.Interval != "2m" {
		t.Fatalf("upload/notebook env not applied: watch=%+v nlm=%+v", cfg.Watch, cfg.NotebookLM)
	}
}
