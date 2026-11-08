package downloader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/retry"
	"github.com/rabesss/impartus-cli/internal/secrets"
)

// fetchDecryptionKey downloads and derives the AES key for a playlist. Each
// attempt waits on the API rate limiter; transient failures (5xx, 429,
// timeouts, connection resets) are retried under the downloader's retry
// policy, while 401 and other permanent failures stop at once.
func (d *Downloader) fetchDecryptionKey(ctx context.Context, keyURL string) ([]byte, error) {
	return d.fetchDecryptionKeyWithPolicy(ctx, keyURL, d.retry)
}

// playbackKeyRetryBudget caps retries of the playback key request: playback
// is interactive and has no progress UI, so it must not wait out the full
// download retry budget.
const playbackKeyRetryBudget = 15 * time.Second

// fetchPlaybackDecryptionKey fetches the key for interactive playback with
// the retry budget capped at playbackKeyRetryBudget.
func (d *Downloader) fetchPlaybackDecryptionKey(ctx context.Context, keyURL string) ([]byte, error) {
	policy := d.retry
	if policy.Budget > playbackKeyRetryBudget {
		policy.Budget = playbackKeyRetryBudget
	}
	return d.fetchDecryptionKeyWithPolicy(ctx, keyURL, policy)
}

func (d *Downloader) fetchDecryptionKeyWithPolicy(ctx context.Context, keyURL string, policy retry.Policy) ([]byte, error) {
	if d.mediaOriginErr != nil {
		return nil, d.mediaOriginErr
	}
	var key []byte
	policy.OnRetry = func(attempt int, delay time.Duration, err error) {
		d.logDiagnostic("retrying decryption key request after attempt %d in %s: %s", attempt, delay.Round(time.Millisecond), secrets.ScrubError(err))
	}
	attempts, err := policy.Do(ctx, func(ctx context.Context) error {
		var attemptErr error
		key, attemptErr = d.fetchDecryptionKeyOnce(ctx, keyURL)
		return attemptErr
	})
	if err != nil {
		if attempts > 1 && ctx.Err() == nil {
			return nil, fmt.Errorf("decryption key request failed after %d attempts: %w", attempts, err)
		}
		return nil, err
	}
	return key, nil
}

func (d *Downloader) fetchDecryptionKeyOnce(ctx context.Context, keyURL string) ([]byte, error) {
	if err := d.rateLimiter.WaitForAPI(ctx); err != nil {
		return nil, err
	}
	resp, err := d.client.GetAuthorizedWithTokenForOrigins(ctx, keyURL, d.config.Token, d.mediaOrigins...)
	if err != nil {
		return nil, err
	}
	defer func() { closeErr := resp.Body.Close(); _ = closeErr }()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("decryption key request failed with status %d: %w", resp.StatusCode, &client.AuthenticationError{
			Operation:  "decryption key",
			StatusCode: resp.StatusCode,
		})
	}
	if resp.StatusCode != http.StatusOK {
		return nil, retry.NewStatusError(resp, time.Now(), fmt.Errorf("decryption key request failed with status %d", resp.StatusCode))
	}
	keyURLContent, err := io.ReadAll(io.LimitReader(resp.Body, maxDecryptionKeyResponseSize+1))
	if err != nil {
		zeroKey(keyURLContent)
		return nil, fmt.Errorf("failed to read decryption key response: %w", secrets.SanitizeErrorWithToken(err, d.config.Token))
	}
	if int64(len(keyURLContent)) > maxDecryptionKeyResponseSize {
		zeroKey(keyURLContent)
		return nil, fmt.Errorf("decryption key response exceeds max size %d bytes", maxDecryptionKeyResponseSize)
	}
	derivedKey := deriveDecryptionKey(keyURLContent)
	zeroKey(keyURLContent) // zero raw key material after deriving
	return derivedKey, nil
}
