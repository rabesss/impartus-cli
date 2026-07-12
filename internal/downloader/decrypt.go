package downloader

import (
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"os"
	"strings"
)

// DecryptAES performs AES-128-CBC decryption with a zero IV and removes PKCS7 padding.
func DecryptAES(encrypted []byte, key []byte) ([]byte, error) {
	ciphertext := append([]byte(nil), encrypted...)
	return DecryptAESInPlace(ciphertext, key)
}

// DecryptAESInPlace performs AES-CBC decryption in the provided byte slice and removes PKCS7 padding.
func DecryptAESInPlace(encrypted []byte, key []byte) ([]byte, error) {
	if len(key) != 16 && len(key) != 24 && len(key) != 32 {
		return nil, fmt.Errorf("invalid AES key length: %d", len(key))
	}
	if len(encrypted) == 0 || len(encrypted)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("ciphertext length %d is not a multiple of block size %d", len(encrypted), aes.BlockSize)
	}

	iv := make([]byte, 16)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(encrypted, encrypted)

	return removePKCS7Padding(encrypted), nil
}

// removePKCS7Padding strips PKCS7 padding from decrypted data.
// Returns the original slice if padding is invalid or absent.
func removePKCS7Padding(data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	paddingLen := int(data[len(data)-1])
	if paddingLen <= 0 || paddingLen > aes.BlockSize || paddingLen > len(data) {
		return data
	}
	// Verify all padding bytes match the expected value
	for i := len(data) - paddingLen; i < len(data); i++ {
		if data[i] != byte(paddingLen) {
			return data
		}
	}
	return data[:len(data)-paddingLen]
}

// deriveDecryptionKey transforms the raw key from the upstream API into the
// actual AES decryption key. The upstream response includes a 2-byte header
// prefix followed by the key bytes in reversed order. This function strips
// the header and reverses the remaining bytes to recover the usable key.
// It operates on a copy so the caller's input slice is not mutated.
func deriveDecryptionKey(input []byte) []byte {
	data := make([]byte, len(input))
	copy(data, input)
	if len(data) < 2 {
		return data
	}
	data = data[2:]
	for i, j := 0, len(data)-1; i < j; i, j = i+1, j-1 {
		data[i], data[j] = data[j], data[i]
	}
	return data
}

// zeroKey overwrites the key slice with zeros to reduce the window during
// which decryption key material remains in memory.
func zeroKey(key []byte) {
	for i := range key {
		key[i] = 0
	}
}

func (d *Downloader) decryptChunk(filePath string, key []byte) (string, error) {
	// G304: file paths are constructed from validated config and internal data
	// #nosec G304
	infile, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read encrypted file %s: %w", filePath, err)
	}
	return d.decryptChunkBytes(filePath, infile, key)
}

func (d *Downloader) decryptChunkBytes(filePath string, infile []byte, key []byte) (string, error) {
	if len(filePath) < 6 {
		return "", fmt.Errorf("invalid file path: %s", filePath)
	}
	if !strings.HasSuffix(filePath, ".temp") {
		return "", fmt.Errorf("invalid file path extension: %s", filePath)
	}

	// Work on a copy of the key so the caller's shared key is not zeroed,
	// allowing safe concurrent reuse across chunks and pipeline workers.
	// The copy is zeroed after use to limit key material lifetime.
	keyCopy := make([]byte, len(key))
	copy(keyCopy, key)
	defer zeroKey(keyCopy)

	outPath := strings.TrimSuffix(filePath, ".temp")
	plainText, err := DecryptAES(infile, keyCopy)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	// G703: path components are from validated config and sanitized input
	// #nosec G703
	if err := os.WriteFile(outPath, plainText, 0o600); err != nil {
		return "", fmt.Errorf("failed to write decrypted file %s: %w", outPath, err)
	}
	return outPath, nil
}
