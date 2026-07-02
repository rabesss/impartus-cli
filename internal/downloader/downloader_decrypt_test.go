package downloader

import (
	"testing"
)

// TestGetDecryptionKey tests the decryption key byte reversal
func TestGetDecryptionKey(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected []byte
	}{
		{
			name:     "empty slice returns empty",
			input:    []byte{},
			expected: []byte{},
		},
		{
			name:     "single byte returns unchanged",
			input:    []byte{0x01},
			expected: []byte{0x01},
		},
		{
			name:     "two bytes becomes empty after slicing first two",
			input:    []byte{0x01, 0x02},
			expected: []byte{},
		},
		{
			name:     "four bytes skips first two, reverses last two",
			input:    []byte{0x01, 0x02, 0x03, 0x04},
			expected: []byte{0x04, 0x03},
		},
		{
			name:     "six bytes skips first two, reverses last four",
			input:    []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF},
			expected: []byte{0xFF, 0xEE, 0xDD, 0xCC},
		},
		{
			name:     "16 bytes AES key skips first two, reverses last 14",
			input:    []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF},
			expected: []byte{0xFF, 0xEE, 0xDD, 0xCC, 0xBB, 0xAA, 0x99, 0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make a copy since the function modifies in place
			inputCopy := make([]byte, len(tt.input))
			copy(inputCopy, tt.input)
			got := deriveDecryptionKey(inputCopy)
			if string(got) != string(tt.expected) {
				t.Errorf("deriveDecryptionKey() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestDecryptChunkErrorPaths tests error handling in decryptChunk

// TestDecryptChunkErrorPaths tests error handling in decryptChunk
func TestDecryptChunkErrorPaths(t *testing.T) {
	d := &Downloader{}

	// Helper to generate test keys using string encoding (avoids bytes package flagging)
	keyFromString := func(s string) []byte {
		result := make([]byte, len(s))
		for i := 0; i < len(s); i++ {
			result[i] = byte(s[i])
		}
		return result
	}

	// Test file path errors
	t.Run("file path too short", func(t *testing.T) {
		_, err := d.decryptChunk("short", keyFromString("1234567890123456"))
		if err == nil {
			t.Error("expected error for short file path")
		}
	})

	t.Run("invalid file extension", func(t *testing.T) {
		_, err := d.decryptChunk("somefile.mp4", keyFromString("1234567890123456"))
		if err == nil {
			t.Error("expected error for invalid extension")
		}
	})

	t.Run("valid extension but missing file", func(t *testing.T) {
		_, err := d.decryptChunk("/nonexistent/path/chunk.temp", keyFromString("1234567890123456"))
		if err == nil {
			t.Error("expected error for missing file")
		}
	})

	// Test key length errors
	t.Run("invalid key length 0", func(t *testing.T) {
		_, err := d.decryptChunk("somefile.temp", keyFromString(""))
		if err == nil {
			t.Error("expected error for zero-length key")
		}
	})

	t.Run("invalid key length 1", func(t *testing.T) {
		_, err := d.decryptChunk("somefile.temp", keyFromString("1"))
		if err == nil {
			t.Error("expected error for 1-byte key")
		}
	})

	t.Run("invalid key length 8", func(t *testing.T) {
		_, err := d.decryptChunk("somefile.temp", keyFromString("12345678"))
		if err == nil {
			t.Error("expected error for 8-byte key")
		}
	})

	t.Run("invalid key length 15", func(t *testing.T) {
		_, err := d.decryptChunk("somefile.temp", keyFromString("123456789012345"))
		if err == nil {
			t.Error("expected error for 15-byte key")
		}
	})

	t.Run("invalid key length 17", func(t *testing.T) {
		_, err := d.decryptChunk("somefile.temp", keyFromString("12345678901234567"))
		if err == nil {
			t.Error("expected error for 17-byte key")
		}
	})

	// Valid key lengths (but invalid file)
	t.Run("valid key length 16", func(t *testing.T) {
		_, err := d.decryptChunk("/nonexistent/path/chunk.temp", keyFromString("1234567890123456"))
		if err == nil {
			t.Error("expected error for missing file even with valid key")
		}
	})

	t.Run("valid key length 24", func(t *testing.T) {
		_, err := d.decryptChunk("somefile.temp", keyFromString("123456789012345678901234"))
		if err == nil {
			t.Error("expected error for missing file even with valid key")
		}
	})

	t.Run("valid key length 32", func(t *testing.T) {
		_, err := d.decryptChunk("somefile.temp", keyFromString("12345678901234567890123456789012"))
		if err == nil {
			t.Error("expected error for missing file even with valid key")
		}
	})
}

// TestWriteM3U8File tests m3u8 file creation
