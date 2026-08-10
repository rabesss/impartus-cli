package app

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/player"
)

// Playback owns one mpv session and its corresponding loopback stream proxy.
type Playback struct {
	player       managedPlayer
	failures     <-chan error
	cleanupProxy func()
	closeOnce    sync.Once
	closeErr     error
}

// StartPlayback starts a loopback stream, launches supervised mpv idle, and
// sends the capability URL only through JSON IPC.
func (service *Service) StartPlayback(ctx context.Context, playlist client.ParsedPlaylist) (*Playback, error) {
	if service == nil || service.streams == nil || service.startPlayer == nil {
		return nil, errors.New("application playback service is not configured")
	}
	stream, err := service.streams.StartPlaybackStream(ctx, playlist)
	if err != nil {
		return nil, fmt.Errorf("start local playback stream: %w", err)
	}
	managed, err := service.startPlayer(ctx, service.playerOptions)
	if err != nil {
		stream.Cleanup()
		return nil, fmt.Errorf("start supervised mpv: %w", err)
	}
	playback := &Playback{player: managed, failures: stream.Failures, cleanupProxy: stream.Cleanup}
	if err := managed.Load(ctx, stream.URL); err != nil {
		closeErr := playback.Close(context.Background())
		return nil, errors.Join(fmt.Errorf("load lecture in mpv: %w", err), closeErr)
	}
	return playback, nil
}

// PlaySequential preserves the existing CLI behavior of playing each selected
// lecture in order, while keeping process and proxy ownership explicit.
func (service *Service) PlaySequential(ctx context.Context, lectures client.Lectures, onStart func(client.ParsedPlaylist)) error {
	if service == nil || service.streams == nil {
		return errors.New("application playback service is not configured")
	}
	playlists, err := service.streams.FetchLecturePlaylists(ctx, lectures)
	if err != nil {
		return err
	}
	if len(playlists) == 0 {
		return errors.New("no playlists available for selected lectures")
	}
	for _, playlist := range playlists {
		if onStart != nil {
			onStart(playlist)
		}
		playback, err := service.StartPlayback(ctx, playlist)
		if err != nil {
			return err
		}
		waitErr := playback.WaitForEnd(ctx)
		closeErr := playback.Close(context.Background())
		if waitErr != nil || closeErr != nil {
			return errors.Join(waitErr, closeErr)
		}
	}
	return nil
}

// Events exposes the player's state stream for the TUI.
func (playback *Playback) Events() <-chan player.Event { return playback.player.Events() }

// WaitForEnd waits for media completion or player/process failure.
func (playback *Playback) WaitForEnd(ctx context.Context) error {
	select {
	case failure := <-playback.failures:
		return failure
	default:
	}

	playerResult := make(chan error, 1)
	go func() { playerResult <- playback.player.WaitForEnd(ctx) }()
	select {
	case failure := <-playback.failures:
		return failure
	case err := <-playerResult:
		select {
		case failure := <-playback.failures:
			return failure
		default:
			return playbackWaitResult(ctx, err)
		}
	case <-ctx.Done():
		return playbackCancellationResult(ctx, playback.failures, playerResult)
	}
}

func playbackCancellationResult(ctx context.Context, failures <-chan error, playerResult <-chan error) error {
	select {
	case failure := <-failures:
		if failure != nil {
			return failure
		}
	default:
	}
	select {
	case err := <-playerResult:
		return playbackWaitResult(ctx, err)
	default:
		return ctx.Err()
	}
}

func playbackWaitResult(ctx context.Context, playerErr error) error {
	if playerErr != nil {
		return playerErr
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	return nil
}

// Pause controls playback pause state.
func (playback *Playback) Pause(ctx context.Context, paused bool) error {
	return playback.player.Pause(ctx, paused)
}

// SeekRelative seeks by signed seconds.
func (playback *Playback) SeekRelative(ctx context.Context, seconds float64) error {
	return playback.player.SeekRelative(ctx, seconds)
}

// SetVolume sets volume in percent.
func (playback *Playback) SetVolume(ctx context.Context, volume float64) error {
	return playback.player.SetVolume(ctx, volume)
}

// SetMute changes mute state.
func (playback *Playback) SetMute(ctx context.Context, muted bool) error {
	return playback.player.SetMute(ctx, muted)
}

// SetSpeed changes playback speed.
func (playback *Playback) SetSpeed(ctx context.Context, speed float64) error {
	return playback.player.SetSpeed(ctx, speed)
}

// CycleVideo selects the next video track/view.
func (playback *Playback) CycleVideo(ctx context.Context) error {
	return playback.player.CycleVideo(ctx)
}

// Close tears down the player first, then the capability-bearing proxy.
func (playback *Playback) Close(ctx context.Context) error {
	if playback == nil {
		return nil
	}
	playback.closeOnce.Do(func() {
		if playback.player != nil {
			playback.closeErr = playback.player.Close(ctx)
		}
		if playback.cleanupProxy != nil {
			playback.cleanupProxy()
		}
	})
	return playback.closeErr
}
