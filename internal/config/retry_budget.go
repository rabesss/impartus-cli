package config

import (
	"fmt"
	"time"
)

// DefaultRetryBudget is the default per-request retry budget.
const DefaultRetryBudget = "3m"

const maxRetryBudget = 30 * time.Minute

func (c *Config) validateRetryBudget() error {
	if c.RetryBudget == "" {
		return nil
	}
	budget, err := time.ParseDuration(c.RetryBudget)
	if err != nil {
		return fmt.Errorf("invalid retryBudget: %w", err)
	}
	if budget < 0 || budget > maxRetryBudget {
		return fmt.Errorf("retryBudget must be between 0s and 30m, got %v", budget)
	}
	return nil
}

// RetryBudgetDuration returns the parsed retry budget. An empty or invalid
// value (which Validate rejects) falls back to the default.
func (c *Config) RetryBudgetDuration() time.Duration {
	value := c.RetryBudget
	if value == "" {
		value = DefaultRetryBudget
	}
	budget, err := time.ParseDuration(value)
	if err != nil || budget < 0 || budget > maxRetryBudget {
		budget, _ = time.ParseDuration(DefaultRetryBudget) //nolint:errcheck // constant
	}
	return budget
}
