// Package downloader handles downloading, decrypting, and joining video lecture chunks.
package downloader

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

// DownloadedPlaylist holds the result of downloading a complete playlist including chunk paths for both views.
type DownloadedPlaylist struct {
	FirstViewChunks  []string
	SecondViewChunks []string
	Playlist         client.ParsedPlaylist
}

// M3U8File represents temporary M3U8 manifest files created for FFmpeg input.
type M3U8File struct {
	FirstViewFile  string
	SecondViewFile string
	Playlist       client.ParsedPlaylist
}

// JoinResult contains the output file paths produced by joining downloaded chunks.
type JoinResult struct {
	LeftOutput  string
	RightOutput string
	BothOutput  string
}

// viewConfig holds the view-specific parameters for downloading chunks.
// SkipView is the Views config value that means "skip this view entirely".
// Label is the human-readable name used in progress bars and file paths.
type viewConfig struct {
	SkipView string
	Label    string
}

var (
	firstViewConfig  = viewConfig{SkipView: "right", Label: "left"}
	secondViewConfig = viewConfig{SkipView: "left", Label: "right"}
)

// redactURL strips sensitive query parameters (tokens, secrets, keys) from a URL.
func redactURL(rawURL string) string {
	if rawURL == "" {
		return rawURL
	}
	// Find the query string start
	idx := strings.IndexByte(rawURL, '?')
	if idx < 0 {
		return rawURL
	}
	base := rawURL[:idx]
	query := rawURL[idx+1:]
	sensitiveParams := map[string]bool{
		"access_token": true,
		"token":        true,
		"sig":          true,
		"signature":    true,
		"secret":       true,
		"key":          true,
		"api_key":      true,
		"auth":         true,
	}
	parts := strings.Split(query, "&")
	redacted := make([]string, 0, len(parts))
	for _, part := range parts {
		if eqIdx := strings.IndexByte(part, '='); eqIdx > 0 {
			key := part[:eqIdx]
			if sensitiveParams[key] {
				redacted = append(redacted, key+"=REDACTED")
				continue
			}
		}
		redacted = append(redacted, part)
	}
	return base + "?" + strings.Join(redacted, "&")
}

// Downloader orchestrates chunk downloading, AES decryption, and FFmpeg-based joining of video lectures.
type Downloader struct {
	config        *config.Config
	client        *client.Client
	rateLimiter   *RateLimiter
	maxRetries    int
	ffmpegPath    string
	playlistSlots chan struct{}
}

// New creates a new Downloader with the given config and API client.
func New(cfg *config.Config, apiClient *client.Client) *Downloader {
	if cfg == nil {
		cfg = &config.Config{}
	}
	cfg.ApplyDefaults()
	if apiClient == nil {
		apiClient = client.New(nil, nil)
	}
	playlistSlots := make(chan struct{}, safeConcurrentPlaylists(cfg))
	return &Downloader{
		config:        cfg,
		client:        apiClient,
		rateLimiter:   NewRateLimiterFromConfig(cfg),
		maxRetries:    3,
		playlistSlots: playlistSlots,
		ffmpegPath:    "ffmpeg",
	}
}

// FetchLecturePlaylists delegates to client.GetPlaylists.
// Kept as a method on Downloader so callers don't need direct client access,
// enabling future caching/retry interception at this layer.
func (d *Downloader) FetchLecturePlaylists(ctx context.Context, lectures []client.Lecture) ([]client.ParsedPlaylist, error) {
	return d.client.GetPlaylists(ctx, d.config, lectures)
}

func (d *Downloader) downloadLecturePlaylists(ctx context.Context, playlists []client.ParsedPlaylist, p *mpb.Progress, tracker *ProgressTracker) ([]DownloadedPlaylist, error) {
	results := make([]DownloadedPlaylist, 0, len(playlists))
	for _, playlist := range playlists {
		downloadedPlaylist, err := d.DownloadPlaylist(ctx, playlist, p, tracker)
		if err != nil {
			return results, err
		}
		results = append(results, downloadedPlaylist)
	}
	return results, nil
}

func safeConcurrentPlaylists(cfg *config.Config) int {
	const browserObservedMediaBurst = 24
	if cfg.DownloadWorkersPerLecture <= 0 {
		return 1
	}
	limit := browserObservedMediaBurst / cfg.DownloadWorkersPerLecture
	if limit < 1 {
		limit = 1
	}
	if cfg.NumWorkers > 0 && cfg.NumWorkers < limit {
		return cfg.NumWorkers
	}
	return limit
}

func (d *Downloader) acquirePlaylistSlot(ctx context.Context) (func(), error) {
	if d.playlistSlots == nil {
		return func() {}, nil
	}
	select {
	case d.playlistSlots <- struct{}{}:
		return func() { <-d.playlistSlots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// DownloadPlaylist downloads all chunks for both views of a playlist and returns the result.
func (d *Downloader) DownloadPlaylist(ctx context.Context, playlist client.ParsedPlaylist, p *mpb.Progress, tracker *ProgressTracker) (DownloadedPlaylist, error) {
	releasePlaylistSlot, err := d.acquirePlaylistSlot(ctx)
	if err != nil {
		return DownloadedPlaylist{}, err
	}
	defer releasePlaylistSlot()

	decryptionKey, err := d.fetchDecryptionKey(ctx, playlist.KeyURL)
	if err != nil {
		return DownloadedPlaylist{}, err
	}
	//nolint:gosec // G301: 0755 is standard for user download directories
	if err := os.MkdirAll(d.config.TempDirLocation, 0o755); err != nil {
		return DownloadedPlaylist{}, err
	}

	if d.config.EnablePipeline {
		return d.downloadPlaylistPipelined(ctx, playlist, decryptionKey, p, tracker)
	}

	downloadedPlaylist := DownloadedPlaylist{Playlist: playlist}
	var firstFailed, secondFailed int
	downloadedPlaylist.FirstViewChunks, firstFailed = d.downloadViewChunks(ctx, p, tracker, playlist, playlist.FirstViewURLs, firstViewConfig, decryptionKey)
	downloadedPlaylist.SecondViewChunks, secondFailed = d.downloadViewChunks(ctx, p, tracker, playlist, playlist.SecondViewURLs, secondViewConfig, decryptionKey)
	if firstFailed > 0 || secondFailed > 0 {
		return downloadedPlaylist, fmt.Errorf("download incomplete: %d first-view and %d second-view chunks failed", firstFailed, secondFailed)
	}

	return downloadedPlaylist, nil
}

func (d *Downloader) downloadPlaylistPipelined(ctx context.Context, playlist client.ParsedPlaylist, decryptionKey []byte, p *mpb.Progress, tracker *ProgressTracker) (DownloadedPlaylist, error) {
	totalChunks := d.totalChunksForPlaylist(playlist)

	downloadedPlaylist := DownloadedPlaylist{Playlist: playlist}
	if totalChunks == 0 {
		return downloadedPlaylist, nil
	}

	pipeline := d.newLecturePipeline(ctx, playlist, decryptionKey, tracker)
	pipeline.Start()

	downloadBar := d.newPipelineBar(p, playlist.SeqNo, totalChunks)
	submitErrCh := make(chan error, 1)
	go func() {
		err := d.submitPipelineTasks(pipeline, playlist)
		if err != nil {
			pipeline.cancelPipeline()
		}
		pipeline.FinishSubmission(totalChunks)
		submitErrCh <- err
	}()

	monitorDone := d.monitorPipelineProgress(downloadBar, pipeline, totalChunks)
	result := pipeline.Collect()
	submitErr := <-submitErrCh

	d.stopPipelineMonitor(monitorDone, downloadBar, totalChunks)
	if submitErr != nil {
		return downloadedPlaylist, submitErr
	}

	if len(result.FailedChunks) > 0 {
		return downloadedPlaylist, fmt.Errorf("%d chunks failed to download: %v", len(result.FailedChunks), result.FailedChunks)
	}

	downloadedPlaylist.FirstViewChunks = result.FirstViewChunks
	downloadedPlaylist.SecondViewChunks = result.SecondViewChunks

	return downloadedPlaylist, nil
}

// DownloadAndJoinPlaylist downloads a playlist and then joins the chunks into the final output file(s).
func (d *Downloader) DownloadAndJoinPlaylist(ctx context.Context, playlist client.ParsedPlaylist, p *mpb.Progress, tracker *ProgressTracker) (JoinResult, error) {
	downloadedPlaylist, err := d.DownloadPlaylist(ctx, playlist, p, tracker)
	if err != nil {
		return JoinResult{}, err
	}

	metadataFile, err := d.CreateTempM3U8File(downloadedPlaylist)
	if err != nil {
		return JoinResult{}, err
	}

	return d.JoinLectureOutput(ctx, metadataFile)
}

// JoinLectureOutput joins the chunks described by the M3U8 file into final output, choosing audio or video mode based on config.
func (d *Downloader) JoinLectureOutput(ctx context.Context, file M3U8File) (JoinResult, error) {
	if d.config.AudioOnly {
		return d.joinAudioOutput(ctx, file)
	}
	return d.joinVideoOutput(ctx, file)
}

// CreateTempM3U8File writes temporary M3U8 manifest files for each view and returns the file references.
func (d *Downloader) CreateTempM3U8File(downloadedPlaylist DownloadedPlaylist) (M3U8File, error) {
	m3u8File := M3U8File{Playlist: downloadedPlaylist.Playlist}
	//nolint:gosec // G301: 0755 is standard for user download directories
	if err := os.MkdirAll(d.config.TempDirLocation, 0o755); err != nil {
		return m3u8File, err
	}

	if len(downloadedPlaylist.FirstViewChunks) > 0 {
		firstPath := fmt.Sprintf("%s/%d_first.m3u8", d.config.TempDirLocation, downloadedPlaylist.Playlist.ID)
		if err := writeM3U8File(firstPath, downloadedPlaylist.FirstViewChunks); err != nil {
			return m3u8File, err
		}
		m3u8File.FirstViewFile = firstPath
	}

	if len(downloadedPlaylist.SecondViewChunks) > 0 {
		secondPath := fmt.Sprintf("%s/%d_second.m3u8", d.config.TempDirLocation, downloadedPlaylist.Playlist.ID)
		if err := writeM3U8File(secondPath, downloadedPlaylist.SecondViewChunks); err != nil {
			return m3u8File, err
		}
		m3u8File.SecondViewFile = secondPath
	}

	return m3u8File, nil
}

func writeM3U8File(path string, chunks []string) error {
	//nolint:gosec // G304: file paths are constructed from validated config and internal data
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { closeErr := f.Close(); _ = closeErr }()

	_, err = f.WriteString(`#EXTM3U
#EXT-X-VERSION:3
#EXT-X-MEDIA-SEQUENCE:0
#EXT-X-ALLOW-CACHE:YES
#EXT-X-TARGETDURATION:11
#EXT-X-KEY:METHOD=NONE
	`)
	if err != nil {
		return err
	}

	manifestDir := filepath.Dir(path)
	for _, chunk := range chunks {
		_, err = f.WriteString("#EXTINF:1\n")
		if err != nil {
			return err
		}
		// If the chunk path is absolute or on a different volume, use it as-is;
		// otherwise compute a relative path from the manifest directory.
		chunkPath := chunk
		if !filepath.IsAbs(chunk) {
			if rel, relErr := filepath.Rel(manifestDir, chunk); relErr == nil {
				chunkPath = rel
			}
		}
		_, err = f.WriteString(chunkPath + "\n")
		if err != nil {
			return err
		}
	}

	_, err = f.WriteString("#EXT-X-ENDLIST")
	return err
}

func (d *Downloader) fetchDecryptionKey(ctx context.Context, keyURL string) ([]byte, error) {
	if err := d.rateLimiter.WaitForAPI(ctx); err != nil {
		return nil, err
	}

	resp, err := d.client.GetAuthorizedWithToken(ctx, keyURL, d.config.Token)
	if err != nil {
		return nil, err
	}
	defer func() { closeErr := resp.Body.Close(); _ = closeErr }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("decryption key request failed with status %d", resp.StatusCode)
	}

	keyURLContent, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return getDecryptionKey(keyURLContent), nil
}

// getDecryptionKey transforms the raw key from the upstream API into the
// actual AES decryption key. The upstream response includes a 2-byte header
// prefix followed by the key bytes in reversed order. This function strips
// the header and reverses the remaining bytes to recover the usable key.
func getDecryptionKey(encryptionKey []byte) []byte {
	if len(encryptionKey) < 2 {
		return encryptionKey
	}
	encryptionKey = encryptionKey[2:]
	for i, j := 0, len(encryptionKey)-1; i < j; i, j = i+1, j-1 {
		encryptionKey[i], encryptionKey[j] = encryptionKey[j], encryptionKey[i]
	}
	return encryptionKey
}

func (d *Downloader) decryptChunk(filePath string, key []byte) (string, error) {
	//nolint:gosec // G304: file paths are constructed from validated config and internal data
	infile, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read encrypted file %s: %w", filePath, err)
	}
	return d.decryptChunkBytes(filePath, infile, key)
}

func (d *Downloader) decryptChunkBytes(filePath string, infile []byte, key []byte) (string, error) {
	if len(filePath) < 6 {
		return "", fmt.Errorf("invalid file path: %s", filePath)
	}
	if !strings.HasSuffix(filePath, ".temp") {
		return "", fmt.Errorf("invalid file path extension: %s", filePath)
	}
	if len(key) != 16 && len(key) != 24 && len(key) != 32 {
		return "", fmt.Errorf("invalid AES key length: %d", len(key))
	}
	if len(infile) == 0 || len(infile)%aes.BlockSize != 0 {
		return "", fmt.Errorf("ciphertext length %d is not a multiple of block size %d", len(infile), aes.BlockSize)
	}

	outPath := strings.TrimSuffix(filePath, ".temp")
	iv := bytes.Repeat([]byte{0}, 16)

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create AES cipher: %w", err)
	}

	mode := cipher.NewCBCDecrypter(block, iv)
	plainText := make([]byte, len(infile))
	mode.CryptBlocks(plainText, infile)

	// Remove PKCS7 padding from plaintext
	plainText = removePKCS7Padding(plainText)

	//nolint:gosec // G703: path components are from validated config and sanitized input
	if err := os.WriteFile(outPath, plainText, 0o600); err != nil {
		return "", fmt.Errorf("failed to write decrypted file %s: %w", outPath, err)
	}
	return outPath, nil
}

// removePKCS7Padding strips PKCS7 padding from decrypted data.
// Returns the original slice if padding is invalid or absent.
func removePKCS7Padding(data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	paddingLen := int(data[len(data)-1])
	if paddingLen <= 0 || paddingLen > aes.BlockSize || paddingLen > len(data) {
		return data
	}
	// Verify all padding bytes match the expected value
	for i := len(data) - paddingLen; i < len(data); i++ {
		if data[i] != byte(paddingLen) {
			return data
		}
	}
	return data[:len(data)-paddingLen]
}

func (d *Downloader) downloadURL(ctx context.Context, url string, id int, chunk int, view string) (string, int64, error) {
	if err := d.rateLimiter.WaitForDownload(ctx); err != nil {
		return "", 0, err
	}

	resp, err := d.client.GetAuthorizedWithToken(ctx, url, d.config.Token)
	if err != nil {
		return "", 0, fmt.Errorf("chunk request failed for URL %s: %w", redactURL(url), err)
	}
	defer func() { closeErr := resp.Body.Close(); _ = closeErr }()
	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 512))
		if readErr != nil {
			return "", 0, fmt.Errorf("chunk request failed with status %d and unreadable error body: %w", resp.StatusCode, readErr)
		}
		message := strings.TrimSpace(string(body))
		if message == "" {
			return "", 0, fmt.Errorf("chunk request failed with status %d for URL %s", resp.StatusCode, redactURL(url))
		}
		return "", 0, fmt.Errorf("chunk request failed with status %d for URL %s: %s", resp.StatusCode, redactURL(url), message)
	}

	outFilepath := filepath.Join(d.config.TempDirLocation, fmt.Sprintf("%d_%s_%04d.ts.temp", id, view, chunk))
	//nolint:gosec // G304: file paths are constructed from validated config and internal data
	outFile, err := os.Create(outFilepath)
	if err != nil {
		return "", 0, fmt.Errorf("could not create file for chunk %d: %w", chunk, err)
	}
	defer func() { closeErr := outFile.Close(); _ = closeErr }()

	bytesWritten, err := io.Copy(outFile, resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("could not write chunk %d: %w", chunk, err)
	}

	return outFilepath, bytesWritten, nil
}

func (d *Downloader) downloadURLBytes(ctx context.Context, url string, id int, chunk int, view string) (string, []byte, int64, error) {
	if err := d.rateLimiter.WaitForDownload(ctx); err != nil {
		return "", nil, 0, err
	}

	resp, err := d.client.GetAuthorizedWithToken(ctx, url, d.config.Token)
	if err != nil {
		return "", nil, 0, fmt.Errorf("chunk request failed for URL %s: %w", redactURL(url), err)
	}
	defer func() { closeErr := resp.Body.Close(); _ = closeErr }()
	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 512))
		if readErr != nil {
			return "", nil, 0, fmt.Errorf("chunk request failed with status %d and unreadable error body: %w", resp.StatusCode, readErr)
		}
		message := strings.TrimSpace(string(body))
		if message == "" {
			return "", nil, 0, fmt.Errorf("chunk request failed with status %d for URL %s", resp.StatusCode, redactURL(url))
		}
		return "", nil, 0, fmt.Errorf("chunk request failed with status %d for URL %s: %s", resp.StatusCode, redactURL(url), message)
	}

	outFilepath := filepath.Join(d.config.TempDirLocation, fmt.Sprintf("%d_%s_%04d.ts.temp", id, view, chunk))
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, 0, fmt.Errorf("could not read chunk %d: %w", chunk, err)
	}
	return outFilepath, data, int64(len(data)), nil
}

func (d *Downloader) downloadBytesWithRetry(ctx context.Context, url string, id int, chunk int, view string, maxRetries int, tracker *ProgressTracker) (string, []byte, error) {
	var lastErr error
	baseDelay := 1 * time.Second
	for attempt := 0; attempt < maxRetries; attempt++ {
		filePath, data, bytesDownloaded, err := d.downloadURLBytes(ctx, url, id, chunk, view)
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
	var lastErr error
	baseDelay := 1 * time.Second
	for attempt := 0; attempt < maxRetries; attempt++ {
		filePath, bytesDownloaded, err := d.downloadURL(ctx, url, id, chunk, view)
		if err == nil {
			if tracker != nil {
				ChunkCompleted(tracker, bytesDownloaded)
			}
			return filePath, nil
		}

		lastErr = err
		if attempt < maxRetries-1 {
			delay := retryDelay(baseDelay, attempt)
			if waitErr := waitForRetry(ctx, delay); waitErr != nil {
				return "", waitErr
			}
		}
	}
	return "", fmt.Errorf("failed after %d attempts: %w", maxRetries, lastErr)
}

func (d *Downloader) downloadViewChunks(ctx context.Context, p *mpb.Progress, tracker *ProgressTracker, playlist client.ParsedPlaylist, urls []string, vc viewConfig, decryptionKey []byte) ([]string, int) {
	if len(urls) == 0 || d.config.Views == vc.SkipView {
		return nil, 0
	}

	bar := d.newViewBar(p, len(urls), playlist.SeqNo, vc.Label)
	chunks := make([]string, 0, len(urls))
	failed := 0
	for i, url := range urls {
		chunkPath, err := d.downloadAndDecryptChunk(ctx, url, playlist.ID, i, vc.Label, decryptionKey, tracker)
		if bar != nil {
			bar.Increment()
		}
		if err != nil || chunkPath == "" {
			log.Printf("chunk %d failed for %s view: %v", i, vc.Label, err)
			failed++
			continue
		}
		chunks = append(chunks, chunkPath)
	}

	return chunks, failed
}

func (d *Downloader) newViewBar(p *mpb.Progress, total, seqNo int, label string) *mpb.Bar {
	if p == nil {
		return nil
	}
	return p.AddBar(int64(total),
		mpb.PrependDecorators(
			decor.Name(fmt.Sprintf("Downloading Lec %03d %s view ", seqNo, label), decor.WCSyncWidth),
			decor.CountersNoUnit("%d / %d", decor.WCSyncWidth),
		),
		mpb.AppendDecorators(decor.Percentage(decor.WCSyncWidth)),
	)
}

func (d *Downloader) downloadAndDecryptChunk(ctx context.Context, url string, playlistID, chunkIndex int, view string, decryptionKey []byte, tracker *ProgressTracker) (string, error) {
	filePath, err := d.downloadWithRetry(ctx, url, playlistID, chunkIndex, view, d.maxRetries, tracker)
	if err != nil {
		return "", fmt.Errorf("download failed for chunk %d: %w", chunkIndex, err)
	}
	chunkPath, err := d.decryptChunk(filePath, decryptionKey)
	if err != nil {
		return "", fmt.Errorf("decrypt failed for chunk %d: %w", chunkIndex, err)
	}
	return chunkPath, nil
}

func sumPipelineStats(stats PipelineStats) int {
	return stats.FirstViewChunks + stats.SecondViewChunks + stats.FailedChunks
}

func (d *Downloader) totalChunksForPlaylist(playlist client.ParsedPlaylist) int {
	totalChunks := 0
	if d.config.Views != "right" {
		totalChunks += len(playlist.FirstViewURLs)
	}
	if d.config.Views != "left" {
		totalChunks += len(playlist.SecondViewURLs)
	}
	return totalChunks
}

func (d *Downloader) newLecturePipeline(ctx context.Context, playlist client.ParsedPlaylist, decryptionKey []byte, tracker *ProgressTracker) *LecturePipeline {
	pipelineConfig := PipelineConfig{
		Context:         ctx,
		DownloadWorkers: d.config.DownloadWorkersPerLecture,
		DecryptWorkers:  d.config.DecryptWorkersPerLecture,
		DecryptionKey:   decryptionKey,
		LectureID:       playlist.ID,
		LectureSeqNo:    playlist.SeqNo,
		ProgressTracker: tracker,
	}
	return NewLecturePipeline(pipelineConfig, d)
}

func (d *Downloader) newPipelineBar(p *mpb.Progress, seqNo, totalChunks int) *mpb.Bar {
	if p == nil {
		return nil
	}
	return p.AddBar(int64(totalChunks),
		mpb.PrependDecorators(
			decor.Name(fmt.Sprintf("Downloading Lec %03d ", seqNo), decor.WCSyncWidth),
			decor.CountersNoUnit("%d / %d", decor.WCSyncWidth),
		),
		mpb.AppendDecorators(decor.Percentage(decor.WCSyncWidth)),
	)
}

func (d *Downloader) submitPipelineTasks(pipeline *LecturePipeline, playlist client.ParsedPlaylist) error {
	if err := submitPipelineViewTasks(pipeline, playlist.FirstViewURLs, d.config.Views != "right", "first", playlist); err != nil {
		return err
	}
	return submitPipelineViewTasks(pipeline, playlist.SecondViewURLs, d.config.Views != "left", "second", playlist)
}

func submitPipelineViewTasks(pipeline *LecturePipeline, urls []string, enabled bool, view string, playlist client.ParsedPlaylist) error {
	if !enabled {
		return nil
	}
	for i, url := range urls {
		if err := pipeline.SubmitDownload(ChunkTask{ChunkID: i, URL: url, View: view, LectureID: playlist.ID, LectureSeqNo: playlist.SeqNo}); err != nil {
			return err
		}
	}
	return nil
}

func (d *Downloader) monitorPipelineProgress(downloadBar *mpb.Bar, pipeline *LecturePipeline, totalChunks int) chan struct{} {
	if downloadBar == nil {
		return nil
	}
	monitorDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-monitorDone:
				return
			case <-ticker.C:
				d.updatePipelineBar(downloadBar, pipeline, totalChunks)
			}
		}
	}()
	return monitorDone
}

func (d *Downloader) updatePipelineBar(downloadBar *mpb.Bar, pipeline *LecturePipeline, totalChunks int) {
	stats := pipeline.GetStats()
	processed := int64(sumPipelineStats(stats))
	if processed > int64(totalChunks) {
		processed = int64(totalChunks)
	}
	downloadBar.SetCurrent(processed)
}

func (d *Downloader) stopPipelineMonitor(monitorDone chan struct{}, downloadBar *mpb.Bar, totalChunks int) {
	if monitorDone == nil {
		return
	}
	close(monitorDone)
	downloadBar.SetCurrent(int64(totalChunks))
}

func (d *Downloader) joinAudioOutput(ctx context.Context, file M3U8File) (JoinResult, error) {
	result := JoinResult{}
	left, err := d.joinIfPresent(ctx, file.FirstViewFile, d.config.Views != "right", fmt.Sprintf("LEC %03d %s LEFT VIEW.%s", file.Playlist.SeqNo, file.Playlist.Title, d.config.AudioFormat), func(ctx context.Context, path, name string) (string, error) {
		return d.JoinChunksFromM3U8AudioOnly(ctx, path, name, d.config.AudioFormat)
	})
	if err != nil {
		return result, err
	}
	right, err := d.joinIfPresent(ctx, file.SecondViewFile, d.config.Views != "left", fmt.Sprintf("LEC %03d %s RIGHT VIEW.%s", file.Playlist.SeqNo, file.Playlist.Title, d.config.AudioFormat), func(ctx context.Context, path, name string) (string, error) {
		return d.JoinChunksFromM3U8AudioOnly(ctx, path, name, d.config.AudioFormat)
	})
	if err != nil {
		return result, err
	}
	result.LeftOutput = left
	result.RightOutput = right
	if left != "" && right != "" && d.config.Views == "both" {
		both, joinErr := d.CreateBothViewsAudioOutput(ctx, left, fmt.Sprintf("LEC %03d %s", file.Playlist.SeqNo, file.Playlist.Title), d.config.AudioFormat)
		if joinErr != nil {
			return result, joinErr
		}
		result.BothOutput = both
	}
	return result, nil
}

func (d *Downloader) joinVideoOutput(ctx context.Context, file M3U8File) (JoinResult, error) {
	result := JoinResult{}
	left, err := d.joinIfPresent(ctx, file.FirstViewFile, d.config.Views != "right", fmt.Sprintf("LEC %03d %s LEFT VIEW.mp4", file.Playlist.SeqNo, file.Playlist.Title), d.JoinChunksFromM3U8)
	if err != nil {
		return result, err
	}
	right, err := d.joinIfPresent(ctx, file.SecondViewFile, d.config.Views != "left", fmt.Sprintf("LEC %03d %s RIGHT VIEW.mp4", file.Playlist.SeqNo, file.Playlist.Title), d.JoinChunksFromM3U8)
	if err != nil {
		return result, err
	}
	result.LeftOutput = left
	result.RightOutput = right
	if left != "" && right != "" && d.config.Views == "both" {
		both, joinErr := d.JoinViews(ctx, left, right, fmt.Sprintf("LEC %03d %s", file.Playlist.SeqNo, file.Playlist.Title))
		if joinErr != nil {
			return result, joinErr
		}
		result.BothOutput = both
	}
	return result, nil
}

func (d *Downloader) joinIfPresent(ctx context.Context, path string, enabled bool, title string, join func(context.Context, string, string) (string, error)) (string, error) {
	if path == "" || !enabled {
		return "", nil
	}
	return join(ctx, path, title)
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

func validateFFmpegArgs(args ...string) error {
	for _, arg := range args {
		if strings.TrimSpace(arg) == "" {
			return errors.New("ffmpeg arguments must not be empty")
		}
		if strings.ContainsRune(arg, '\x00') {
			return errors.New("ffmpeg arguments must not contain null bytes")
		}
	}
	return nil
}
