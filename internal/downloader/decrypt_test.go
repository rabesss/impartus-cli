package downloader

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"os"
	"path/filepath"
	"testing"
)

// encryptCBCZeroIV mirrors the upstream encryption: PKCS7-pad then AES-CBC
// encrypt with a zero IV, which DecryptAES is expected to reverse.
func encryptCBCZeroIV(t *testing.T, plaintext, key []byte) []byte {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	padLen := aes.BlockSize - len(plaintext)%aes.BlockSize
	padded := append([]byte(nil), plaintext...)
	for i := 0; i < padLen; i++ {
		padded = append(padded, byte(padLen))
	}
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, make([]byte, aes.BlockSize)).CryptBlocks(out, padded)
	return out
}

func TestDecryptAESRoundTrip(t *testing.T) {
	key := []byte("0123456789abcdef") // 16-byte AES-128 key
	cases := map[string]string{
		"non-aligned":   "hello world",
		"block-aligned": "0123456789abcdef",
		"empty":         "",
		"multi-block":   "the quick brown fox jumps over the lazy dog!!",
	}
	for name, pt := range cases {
		t.Run(name, func(t *testing.T) {
			ciphertext := encryptCBCZeroIV(t, []byte(pt), key)
			got, err := DecryptAES(ciphertext, key)
			if err != nil {
				t.Fatalf("DecryptAES: %v", err)
			}
			if string(got) != pt {
				t.Fatalf("round-trip = %q, want %q", got, pt)
			}
		})
	}
}

func TestDecryptAESDoesNotMutateInput(t *testing.T) {
	key := []byte("0123456789abcdef")
	ciphertext := encryptCBCZeroIV(t, []byte("immutable input"), key)
	original := append([]byte(nil), ciphertext...)
	if _, err := DecryptAES(ciphertext, key); err != nil {
		t.Fatalf("DecryptAES: %v", err)
	}
	if !bytes.Equal(ciphertext, original) {
		t.Fatal("DecryptAES mutated its input slice")
	}
}

func TestDecryptAESInPlaceErrors(t *testing.T) {
	valid := make([]byte, aes.BlockSize)
	tests := []struct {
		name string
		data []byte
		key  []byte
	}{
		{"short key", valid, make([]byte, 8)},
		{"odd key", valid, make([]byte, 17)},
		{"empty ciphertext", nil, make([]byte, 16)},
		{"non-block-multiple", make([]byte, aes.BlockSize+1), make([]byte, 16)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := DecryptAESInPlace(tt.data, tt.key); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestDecryptAESInPlaceAcceptsValidKeyLengths(t *testing.T) {
	for _, n := range []int{16, 24, 32} {
		data := encryptCBCZeroIV(t, []byte("payload"), make([]byte, n))
		if _, err := DecryptAESInPlace(data, make([]byte, n)); err != nil {
			t.Fatalf("key length %d rejected: %v", n, err)
		}
	}
}

func TestRemovePKCS7Padding(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want []byte
	}{
		{"valid padding", []byte{'a', 'b', 0x02, 0x02}, []byte{'a', 'b'}},
		{"full removal", []byte{0x03, 0x03, 0x03}, []byte{}},
		{"empty", []byte{}, []byte{}},
		{"zero pad byte", []byte{'a', 0x00}, []byte{'a', 0x00}},
		{"pad larger than block", append(bytes.Repeat([]byte{'x'}, 4), byte(aes.BlockSize+1)), append(bytes.Repeat([]byte{'x'}, 4), byte(aes.BlockSize+1))},
		{"pad exceeds length", []byte{0x05}, []byte{0x05}},
		{"inconsistent padding", []byte{'a', 0x01, 0x02}, []byte{'a', 0x01, 0x02}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := removePKCS7Padding(tt.in); !bytes.Equal(got, tt.want) {
				t.Fatalf("removePKCS7Padding(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestDecryptChunkBytes(t *testing.T) {
	key := []byte("0123456789abcdef")
	plaintext := []byte("decrypted chunk contents")
	ciphertext := encryptCBCZeroIV(t, plaintext, key)
	d := &Downloader{}

	t.Run("success writes decrypted file", func(t *testing.T) {
		inPath := filepath.Join(t.TempDir(), "chunk0.temp")
		outPath, err := d.decryptChunkBytes(inPath, ciphertext, key)
		if err != nil {
			t.Fatalf("decryptChunkBytes: %v", err)
		}
		if outPath != inPath[:len(inPath)-len(".temp")] {
			t.Fatalf("unexpected outPath %q", outPath)
		}
		written, err := os.ReadFile(outPath)
		if err != nil {
			t.Fatalf("read output: %v", err)
		}
		if !bytes.Equal(written, plaintext) {
			t.Fatalf("written = %q, want %q", written, plaintext)
		}
	})

	t.Run("rejects non-temp extension", func(t *testing.T) {
		if _, err := d.decryptChunkBytes(filepath.Join(t.TempDir(), "chunk.bin"), ciphertext, key); err == nil {
			t.Fatal("expected error for non-.temp path")
		}
	})

	t.Run("rejects too-short path", func(t *testing.T) {
		if _, err := d.decryptChunkBytes("a.go", ciphertext, key); err == nil {
			t.Fatal("expected error for short path")
		}
	})
}
