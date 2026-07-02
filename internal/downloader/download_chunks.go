package downloader

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rabesss/impartus-cli/internal/secrets"
)

// doDownloadChunk performs a single HTTP download, writing to a file or reading to memory
// depending on the toMemory flag. It handles rate limiting, error status codes, and
// returns the file path (when toMemory=false) or data bytes (when toMemory=true).
func (d *Downloader) doDownloadChunk(ctx context.Context, url string, id int, chunk int, view string, toMemory bool) (string, []byte, int64, error) {
	if err := d.rateLimiter.WaitForDownload(ctx); err != nil {
		return "", nil, 0, err
	}

	resp, err := d.client.GetAuthorizedWithToken(ctx, url, d.config.Token)
	if err != nil {
		return "", nil, 0, fmt.Errorf("chunk request failed for URL %s: %w", secrets.RedactURL(url), err)
	}
	defer func() { closeErr := resp.Body.Close(); _ = closeErr }()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 512))
		if readErr != nil {
			return "", nil, 0, fmt.Errorf("chunk request failed with status %d and unreadable error body: %w", resp.StatusCode, readErr)
		}
		message := strings.TrimSpace(string(body))
		if message == "" {
			return "", nil, 0, fmt.Errorf("chunk request failed with status %d for URL %s", resp.StatusCode, secrets.RedactURL(url))
		}
		return "", nil, 0, fmt.Errorf("chunk request failed with status %d for URL %s: %s", resp.StatusCode, secrets.RedactURL(url), message)
	}

	outFilepath := filepath.Join(d.config.TempDirLocation, fmt.Sprintf("%d_%s_%04d.ts.temp", id, view, chunk))

	if toMemory {
		data, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return "", nil, 0, fmt.Errorf("could not read chunk %d: %w", chunk, readErr)
		}
		return outFilepath, data, int64(len(data)), nil
	}

	// G304: file paths are constructed from validated config and internal data
	// #nosec G304
	outFile, createErr := os.Create(outFilepath)
	if createErr != nil {
		return "", nil, 0, fmt.Errorf("could not create file for chunk %d: %w", chunk, createErr)
	}
	defer func() { closeErr := outFile.Close(); _ = closeErr }()

	bytesWritten, copyErr := io.Copy(outFile, resp.Body)
	if copyErr != nil {
		return "", nil, 0, fmt.Errorf("could not write chunk %d: %w", chunk, copyErr)
	}

	return outFilepath, nil, bytesWritten, nil
}

func (d *Downloader) downloadURL(ctx context.Context, url string, id int, chunk int, view string) (string, int64, error) {
	filePath, _, bytesWritten, err := d.doDownloadChunk(ctx, url, id, chunk, view, false)
	return filePath, bytesWritten, err
}

// downloadChunkWithRetry performs a download with exponential backoff retry logic.
// When toMemory is true, it returns data in the byte slice; otherwise it writes to a file.
func (d *Downloader) downloadChunkWithRetry(ctx context.Context, url string, id int, chunk int, view string, maxRetries int, tracker *ProgressTracker, toMemory bool) (string, []byte, error) {
	var lastErr error
	baseDelay := 1 * time.Second
	for attempt := 0; attempt < maxRetries; attempt++ {
		filePath, data, bytesDownloaded, err := d.doDownloadChunk(ctx, url, id, chunk, view, toMemory)
		if err == nil {
			if tracker != nil {
				ChunkCompleted(tracker, bytesDownloaded)
			}
			return filePath, data, nil
		}

		lastErr = err
		if attempt < maxRetries-1 {
			delay := retryDelay(baseDelay, attempt)
			if waitErr := waitForRetry(ctx, delay); waitErr != nil {
				return "", nil, waitErr
			}
		}
	}
	return "", nil, fmt.Errorf("failed after %d attempts: %w", maxRetries, lastErr)
}

func (d *Downloader) downloadWithRetry(ctx context.Context, url string, id int, chunk int, view string, maxRetries int, tracker *ProgressTracker) (string, error) {
	filePath, _, err := d.downloadChunkWithRetry(ctx, url, id, chunk, view, maxRetries, tracker, false)
	return filePath, err
}

func (d *Downloader) downloadBytesWithRetry(ctx context.Context, url string, id int, chunk int, view string, maxRetries int, tracker *ProgressTracker) (string, []byte, error) {
	return d.downloadChunkWithRetry(ctx, url, id, chunk, view, maxRetries, tracker, true)
}

func retryDelay(baseDelay time.Duration, attempt int) time.Duration {
	if attempt <= 0 {
		return baseDelay
	}
	if attempt >= 62 {
		attempt = 62
	}
	multiplier := int64(math.Pow(2, float64(attempt)))
	return time.Duration(int64(baseDelay) * multiplier)
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
