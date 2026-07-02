package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func minimalValidConfig() *Config {
	return &Config{
		Username: "user",
		Password: "pass",
		BaseURL:  "https://example.com",
		Quality:  "450",
		Views:    "both",
	}
}

func TestApplyDefaultsAndValidateWithMinimalValidConfig(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.ApplyDefaults()

	if cfg.NumWorkers != 5 || cfg.AudioFormat != "mp3" || cfg.TempDirLocation != "./temp" {
		t.Fatalf("expected core defaults to be applied, got %+v", cfg)
	}
	if cfg.RateLimit != 100 || cfg.APIRateLimit != 2 || cfg.HTTPTimeout != "10m" {
		t.Fatalf("expected rate/time defaults to be applied, got %+v", cfg)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected minimal config to validate, got %v", err)
	}
}

func TestValidateRejectsInvalidViewsQualityAndMissingCredentials(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name: "invalid quality",
			mutate: func(cfg *Config) {
				cfg.Quality = "1080"
			},
			wantErr: "quality must be one of",
		},
		{
			name: "invalid views",
			mutate: func(cfg *Config) {
				cfg.Views = "sideways"
			},
			wantErr: "views must be one of",
		},
		{
			name: "missing credentials",
			mutate: func(cfg *Config) {
				cfg.Username = ""
			},
			wantErr: "username and password are required",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := minimalValidConfig()
			cfg.ApplyDefaults()
			tc.mutate(cfg)

			err := cfg.Validate()
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestLoadResolvedUsesEnvOverConfigFileWithDeterministicPrecedence(t *testing.T) {
	t.Setenv("IMPARTUS_USERNAME", "env-user")
	t.Setenv("IMPARTUS_PASSWORD", "env-pass")
	t.Setenv("IMPARTUS_BASE_URL", "https://env.example.com")
	t.Setenv("IMPARTUS_QUALITY", "720")
	t.Setenv("IMPARTUS_VIEWS", "first")
	t.Setenv("IMPARTUS_DOWNLOAD_LOCATION", "/tmp/env-downloads")
	t.Setenv("IMPARTUS_AUDIO_ONLY", "true")
	t.Setenv("IMPARTUS_AUDIO_FORMAT", "opus")

	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body, err := json.Marshal(Config{
		Username:         "file-user",
		Password:         "file-pass",
		BaseURL:          "https://file.example.com",
		Quality:          "450",
		Views:            "both",
		DownloadLocation: "/tmp/file-downloads",
		AudioOnly:        false,
		AudioFormat:      "mp3",
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	writeErr := os.WriteFile(path, body, 0o600)
	if writeErr != nil {
		t.Fatalf("WriteFile: %v", writeErr)
	}

	cfg, err := LoadResolved(path)
	if err != nil {
		t.Fatalf("LoadResolved returned error: %v", err)
	}

	if cfg.Username != "env-user" || cfg.Password != "env-pass" || cfg.BaseURL != "https://env.example.com" {
		t.Fatalf("expected env credentials and base URL to override file: %+v", cfg)
	}
	if cfg.Quality != "720" || cfg.Views != "left" {
		t.Fatalf("expected env quality/views to override file (views normalized): %+v", cfg)
	}
	if cfg.DownloadLocation != "/tmp/env-downloads" || !cfg.AudioOnly || cfg.AudioFormat != "opus" {
		t.Fatalf("expected env optional values to override file: %+v", cfg)
	}
}

func writeTempConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestLoadResolvedEnableJitterDefaultsTrueWhenOmitted(t *testing.T) {
	path := writeTempConfig(t, `{
		"username": "u", "password": "p", "baseUrl": "https://example.com",
		"quality": "450", "views": "both"
	}`)
	cfg, err := LoadResolved(path)
	if err != nil {
		t.Fatalf("LoadResolved: %v", err)
	}
	if !cfg.EnableJitter {
		t.Error("expected EnableJitter to default true when omitted from config")
	}
}

func TestLoadResolvedEnableJitterHonorsExplicitFalse(t *testing.T) {
	path := writeTempConfig(t, `{
		"username": "u", "password": "p", "baseUrl": "https://example.com",
		"quality": "450", "views": "both", "enableJitter": false
	}`)
	cfg, err := LoadResolved(path)
	if err != nil {
		t.Fatalf("LoadResolved: %v", err)
	}
	if cfg.EnableJitter {
		t.Error("expected EnableJitter to stay false when explicitly disabled in config")
	}
}

func TestLoadResolvedEnableJitterHonorsExplicitFalseLowercaseKey(t *testing.T) {
	// json.Unmarshal matches field tags case-insensitively, so "enablejitter"
	// (lowercase) parses as false. jsonKeyPresent must detect it the same way,
	// otherwise an explicit false would be silently overwritten to true.
	path := writeTempConfig(t, `{
		"username": "u", "password": "p", "baseUrl": "https://example.com",
		"quality": "450", "views": "both", "enablejitter": false
	}`)
	cfg, err := LoadResolved(path)
	if err != nil {
		t.Fatalf("LoadResolved: %v", err)
	}
	if cfg.EnableJitter {
		t.Error("expected EnableJitter to stay false when set via a lowercase key")
	}
}

func TestLoadResolvedEnableJitterEnvOverridesDefault(t *testing.T) {
	t.Setenv("IMPARTUS_ENABLE_JITTER", "false")
	path := writeTempConfig(t, `{
		"username": "u", "password": "p", "baseUrl": "https://example.com",
		"quality": "450", "views": "both"
	}`)
	cfg, err := LoadResolved(path)
	if err != nil {
		t.Fatalf("LoadResolved: %v", err)
	}
	if cfg.EnableJitter {
		t.Error("expected IMPARTUS_ENABLE_JITTER=false to disable jitter despite omitted config key")
	}
}

func TestLoadResolvedReturnsValidationErrorsBeforeRemoteWork(t *testing.T) {
	t.Setenv("IMPARTUS_USERNAME", "env-user")
	t.Setenv("IMPARTUS_PASSWORD", "env-pass")
	t.Setenv("IMPARTUS_BASE_URL", "https://env.example.com")
	t.Setenv("IMPARTUS_QUALITY", "450")
	t.Setenv("IMPARTUS_VIEWS", "sideways")

	_, err := LoadResolved("")
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "views must be one of") {
		t.Fatalf("expected views validation error, got %v", err)
	}
}

func TestNormalizeViewsMapsAliasesToCanonicalNames(t *testing.T) {
	cases := []struct {
		input, want string
	}{
		{"first", "left"},
		{"second", "right"},
		{"First", "left"},
		{"SECOND", "right"},
		{"both", "both"},
		{"left", "left"},
		{"right", "right"},
		{"  both  ", "both"},
	}
	for _, tc := range cases {
		got := NormalizeViews(tc.input)
		if got != tc.want {
			t.Errorf("NormalizeViews(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestOneOfMatchesCorrectly(t *testing.T) {
	if !OneOf("450", "144", "450", "720") {
		t.Error("expected 450 to be in set")
	}
	if OneOf("1080", "144", "450", "720") {
		t.Error("expected 1080 to not be in set")
	}
	if !OneOf("a", "a") {
		t.Error("expected single element match")
	}
}

func TestValidateRejectsOutOfRangeWorkersAndRates(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			"numWorkers too low",
			func(c *Config) { c.NumWorkers = 0 },
			"numWorkers must be between 1 and 50",
		},
		{
			"numWorkers too high",
			func(c *Config) { c.NumWorkers = 51 },
			"numWorkers must be between 1 and 50",
		},
		{
			"rateLimit too low",
			func(c *Config) { c.RateLimit = 0.01 },
			"rateLimit must be between",
		},
		{
			"apiRateLimit too high",
			func(c *Config) { c.APIRateLimit = 25 },
			"apiRateLimit must be between",
		},
		{
			"missing baseURL",
			func(c *Config) { c.BaseURL = "" },
			"baseUrl is required",
		},
		{
			"invalid audioFormat",
			func(c *Config) { c.AudioOnly = true; c.AudioFormat = "wav" },
			"audioFormat must be one of",
		},
		{
			"invalid httpTimeout",
			func(c *Config) { c.HTTPTimeout = "abc" },
			"invalid httpTimeout",
		},
		{
			"httpTimeout too short",
			func(c *Config) { c.HTTPTimeout = "1s" },
			"httpTimeout must be between",
		},
		{
			"downloadWorkers too high",
			func(c *Config) { c.DownloadWorkersPerLecture = 15 },
			"downloadWorkersPerLecture must be between",
		},
		{
			"decryptWorkers too low",
			func(c *Config) { c.DecryptWorkersPerLecture = 0 },
			"decryptWorkersPerLecture must be between",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := minimalValidConfig()
			cfg.ApplyDefaults()
			tc.mutate(cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestParseRejectsMalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{invalid"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := Parse(path)
	if err == nil {
		t.Fatal("expected parse error for malformed JSON")
	}
}

func TestLoadResolvedNoConfigFileUsesEnvOnly(t *testing.T) {
	t.Setenv("IMPARTUS_USERNAME", "env-user")
	t.Setenv("IMPARTUS_PASSWORD", "env-pass")
	t.Setenv("IMPARTUS_BASE_URL", "https://env.example.com")
	t.Setenv("IMPARTUS_QUALITY", "450")

	// Use temp dir as CWD so config.json is not found
	origDir, err := os.Getwd()
	if err != nil {
		t.Skipf("cannot get cwd: %v", err)
	}
	chdirErr := os.Chdir(t.TempDir())
	if chdirErr != nil {
		t.Skipf("cannot chdir: %v", chdirErr)
	}
	defer func() {
		if restoreErr := os.Chdir(origDir); restoreErr != nil {
			t.Fatalf("cannot restore cwd: %v", restoreErr)
		}
	}()

	cfg, err := LoadResolved("")
	if err != nil {
		t.Fatalf("LoadResolved: %v", err)
	}
	if cfg.Username != "env-user" {
		t.Errorf("expected env-user, got %s", cfg.Username)
	}
}

func TestProgressTrackingValidation(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.ApplyDefaults()
	cfg.ProgressTracking.Enabled = true
	cfg.ProgressTracking.UpdateInterval = "100ms"
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "updateInterval must be between") {
		t.Fatalf("expected interval validation error, got %v", err)
	}
}

func TestPipelineWarningForInefficientWorkers(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.ApplyDefaults()
	cfg.EnablePipeline = true
	cfg.DownloadWorkersPerLecture = 1
	cfg.DecryptWorkersPerLecture = 5
	// Should return nil error (just prints warning)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestNormalizeViews(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"first maps to left", "first", "left"},
		{"second maps to right", "second", "right"},
		{"both passthrough", "both", "both"},
		{"left passthrough", "left", "left"},
		{"right passthrough", "right", "right"},
		{"case insensitive", "First", "left"},
		{"trims whitespace", "  second  ", "right"},
		{"empty passthrough", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeViews(tt.in); got != tt.want {
				t.Errorf("NormalizeViews(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
