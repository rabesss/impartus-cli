package downloader

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

func TestDownloadPlaylistDefaultPipelineBoundsRequestsAndPreservesOrder(t *testing.T) {
	const (
		downloadWorkers = 3
		chunksPerView   = 6
	)

	key := []byte("0123456789abcdef")
	encryptedChunks := downloadSpeedEncryptedChunks(t, key, chunksPerView)
	var activeRequests atomic.Int64
	var maxActiveRequests atomic.Int64
	concurrentWave := make(chan struct{})
	var releaseWave sync.Once

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/key" {
			writeDownloadSpeedResponse(w, fakeKeyResponse(key))
			return
		}

		view, index, ok := parseBenchmarkChunkPath(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}

		active := activeRequests.Add(1)
		defer activeRequests.Add(-1)
		for {
			observed := maxActiveRequests.Load()
			if active <= observed || maxActiveRequests.CompareAndSwap(observed, active) {
				break
			}
		}
		if active == downloadWorkers {
			releaseWave.Do(func() { close(concurrentWave) })
		}

		select {
		case <-concurrentWave:
		case <-r.Context().Done():
			return
		case <-time.After(2 * time.Second):
			http.Error(w, "downloads did not overlap", http.StatusGatewayTimeout)
			return
		}

		writeDownloadSpeedResponse(w, encryptedChunks[fmt.Sprintf("%s/%d", view, index)])
	}))
	defer server.Close()

	tempDir := t.TempDir()
	cfg := loadDownloadSpeedConfig(t, server.URL, tempDir)
	if !cfg.EnablePipeline {
		t.Fatal("test precondition: omitted enablePipeline must resolve to true")
	}
	cfg.DownloadWorkersPerLecture = downloadWorkers
	cfg.DecryptWorkersPerLecture = 2

	d := New(cfg, client.New(server.Client(), nil))
	result, err := d.DownloadPlaylist(t.Context(), downloadSpeedPlaylist(server.URL, chunksPerView), nil, nil)
	if err != nil {
		t.Fatalf("DownloadPlaylist: %v", err)
	}

	if got := maxActiveRequests.Load(); got != downloadWorkers {
		t.Fatalf("maximum active chunk requests = %d, want exactly %d", got, downloadWorkers)
	}
	assertDownloadedView(t, result.FirstViewChunks, "first", chunksPerView)
	assertDownloadedView(t, result.SecondViewChunks, "second", chunksPerView)

	tempFiles, err := filepath.Glob(filepath.Join(tempDir, "*.ts.temp"))
	if err != nil {
		t.Fatalf("Glob encrypted temp files: %v", err)
	}
	if len(tempFiles) != 0 {
		t.Fatalf("pipeline left %d encrypted temp files; want none", len(tempFiles))
	}
}

func TestDownloadPlaylistDefaultPipelineHonorsCancellation(t *testing.T) {
	key := []byte("0123456789abcdef")
	requestStarted := make(chan struct{})
	var startedOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/key" {
			writeDownloadSpeedResponse(w, fakeKeyResponse(key))
			return
		}
		startedOnce.Do(func() { close(requestStarted) })
		<-r.Context().Done()
	}))
	defer server.Close()

	cfg := &config.Config{
		Token:                     "test-token",
		TempDirLocation:           t.TempDir(),
		Views:                     "left",
		EnablePipeline:            true,
		DownloadWorkersPerLecture: 3,
		DecryptWorkersPerLecture:  2,
		RateLimit:                 100,
		APIRateLimit:              20,
	}
	d := New(cfg, client.New(server.Client(), nil))
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := d.DownloadPlaylist(ctx, client.ParsedPlaylist{
			ID:            42,
			SeqNo:         1,
			KeyURL:        server.URL + "/key",
			FirstViewURLs: []string{server.URL + "/chunk?access_token=must-not-leak"},
		}, nil, nil)
		done <- err
	}()

	select {
	case <-requestStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("chunk request did not start")
	}
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("DownloadPlaylist returned nil error after cancellation")
		}
		if strings.Contains(err.Error(), "must-not-leak") {
			t.Fatalf("DownloadPlaylist cancellation error leaked URL secret: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("DownloadPlaylist did not return promptly after cancellation")
	}
}

func TestDownloadPlaylistPipelineUsesDownloaderRetryLimit(t *testing.T) {
	key := []byte("0123456789abcdef")
	var chunkRequests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/key" {
			writeDownloadSpeedResponse(w, fakeKeyResponse(key))
			return
		}
		chunkRequests.Add(1)
		http.Error(w, "temporary upstream failure", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cfg := &config.Config{
		Token:                     "test-token",
		TempDirLocation:           t.TempDir(),
		Views:                     "left",
		EnablePipeline:            true,
		DownloadWorkersPerLecture: 1,
		DecryptWorkersPerLecture:  1,
		RateLimit:                 100,
		APIRateLimit:              20,
	}
	d := New(cfg, client.New(server.Client(), nil))
	d.maxRetries = 1

	_, err := d.DownloadPlaylist(t.Context(), client.ParsedPlaylist{
		ID:            42,
		SeqNo:         1,
		KeyURL:        server.URL + "/key",
		FirstViewURLs: []string{server.URL + "/chunk"},
	}, nil, nil)
	if err == nil {
		t.Fatal("DownloadPlaylist returned nil error for HTTP 503")
	}
	if got := chunkRequests.Load(); got != 1 {
		t.Fatalf("chunk requests = %d, want 1 configured attempt", got)
	}
}

func TestDownloadPlaylistDefaultPipelineRejectsOversizedChunkBeforeReadingBody(t *testing.T) {
	key := []byte("0123456789abcdef")
	var chunkRequests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/key" {
			writeDownloadSpeedResponse(w, fakeKeyResponse(key))
			return
		}
		chunkRequests.Add(1)
		w.Header().Set("Content-Length", strconv.FormatInt(maxChunkSize+1, 10))
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer server.Close()

	tempDir := t.TempDir()
	cfg := &config.Config{
		Token:                     "test-token",
		TempDirLocation:           tempDir,
		Views:                     "left",
		EnablePipeline:            true,
		DownloadWorkersPerLecture: 1,
		DecryptWorkersPerLecture:  1,
		RateLimit:                 100,
		APIRateLimit:              20,
	}
	d := New(cfg, client.New(server.Client(), nil))
	d.maxRetries = 1
	ctx, cancel := context.WithTimeout(t.Context(), 750*time.Millisecond)
	defer cancel()

	started := time.Now()
	_, err := d.DownloadPlaylist(ctx, client.ParsedPlaylist{
		ID:            42,
		SeqNo:         1,
		KeyURL:        server.URL + "/key",
		FirstViewURLs: []string{server.URL + "/oversized"},
	}, nil, nil)
	elapsed := time.Since(started)
	if err == nil {
		t.Fatal("DownloadPlaylist returned nil error for oversized chunk")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("oversized chunk was read until the test deadline instead of rejected from Content-Length")
	}
	if elapsed >= 500*time.Millisecond {
		t.Fatalf("oversized chunk rejection took %v, want under 500ms", elapsed)
	}
	if got := chunkRequests.Load(); got != 1 {
		t.Fatalf("chunk requests = %d, want 1", got)
	}
	files, globErr := filepath.Glob(filepath.Join(tempDir, "*.ts*"))
	if globErr != nil {
		t.Fatalf("Glob chunk files: %v", globErr)
	}
	if len(files) != 0 {
		t.Fatalf("oversized response created chunk files: %v", files)
	}
}

func BenchmarkDownloadPlaylistLatency(b *testing.B) {
	const (
		chunksPerView = 6
		chunkLatency  = 10 * time.Millisecond
	)

	key := []byte("0123456789abcdef")
	encryptedChunks := downloadSpeedEncryptedChunks(b, key, chunksPerView)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/key" {
			writeDownloadSpeedResponse(w, fakeKeyResponse(key))
			return
		}
		view, index, ok := parseBenchmarkChunkPath(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}
		time.Sleep(chunkLatency)
		writeDownloadSpeedResponse(w, encryptedChunks[fmt.Sprintf("%s/%d", view, index)])
	}))
	defer server.Close()

	for _, tc := range []struct {
		name           string
		enablePipeline bool
	}{
		{name: "serial-explicit-opt-out", enablePipeline: false},
		{name: "pipeline-default", enablePipeline: true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			cfg := &config.Config{
				Token:                     "test-token",
				TempDirLocation:           b.TempDir(),
				Views:                     "both",
				EnablePipeline:            tc.enablePipeline,
				DownloadWorkersPerLecture: 3,
				DecryptWorkersPerLecture:  2,
				RateLimit:                 100,
				APIRateLimit:              20,
			}
			d := New(cfg, client.New(server.Client(), nil))
			playlist := downloadSpeedPlaylist(server.URL, chunksPerView)
			chunks := 2 * chunksPerView

			b.ResetTimer()
			started := time.Now()
			for i := 0; i < b.N; i++ {
				if _, err := d.DownloadPlaylist(b.Context(), playlist, nil, nil); err != nil {
					b.Fatalf("DownloadPlaylist: %v", err)
				}
			}
			elapsed := time.Since(started)
			b.ReportMetric(float64(b.N*chunks)/elapsed.Seconds(), "chunks/s")
		})
	}
}

func loadDownloadSpeedConfig(t *testing.T, serverURL, tempDir string) *config.Config {
	t.Helper()
	body := map[string]any{
		"username":                  "user",
		"password":                  "pass",
		"baseUrl":                   serverURL,
		"quality":                   "144",
		"views":                     "both",
		"tempDirLocation":           tempDir,
		"downloadLocation":          tempDir,
		"rateLimit":                 100,
		"apiRateLimit":              20,
		"downloadWorkersPerLecture": 3,
		"decryptWorkersPerLecture":  2,
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("Marshal config: %v", err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if writeErr := os.WriteFile(path, data, 0o600); writeErr != nil {
		t.Fatalf("WriteFile config: %v", writeErr)
	}
	cfg, err := config.LoadResolved(path)
	if err != nil {
		t.Fatalf("LoadResolved config: %v", err)
	}
	return cfg
}

func downloadSpeedPlaylist(serverURL string, chunksPerView int) client.ParsedPlaylist {
	playlist := client.ParsedPlaylist{ID: 42, SeqNo: 1, KeyURL: serverURL + "/key"}
	for i := 0; i < chunksPerView; i++ {
		playlist.FirstViewURLs = append(playlist.FirstViewURLs, fmt.Sprintf("%s/chunk/first/%d", serverURL, i))
		playlist.SecondViewURLs = append(playlist.SecondViewURLs, fmt.Sprintf("%s/chunk/second/%d", serverURL, i))
	}
	return playlist
}

func parseBenchmarkChunkPath(path string) (string, int, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 3 || parts[0] != "chunk" || (parts[1] != "first" && parts[1] != "second") {
		return "", 0, false
	}
	index, err := strconv.Atoi(parts[2])
	if err != nil || index < 0 {
		return "", 0, false
	}
	return parts[1], index, true
}

func assertDownloadedView(t *testing.T, paths []string, view string, wantChunks int) {
	t.Helper()
	if len(paths) != wantChunks {
		t.Fatalf("%s view chunks = %d, want %d", view, len(paths), wantChunks)
	}
	for i, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%s chunk %d): %v", view, i, err)
		}
		want := fmt.Sprintf("%s-%d", view, i)
		if string(data) != want {
			t.Fatalf("%s chunk %d content = %q, want %q", view, i, data, want)
		}
	}
}

func encryptDownloadSpeedChunk(tb testing.TB, plaintext, key []byte) []byte {
	tb.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		tb.Fatalf("NewCipher: %v", err)
	}
	padding := aes.BlockSize - len(plaintext)%aes.BlockSize
	padded := make([]byte, len(plaintext)+padding)
	copy(padded, plaintext)
	for i := len(plaintext); i < len(padded); i++ {
		padded[i] = byte(padding)
	}
	encrypted := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, make([]byte, aes.BlockSize)).CryptBlocks(encrypted, padded)
	return encrypted
}

func downloadSpeedEncryptedChunks(tb testing.TB, key []byte, chunksPerView int) map[string][]byte {
	tb.Helper()
	chunks := make(map[string][]byte, 2*chunksPerView)
	for _, view := range []string{"first", "second"} {
		for i := 0; i < chunksPerView; i++ {
			path := fmt.Sprintf("%s/%d", view, i)
			chunks[path] = encryptDownloadSpeedChunk(tb, []byte(fmt.Sprintf("%s-%d", view, i)), key)
		}
	}
	return chunks
}

func writeDownloadSpeedResponse(w http.ResponseWriter, data []byte) {
	if _, err := w.Write(data); err != nil {
		return
	}
}
