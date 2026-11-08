package client

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/config"
)

func parseEdgePlaylist(text string) (ParsedPlaylist, error) {
	return parsePlaylist(bufio.NewScanner(strings.NewReader(text)), encryptionTestBase, 1, "Lecture", 1)
}

func TestParsePlaylistKeyRotation(t *testing.T) {
	tests := []struct {
		name, playlist, wantErr string
	}{
		{
			name:     "same key repeated",
			playlist: "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\nseg0.ts\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\nseg1.ts\n",
		},
		{
			name:     "same key after a discontinuity",
			playlist: "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\nseg0.ts\n#EXT-X-DISCONTINUITY\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\nseg1.ts\n",
		},
		{
			name:     "same key spelled relative and absolute",
			playlist: "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\nseg0.ts\n#EXT-X-KEY:METHOD=AES-128,URI=\"https://media.example.test/hls/key.bin\"\nseg1.ts\n",
		},
		{
			name:     "rotated key",
			playlist: "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key1.bin\"\nseg0.ts\n#EXT-X-KEY:METHOD=AES-128,URI=\"key2.bin\"\nseg1.ts\n",
			wantErr:  "playlist rotates encryption keys",
		},
		{
			name:     "rotated key in the second view",
			playlist: "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key1.bin\"\nseg0.ts\n#EXT-X-DISCONTINUITY\n#EXT-X-KEY:METHOD=AES-128,URI=\"key2.bin\"\nseg1.ts\n",
			wantErr:  "playlist rotates encryption keys",
		},
		{
			name:     "trailing key after the last segment is unused",
			playlist: "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key1.bin\"\nseg0.ts\n#EXT-X-KEY:METHOD=AES-128,URI=\"key2.bin\"\n#EXT-X-ENDLIST\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseEdgePlaylist(tt.playlist)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("parsePlaylist() error = %v", err)
				}
				if got.KeyURL != "https://media.example.test/hls/key.bin" && got.KeyURL != "https://media.example.test/hls/key1.bin" {
					t.Fatalf("KeyURL = %q, want the first segment's key", got.KeyURL)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("parsePlaylist() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestParsePlaylistKeyIV(t *testing.T) {
	const secretIV = "0x00112233445566778899AABBCCDDEEFF"
	tests := []struct {
		name, iv, wantErr string
	}{
		{name: "zero", iv: "0x00000000000000000000000000000000"},
		{name: "short zero, upper-case prefix", iv: "0X0"},
		{name: "non-zero", iv: secretIV, wantErr: "only a zero IV is supported"},
		{name: "non-zero low bit", iv: "0x1", wantErr: "only a zero IV is supported"},
		{name: "no prefix", iv: "00000000000000000000000000000000", wantErr: "malformed IV"},
		{name: "empty digits", iv: "0x", wantErr: "malformed IV"},
		{name: "non-hex digit", iv: "0x0000g", wantErr: "malformed IV"},
		{name: "longer than 128 bits", iv: "0x" + strings.Repeat("0", 33), wantErr: "malformed IV"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			playlist := "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\",IV=" + tt.iv + "\nseg0.ts\n"
			got, err := parseEdgePlaylist(playlist)
			if tt.wantErr == "" {
				if err != nil || got.KeyURL == "" {
					t.Fatalf("parsePlaylist() = %q, %v; want the key kept", got.KeyURL, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("parsePlaylist() error = %v, want %q", err, tt.wantErr)
			}
			if strings.Contains(err.Error(), strings.TrimPrefix(tt.iv, "0x")) && len(tt.iv) > 4 {
				t.Fatalf("parsePlaylist() error %q echoed the IV", err)
			}
		})
	}

	// METHOD=NONE ignores an IV.
	if _, err := parseEdgePlaylist("#EXTM3U\n#EXT-X-KEY:METHOD=NONE,IV=" + secretIV + "\nseg0.ts\n"); err != nil {
		t.Fatalf("parsePlaylist(METHOD=NONE with IV) error = %v", err)
	}
}

func TestParseStreamInfosReportsMalformedOnlyResponse(t *testing.T) {
	body := "#EXTM3U\n\nnot a url\nhttps://media.example.test/hls/noresolution/index.m3u8\n"
	infos, err := ParseStreamInfosFromBody([]byte(body))
	if !errors.Is(err, ErrNoStreams) || len(infos) != 0 {
		t.Fatalf("ParseStreamInfosFromBody() = %v, %v; want ErrNoStreams", infos, err)
	}
	if !strings.Contains(err.Error(), "2 malformed lines") || strings.Contains(err.Error(), "not a url") {
		t.Fatalf("error = %q, want a count of malformed lines without their text", err)
	}

	infos, err = ParseStreamInfosFromBody([]byte("#EXTM3U\n\n"))
	if err != nil || len(infos) != 0 {
		t.Fatalf("ParseStreamInfosFromBody(empty) = %v, %v; want no streams and no error", infos, err)
	}

	infos, err = ParseStreamInfosFromBody([]byte("junk\nhttps://media.example.test/hls/1280x720/index.m3u8\n"))
	if err != nil || len(infos) != 1 {
		t.Fatalf("ParseStreamInfosFromBody(mixed) = %v, %v; want the one stream", infos, err)
	}
}

func streamInfoServer(t *testing.T, bodies map[string]string) *httptest.Server {
	t.Helper()
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hls/1280x720/index.m3u8" {
			_, _ = io.WriteString(w, "#EXTM3U\n#EXTINF:10.0,\nseg0.ts\n#EXT-X-ENDLIST\n") //nolint:errcheck
			return
		}
		body, ok := bodies[r.URL.Query().Get("ttid")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, strings.ReplaceAll(body, "SERVER", serverURL)) //nolint:errcheck
	}))
	t.Cleanup(server.Close)
	serverURL = server.URL
	return server
}

func TestGetPlaylistsReportsLecturesWithoutStreams(t *testing.T) {
	server := streamInfoServer(t, map[string]string{
		"1": "",
		"2": "not a stream\n",
		"3": "SERVER/hls/1280x720/index.m3u8\n",
	})
	cfg := &config.Config{BaseURL: server.URL, Token: "request-token", Quality: "720"}
	c := New(server.Client(), nil)

	_, err := c.GetPlaylists(context.Background(), cfg, Lectures{{TTID: 1, SeqNo: 1}, {TTID: 2, SeqNo: 2}})
	if !errors.Is(err, ErrNoStreams) || !strings.Contains(err.Error(), "2 of 2 lectures") {
		t.Fatalf("GetPlaylists(no streams) error = %v, want ErrNoStreams for 2 of 2 lectures", err)
	}

	playlists, err := c.GetPlaylists(context.Background(), cfg, Lectures{{TTID: 1, SeqNo: 1}, {TTID: 3, SeqNo: 3}})
	if err != nil || len(playlists) != 1 {
		t.Fatalf("GetPlaylists(mixed) = %d playlists, %v; want the one lecture with a stream", len(playlists), err)
	}
}
