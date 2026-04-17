package main

import (
	"bufio"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
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
	got := client.ParsePlaylist(scanner, 123, "Lecture", 1)

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
	got := client.ParsePlaylist(scanner, 456, "Lecture-2", 2)

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
