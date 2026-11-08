package client

import (
	"bufio"
	"strings"
	"testing"
)

const encryptionTestBase = "https://media.example.test/hls/index.m3u8"

func TestParsePlaylistEncryptionStates(t *testing.T) {
	tests := []struct {
		name     string
		playlist string
		wantKey  string
		wantSegs int
	}{
		{
			name:     "no key is clear",
			playlist: "#EXTM3U\n#EXT-X-VERSION:3\n#EXTINF:10.0,\nseg0.ts\n#EXT-X-ENDLIST\n",
			wantKey:  "",
			wantSegs: 1,
		},
		{
			name:     "METHOD=NONE is clear",
			playlist: "#EXTM3U\n#EXT-X-KEY:METHOD=NONE\n#EXTINF:10.0,\nseg0.ts\n",
			wantKey:  "",
			wantSegs: 1,
		},
		{
			name:     "AES-128",
			playlist: "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\n#EXTINF:10.0,\nseg0.ts\n",
			wantKey:  "https://media.example.test/hls/key.bin",
			wantSegs: 1,
		},
		{
			name:     "legacy non-standard method keeps the key",
			playlist: "#EXTM3U\n#EXT-X-KEY:METHOD=ENCRYPTION,URI=\"key.bin\"\nseg0.ts\n",
			wantKey:  "https://media.example.test/hls/key.bin",
			wantSegs: 1,
		},
		{
			name:     "lower-case method keeps the key",
			playlist: "#EXTM3U\n#EXT-X-KEY:METHOD=aes-128,URI=\"key.bin\"\nseg0.ts\n",
			wantKey:  "https://media.example.test/hls/key.bin",
			wantSegs: 1,
		},
		{
			name:     "key line before NONE applies to no segment",
			playlist: "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\n#EXT-X-KEY:METHOD=NONE\nseg0.ts\n",
			wantKey:  "",
			wantSegs: 1,
		},
		{
			name:     "key without segments is kept",
			playlist: "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\n",
			wantKey:  "https://media.example.test/hls/key.bin",
			wantSegs: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePlaylist(bufio.NewScanner(strings.NewReader(tt.playlist)), encryptionTestBase, 1, "Lecture", 1)
			if err != nil {
				t.Fatalf("parsePlaylist() error = %v", err)
			}
			if got.KeyURL != tt.wantKey {
				t.Fatalf("KeyURL = %q, want %q", got.KeyURL, tt.wantKey)
			}
			if len(got.FirstViewURLs) != tt.wantSegs {
				t.Fatalf("segments = %d, want %d", len(got.FirstViewURLs), tt.wantSegs)
			}
		})
	}
}

func TestParsePlaylistRejectsUnsupportedEncryption(t *testing.T) {
	tests := []struct {
		name     string
		playlist string
		want     string
	}{
		{
			name:     "SAMPLE-AES",
			playlist: "#EXTM3U\n#EXT-X-KEY:METHOD=SAMPLE-AES,URI=\"key.bin\"\nseg0.ts\n",
			want:     "unsupported HLS encryption method",
		},
		{
			name:     "SAMPLE-AES-CTR",
			playlist: "#EXTM3U\n#EXT-X-KEY:METHOD=SAMPLE-AES-CTR,URI=\"key.bin\"\nseg0.ts\n",
			want:     "unsupported HLS encryption method",
		},
		{
			name:     "encrypted key with no URI",
			playlist: "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128\nseg0.ts\n",
			want:     "no URI",
		},
		{
			name:     "clear then encrypted",
			playlist: "#EXTM3U\nseg0.ts\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\nseg1.ts\n",
			want:     "mixes clear and encrypted",
		},
		{
			name:     "encrypted then clear",
			playlist: "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\nseg0.ts\n#EXT-X-KEY:METHOD=NONE\nseg1.ts\n",
			want:     "mixes clear and encrypted",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parsePlaylist(bufio.NewScanner(strings.NewReader(tt.playlist)), encryptionTestBase, 1, "Lecture", 1)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("parsePlaylist() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestParsePlaylistUnsupportedMethodIsNotEchoed(t *testing.T) {
	const marker = "secret-bearer-abc123"
	playlist := "#EXTM3U\n#EXT-X-KEY:METHOD=SAMPLE-AES-" + marker + ",URI=\"key.bin\"\nseg0.ts\n"
	_, err := parsePlaylist(bufio.NewScanner(strings.NewReader(playlist)), encryptionTestBase, 1, "Lecture", 1)
	if err == nil || strings.Contains(err.Error(), marker) {
		t.Fatalf("parsePlaylist() error = %v, want a fixed unsupported-method error", err)
	}
}
