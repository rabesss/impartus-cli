package tui

import (
	"context"
	"testing"

	"github.com/rabesss/impartus-cli/internal/library"
	"github.com/rabesss/impartus-cli/internal/player"
)

type updatePlaybackStub struct {
	events chan player.Event
}

func (stub *updatePlaybackStub) Events() <-chan player.Event                 { return stub.events }
func (stub *updatePlaybackStub) WaitForEnd(context.Context) error            { return nil }
func (stub *updatePlaybackStub) Pause(context.Context, bool) error           { return nil }
func (stub *updatePlaybackStub) SeekRelative(context.Context, float64) error { return nil }
func (stub *updatePlaybackStub) SeekAbsolute(context.Context, float64) error { return nil }
func (stub *updatePlaybackStub) SetVolume(context.Context, float64) error    { return nil }
func (stub *updatePlaybackStub) SetMute(context.Context, bool) error         { return nil }
func (stub *updatePlaybackStub) SetSpeed(context.Context, float64) error     { return nil }
func (stub *updatePlaybackStub) CycleVideo(context.Context) error            { return nil }
func (stub *updatePlaybackStub) Close(context.Context) error                 { return nil }

func TestPlaybackStartedResetsTransportDefaults(t *testing.T) {
	t.Parallel()

	model := Model{
		ctx: context.Background(), paused: true, muted: true, volume: 130, speed: 3,
	}
	updated, _ := model.updatePlaybackStarted(playbackStartedMsg{
		playback: &updatePlaybackStub{events: make(chan player.Event)},
		resume:   library.PlaybackState{PositionSeconds: 12, DurationSeconds: 120},
	})
	got, ok := updated.(Model)
	if !ok {
		t.Fatalf("updated model type = %T, want tui.Model", updated)
	}
	if got.paused || got.muted || got.volume != 100 || got.speed != 1 {
		t.Fatalf("transport state = paused:%v muted:%v volume:%v speed:%v, want fresh mpv defaults", got.paused, got.muted, got.volume, got.speed)
	}
}

func TestSeekCompletionDoesNotOverrideObservedPosition(t *testing.T) {
	t.Parallel()

	playback := &updatePlaybackStub{events: make(chan player.Event)}
	model := Model{
		ctx: context.Background(), playback: playback, playbackGeneration: 1,
		position: 100, duration: 100,
		playbackControls:    []playbackControlRequest{{action: "seek", value: 10}},
		playbackControlBusy: true,
	}
	updated, _ := model.updatePlaybackControl(playbackControlMsg{generation: 1, action: "seek", value: 10})
	got, ok := updated.(Model)
	if !ok {
		t.Fatalf("updated model type = %T, want tui.Model", updated)
	}
	if got.position != 100 {
		t.Fatalf("position = %v, want latest absolute telemetry 100", got.position)
	}
}
