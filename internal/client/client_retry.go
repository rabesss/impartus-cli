package client

import (
	"context"
	"fmt"

	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/retry"
)

// requestRetryPolicy returns the retry policy for stream info and playlist
// requests: jittered exponential backoff within cfg's retry budget.
func (c *Client) requestRetryPolicy(cfg *config.Config) retry.Policy {
	policy := retry.Default(cfg.RetryBudgetDuration())
	if c.retryClock != nil {
		policy.Clock = c.retryClock
	}
	if c.retryJitter != nil {
		policy.Jitter = c.retryJitter
	}
	return policy
}

// retryRequest runs one API request under policy. Transient failures (5xx,
// 429, timeouts, connection resets) are retried; 401, other 4xx, origin policy
// refusals and size limits fail at once. Each attempt closes its own response
// body before the next starts.
func retryRequest[T any](ctx context.Context, policy retry.Policy, operation string, op func(context.Context) (T, error)) (T, error) {
	var result T
	attempts, err := policy.Do(ctx, func(ctx context.Context) error {
		var attemptErr error
		result, attemptErr = op(ctx)
		return attemptErr
	})
	if err != nil {
		var zero T
		if attempts > 1 && ctx.Err() == nil {
			return zero, fmt.Errorf("%s request failed after %d attempts: %w", operation, attempts, err)
		}
		return zero, err
	}
	return result, nil
}
