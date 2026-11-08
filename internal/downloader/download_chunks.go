package downloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/retry"
	"github.com/rabesss/impartus-cli/internal/secrets"
)

// maxChunkSize caps every single-chunk response, whether streamed to disk or
// read into memory. It mirrors the play server's maxSegmentSize limit (50 MiB).
const maxChunkSize = 50 * 1024 * 1024 // 50 MB

var errDownloadSizeLimit = errors.New("download exceeds size limit")

const chunkErrorBodyLimit int64 = 512

const chunkErrorBodyOmitted = client.UpstreamErrorBodyOmitted

func copyWithLimit(dst io.Writer, src io.Reader, limit int64) (int64, error) {
	written, err := io.Copy(dst, io.LimitReader(src, limit+1))
	if err != nil {
		return written, err
	}
	if written > limit {
		return written, errDownloadSizeLimit
	}
	return written, nil
}

// doDownloadChunk performs a single HTTP download, writing to a file or reading to memory
// depending on the toMemory flag. It handles rate limiting, error status codes, and
// returns the file path (when toMemory=false) or data bytes (when toMemory=true).
// chunkPath is the decrypted chunk file for one view's chunk in the current
// workspace. The encrypted download uses it with a ".temp" suffix.
func (d *Downloader) chunkPath(id int, view string, chunk int) string {
	return filepath.Join(d.config.TempDirLocation, fmt.Sprintf("%d_%s_%04d.ts", id, view, chunk))
}

func (d *Downloader) doDownloadChunk(ctx context.Context, url string, id int, chunk int, view string, toMemory bool) (string, []byte, int64, error) {
	return d.doDownloadChunkWithLimit(ctx, url, id, chunk, view, toMemory, maxChunkSize)
}

//nolint:gocyclo // Keep response, size and staging-file checks in one flow.
func (d *Downloader) doDownloadChunkWithLimit(ctx context.Context, url string, id int, chunk int, view string, toMemory bool, limit int64) (string, []byte, int64, error) {
	if d.mediaOriginErr != nil {
		return "", nil, 0, d.mediaOriginErr
	}
	if err := d.rateLimiter.WaitForDownload(ctx); err != nil {
		return "", nil, 0, err
	}

	resp, err := d.client.GetAuthorizedWithMediaPolicy(ctx, url, d.config.Token, d.mediaPolicy())
	if err != nil {
		if strictErr := strictOriginRefusal(err); strictErr != nil {
			// Nothing was sent. Report only the refused origin, never the
			// chunk URL, whose path or query may carry signing material.
			return "", nil, 0, fmt.Errorf("chunk %d request refused: %w", chunk, strictErr)
		}
		return "", nil, 0, fmt.Errorf("chunk request failed for URL %s: %w", secrets.RedactURLWithToken(url, d.config.Token), err)
	}
	defer func() { closeErr := resp.Body.Close(); _ = closeErr }()

	if statusErr := chunkResponseError(resp, url, d.config.Token); statusErr != nil {
		return "", nil, 0, statusErr
	}

	outFilepath := d.chunkPath(id, view, chunk) + ".temp"
	if resp.ContentLength > limit {
		return "", nil, 0, fmt.Errorf("chunk %d exceeds max size %d bytes: %w", chunk, limit, errDownloadSizeLimit)
	}

	if toMemory {
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, limit+1))
		if readErr != nil {
			return "", nil, 0, fmt.Errorf("could not read chunk %d: %w", chunk, secrets.SanitizeErrorWithToken(readErr, d.config.Token))
		}
		if int64(len(data)) > limit {
			return "", nil, 0, fmt.Errorf("chunk %d exceeds max size %d bytes: %w", chunk, limit, errDownloadSizeLimit)
		}
		return outFilepath, data, int64(len(data)), nil
	}
	// A resumable workspace may hold an earlier attempt's file here; replace
	// it with a new file rather than writing through whatever is there.
	if removeErr := os.Remove(outFilepath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return "", nil, 0, fmt.Errorf("could not replace file for chunk %d: %w", chunk, removeErr)
	}
	// G304: file paths are constructed from validated config and internal data
	// #nosec G304
	outFile, createErr := os.OpenFile(outFilepath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if createErr != nil {
		return "", nil, 0, fmt.Errorf("could not create file for chunk %d: %w", chunk, createErr)
	}
	removePartial := true
	defer func() {
		_ = outFile.Close() //nolint:errcheck
		if removePartial {
			_ = os.Remove(outFilepath) //nolint:errcheck
		}
	}()

	bytesWritten, copyErr := copyWithLimit(outFile, resp.Body, limit)
	if copyErr != nil {
		if errors.Is(copyErr, errDownloadSizeLimit) {
			return "", nil, 0, fmt.Errorf("chunk %d exceeds max size %d bytes: %w", chunk, limit, copyErr)
		}
		return "", nil, 0, fmt.Errorf("could not write chunk %d: %w", chunk, secrets.SanitizeErrorWithToken(copyErr, d.config.Token))
	}
	if closeErr := outFile.Close(); closeErr != nil {
		return "", nil, 0, fmt.Errorf("could not close chunk %d: %w", chunk, closeErr)
	}
	removePartial = false

	return outFilepath, nil, bytesWritten, nil
}

func chunkResponseError(resp *http.Response, url, token string) error {
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("chunk request failed with status %d for URL %s: %w", resp.StatusCode, secrets.RedactURLWithToken(url, token), &client.AuthenticationError{
			Operation:  "chunk",
			StatusCode: resp.StatusCode,
		})
	}
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	return retry.NewStatusError(resp, time.Now(), chunkStatusError(resp, url, token))
}

func chunkStatusError(resp *http.Response, url, token string) error {
	message, readErr := client.ReadSanitizedErrorBody(resp.Body, chunkErrorBodyLimit, token)
	if readErr != nil {
		return fmt.Errorf("chunk request failed with status %d and unreadable error body: %w", resp.StatusCode, readErr)
	}
	if message == "" {
		return fmt.Errorf("chunk request failed with status %d for URL %s", resp.StatusCode, secrets.RedactURLWithToken(url, token))
	}
	return fmt.Errorf("chunk request failed with status %d for URL %s: %s", resp.StatusCode, secrets.RedactURLWithToken(url, token), message)
}

func (d *Downloader) downloadURL(ctx context.Context, url string, id int, chunk int, view string) (string, int64, error) {
	filePath, _, bytesWritten, err := d.doDownloadChunk(ctx, url, id, chunk, view, false)
	return filePath, bytesWritten, err
}

// downloadChunkWithRetry downloads one chunk under the downloader's retry
// policy: transient failures (5xx including 516, 429, timeouts, connection
// resets) are retried with jittered exponential backoff within the retry
// budget, and permanent failures (401, other 4xx, origin policy, size limits)
// fail at once. When toMemory is true, it returns data in the byte slice;
// otherwise it writes to a file.
func (d *Downloader) downloadChunkWithRetry(ctx context.Context, url string, id int, chunk int, view string, tracker *ProgressTracker, toMemory bool) (string, []byte, error) {
	var filePath string
	var data []byte
	var bytesDownloaded int64
	policy := d.retry
	policy.OnRetry = func(attempt int, delay time.Duration, err error) {
		d.logDiagnostic("retrying %s view chunk %d after attempt %d in %s: %s", view, chunk, attempt, delay.Round(time.Millisecond), secrets.ScrubError(err))
	}
	attempts, err := policy.Do(ctx, func(ctx context.Context) error {
		var attemptErr error
		filePath, data, bytesDownloaded, attemptErr = d.doDownloadChunk(ctx, url, id, chunk, view, toMemory)
		return attemptErr
	})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
			return "", nil, err
		}
		return "", nil, fmt.Errorf("failed after %d attempts: %w", attempts, err)
	}
	if tracker != nil {
		ChunkCompleted(tracker, bytesDownloaded)
	}
	return filePath, data, nil
}

func (d *Downloader) downloadWithRetry(ctx context.Context, url string, id int, chunk int, view string, tracker *ProgressTracker) (string, error) {
	filePath, _, err := d.downloadChunkWithRetry(ctx, url, id, chunk, view, tracker, false)
	return filePath, err
}

func (d *Downloader) downloadBytesWithRetry(ctx context.Context, url string, id int, chunk int, view string, tracker *ProgressTracker) (string, []byte, error) {
	return d.downloadChunkWithRetry(ctx, url, id, chunk, view, tracker, true)
}
