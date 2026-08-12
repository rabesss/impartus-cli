package tui

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/rabesss/impartus-cli/internal/player"
)

type cleanupPlayback struct {
	events  chan player.Event
	closed  atomic.Int32
	release chan struct{}
	once    atomic.Bool
}

func (playback *cleanupPlayback) Events() <-chan player.Event { return playback.events }
func (playback *cleanupPlayback) WaitForEnd(context.Context) error {
	if playback.release != nil {
		<-playback.release
	}
	return nil
}
func (playback *cleanupPlayback) Pause(context.Context, bool) error { return nil }
func (playback *cleanupPlayback) SeekRelative(context.Context, float64) error {
	return nil
}
func (playback *cleanupPlayback) SeekAbsolute(context.Context, float64) error { return nil }
func (playback *cleanupPlayback) SetVolume(context.Context, float64) error    { return nil }
func (playback *cleanupPlayback) SetMute(context.Context, bool) error         { return nil }
func (playback *cleanupPlayback) SetSpeed(context.Context, float64) error     { return nil }
func (playback *cleanupPlayback) CycleVideo(context.Context) error            { return nil }
func (playback *cleanupPlayback) Close(context.Context) error {
	playback.closed.Add(1)
	if playback.release != nil && playback.once.CompareAndSwap(false, true) {
		close(playback.release)
	}
	return nil
}

func TestOperationTrackerSkipsCommandsThatNeverStartedBeforeShutdown(t *testing.T) {
	tracker := &operationTracker{}
	var ran atomic.Bool
	command := tracker.command(func() tea.Msg {
		ran.Store(true)
		return nil
	})

	done := make(chan struct{})
	go func() {
		tracker.StopAndWait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown waited for a command Bubble Tea never started")
	}

	if message := command(); message != nil {
		t.Fatalf("stopped command returned message %T, want nil", message)
	}
	if ran.Load() {
		t.Fatal("command started after the operation tracker stopped")
	}
}

func TestRuntimeShutdownClosesPlaybackOwnedByTheTUI(t *testing.T) {
	model := New(context.Background(), nil)
	playback := &cleanupPlayback{events: make(chan player.Event)}
	if _, err := model.playbacks.adopt(playback); err != nil {
		t.Fatalf("adopt playback: %v", err)
	}

	if err := model.shutdown(); err != nil {
		t.Fatalf("shutdown() error = %v", err)
	}
	if got := playback.closed.Load(); got != 1 {
		t.Fatalf("playback close count = %d, want 1", got)
	}
}

func TestRuntimeShutdownClosesPlaybackBeforeWaitingForFinishDrain(t *testing.T) {
	model := New(context.Background(), nil)
	playback := &cleanupPlayback{events: make(chan player.Event), release: make(chan struct{})}
	if _, err := model.playbacks.adopt(playback); err != nil {
		t.Fatalf("adopt playback: %v", err)
	}
	started := make(chan struct{})
	command := model.operations.command(func() tea.Msg {
		close(started)
		return playbackFinishedMsg{err: playback.WaitForEnd(context.Background())}
	})
	go command()
	<-started
	done := make(chan error, 1)
	go func() { done <- model.shutdown() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown() error = %v", err)
		}
	case <-time.After(time.Second):
		if playback.once.CompareAndSwap(false, true) {
			close(playback.release)
		}
		t.Fatal("shutdown waited for playback drain before closing its owner")
	}
}

func TestRunAndShutdownRecoversPanicAndClosesOwnedPlayback(t *testing.T) {
	model := New(context.Background(), nil)
	playback := &cleanupPlayback{events: make(chan player.Event)}
	if _, err := model.playbacks.adopt(playback); err != nil {
		t.Fatalf("adopt playback: %v", err)
	}
	err := runAndShutdown(model, func() error {
		panic("render panic fixture")
	})
	if err == nil || !strings.Contains(err.Error(), "terminal UI panicked") {
		t.Fatalf("runAndShutdown() error = %v", err)
	}
	if got := playback.closed.Load(); got != 1 {
		t.Fatalf("playback close count = %d, want 1", got)
	}
}

func TestQuitClosesPlaybackEvenIfAnotherScreenWasRendered(t *testing.T) {
	model := New(context.Background(), nil)
	playback := &cleanupPlayback{events: make(chan player.Event)}
	lease, err := model.playbacks.adopt(playback)
	if err != nil {
		t.Fatalf("adopt playback: %v", err)
	}
	model.playback = playback
	model.playbackLease = lease
	model.screen = screenLibrary

	_, command := model.quit()
	if command == nil {
		t.Fatal("quit did not schedule active playback teardown")
	}
	if message := command(); message == nil {
		t.Fatal("playback teardown returned nil message")
	}
	if got := playback.closed.Load(); got != 1 {
		t.Fatalf("playback close count = %d, want 1", got)
	}
}

func TestLifecycleCancellationFinishesPlaybackBeforeQuit(t *testing.T) {
	model := New(context.Background(), nil)
	playback := &cleanupPlayback{events: make(chan player.Event)}
	lease, err := model.playbacks.adopt(playback)
	if err != nil {
		t.Fatalf("adopt playback: %v", err)
	}
	model.playback = playback
	model.playbackLease = lease
	model.screen = screenPlayback

	updated, finishCommand := model.Update(lifecycleCanceledMsg{})
	finishing, ok := updated.(Model)
	if !ok {
		t.Fatalf("lifecycle cancellation returned %T, want Model", updated)
	}
	if finishCommand == nil {
		t.Fatal("lifecycle cancellation bypassed playback teardown")
	}
	finished, quitCommand := finishing.Update(finishCommand())
	if quitCommand == nil {
		t.Fatal("lifecycle cancellation did not quit after playback teardown")
	}
	if _, ok := quitCommand().(tea.QuitMsg); !ok {
		t.Fatal("lifecycle cancellation did not return tea.Quit after teardown")
	}
	if got := playback.closed.Load(); got != 1 {
		t.Fatalf("playback close count = %d, want 1; final model %T", got, finished)
	}
}
