package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

// sanitizeFilename strips path separators and applies filepath.Base to prevent
// path traversal when interpolating untrusted values (e.g. lecture.Topic) into filenames.
func sanitizeFilename(s string) string {
	s = strings.ReplaceAll(s, string(filepath.Separator), "_")
	s = strings.ReplaceAll(s, "/", "_")
	return filepath.Base(s)
}

// maxSlideSize caps a single generated slide PDF at 100 MiB. The streaming
// check remains necessary when upstream omits or misstates Content-Length.
const maxSlideSize int64 = 100 * 1024 * 1024

var errSlideSizeLimit = errors.New("slide exceeds size limit")

func downloadLectureSlide(ctx context.Context, c *client.Client, cfg *config.Config, lecture client.Lecture) error {
	return downloadLectureSlideWithLimit(ctx, c, cfg, lecture, maxSlideSize)
}

func downloadLectureSlideWithLimit(ctx context.Context, c *client.Client, cfg *config.Config, lecture client.Lecture, limit int64) error {
	if cfg.BaseURL == "" {
		return errors.New("baseUrl is required")
	}

	// G301: 0755 is standard for user download directories
	// #nosec G301
	if err := os.MkdirAll(cfg.DownloadLocation, 0o755); err != nil {
		return err
	}

	url := fmt.Sprintf("%s/videos/%d/auto-generated-pdf", cfg.BaseURL, lecture.VideoID)
	resp, err := c.GetAuthorizedWithToken(ctx, url, cfg.Token)
	if err != nil {
		return err
	}
	bodyClosed := false
	defer func() {
		if !bodyClosed {
			_ = resp.Body.Close() //nolint:errcheck
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 512))
		if readErr != nil {
			return fmt.Errorf("slide download failed for lecture %d with status %d and unreadable body: %w", lecture.SeqNo, resp.StatusCode, readErr)
		}
		return fmt.Errorf("slide download failed for lecture %d with status %d: %s", lecture.SeqNo, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if resp.ContentLength > limit {
		return fmt.Errorf("slide download exceeds max size %d bytes: %w", limit, errSlideSizeLimit)
	}

	filePath := filepath.Join(cfg.DownloadLocation, fmt.Sprintf("LEC %03d %s.pdf", lecture.SeqNo, sanitizeFilename(lecture.Topic)))
	// G304: file paths are constructed from validated config and internal data.
	// #nosec G304
	f, err := os.CreateTemp(cfg.DownloadLocation, ".slide-*.part")
	if err != nil {
		return err
	}
	partPath := f.Name()
	removePart := true
	defer func() {
		_ = f.Close() //nolint:errcheck
		if removePart {
			_ = os.Remove(partPath) //nolint:errcheck
		}
	}()

	written, copyErr := io.Copy(f, io.LimitReader(resp.Body, limit+1))
	if copyErr != nil {
		return fmt.Errorf("write slide download: %w", copyErr)
	}
	if written > limit {
		return fmt.Errorf("slide download exceeds max size %d bytes: %w", limit, errSlideSizeLimit)
	}
	if closeErr := f.Close(); closeErr != nil {
		return fmt.Errorf("close slide download: %w", closeErr)
	}
	if closeErr := resp.Body.Close(); closeErr != nil {
		return fmt.Errorf("close slide response: %w", closeErr)
	}
	bodyClosed = true
	if finalizeErr := finalizeSlideDownload(partPath, filePath); finalizeErr != nil {
		return finalizeErr
	}
	removePart = false
	return nil
}

func finalizeSlideDownload(partPath, filePath string) error {
	mode := os.FileMode(0o644)
	info, err := os.Stat(filePath)
	if err == nil {
		mode = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect existing slide download: %w", err)
	}
	if err := os.Chmod(partPath, mode); err != nil {
		return fmt.Errorf("set slide download permissions: %w", err)
	}
	if err := os.Rename(partPath, filePath); err != nil {
		return fmt.Errorf("finalize slide download: %w", err)
	}
	return nil
}
