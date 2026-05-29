package cli

import (
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

func TestCountChunks(t *testing.T) {
	playlists := []client.ParsedPlaylist{
		{FirstViewURLs: []string{"a", "b", "c"}, SecondViewURLs: []string{"d", "e"}},
	}
	tests := []struct {
		views string
		want  int
	}{
		{"both", 5},
		{"left", 3},
		{"right", 2},
		{config.NormalizeViews("first"), 3},
		{config.NormalizeViews("second"), 2},
	}
	for _, tt := range tests {
		if got := countChunks(playlists, tt.views); got != tt.want {
			t.Errorf("countChunks(views=%q) = %d, want %d", tt.views, got, tt.want)
		}
	}
}

func TestCountNoAudioLectures(t *testing.T) {
	lectures := client.Lectures{{NoAudio: 1}, {NoAudio: 0}, {NoAudio: 1}}
	if got := countNoAudioLectures(lectures); got != 2 {
		t.Errorf("countNoAudioLectures() = %d, want 2", got)
	}
}
