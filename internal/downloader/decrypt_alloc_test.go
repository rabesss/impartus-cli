//go:build !race

// Race instrumentation changes allocation behavior, so these guards skip -race builds.

package downloader

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Decrypting a chunk in place allocates about 1.3 KiB. The slack tolerates runtime
// noise and still fails on any extra copy of a chunk.
const (
	decryptAllocationChunkSize = 1 << 20
	decryptAllocationRuns      = 20
	decryptAllocationSlack     = 128 << 10
)

func TestDecryptChunkBytesDecryptsInPlace(t *testing.T) {
	plaintext, ciphertext, key := decryptAllocationFixture(t)
	buffer := make([]byte, len(ciphertext))
	outputPath := filepath.Join(t.TempDir(), "chunk.ts")
	tempPath := outputPath + ".temp"
	d := &Downloader{}

	bytesPerOp := measureAllocatedBytesPerCall(t, decryptAllocationRuns, func() error {
		if err := os.Remove(outputPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		copy(buffer, ciphertext)
		if _, err := d.decryptChunkBytes(tempPath, buffer, key); err != nil {
			return err
		}
		if !bytes.Equal(buffer[:len(plaintext)], plaintext) {
			return errors.New("decryptChunkBytes did not decrypt the caller buffer")
		}
		return nil
	})
	t.Logf("decryptChunkBytes allocated %.0f bytes/op", bytesPerOp)
	if bytesPerOp >= decryptAllocationSlack {
		t.Fatalf("decryptChunkBytes allocated %.0f bytes/op, want less than %d", bytesPerOp, decryptAllocationSlack)
	}

	written, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read decrypted output: %v", err)
	}
	if !bytes.Equal(written, plaintext) {
		t.Fatal("decrypted output changed")
	}
}

func TestDecryptChunkReadsChunkOnce(t *testing.T) {
	plaintext, ciphertext, key := decryptAllocationFixture(t)
	outputPath := filepath.Join(t.TempDir(), "chunk.ts")
	tempPath := outputPath + ".temp"
	d := &Downloader{}

	bytesPerOp := measureAllocatedBytesPerCall(t, decryptAllocationRuns, func() error {
		if err := os.Remove(outputPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.WriteFile(tempPath, ciphertext, 0o600); err != nil {
			return err
		}
		_, err := d.decryptChunk(tempPath, key)
		return err
	})
	t.Logf("decryptChunk allocated %.0f bytes/op", bytesPerOp)
	// Reading the encrypted file allocates the chunk once; decryption must not copy it again.
	limit := decryptAllocationChunkSize + decryptAllocationSlack
	if bytesPerOp >= float64(limit) {
		t.Fatalf("decryptChunk allocated %.0f bytes/op, want less than %d", bytesPerOp, limit)
	}

	written, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read decrypted output: %v", err)
	}
	if !bytes.Equal(written, plaintext) {
		t.Fatal("decrypted output changed")
	}
}

func BenchmarkDecryptChunkBytes(b *testing.B) {
	plaintext, ciphertext, key := decryptAllocationFixture(b)
	outputPath := filepath.Join(b.TempDir(), "chunk.ts")
	tempPath := outputPath + ".temp"
	buffer := make([]byte, len(ciphertext))
	d := &Downloader{}

	b.SetBytes(int64(len(plaintext)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := os.Remove(outputPath); err != nil && !os.IsNotExist(err) {
			b.Fatalf("remove previous output: %v", err)
		}
		copy(buffer, ciphertext)
		if _, err := d.decryptChunkBytes(tempPath, buffer, key); err != nil {
			b.Fatalf("decryptChunkBytes: %v", err)
		}
	}
}

func decryptAllocationFixture(tb testing.TB) (plaintext, ciphertext, key []byte) {
	tb.Helper()
	key = []byte("0123456789abcdef")
	plaintext = make([]byte, decryptAllocationChunkSize)
	for i := range plaintext {
		plaintext[i] = byte((i*73 + i/17) % 251)
	}
	ciphertext = encryptCBCZeroIV(tb, plaintext, key)
	return plaintext, ciphertext, key
}

func measureAllocatedBytesPerCall(tb testing.TB, runs int, fn func() error) float64 {
	tb.Helper()
	runtime.GC()
	previousMaxProcs := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previousMaxProcs)

	if err := fn(); err != nil {
		tb.Fatalf("warm-up call: %v", err)
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := 0; i < runs; i++ {
		if err := fn(); err != nil {
			tb.Fatalf("call %d: %v", i+1, err)
		}
	}
	runtime.ReadMemStats(&after)
	return float64(after.TotalAlloc-before.TotalAlloc) / float64(runs)
}
