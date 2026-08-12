package config

import (
	"strings"
	"testing"
	"time"
)

func TestWatchDefaultsAreGenericAndAudioEfficient(t *testing.T) {
	t.Parallel()

	cfg := minimalValidConfig()
	cfg.Watch.Enabled = true
	cfg.Watch.Targets = []WatchTarget{{SubjectID: 67, SessionID: 8, Label: "Algorithms"}}
	cfg.ApplyDefaults()
	cfg.ApplyWatchMediaDefaults()

	if cfg.Watch.PollInterval != "5m" || cfg.Watch.MaxLecturesPerCycle != 3 || cfg.Watch.MaxRetries != 3 {
		t.Fatalf("watch defaults = %+v", cfg.Watch)
	}
	if !cfg.AudioOnly || !cfg.SkipNoAudio || cfg.Quality != "144" || cfg.Views != "left" || cfg.AudioFormat != "mp3" {
		t.Fatalf("watch media defaults = audioOnly:%v skipNoAudio:%v quality:%q views:%q format:%q", cfg.AudioOnly, cfg.SkipNoAudio, cfg.Quality, cfg.Views, cfg.AudioFormat)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestWatchValidationRejectsInvalidShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*WatchConfig)
		wantErr string
	}{
		{name: "missing targets", mutate: func(w *WatchConfig) { w.Targets = nil }, wantErr: "watch.targets is required"},
		{name: "duplicate target", mutate: func(w *WatchConfig) { w.Targets = append(w.Targets, w.Targets[0]) }, wantErr: "duplicate"},
		{name: "short interval", mutate: func(w *WatchConfig) { w.PollInterval = time.Minute.String() }, wantErr: "between 5m and 24h"},
		{name: "budget", mutate: func(w *WatchConfig) { w.MaxLecturesPerCycle = -1 }, wantErr: "maxLecturesPerCycle"},
		{name: "retries", mutate: func(w *WatchConfig) { w.MaxRetries = -1 }, wantErr: "maxRetries"},
		{name: "quality", mutate: func(w *WatchConfig) { w.Quality = "1080" }, wantErr: "watch.quality"},
		{name: "views", mutate: func(w *WatchConfig) { w.Views = "sideways" }, wantErr: "watch.views"},
		{name: "format", mutate: func(w *WatchConfig) { w.AudioFormat = "wav" }, wantErr: "watch.audioFormat"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := minimalValidConfig()
			cfg.Watch = WatchConfig{
				Enabled: true, PollInterval: "5m", MaxLecturesPerCycle: 3, MaxRetries: 3,
				Quality: "144", Views: "left", AudioFormat: "mp3",
				Targets: []WatchTarget{{SubjectID: 67, SessionID: 8}},
			}
			cfg.ApplyDefaults()
			test.mutate(&cfg.Watch)
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Validate() error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func TestWatchEnvironmentOverridesContainNoProviderSettings(t *testing.T) {
	t.Setenv("IMPARTUS_WATCH_ENABLED", "true")
	t.Setenv("IMPARTUS_WATCH_POLL_INTERVAL", "10m")
	t.Setenv("IMPARTUS_WATCH_MAX_LECTURES_PER_CYCLE", "5")
	t.Setenv("IMPARTUS_WATCH_MAX_RETRIES", "4")
	t.Setenv("IMPARTUS_WATCH_QUALITY", "450")
	t.Setenv("IMPARTUS_WATCH_VIEWS", "second")
	t.Setenv("IMPARTUS_WATCH_AUDIO_FORMAT", "opus")

	cfg := minimalValidConfig()
	cfg.Watch.Targets = []WatchTarget{{SubjectID: 67, SessionID: 8}}
	if err := applyEnvOverrides(cfg); err != nil {
		t.Fatalf("applyEnvOverrides() error = %v", err)
	}
	cfg.ApplyDefaults()
	if !cfg.Watch.Enabled || cfg.Watch.PollInterval != "10m" || cfg.Watch.MaxLecturesPerCycle != 5 || cfg.Watch.MaxRetries != 4 {
		t.Fatalf("watch env overrides = %+v", cfg.Watch)
	}
	if cfg.Watch.Quality != "450" || cfg.Watch.Views != "right" || cfg.Watch.AudioFormat != "opus" {
		t.Fatalf("watch media env overrides = %+v", cfg.Watch)
	}
}
