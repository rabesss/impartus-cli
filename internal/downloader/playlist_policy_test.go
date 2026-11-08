package downloader

import (
	"bufio"
	"errors"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
)

// An invalid media origin policy fails every request, so a clear playlist,
// which fetches no key, must still report it up front and send nothing.
func TestClearPlaylistReportsInvalidMediaOriginPolicy(t *testing.T) {
	server, requests := clearHLSServer(t)
	cfg := loadDownloadSpeedConfig(t, server.URL, t.TempDir())
	cfg.Views = "first"
	cfg.MediaOrigins = []string{"http://cdn.example.test"}
	d := New(cfg, client.New(server.Client(), nil))
	playlist := client.ParsedPlaylist{
		ID:             7,
		SeqNo:          1,
		FirstViewURLs:  []string{server.URL + "/hls/seg0.ts"},
		FirstDurations: []float64{10},
	}

	if _, err := d.DownloadPlaylist(t.Context(), playlist, nil, nil); !errors.Is(err, client.ErrInsecureMediaURL) {
		t.Fatalf("DownloadPlaylist() error = %v, want ErrInsecureMediaURL", err)
	}
	if _, err := d.DownloadAndJoinPlaylist(t.Context(), playlist, nil, nil); !errors.Is(err, client.ErrInsecureMediaURL) {
		t.Fatalf("DownloadAndJoinPlaylist() error = %v, want ErrInsecureMediaURL", err)
	}
	stream, err := d.StartPlaybackStream(t.Context(), playlist)
	if err == nil {
		stream.Cleanup()
	}
	if !errors.Is(err, client.ErrInsecureMediaURL) {
		t.Fatalf("StartPlaybackStream() error = %v, want ErrInsecureMediaURL", err)
	}
	if got := requests(); len(got) != 0 {
		t.Fatalf("requests = %v, want none", got)
	}
}

// A keyed playlist with no segments no longer carries a key URL, so nothing
// fetches the key for it.
func TestKeyedPlaylistWithoutSegmentsFetchesNoKey(t *testing.T) {
	server, requests := clearHLSServer(t)
	cfg := loadDownloadSpeedConfig(t, server.URL, t.TempDir())
	cfg.Views = "first"
	d := New(cfg, client.New(server.Client(), nil))
	text := "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"" + server.URL + "/hls/key.bin\"\n#EXT-X-ENDLIST\n"
	playlist, err := client.ParsePlaylist(bufio.NewScanner(strings.NewReader(text)), 7, "Lecture", 1)
	if err != nil {
		t.Fatalf("ParsePlaylist() error = %v", err)
	}
	if playlist.KeyURL != "" {
		t.Fatalf("KeyURL = %q, want none for a playlist without segments", playlist.KeyURL)
	}

	if _, err := d.DownloadPlaylist(t.Context(), playlist, nil, nil); err != nil {
		t.Fatalf("DownloadPlaylist() error = %v", err)
	}
	if _, err := d.DownloadAndJoinPlaylist(t.Context(), playlist, nil, nil); !errors.Is(err, ErrNoSelectedMedia) {
		t.Fatalf("DownloadAndJoinPlaylist() error = %v, want ErrNoSelectedMedia", err)
	}
	stream, err := d.StartPlaybackStream(t.Context(), playlist)
	if err == nil {
		stream.Cleanup()
		t.Fatal("StartPlaybackStream() error = nil, want no playable views")
	}
	if got := requests(); len(got) != 0 {
		t.Fatalf("requests = %v, want none", got)
	}
}
