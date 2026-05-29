// Package downloader handles downloading, decrypting, and joining video lecture chunks.
package downloader

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

// DownloadedPlaylist holds the result of downloading a complete playlist.
type DownloadedPlaylist struct {
	FirstViewChunks  []string
	SecondViewChunks []string
	Playlist         client.ParsedPlaylist
}

// M3U8File represents temporary M3U8 manifest files for FFmpeg input.
type M3U8File struct {
	FirstViewFile  string
	SecondViewFile string
	Playlist       client.ParsedPlaylist
}

// JoinResult contains the output file paths from joining downloaded chunks.
type JoinResult struct {
	LeftOutput  string
	RightOutput string
	BothOutput  string
}

type viewConfig struct{ SkipView, Label string }

var (
	firstViewConfig  = viewConfig{SkipView: "right", Label: "left"}
	secondViewConfig = viewConfig{SkipView: "left", Label: "right"}
)

var sensitiveParams = map[string]bool{
	"access_token": true, "token": true, "sig": true, "signature": true,
	"secret": true, "key": true, "api_key": true, "auth": true,
}

func redactURL(rawURL string) string {
	if rawURL == "" {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	params := u.Query()
	for key := range params {
		for s := range sensitiveParams {
			if strings.EqualFold(key, s) {
				params.Set(key, "REDACTED")
				break
			}
		}
	}
	u.RawQuery = params.Encode()
	return u.String()
}

// Downloader orchestrates chunk downloading, decryption, and FFmpeg joining.
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

// FetchLecturePlaylists delegates to client.GetPlaylists.
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

// DownloadPlaylist downloads all chunks for both views of a playlist.
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
	if err := os.MkdirAll(d.config.TempDirLocation, 0o755); err != nil { // #nosec G301
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

// DownloadAndJoinPlaylist downloads a playlist and joins the chunks into final output file(s).
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

// JoinLectureOutput joins the chunks described by the M3U8 file into final output.
func (d *Downloader) JoinLectureOutput(ctx context.Context, file M3U8File) (JoinResult, error) {
	if d.config.AudioOnly {
		return d.joinAudioOutput(ctx, file)
	}
	return d.joinVideoOutput(ctx, file)
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

func (d *Downloader) downloadViewChunks(ctx context.Context, p *mpb.Progress, tracker *ProgressTracker, playlist client.ParsedPlaylist, urls []string, vc viewConfig, decryptionKey []byte) ([]string, int) {
	if len(urls) == 0 || d.config.Views == vc.SkipView {
		return nil, 0
	}
	bar := d.newViewBar(p, len(urls), playlist.SeqNo, vc.Label)
	chunks := make([]string, 0, len(urls))
	failed := 0
	for i, chunkURL := range urls {
		chunkPath, err := d.downloadAndDecryptChunk(ctx, chunkURL, playlist.ID, i, vc.Label, decryptionKey, tracker)
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

func (d *Downloader) downloadAndDecryptChunk(ctx context.Context, chunkURL string, playlistID, chunkIndex int, view string, decryptionKey []byte, tracker *ProgressTracker) (string, error) {
	filePath, err := d.downloadWithRetry(ctx, chunkURL, playlistID, chunkIndex, view, d.maxRetries, tracker)
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
	for i, chunkURL := range urls {
		if err := pipeline.SubmitDownload(ChunkTask{ChunkID: i, URL: chunkURL, View: view, LectureID: playlist.ID, LectureSeqNo: playlist.SeqNo}); err != nil {
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
				stats := pipeline.GetStats()
				processed := int64(sumPipelineStats(stats))
				if processed > int64(totalChunks) {
					processed = int64(totalChunks)
				}
				downloadBar.SetCurrent(processed)
			}
		}
	}()
	return monitorDone
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
