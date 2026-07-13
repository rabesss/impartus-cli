package client

import (
	"bufio"
	"strings"
	"testing"
)

func TestParsePlaylistSingleView(t *testing.T) {
	playlist := `#EXTM3U
#EXT-X-KEY:METHOD=ENCRYPTION,URI="https://key.placeholder.test"
#EXTINF:10,
https://cdn.placeholder.test/000.ts
#EXTINF:10,
https://cdn.placeholder.test/001.ts
`

	scanner := bufio.NewScanner(strings.NewReader(playlist))
	got, err := ParsePlaylist(scanner, 123, "Lecture", 1)
	if err != nil {
		t.Fatalf("ParsePlaylist() unexpected error: %v", err)
	}

	if got.KeyURL != "https://key.placeholder.test" {
		t.Fatalf("expected key URL, got %q", got.KeyURL)
	}
	if got.HasMultipleViews {
		t.Fatalf("expected single view playlist")
	}
	if len(got.FirstViewURLs) != 2 || len(got.SecondViewURLs) != 0 {
		t.Fatalf("unexpected view URL counts: first=%d second=%d", len(got.FirstViewURLs), len(got.SecondViewURLs))
	}
}

func TestParsePlaylistMultiView(t *testing.T) {
	playlist := `#EXTM3U
#EXT-X-KEY:METHOD=ENCRYPTION,URI="https://key.placeholder.test"
#EXTINF:10,
https://cdn.placeholder.test/left-000.ts
#EXT-X-DISCONTINUITY
#EXTINF:10,
https://cdn.placeholder.test/right-000.ts
`

	scanner := bufio.NewScanner(strings.NewReader(playlist))
	got, err := ParsePlaylist(scanner, 456, "Lecture-2", 2)
	if err != nil {
		t.Fatalf("ParsePlaylist() unexpected error: %v", err)
	}

	if !got.HasMultipleViews {
		t.Fatalf("expected multi-view playlist")
	}
	if len(got.FirstViewURLs) != 1 || len(got.SecondViewURLs) != 1 {
		t.Fatalf("unexpected view URL counts: first=%d second=%d", len(got.FirstViewURLs), len(got.SecondViewURLs))
	}
	if got.FirstViewURLs[0] != "https://cdn.placeholder.test/left-000.ts" {
		t.Fatalf("unexpected first view URL: %q", got.FirstViewURLs[0])
	}
	if got.SecondViewURLs[0] != "https://cdn.placeholder.test/right-000.ts" {
		t.Fatalf("unexpected second view URL: %q", got.SecondViewURLs[0])
	}
}

func TestParsePlaylistResolvesMediaReferences(t *testing.T) {
	playlist := "#EXTM3U\r\n\r\n#EXT-X-KEY:METHOD=AES-128,URI=\"../keys/key.bin\"\r\n#EXTINF:10,\r\n left-000.ts \r\n#EXT-X-DISCONTINUITY\r\n#EXTINF:10,\r\n//cdn.placeholder.test/right-000.ts\r\n"
	scanner := bufio.NewScanner(strings.NewReader(playlist))

	got, err := parsePlaylist(scanner, "https://media.placeholder.test/course/index.m3u8", 789, "Lecture-3", 3)
	if err != nil {
		t.Fatalf("parsePlaylist() unexpected error: %v", err)
	}
	if got.KeyURL != "https://media.placeholder.test/keys/key.bin" {
		t.Fatalf("unexpected key URL: %q", got.KeyURL)
	}
	if len(got.FirstViewURLs) != 1 || got.FirstViewURLs[0] != "https://media.placeholder.test/course/left-000.ts" {
		t.Fatalf("unexpected first-view URLs: %#v", got.FirstViewURLs)
	}
	if len(got.SecondViewURLs) != 1 || got.SecondViewURLs[0] != "https://cdn.placeholder.test/right-000.ts" {
		t.Fatalf("unexpected second-view URLs: %#v", got.SecondViewURLs)
	}
}

func TestParsePlaylistIgnoresNonEXTComments(t *testing.T) {
	playlist := "#EXTM3U\n#EXTINF:4,\nfirst.ts\n#EXTINF:5,\n# vendor comment\nsecond.ts\n"
	scanner := bufio.NewScanner(strings.NewReader(playlist))

	got, err := parsePlaylist(scanner, "https://media.placeholder.test/course/index.m3u8", 1, "Lecture", 1)
	if err != nil {
		t.Fatalf("parsePlaylist() unexpected error: %v", err)
	}
	wantURLs := []string{
		"https://media.placeholder.test/course/first.ts",
		"https://media.placeholder.test/course/second.ts",
	}
	if len(got.FirstViewURLs) != len(wantURLs) {
		t.Fatalf("FirstViewURLs = %#v, want %#v", got.FirstViewURLs, wantURLs)
	}
	for i := range wantURLs {
		if got.FirstViewURLs[i] != wantURLs[i] {
			t.Fatalf("FirstViewURLs[%d] = %q, want %q", i, got.FirstViewURLs[i], wantURLs[i])
		}
	}
	if len(got.FirstDurations) != 2 || got.FirstDurations[0] != 4 || got.FirstDurations[1] != 5 {
		t.Fatalf("FirstDurations = %#v, want [4 5]", got.FirstDurations)
	}
}

func TestParsePlaylistDiscontinuitySequenceDoesNotChangeViews(t *testing.T) {
	playlist := "#EXTM3U\n#EXT-X-DISCONTINUITY-SEQUENCE:7\n#EXTINF:4,\nfirst.ts\n#EXTINF:5,\nsecond.ts\n"
	scanner := bufio.NewScanner(strings.NewReader(playlist))

	got, err := parsePlaylist(scanner, "https://media.placeholder.test/course/index.m3u8", 1, "Lecture", 1)
	if err != nil {
		t.Fatalf("parsePlaylist() unexpected error: %v", err)
	}
	if got.HasMultipleViews {
		t.Fatal("DISCONTINUITY-SEQUENCE incorrectly created a second view")
	}
	if len(got.FirstViewURLs) != 2 || len(got.SecondViewURLs) != 0 {
		t.Fatalf("unexpected view URL counts: first=%d second=%d", len(got.FirstViewURLs), len(got.SecondViewURLs))
	}
	if got.FirstViewURLs[0] != "https://media.placeholder.test/course/first.ts" ||
		got.FirstViewURLs[1] != "https://media.placeholder.test/course/second.ts" {
		t.Fatalf("unexpected first-view URLs: %#v", got.FirstViewURLs)
	}
}

func TestParsePlaylistRejectsInvalidMediaReferences(t *testing.T) {
	tests := []struct {
		name     string
		playlist string
		want     string
	}{
		{name: "invalid key scheme", playlist: "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"file:///key.bin\"\nsegment.ts\n", want: "key URI"},
		{name: "invalid segment scheme", playlist: "#EXTM3U\nfile:///segment.ts\n", want: "segment URI"},
		{name: "malformed segment", playlist: "#EXTM3U\nhttp://[::1\n", want: "segment URI"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scanner := bufio.NewScanner(strings.NewReader(tt.playlist))
			_, err := parsePlaylist(scanner, "https://media.placeholder.test/course/index.m3u8", 1, "Lecture", 1)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("parsePlaylist() error = %v, want contextual %q error", err, tt.want)
			}
		})
	}
}
