package downloader

import (
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/rabesss/impartus-cli/internal/secrets"
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
	outPath, err := d.decryptChunkBytes(filePath, infile, key)
	if err != nil {
		return "", err
	}
	// The encrypted copy is no longer needed; dropping it halves the disk a
	// kept (resumable) workspace uses.
	if removeErr := os.Remove(filePath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		d.logDiagnostic("warning: failed to remove encrypted chunk: %s", secrets.ScrubError(removeErr))
	}
	return outPath, nil
}

// decryptChunkBytes decrypts infile in place, so the caller must transfer
// ownership of the slice. A nil key means a clear playlist: infile is written
// unchanged.
func (d *Downloader) decryptChunkBytes(filePath string, infile []byte, key []byte) (string, error) {
	if len(filePath) < 6 {
		return "", fmt.Errorf("invalid file path: %s", filePath)
	}
	if !strings.HasSuffix(filePath, ".temp") {
		return "", fmt.Errorf("invalid file path extension: %s", filePath)
	}

	outPath := strings.TrimSuffix(filePath, ".temp")
	if key == nil {
		// Clear HLS: the downloaded bytes are the media.
		if err := writeChunkAtomically(outPath, infile, d.resume != nil); err != nil {
			return "", fmt.Errorf("failed to write chunk file %s: %w", outPath, err)
		}
		return outPath, nil
	}

	// Work on a copy of the key so the caller's shared key is not zeroed,
	// allowing safe concurrent reuse across chunks and pipeline workers.
	// The copy is zeroed after use to limit key material lifetime.
	keyCopy := make([]byte, len(key))
	copy(keyCopy, key)
	defer zeroKey(keyCopy)

	plainText, err := DecryptAESInPlace(infile, keyCopy)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	if err := writeChunkAtomically(outPath, plainText, d.resume != nil); err != nil {
		return "", fmt.Errorf("failed to write decrypted file %s: %w", outPath, err)
	}
	return outPath, nil
}

// writeChunkAtomically writes data to path+".part" and renames it into place,
// so a final chunk file exists only once it is complete. A resumable
// workspace treats that file as done on the next attempt, so durable also
// fsyncs the data before the rename.
func writeChunkAtomically(path string, data []byte, durable bool) (err error) {
	partial := path + ".part"
	// Never write through whatever is already at the staging path: remove it
	// and create a new file exclusively, which also refuses a symlink.
	if removeErr := os.Remove(partial); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return removeErr
	}
	// G304/G703: path components are from validated config and sanitized input
	// #nosec G304 G703
	file, err := os.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(partial) //nolint:errcheck // best effort; the next attempt replaces it
		}
	}()
	if _, err = file.Write(data); err != nil {
		_ = file.Close() //nolint:errcheck // the write error is the one to report
		return err
	}
	if durable {
		if err = file.Sync(); err != nil {
			_ = file.Close() //nolint:errcheck // the sync error is the one to report
			return err
		}
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(partial, path)
}
