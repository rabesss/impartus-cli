package downloader

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

func TestPlayServerWorkflow(t *testing.T) {
	// 1. Define test key and plaintext
	testKey := []byte("0123456789abcdef")                   // 16 bytes AES key
	plaintext := []byte("hello world 1234hello world 1234") // 32 bytes (multiple of block size)

	// Pad with PKCS7 (16 bytes of value 16)
	paddedPlaintext := make([]byte, len(plaintext)+16)
	copy(paddedPlaintext, plaintext)
	for i := 0; i < 16; i++ {
		paddedPlaintext[len(plaintext)+i] = 16
	}

	// Encrypt using AES-CBC with zero IV
	block, err := aes.NewCipher(testKey)
	if err != nil {
		t.Fatalf("failed to create cipher: %v", err)
	}
	iv := make([]byte, 16)
	encrypter := cipher.NewCBCEncrypter(block, iv)
	ciphertext := make([]byte, len(paddedPlaintext))
	encrypter.CryptBlocks(ciphertext, paddedPlaintext)

	// 2. Setup mock HTTP server for the Impartus API
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "key") {
			// Return transformed key (2 header bytes + reversed key bytes)
			rev := make([]byte, len(testKey))
			for i := range testKey {
				rev[i] = testKey[len(testKey)-1-i]
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(append([]byte{0, 0}, rev...)) //nolint:errcheck
			return
		}
		if strings.Contains(r.URL.Path, "segment") {
			w.Header().Set("Content-Type", "video/MP2T")
			_, _ = w.Write(ciphertext) //nolint:errcheck
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	// 3. Create downloader with configured mock client
	cfg := &config.Config{
		Views: "both",
	}
	cfg.ApplyDefaults()

	apiClient := client.New(nil, nil)
	d := New(cfg, apiClient)

	// 4. Setup mock playlist structure pointing to the mock server URLs
	playlist := client.ParsedPlaylist{
		KeyURL:           ts.URL + "/key",
		FirstViewURLs:    []string{ts.URL + "/segment/left/0"},
		SecondViewURLs:   []string{ts.URL + "/segment/right/0"},
		HasMultipleViews: true,
	}

	ctx := context.Background()
	playURL, cleanup, err := d.StartPlayServer(ctx, playlist)
	if err != nil {
		t.Fatalf("StartPlayServer failed: %v", err)
	}
	defer cleanup()

	// 5. Test master.m3u8 endpoint
	resp, err := http.Get(playURL) //nolint:noctx // test client GET ok without ctx
	if err != nil {
		t.Fatalf("failed to fetch master playlist: %v", err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	masterContent, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read master: %v", err)
	}
	if !strings.Contains(string(masterContent), "left.m3u8") || !strings.Contains(string(masterContent), "right.m3u8") {
		t.Fatalf("master.m3u8 content invalid: %s", string(masterContent))
	}

	// 6. Test left.m3u8 endpoint
	// Extract left.m3u8 URL from master playlist
	lines := strings.Split(string(masterContent), "\n")
	var leftM3U8URL string
	for _, line := range lines {
		if strings.Contains(line, "left.m3u8") {
			leftM3U8URL = line
			break
		}
	}
	if leftM3U8URL == "" {
		t.Fatal("left.m3u8 URL not found in master playlist")
	}

	respLeft, err := http.Get(leftM3U8URL) //nolint:noctx // test client GET ok without ctx
	if err != nil {
		t.Fatalf("failed to fetch left playlist: %v", err)
	}
	defer func() { _ = respLeft.Body.Close() }() //nolint:errcheck

	leftContent, err := io.ReadAll(respLeft.Body)
	if err != nil {
		t.Fatalf("failed to read left playlist: %v", err)
	}
	if !strings.Contains(string(leftContent), "/segment/left/0") {
		t.Fatalf("left.m3u8 content invalid: %s", string(leftContent))
	}

	// 7. Test segment endpoint (requests segment and asserts decrypted output)
	linesLeft := strings.Split(string(leftContent), "\n")
	var segmentURL string
	for _, line := range linesLeft {
		if strings.Contains(line, "/segment/left/0") {
			segmentURL = line
			break
		}
	}
	if segmentURL == "" {
		t.Fatal("segment URL not found in left playlist")
	}

	respSeg, err := http.Get(segmentURL) //nolint:noctx // test client GET ok without ctx
	if err != nil {
		t.Fatalf("failed to fetch segment: %v", err)
	}
	defer func() { _ = respSeg.Body.Close() }() //nolint:errcheck

	if respSeg.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 for segment, got %d", respSeg.StatusCode)
	}

	segContent, err := io.ReadAll(respSeg.Body)
	if err != nil {
		t.Fatalf("failed to read segment: %v", err)
	}

	if string(segContent) != string(plaintext) {
		t.Fatalf("decrypted segment content mismatch. Got %q, want %q", string(segContent), string(plaintext))
	}
}
