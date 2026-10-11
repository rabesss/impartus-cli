package downloader

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

// This fixture is overlaid unchanged into both commits by scripts/performance/run.py.
// Resetting the caller buffer and checking the full plaintext are measured too.
func BenchmarkPerformanceDecrypt(b *testing.B) {
	for _, fixture := range []struct {
		size int
		hash string
	}{
		{65536, "b1db917024b8e890310d1486f0a8445d6a332110f0211c7d8cb258a3c122b526"},
		{1048576, "88198f2837ba0ec2c86d75e9b561a102c20ebc72125001292de1839c525106f6"},
	} {
		b.Run(fmt.Sprintf("bytes-%d", fixture.size), func(b *testing.B) {
			key := []byte("0123456789abcdef")
			plain := make([]byte, fixture.size)
			for i := range plain {
				plain[i] = byte((i*73 + i/17) % 251)
			}
			decoded, err := hex.DecodeString(fixture.hash)
			if err != nil {
				b.Fatal(err)
			}
			var want [sha256.Size]byte
			copy(want[:], decoded)
			if sha256.Sum256(plain) != want {
				b.Fatal("plaintext fixture changed")
			}
			ciphertext := performanceEncrypt(b, plain, key)
			buffer := make([]byte, len(ciphertext))
			b.SetBytes(int64(fixture.size))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				copy(buffer, ciphertext)
				got, err := DecryptAESInPlace(buffer, key)
				if err != nil || len(got) != fixture.size || sha256.Sum256(got) != want {
					b.Fatalf("decryption changed or omitted work: bytes=%d err=%v", len(got), err)
				}
			}
			b.ReportMetric(float64(fixture.size), "plaintext-bytes/op")
		})
	}
}

func TestPerformanceDownloadWork(t *testing.T) {
	const workers, perView = 3, 6
	key := []byte("0123456789abcdef")
	plaintext := make(map[string][]byte)
	encrypted := make(map[string][]byte)
	for view, name := range []string{"first", "second"} {
		for index := 0; index < perView; index++ {
			path := fmt.Sprintf("/%s/%d", name, index)
			data := make([]byte, 64)
			for i := range data {
				data[i] = byte((i*17 + index*11 + view*37) % 251)
			}
			plaintext[path] = data
			encrypted[path] = performanceEncrypt(t, data, key)
		}
	}
	var keys, chunks, active, peak, servedBytes atomic.Int64
	wave := make(chan struct{})
	var release sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/key" {
			keys.Add(1)
			response := []byte{0, 0}
			for i := len(key) - 1; i >= 0; i-- {
				response = append(response, key[i])
			}
			if _, writeErr := w.Write(response); writeErr != nil {
				return
			}
			return
		}
		data, ok := encrypted[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		chunks.Add(1)
		now := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); now > old && !peak.CompareAndSwap(old, now); old = peak.Load() {
		}
		if now == workers {
			release.Do(func() { close(wave) })
		}
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-wave:
		case <-r.Context().Done():
			return
		case <-timer.C:
			http.Error(w, "fixture requires three concurrent requests", http.StatusGatewayTimeout)
			return
		}
		written, writeErr := w.Write(data)
		servedBytes.Add(int64(written))
		if writeErr != nil {
			return
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	temp := t.TempDir()
	cfg := &config.Config{Token: "synthetic", TempDirLocation: temp, Views: "both", EnablePipeline: true,
		DownloadWorkersPerLecture: workers, DecryptWorkersPerLecture: 2, RateLimit: 1000, APIRateLimit: 1000}
	downloader := New(cfg, client.New(server.Client(), nil))
	playlist := client.ParsedPlaylist{ID: 42, SeqNo: 1, KeyURL: server.URL + "/key"}
	for index := 0; index < perView; index++ {
		playlist.FirstViewURLs = append(playlist.FirstViewURLs, fmt.Sprintf("%s/first/%d", server.URL, index))
		playlist.SecondViewURLs = append(playlist.SecondViewURLs, fmt.Sprintf("%s/second/%d", server.URL, index))
	}
	result, err := downloader.DownloadPlaylist(ctx, playlist, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var all []byte
	for view, paths := range [][]string{result.FirstViewChunks, result.SecondViewChunks} {
		if len(paths) != perView {
			t.Fatalf("view %d: got %d chunks, want %d", view, len(paths), perView)
		}
		for index, path := range paths {
			data, readErr := os.ReadFile(path)
			name := []string{"first", "second"}[view]
			if readErr != nil || !bytes.Equal(data, plaintext[fmt.Sprintf("/%s/%d", name, index)]) {
				t.Fatalf("view %d chunk %d changed or reordered plaintext: %v", view, index, readErr)
			}
			all = append(all, data...)
		}
	}
	if keys.Load() != 1 || chunks.Load() != 12 || peak.Load() != workers || servedBytes.Load() != 960 {
		t.Fatalf("work changed: keys=%d chunks=%d concurrency=%d bodyBytes=%d", keys.Load(), chunks.Load(), peak.Load(), servedBytes.Load())
	}
	left, err := filepath.Glob(filepath.Join(temp, "*.ts.temp"))
	if err != nil || len(left) != 0 {
		t.Fatalf("encrypted temporary files remain: %v %v", left, err)
	}
	if output := os.Getenv("IMPARTUS_PERF_DOWNLOAD_REPORT"); output != "" {
		report := map[string]any{"schemaVersion": 1, "scenario": "download-work-v1", "keyRequests": keys.Load(),
			"chunkRequests": chunks.Load(), "maxConcurrentRequests": peak.Load(), "chunkBodyBytes": servedBytes.Load(),
			"plaintextBytes": len(all), "plaintextSha256": fmt.Sprintf("%x", sha256.Sum256(all)), "encryptedTempFiles": len(left)}
		data, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(output, append(data, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func performanceEncrypt(tb testing.TB, plain, key []byte) []byte {
	tb.Helper()
	padding := aes.BlockSize - len(plain)%aes.BlockSize
	padded := make([]byte, len(plain)+padding)
	copy(padded, plain)
	for i := len(plain); i < len(padded); i++ {
		padded[i] = byte(padding)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		tb.Fatal(err)
	}
	encrypted := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, make([]byte, aes.BlockSize)).CryptBlocks(encrypted, padded)
	return encrypted
}
