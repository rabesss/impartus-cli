package downloader

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

func TestDownloadAndJoinPlaylistWorkspaceIsolation(t *testing.T) {
	for _, pipelineEnabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("pipeline=%t", pipelineEnabled), func(t *testing.T) {
			tempBase := t.TempDir()
			downloadDir := t.TempDir()
			writeWorkspaceSentinel(t, tempBase)
			ffmpegPath := writeWorkspaceFFmpegScript(t, "joined output")
			decryptionKey := []byte("1234567890123456")

			var chunkArrivals atomic.Int32
			releaseChunks := make(chan struct{})
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/key":
					if _, err := w.Write(fakeKeyResponse(decryptionKey)); err != nil {
						return
					}
				case "/chunk.ts":
					if chunkArrivals.Add(1) == 2 {
						close(releaseChunks)
					}
					select {
					case <-releaseChunks:
						if _, err := w.Write(make([]byte, 16)); err != nil {
							return
						}
					case <-r.Context().Done():
					}
				default:
					http.NotFound(w, r)
				}
			}))
			defer ts.Close()

			d := workspaceTestDownloader(tempBase, downloadDir, ts, ffmpegPath, pipelineEnabled)
			playlists := []client.ParsedPlaylist{
				{KeyURL: ts.URL + "/key", Title: "Concurrent A", ID: 42, SeqNo: 1, FirstViewURLs: []string{ts.URL + "/chunk.ts"}},
				{KeyURL: ts.URL + "/key", Title: "Concurrent B", ID: 42, SeqNo: 2, FirstViewURLs: []string{ts.URL + "/chunk.ts"}},
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			results := make([]JoinResult, len(playlists))
			errs := make([]error, len(playlists))
			var wg sync.WaitGroup
			for i := range playlists {
				wg.Add(1)
				go func(index int) {
					defer wg.Done()
					results[index], errs[index] = d.DownloadAndJoinPlaylist(ctx, playlists[index], nil, nil)
				}(i)
			}
			wg.Wait()

			manifestPaths := make([]string, len(results))
			for i, result := range results {
				if errs[i] != nil {
					t.Fatalf("DownloadAndJoinPlaylist(%d) error = %v", i, errs[i])
				}
				if result.LeftOutput == "" {
					t.Fatalf("DownloadAndJoinPlaylist(%d) returned no left output", i)
				}
				output, err := os.ReadFile(result.LeftOutput)
				if err != nil {
					t.Fatalf("ReadFile(output %d) failed: %v", i, err)
				}
				if string(output) != "joined output" {
					t.Fatalf("output %d = %q, want %q", i, output, "joined output")
				}
				manifest, err := os.ReadFile(result.LeftOutput + ".input")
				if err != nil {
					t.Fatalf("ReadFile(ffmpeg input %d) failed: %v", i, err)
				}
				manifestPaths[i] = string(manifest)
				assertPathInOwnedWorkspace(t, tempBase, manifestPaths[i])
				if _, err := os.Stat(manifestPaths[i]); !os.IsNotExist(err) {
					t.Fatalf("workspace manifest %q still exists after return (err=%v)", manifestPaths[i], err)
				}
			}
			if filepath.Dir(manifestPaths[0]) == filepath.Dir(manifestPaths[1]) {
				t.Fatalf("concurrent downloads shared workspace %q", filepath.Dir(manifestPaths[0]))
			}
			assertOnlyWorkspaceSentinel(t, tempBase)
		})
	}
}

func TestDownloadAndJoinPlaylistWorkspaceCleanupOnFailure(t *testing.T) {
	tests := []struct {
		name          string
		chunkStatus   int
		chunkBody     []byte
		cancelContext bool
		ffmpegFails   bool
	}{
		{name: "upstream chunk", chunkStatus: http.StatusServiceUnavailable},
		{name: "decryption", chunkStatus: http.StatusOK, chunkBody: make([]byte, 15)},
		{name: "ffmpeg", chunkStatus: http.StatusOK, chunkBody: make([]byte, 16), ffmpegFails: true},
		{name: "cancellation", chunkStatus: http.StatusOK, chunkBody: make([]byte, 16), cancelContext: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempBase := t.TempDir()
			downloadDir := t.TempDir()
			writeWorkspaceSentinel(t, tempBase)
			decryptionKey := []byte("1234567890123456")
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/key":
					if _, err := w.Write(fakeKeyResponse(decryptionKey)); err != nil {
						return
					}
				case "/chunk.ts":
					w.WriteHeader(tt.chunkStatus)
					if _, err := w.Write(tt.chunkBody); err != nil {
						return
					}
				default:
					http.NotFound(w, r)
				}
			}))
			defer ts.Close()

			ffmpegPath := writeWorkspaceFFmpegScript(t, "joined output")
			if tt.ffmpegFails {
				ffmpegPath = writeFailingFFmpegScript(t)
			}
			d := workspaceTestDownloader(tempBase, downloadDir, ts, ffmpegPath, false)
			ctx, cancel := context.WithCancel(context.Background())
			if tt.cancelContext {
				cancel()
			} else {
				defer cancel()
			}

			_, err := d.DownloadAndJoinPlaylist(ctx, client.ParsedPlaylist{
				KeyURL:        ts.URL + "/key",
				Title:         "Failure Lecture",
				ID:            77,
				SeqNo:         1,
				FirstViewURLs: []string{ts.URL + "/chunk.ts"},
			}, nil, nil)
			if err == nil {
				t.Fatal("DownloadAndJoinPlaylist() error = nil, want failure")
			}
			assertOnlyWorkspaceSentinel(t, tempBase)
		})
	}
}

func TestDownloadPlaylistPipelineCancelsContextAfterSuccess(t *testing.T) {
	tempBase := t.TempDir()
	decryptionKey := []byte("1234567890123456")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/key":
			if _, err := w.Write(fakeKeyResponse(decryptionKey)); err != nil {
				return
			}
		case "/chunk.ts":
			if _, err := w.Write(make([]byte, 16)); err != nil {
				return
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	d := workspaceTestDownloader(tempBase, t.TempDir(), ts, writeWorkspaceFFmpegScript(t, "unused"), true)
	var observedPipeline *LecturePipeline
	d.pipelineObserver = func(pipeline *LecturePipeline) {
		observedPipeline = pipeline
	}

	_, err := d.DownloadPlaylist(context.Background(), client.ParsedPlaylist{
		KeyURL:        ts.URL + "/key",
		ID:            88,
		SeqNo:         1,
		FirstViewURLs: []string{ts.URL + "/chunk.ts"},
	}, nil, nil)
	if err != nil {
		t.Fatalf("DownloadPlaylist() error = %v", err)
	}
	if observedPipeline == nil {
		t.Fatal("pipeline lifecycle was not observed")
	}
	select {
	case <-observedPipeline.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("pipeline context remained active after successful orchestration")
	}
}

func workspaceTestDownloader(tempBase, downloadDir string, ts *httptest.Server, ffmpegPath string, pipelineEnabled bool) *Downloader {
	return &Downloader{
		config: &config.Config{
			TempDirLocation:           tempBase,
			DownloadLocation:          downloadDir,
			Views:                     "left",
			Token:                     "token",
			EnablePipeline:            pipelineEnabled,
			DownloadWorkersPerLecture: 2,
			DecryptWorkersPerLecture:  1,
		},
		client:      client.New(ts.Client(), nil),
		rateLimiter: NewRateLimiter(1000.0, 1000.0, false),
		ffmpegPath:  ffmpegPath,
		maxRetries:  1,
	}
}

func writeWorkspaceFFmpegScript(t *testing.T, outputContent string) string {
	t.Helper()
	scriptPath := filepath.Join(t.TempDir(), "ffmpeg")
	script := fmt.Sprintf(`#!/usr/bin/env bash
set -eu
input=""
previous=""
last=""
for arg in "$@"; do
  if [[ "$previous" == "-i" && -z "$input" ]]; then
    input="$arg"
  fi
  previous="$arg"
  last="$arg"
done
printf '%%s' "$input" > "${last}.input"
printf '%%s' %q > "$last"
`, outputContent)
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(fake ffmpeg) failed: %v", err)
	}
	return scriptPath
}

func writeFailingFFmpegScript(t *testing.T) string {
	t.Helper()
	scriptPath := filepath.Join(t.TempDir(), "ffmpeg")
	if err := os.WriteFile(scriptPath, []byte("#!/usr/bin/env bash\nexit 42\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(failing ffmpeg) failed: %v", err)
	}
	return scriptPath
}

func writeWorkspaceSentinel(t *testing.T, tempBase string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(tempBase, "caller-owned.sentinel"), []byte("keep"), 0o600); err != nil {
		t.Fatalf("WriteFile(sentinel) failed: %v", err)
	}
}

func assertOnlyWorkspaceSentinel(t *testing.T, tempBase string) {
	t.Helper()
	entries, err := os.ReadDir(tempBase)
	if err != nil {
		t.Fatalf("ReadDir(temp base) failed: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "caller-owned.sentinel" {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("temp base entries = %v, want only caller-owned sentinel", names)
	}
}

func assertPathInOwnedWorkspace(t *testing.T, tempBase, path string) {
	t.Helper()
	rel, err := filepath.Rel(tempBase, path)
	if err != nil {
		t.Fatalf("Rel(%q, %q) failed: %v", tempBase, path, err)
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) < 2 || parts[0] == ".." || !strings.HasPrefix(parts[0], "lecture-42-") {
		t.Fatalf("path %q is not inside an owned lecture workspace under %q", path, tempBase)
	}
}
