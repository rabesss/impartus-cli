//go:build !windows

package watch_test

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/notebooklm"
	"github.com/rabesss/impartus-cli/internal/watch"
)

func TestWatchPipelineEndToEnd(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	key := []byte("0123456789abcdef")
	encryptedChunk := encryptCBC(t, []byte("fake transport stream"), key)
	server := newFakeImpartusServer(t, key, encryptedChunk)
	defer server.Close()

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o750); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(binDir, "ffmpeg"), `#!/bin/sh
set -eu
last=""
for arg in "$@"; do last="$arg"; done
printf 'ID3fakeaudio' > "$last"
`)
	uploadLog := filepath.Join(dir, "notebooklm-argv.txt")
	uploadLogQuoted := "'" + strings.ReplaceAll(uploadLog, "'", "'\"'\"'") + "'"
	notebooklmCLI := filepath.Join(binDir, "notebooklm")
	writeExecutable(t, notebooklmCLI, fmt.Sprintf(`#!/bin/sh
set -eu
if [ "$1" = "source" ] && [ "$2" = "list" ]; then
  printf '%%s\n' '[]'
  exit 0
fi
printf '%%s\n' "$@" > %[1]s
printf '%%s\n' '{"source_id":"src-e2e","title":"from-fake"}'
`, uploadLogQuoted))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	cfg := &config.Config{
		Username:         "student@example.com",
		Password:         "password",
		BaseURL:          server.URL,
		DownloadLocation: filepath.Join(dir, "downloads"),
		TempDirLocation:  filepath.Join(dir, "temp"),
		Watch: config.WatchConfig{
			Enabled: true,
			Upload:  true,
			Targets: []config.WatchTarget{{
				SubjectID: 1, SessionID: 2, NotebookID: "nb-e2e",
			}},
		},
	}
	cfg.ApplyDefaults()
	cfg.ApplyWatchMediaDefaults()

	apiClient, err := client.NewLoggedIn(context.Background(), cfg)
	if err != nil {
		t.Fatalf("fake Impartus login: %v", err)
	}
	store, err := watch.LoadStore(filepath.Join(dir, "watch-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	uploader := notebooklm.New(notebooklm.Config{
		CLIPath: notebooklmCLI, NotebookID: "nb-e2e",
	})
	watcher := watch.NewFromDownloader(cfg, apiClient, uploader, store, watch.Options{
		Targets: cfg.ResolvedTargets(), Once: true, Upload: true, MaxRetries: 1,
	})

	first, err := watcher.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("first cycle: %v", err)
	}
	if first.New != 1 || first.Downloaded != 1 || first.Uploaded != 1 || first.Failed != 0 {
		t.Fatalf("unexpected first cycle: %+v", first)
	}
	if len(first.Outputs) != 1 {
		t.Fatalf("expected one audio output: %+v", first)
	}
	audio, err := os.ReadFile(first.Outputs[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(audio) != "ID3fakeaudio" {
		t.Fatalf("fake ffmpeg output = %q", audio)
	}
	argv, err := os.ReadFile(uploadLog)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"source\nadd",
		"--notebook\nnb-e2e",
		"--type\nfile",
		first.Outputs[0],
		"[impartus:1:2:101] LEC 001 Integration lecture",
	} {
		if !strings.Contains(string(argv), want) {
			t.Fatalf("NotebookLM argv missing %q:\n%s", want, argv)
		}
	}
	seen, ok := store.Get(1, 2, 101)
	if !ok || seen.Status != watch.StatusUploaded || seen.SourceID != "src-e2e" ||
		seen.UploadKey != "impartus:1:2:101" {
		t.Fatalf("durable state not uploaded: %+v ok=%v", seen, ok)
	}

	second, err := watcher.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("second cycle: %v", err)
	}
	if second.New != 0 || second.Downloaded != 0 || second.Uploaded != 0 || second.Skipped != 1 {
		t.Fatalf("second cycle was not a no-op: %+v", second)
	}
}

func newFakeImpartusServer(t *testing.T, key, encryptedChunk []byte) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/signin":
			if r.Method != http.MethodPost {
				http.Error(w, "method", http.StatusMethodNotAllowed)
				return
			}
			writeJSON(t, w, map[string]any{"success": true, "token": "test-token"})
		case "/subjects/1/lectures/2":
			requireBearer(t, r)
			writeJSON(t, w, []map[string]any{{
				"ttid": 101, "seqNo": 1, "topic": "Integration lecture",
				"subjectId": 1, "sessionId": 2,
			}})
		case "/fetchvideo":
			requireBearer(t, r)
			if r.URL.Query().Get("ttid") != "101" {
				http.Error(w, "bad ttid", http.StatusBadRequest)
				return
			}
			writeResponse(t, w, []byte(fmt.Sprintf("%s/256x144/index.m3u8\n", server.URL)))
		case "/256x144/index.m3u8":
			requireBearer(t, r)
			writeResponse(t, w, []byte("#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"/key\"\n#EXTINF:1,\n/chunk0.ts\n#EXT-X-ENDLIST\n"))
		case "/key":
			requireBearer(t, r)
			writeResponse(t, w, fakeKeyResponse(key))
		case "/chunk0.ts":
			requireBearer(t, r)
			writeResponse(t, w, encryptedChunk)
		default:
			http.NotFound(w, r)
		}
	}))
	return server
}

func writeResponse(t *testing.T, w http.ResponseWriter, data []byte) {
	t.Helper()
	if _, err := w.Write(data); err != nil {
		t.Errorf("write response: %v", err)
	}
}

func requireBearer(t *testing.T, r *http.Request) {
	t.Helper()
	if r.Header.Get("Authorization") != "Bearer test-token" {
		t.Errorf("missing bearer token on %s", r.URL.Path)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

func writeExecutable(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o700); err != nil {
		t.Fatal(err)
	}
}

func encryptCBC(t *testing.T, plaintext, key []byte) []byte {
	t.Helper()
	padding := aes.BlockSize - len(plaintext)%aes.BlockSize
	padded := append(append([]byte(nil), plaintext...), bytes.Repeat([]byte{byte(padding)}, padding)...)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, make([]byte, aes.BlockSize)).CryptBlocks(ciphertext, padded)
	return ciphertext
}

func fakeKeyResponse(key []byte) []byte {
	reversed := make([]byte, len(key))
	for i := range key {
		reversed[i] = key[len(key)-1-i]
	}
	return append([]byte{0, 0}, reversed...)
}
