package config

import (
	"strings"
	"testing"
	"time"
)

func TestRetryBudgetDefaultsToThreeMinutes(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.ApplyDefaults()
	if cfg.RetryBudget != "3m" {
		t.Fatalf("RetryBudget = %q, want 3m", cfg.RetryBudget)
	}
	if got := cfg.RetryBudgetDuration(); got != 3*time.Minute {
		t.Fatalf("RetryBudgetDuration() = %v, want 3m", got)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
}

func TestRetryBudgetExplicitZeroSurvivesDefaults(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.RetryBudget = "0s"
	cfg.ApplyDefaults()
	if cfg.RetryBudget != "0s" {
		t.Fatalf("RetryBudget = %q, want explicit 0s kept", cfg.RetryBudget)
	}
	if got := cfg.RetryBudgetDuration(); got != 0 {
		t.Fatalf("RetryBudgetDuration() = %v, want 0", got)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
}

func TestRetryBudgetEnvOverride(t *testing.T) {
	unsetAllConfigEnv(t)
	t.Setenv("IMPARTUS_RETRY_BUDGET", "45s")
	cfg := minimalValidConfig()
	cfg.RetryBudget = "5m"
	if err := applyEnvOverrides(cfg); err != nil {
		t.Fatalf("applyEnvOverrides() = %v", err)
	}
	cfg.ApplyDefaults()
	if got := cfg.RetryBudgetDuration(); got != 45*time.Second {
		t.Fatalf("RetryBudgetDuration() = %v, want 45s from env", got)
	}
}

func TestRetryBudgetValidation(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  string
	}{
		{"soon", "invalid retryBudget"},
		{"-1s", "retryBudget must be between 0s and 30m"},
		{"31m", "retryBudget must be between 0s and 30m"},
	} {
		t.Run(tc.value, func(t *testing.T) {
			cfg := minimalValidConfig()
			cfg.RetryBudget = tc.value
			cfg.ApplyDefaults()
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() = %v, want %q", err, tc.want)
			}
			if got := cfg.RetryBudgetDuration(); got != 3*time.Minute {
				t.Fatalf("RetryBudgetDuration() = %v, want 3m fallback for invalid value", got)
			}
		})
	}
	cfg := minimalValidConfig()
	cfg.RetryBudget = "30m"
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate(30m) = %v, want accepted", err)
	}
}
