package downloader

import (
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
)

// TestStructTypes tests that struct types can be instantiated
func TestStructTypes(t *testing.T) {
	// Test client.Lecture struct (used by downloader via client package)
	lecture := client.Lecture{
		TTID:  123,
		Topic: "Introduction",
		SeqNo: 1,
	}
	if lecture.TTID != 123 || lecture.Topic != "Introduction" || lecture.SeqNo != 1 {
		t.Errorf("Lecture struct fields incorrect")
	}

	// Test client.StreamInfo struct
	streamInfo := client.StreamInfo{
		Quality: "720",
		URL:     "https://example.com/stream.m3u8",
	}
	if streamInfo.Quality != "720" || streamInfo.URL != "https://example.com/stream.m3u8" {
		t.Errorf("StreamInfo struct fields incorrect")
	}

	// Test DownloadedPlaylist struct
	downloadedPlaylist := DownloadedPlaylist{
		FirstViewChunks:  []string{"chunk1.ts", "chunk2.ts"},
		SecondViewChunks: []string{"chunk3.ts"},
		Playlist:         client.ParsedPlaylist{ID: 1, Title: "Test"},
	}
	if len(downloadedPlaylist.FirstViewChunks) != 2 {
		t.Errorf("DownloadedPlaylist.FirstViewChunks incorrect length")
	}
	if len(downloadedPlaylist.SecondViewChunks) != 1 {
		t.Errorf("DownloadedPlaylist.SecondViewChunks incorrect length")
	}
	_ = downloadedPlaylist // suppress unused variable

	// Test M3U8File struct
	m3u8File := M3U8File{
		FirstViewFile:  "/path/to/first.m3u8",
		SecondViewFile: "/path/to/second.m3u8",
		Playlist:       client.ParsedPlaylist{ID: 1},
	}
	if m3u8File.FirstViewFile == "" {
		t.Errorf("M3U8File.FirstViewFile is empty")
	}
	if m3u8File.SecondViewFile == "" {
		t.Errorf("M3U8File.SecondViewFile is empty")
	}
	_ = m3u8File // suppress unused variable

	// Test JoinResult struct
	joinResult := JoinResult{
		LeftOutput:  "/path/to/left.mp4",
		RightOutput: "/path/to/right.mp4",
		BothOutput:  "/path/to/both.mp4",
	}
	if joinResult.LeftOutput == "" {
		t.Errorf("JoinResult.LeftOutput is empty")
	}
	if joinResult.RightOutput == "" {
		t.Errorf("JoinResult.RightOutput is empty")
	}
	if joinResult.BothOutput == "" {
		t.Errorf("JoinResult.BothOutput is empty")
	}
}

// TestParsedPlaylist tests ParsedPlaylist struct

// TestParsedPlaylist tests ParsedPlaylist struct
func TestParsedPlaylist(t *testing.T) {
	playlist := client.ParsedPlaylist{
		KeyURL:           "https://placeholder.test/key",
		Title:            "Lecture 1",
		FirstViewURLs:    []string{"url1", "url2"},
		SecondViewURLs:   []string{"url3", "url4"},
		ID:               100,
		SeqNo:            5,
		HasMultipleViews: true,
	}

	if playlist.ID != 100 {
		t.Errorf("ParsedPlaylist.ID = %d, want 100", playlist.ID)
	}
	if playlist.SeqNo != 5 {
		t.Errorf("ParsedPlaylist.SeqNo = %d, want 5", playlist.SeqNo)
	}
	if len(playlist.FirstViewURLs) != 2 {
		t.Errorf("ParsedPlaylist.FirstViewURLs length = %d, want 2", len(playlist.FirstViewURLs))
	}
	if len(playlist.SecondViewURLs) != 2 {
		t.Errorf("ParsedPlaylist.SecondViewURLs length = %d, want 2", len(playlist.SecondViewURLs))
	}
	if playlist.KeyURL == "" {
		t.Error("ParsedPlaylist.KeyURL is empty")
	}
	if playlist.Title == "" {
		t.Error("ParsedPlaylist.Title is empty")
	}
	if !playlist.HasMultipleViews {
		t.Error("ParsedPlaylist.HasMultipleViews should be true")
	}
}

// TestJoinIfPresent tests joinIfPresent function
