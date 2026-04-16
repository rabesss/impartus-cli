package downloader

import (
	"bufio"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"fmt"
	"io"
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

type DownloadedPlaylist struct {
	FirstViewChunks  []string
	SecondViewChunks []string
	Playlist         client.ParsedPlaylist
}

type M3U8File struct {
	FirstViewFile  string
	SecondViewFile string
	Playlist       client.ParsedPlaylist
}

type JoinResult struct {
	LeftOutput  string
	RightOutput string
	BothOutput  string
}

type Downloader struct {
	config      *config.Config
	client      *client.Client
	rateLimiter *RateLimiter
	maxRetries  int
	ffmpegPath  string
}

func New(cfg *config.Config, apiClient *client.Client) *Downloader {
	if cfg == nil {
		cfg = &config.Config{}
	}
	cfg.ApplyDefaults()
	if apiClient == nil {
		apiClient = client.New(nil, nil)
	}
	return &Downloader{
		config:      cfg,
		client:      apiClient,
		rateLimiter: NewRateLimiterFromConfig(cfg),
		maxRetries:  3,
		ffmpegPath:  "ffmpeg",
	}
}

func (d *Downloader) FetchLecturePlaylists(ctx context.Context, lectures []client.Lecture) ([]client.ParsedPlaylist, error) {
	parsedPlaylists := make([]client.ParsedPlaylist, 0, len(lectures))
	for _, lecture := range lectures {
		streamInfos, err := d.getStreamInfos(ctx, lecture)
		if err != nil {
			return parsedPlaylists, err
		}

		streamURL := client.SelectStreamByQuality(streamInfos, d.config.Quality, d.config.AudioOnly)
		if streamURL == "" {
			continue
		}

		waitErr := d.rateLimiter.WaitForAPI(ctx)
		if waitErr != nil {
			return parsedPlaylists, waitErr
		}

		resp, err := d.client.GetAuthorizedWithToken(ctx, streamURL, d.config.Token)
		if err != nil {
			return parsedPlaylists, err
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			return parsedPlaylists, fmt.Errorf("fetch playlist for lecture %d: unexpected status %d", lecture.TTID, resp.StatusCode)
		}
		scanner := bufio.NewScanner(resp.Body)
		parsedPlaylists = append(parsedPlaylists, client.ParsePlaylist(scanner, lecture.TTID, lecture.Topic, lecture.SeqNo))
		_ = resp.Body.Close()
	}
	return parsedPlaylists, nil
}

func (d *Downloader) DownloadLecturePlaylists(ctx context.Context, playlists []client.ParsedPlaylist, p *mpb.Progress, tracker *ProgressTracker) ([]DownloadedPlaylist, error) {
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

func (d *Downloader) DownloadPlaylist(ctx context.Context, playlist client.ParsedPlaylist, p *mpb.Progress, tracker *ProgressTracker) (DownloadedPlaylist, error) {
	decryptionKey, err := d.fetchDecryptionKey(ctx, playlist.KeyURL)
	if err != nil {
		return DownloadedPlaylist{}, err
	}

	if d.config.EnablePipeline {
		return d.downloadPlaylistPipelined(ctx, playlist, decryptionKey, p, tracker)
	}

	downloadedPlaylist := DownloadedPlaylist{Playlist: playlist}
	downloadedPlaylist.FirstViewChunks = d.downloadViewChunks(ctx, p, tracker, playlist, playlist.FirstViewURLs, "right", "first", "left", decryptionKey)
	downloadedPlaylist.SecondViewChunks = d.downloadViewChunks(ctx, p, tracker, playlist, playlist.SecondViewURLs, "left", "second", "right", decryptionKey)

	return downloadedPlaylist, nil
}

func (d *Downloader) downloadPlaylistPipelined(_ context.Context, playlist client.ParsedPlaylist, decryptionKey []byte, p *mpb.Progress, tracker *ProgressTracker) (DownloadedPlaylist, error) {
	totalChunks := d.totalChunksForPlaylist(playlist)

	downloadedPlaylist := DownloadedPlaylist{Playlist: playlist}
	if totalChunks == 0 {
		return downloadedPlaylist, nil
	}

	pipeline := d.newLecturePipeline(playlist, decryptionKey, tracker)
	pipeline.Start()

	downloadBar := d.newPipelineBar(p, playlist.SeqNo, totalChunks)
	if err := d.submitPipelineTasks(pipeline, playlist); err != nil {
		return downloadedPlaylist, err
	}

	pipeline.FinishSubmission(totalChunks)
	monitorDone := d.monitorPipelineProgress(downloadBar, pipeline, totalChunks)

	result := pipeline.Collect()

	d.stopPipelineMonitor(monitorDone, downloadBar, totalChunks)

	if len(result.FailedChunks) > 0 {
		return downloadedPlaylist, fmt.Errorf("%d chunks failed to download: %v", len(result.FailedChunks), result.FailedChunks)
	}

	downloadedPlaylist.FirstViewChunks = result.FirstViewChunks
	downloadedPlaylist.SecondViewChunks = result.SecondViewChunks

	return downloadedPlaylist, nil
}

func (d *Downloader) DownloadAndJoinPlaylist(ctx context.Context, playlist client.ParsedPlaylist, p *mpb.Progress, tracker *ProgressTracker) (JoinResult, error) {
	downloadedPlaylist, err := d.DownloadPlaylist(ctx, playlist, p, tracker)
	if err != nil {
		return JoinResult{}, err
	}

	metadataFile, err := d.CreateTempM3U8File(downloadedPlaylist)
	if err != nil {
		return JoinResult{}, err
	}

	return d.JoinLectureOutput(metadataFile)
}

func (d *Downloader) JoinLectureOutput(file M3U8File) (JoinResult, error) {
	if d.config.AudioOnly {
		return d.joinAudioOutput(file)
	}
	return d.joinVideoOutput(file)
}

func (d *Downloader) CreateTempM3U8File(downloadedPlaylist DownloadedPlaylist) (M3U8File, error) {
	m3u8File := M3U8File{Playlist: downloadedPlaylist.Playlist}
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
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

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

	for _, chunk := range chunks {
		_, err = f.WriteString("#EXTINF:1\n")
		if err != nil {
			return err
		}
		_, err = f.WriteString("../" + chunk + "\n")
		if err != nil {
			return err
		}
	}

	_, err = f.WriteString("#EXT-X-ENDLIST")
	return err
}

func (d *Downloader) getStreamInfos(ctx context.Context, lecture client.Lecture) ([]client.StreamInfo, error) {
	if err := d.rateLimiter.WaitForAPI(ctx); err != nil {
		return nil, err
	}
	return d.client.GetStreamInfos(ctx, d.config.BaseURL, d.config.Token, lecture)
}

func (d *Downloader) fetchDecryptionKey(ctx context.Context, keyURL string) ([]byte, error) {
	if err := d.rateLimiter.WaitForAPI(ctx); err != nil {
		return nil, err
	}

	resp, err := d.client.GetAuthorizedWithToken(ctx, keyURL, d.config.Token)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	keyURLContent, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return getDecryptionKey(keyURLContent), nil
}

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
	if len(filePath) < 6 {
		return "", fmt.Errorf("invalid file path: %s", filePath)
	}
	if !strings.HasSuffix(filePath, ".temp") {
		return "", fmt.Errorf("invalid file path extension: %s", filePath)
	}
	if len(key) != 16 && len(key) != 24 && len(key) != 32 {
		return "", fmt.Errorf("invalid AES key length: %d", len(key))
	}

	outPath := strings.TrimSuffix(filePath, ".temp")
	infile, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read encrypted file %s: %w", filePath, err)
	}

	length := 16 - (len(infile) % 16)
	infile = append(infile, bytes.Repeat([]byte{byte(length)}, length)...)
	iv := bytes.Repeat([]byte{0}, 16)

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create AES cipher: %w", err)
	}

	mode := cipher.NewCBCDecrypter(block, iv)
	plainText := make([]byte, len(infile))
	mode.CryptBlocks(plainText, infile)

	if err := os.WriteFile(outPath, plainText, 0o600); err != nil {
		return "", fmt.Errorf("failed to write decrypted file %s: %w", outPath, err)
	}
	return outPath, nil
}

func (d *Downloader) downloadURL(ctx context.Context, url string, id int, chunk int, view string) (string, int64, error) {
	if err := d.rateLimiter.WaitForDownload(ctx); err != nil {
		return "", 0, err
	}

	resp, err := d.client.GetAuthorizedWithToken(ctx, url, d.config.Token)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	err = os.MkdirAll(d.config.TempDirLocation, 0o755)
	if err != nil {
		return "", 0, err
	}

	outFilepath := filepath.Join(d.config.TempDirLocation, fmt.Sprintf("%d_%s_%04d.ts.temp", id, view, chunk))
	outFile, err := os.Create(outFilepath)
	if err != nil {
		return "", 0, fmt.Errorf("could not create file for chunk %d: %w", chunk, err)
	}
	defer outFile.Close()

	bytesWritten, err := io.Copy(outFile, resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("could not write chunk %d: %w", chunk, err)
	}
	if err := outFile.Sync(); err != nil {
		return "", 0, fmt.Errorf("could not sync chunk %d: %w", chunk, err)
	}

	return outFilepath, bytesWritten, nil
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
			time.Sleep(delay)
		}
	}
	return "", fmt.Errorf("failed after %d attempts: %w", maxRetries, lastErr)
}

func (d *Downloader) downloadViewChunks(ctx context.Context, p *mpb.Progress, tracker *ProgressTracker, playlist client.ParsedPlaylist, urls []string, skipView, requestView, displayView string, decryptionKey []byte) []string {
	if len(urls) == 0 || d.config.Views == skipView {
		return nil
	}

	bar := d.newViewBar(p, len(urls), playlist.SeqNo, displayView)
	chunks := make([]string, 0, len(urls))
	for i, url := range urls {
		chunkPath, err := d.downloadAndDecryptChunk(ctx, url, playlist.ID, i, requestView, decryptionKey, tracker)
		if bar != nil {
			bar.Increment()
		}
		if err != nil || chunkPath == "" {
			continue
		}
		chunks = append(chunks, chunkPath)
	}

	return chunks
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

func (d *Downloader) newLecturePipeline(playlist client.ParsedPlaylist, decryptionKey []byte, tracker *ProgressTracker) *LecturePipeline {
	pipelineConfig := PipelineConfig{
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

func (d *Downloader) joinAudioOutput(file M3U8File) (JoinResult, error) {
	result := JoinResult{}
	left, err := d.joinIfPresent(file.FirstViewFile, d.config.Views != "right", fmt.Sprintf("LEC %03d %s LEFT VIEW.%s", file.Playlist.SeqNo, file.Playlist.Title, d.config.AudioFormat), func(path, name string) (string, error) {
		return d.JoinChunksFromM3U8AudioOnly(path, name, d.config.AudioFormat)
	})
	if err != nil {
		return result, err
	}
	right, err := d.joinIfPresent(file.SecondViewFile, d.config.Views != "left", fmt.Sprintf("LEC %03d %s RIGHT VIEW.%s", file.Playlist.SeqNo, file.Playlist.Title, d.config.AudioFormat), func(path, name string) (string, error) {
		return d.JoinChunksFromM3U8AudioOnly(path, name, d.config.AudioFormat)
	})
	if err != nil {
		return result, err
	}
	result.LeftOutput = left
	result.RightOutput = right
	if left != "" && right != "" && d.config.Views == "both" {
		both, joinErr := d.JoinViewsAudioOnly(left, right, fmt.Sprintf("LEC %03d %s", file.Playlist.SeqNo, file.Playlist.Title), d.config.AudioFormat)
		if joinErr != nil {
			return result, joinErr
		}
		result.BothOutput = both
	}
	return result, nil
}

func (d *Downloader) joinVideoOutput(file M3U8File) (JoinResult, error) {
	result := JoinResult{}
	left, err := d.joinIfPresent(file.FirstViewFile, d.config.Views != "right", fmt.Sprintf("LEC %03d %s LEFT VIEW.mp4", file.Playlist.SeqNo, file.Playlist.Title), d.JoinChunksFromM3U8)
	if err != nil {
		return result, err
	}
	right, err := d.joinIfPresent(file.SecondViewFile, d.config.Views != "left", fmt.Sprintf("LEC %03d %s RIGHT VIEW.mp4", file.Playlist.SeqNo, file.Playlist.Title), d.JoinChunksFromM3U8)
	if err != nil {
		return result, err
	}
	result.LeftOutput = left
	result.RightOutput = right
	if left != "" && right != "" && d.config.Views == "both" {
		both, joinErr := d.JoinViews(left, right, fmt.Sprintf("LEC %03d %s", file.Playlist.SeqNo, file.Playlist.Title))
		if joinErr != nil {
			return result, joinErr
		}
		result.BothOutput = both
	}
	return result, nil
}

func (d *Downloader) joinIfPresent(path string, enabled bool, title string, join func(string, string) (string, error)) (string, error) {
	if path == "" || !enabled {
		return "", nil
	}
	return join(path, title)
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
