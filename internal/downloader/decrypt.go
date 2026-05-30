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
func deriveDecryptionKey(encryptionKey []byte) []byte {
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

	outPath := strings.TrimSuffix(filePath, ".temp")
	plainText, err := DecryptAES(infile, key)
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
