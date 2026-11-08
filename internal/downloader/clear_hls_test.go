package downloader

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
)

// clearHLSServer serves unencrypted segments and counts every request path.
func clearHLSServer(t *testing.T) (*httptest.Server, func() map[string]int) {
	t.Helper()
	var mu sync.Mutex
	requests := make(map[string]int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests[r.URL.Path]++
		mu.Unlock()
		switch r.URL.Path {
		case "/hls/seg0.ts":
			_, _ = io.WriteString(w, "segment-bytes-0") //nolint:errcheck
		case "/hls/seg1.ts":
			_, _ = io.WriteString(w, "segment-bytes-1") //nolint:errcheck
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, func() map[string]int {
		mu.Lock()
		defer mu.Unlock()
		copied := make(map[string]int, len(requests))
		for path, count := range requests {
			copied[path] = count
		}
		return copied
	}
}

func TestDownloadPlaylistCopiesClearSegments(t *testing.T) {
	for _, pipeline := range []bool{false, true} {
		name := "serial"
		if pipeline {
			name = "pipeline"
		}
		t.Run(name, func(t *testing.T) {
			server, requests := clearHLSServer(t)
			cfg := loadDownloadSpeedConfig(t, server.URL, t.TempDir())
			cfg.EnablePipeline = pipeline
			cfg.Views = "first"
			d := New(cfg, client.New(server.Client(), nil))

			playlist := client.ParsedPlaylist{
				ID:            7,
				SeqNo:         1,
				FirstViewURLs: []string{server.URL + "/hls/seg0.ts", server.URL + "/hls/seg1.ts"},
			}
			result, err := d.DownloadPlaylist(t.Context(), playlist, nil, nil)
			if err != nil {
				t.Fatalf("DownloadPlaylist(clear playlist) error = %v", err)
			}
			if len(result.FirstViewChunks) != 2 {
				t.Fatalf("first view chunks = %d, want 2", len(result.FirstViewChunks))
			}
			var joined bytes.Buffer
			for _, chunk := range result.FirstViewChunks {
				data, err := os.ReadFile(chunk) // #nosec G304 -- test temp path
				if err != nil {
					t.Fatalf("read chunk: %v", err)
				}
				joined.Write(data)
			}
			if got, want := joined.String(), "segment-bytes-0segment-bytes-1"; got != want {
				t.Fatalf("clear output = %q, want %q", got, want)
			}
			for path := range requests() {
				if !strings.HasPrefix(path, "/hls/seg") {
					t.Fatalf("unexpected request %q for a clear playlist", path)
				}
			}
		})
	}
}

func TestResumeClearPlaylistFetchesOnlyMissingChunks(t *testing.T) {
	forEachDownloadPath(t, func(t *testing.T, pipeline bool) {
		paths := []string{"/c0.ts", "/c1.ts", "/c2.ts"}
		h := newResumeHarness(t, pipeline, 3)
		h.server.clear = true
		playlist := h.server.playlist(paths...)
		playlist.KeyURL = ""

		h.server.fail["/c1.ts"] = http.StatusNotFound
		if _, err := h.run(t, playlist, nil); err == nil {
			t.Fatal("first run error = nil, want chunk 1 failure")
		}
		onlyResumeWorkspace(t, h.tempBase, 42)

		delete(h.server.fail, "/c1.ts")
		h.server.resetCounts()
		result, err := h.run(t, playlist, nil)
		if err != nil {
			t.Fatalf("second run error = %v", err)
		}
		for path, wantCount := range map[string]int{"/key": 0, "/c0.ts": 0, "/c1.ts": 1, "/c2.ts": 0} {
			if got := h.server.count(path); got != wantCount {
				t.Fatalf("second run %s requests = %d, want %d", path, got, wantCount)
			}
		}
		if got, want := readOutput(t, result), h.server.plaintext(paths...); !bytes.Equal(got, want) {
			t.Fatalf("resumed clear output = %q, want %q", got, want)
		}
		assertOnlyWorkspaceSentinel(t, h.tempBase)
	})
}

func TestStartPlaybackStreamServesClearSegments(t *testing.T) {
	server, requests := clearHLSServer(t)
	cfg := loadDownloadSpeedConfig(t, server.URL, t.TempDir())
	d := New(cfg, client.New(server.Client(), nil))
	playlist := client.ParsedPlaylist{
		FirstViewURLs:  []string{server.URL + "/hls/seg0.ts"},
		FirstDurations: []float64{10},
	}

	stream, err := d.StartPlaybackStream(t.Context(), playlist)
	if err != nil {
		t.Fatalf("StartPlaybackStream(clear playlist) error = %v", err)
	}
	defer stream.Cleanup()

	segmentURL := strings.TrimSuffix(stream.URL, "master.m3u8") + "segment/left/0"
	resp, err := http.Get(segmentURL) //nolint:noctx // local test server
	if err != nil {
		t.Fatalf("GET segment: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read segment: %v", err)
	}
	if resp.StatusCode != http.StatusOK || string(body) != "segment-bytes-0" {
		t.Fatalf("segment = %d %q, want 200 %q", resp.StatusCode, body, "segment-bytes-0")
	}
	for path := range requests() {
		if path != "/hls/seg0.ts" {
			t.Fatalf("unexpected request %q for a clear playlist", path)
		}
	}
}

func TestFetchDecryptionKeyRejectsMalformedKeyLength(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Two header bytes and no key material: derives to an empty key,
		// which must never be mistaken for a clear stream.
		_, _ = w.Write([]byte{0, 0}) //nolint:errcheck
	}))
	defer server.Close()
	cfg := loadDownloadSpeedConfig(t, server.URL, t.TempDir())
	d := New(cfg, client.New(server.Client(), nil))

	key, err := d.fetchDecryptionKey(t.Context(), server.URL+"/key")
	if err == nil {
		t.Fatalf("fetchDecryptionKey(empty key) = %v, nil; want an invalid key length error", key)
	}
	if !strings.Contains(err.Error(), "key length") {
		t.Fatalf("fetchDecryptionKey(empty key) error = %v, want a key length error", err)
	}

	stream, err := d.StartPlaybackStream(t.Context(), client.ParsedPlaylist{
		KeyURL:        server.URL + "/key",
		FirstViewURLs: []string{server.URL + "/seg.ts"},
	})
	if err == nil {
		stream.Cleanup()
		t.Fatal("StartPlaybackStream(empty key) error = nil, want an invalid key length error")
	}
}

func TestResumeClearMarkerKeepsAndDiscardsChunks(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "lecture-9-resume-0000000000000009")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	ident, err := os.Lstat(dir)
	if err != nil {
		t.Fatal(err)
	}
	chunk := filepath.Join(dir, "9_left_0000.ts")
	writeChunk := func() {
		t.Helper()
		if err := os.WriteFile(chunk, []byte("chunk"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	state := &resumeState{dir: dir, ident: ident, meta: resumeMeta{Version: resumeFormatVersion, Fingerprint: "f"}}

	if discarded, err := state.verifyClear(); err != nil || discarded {
		t.Fatalf("first verifyClear() = %v, %v; want false, nil", discarded, err)
	}
	writeChunk()
	if discarded, err := state.verifyClear(); err != nil || discarded {
		t.Fatalf("repeat verifyClear() = %v, %v; want false, nil", discarded, err)
	}
	if _, err := os.Stat(chunk); err != nil {
		t.Fatalf("clear chunk was discarded on a repeat clear download: %v", err)
	}
	if discarded, err := state.verifyKey([]byte("0123456789abcdef")); err != nil || !discarded {
		t.Fatalf("verifyKey() after clear = %v, %v; want true, nil", discarded, err)
	}
	if _, err := os.Stat(chunk); !os.IsNotExist(err) {
		t.Fatalf("clear chunk survived a switch to an encrypted key: %v", err)
	}
	writeChunk()
	if discarded, err := state.verifyClear(); err != nil || !discarded {
		t.Fatalf("verifyClear() after a key = %v, %v; want true, nil", discarded, err)
	}
	if _, err := os.Stat(chunk); !os.IsNotExist(err) {
		t.Fatalf("decrypted chunk survived a switch to a clear playlist: %v", err)
	}
}
