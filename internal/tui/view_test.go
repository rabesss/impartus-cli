package tui

import (
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
)

func TestPlaybackViewFormatsPositionsWithHours(t *testing.T) {
	model := Model{
		screen:   screenPlayback,
		lecture:  client.Lecture{Topic: "Long lecture"},
		position: 5712,
		duration: 7200,
		volume:   100,
		speed:    1,
		width:    160,
		height:   24,
	}

	view := model.View().Content
	if !strings.Contains(view, "01:35:12 / 02:00:00") {
		t.Fatalf("playback clock =\n%s", view)
	}
	if !strings.Contains(view, "+/- volume") || !strings.Contains(view, "[/] speed") {
		t.Fatalf("playback controls are not discoverable:\n%s", view)
	}
}
