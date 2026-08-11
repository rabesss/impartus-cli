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
	player              managedPlayer
	failures            <-chan error
	failureMutex        sync.Mutex
	failureWaitOwned    bool
	failureSourceClosed bool
	proxyFailure        error
	proxyFailureReady   bool
	cleanupProxy        func()
	closeOnce           sync.Once
	closeErr            error
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
	playback := newPlayback(managed, stream.Failures, stream.Cleanup)
	if err := managed.Load(ctx, stream.URL); err != nil {
		closeErr := playback.Close(context.Background())
		return nil, errors.Join(fmt.Errorf("load lecture in mpv: %w", err), closeErr)
	}
	return playback, nil
}

func newPlayback(managed managedPlayer, failures <-chan error, cleanupProxy func()) *Playback {
	return &Playback{player: managed, failures: failures, cleanupProxy: cleanupProxy}
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

// PollTerminal reports a terminal proxy or player result without initiating
// lifecycle cancellation. Resume readiness uses it only when its local timer
// expires, before classifying the wait as a timeout.
func (playback *Playback) PollTerminal() (bool, error) {
	if ready, failure := playback.pollProxyFailure(); ready {
		return true, failure
	}
	poller, ok := playback.player.(playbackTerminalPoller)
	if !ok {
		return false, nil
	}
	ready, result := poller.PollTerminal()
	if !ready {
		return playback.pollProxyFailure()
	}
	if failed, failure := playback.pollProxyFailure(); failed {
		return true, failure
	}
	return true, result
}

func (playback *Playback) pollProxyFailure() (bool, error) {
	if playback == nil {
		return false, nil
	}
	playback.failureMutex.Lock()
	defer playback.failureMutex.Unlock()
	if playback.proxyFailureReady {
		return true, playback.proxyFailure
	}
	if playback.failureWaitOwned || playback.failureSourceClosed || playback.failures == nil {
		return false, nil
	}
	return playback.receiveProxyFailureLocked(playback.failures)
}

func (playback *Playback) receiveProxyFailureLocked(failures <-chan error) (bool, error) {
	select {
	case failure, open := <-failures:
		if !open {
			playback.failureSourceClosed = true
			return false, nil
		}
		if failure == nil {
			return false, nil
		}
		playback.proxyFailure = failure
		playback.proxyFailureReady = true
		return true, failure
	default:
	}
	return false, nil
}

func (playback *Playback) beginProxyFailureWait() (bool, error) {
	playback.failureMutex.Lock()
	defer playback.failureMutex.Unlock()
	if playback.proxyFailureReady {
		return true, playback.proxyFailure
	}
	if !playback.failureSourceClosed && playback.failures != nil {
		if ready, failure := playback.receiveProxyFailureLocked(playback.failures); ready {
			return true, failure
		}
	}
	playback.failureWaitOwned = true
	return false, nil
}

func (playback *Playback) endProxyFailureWait() {
	playback.failureMutex.Lock()
	playback.failureWaitOwned = false
	playback.failureMutex.Unlock()
}

func (playback *Playback) proxyFailureSource() <-chan error {
	playback.failureMutex.Lock()
	defer playback.failureMutex.Unlock()
	if playback.failureSourceClosed {
		return nil
	}
	return playback.failures
}

func (playback *Playback) recordProxyFailure(failure error) error {
	if failure == nil {
		return nil
	}
	playback.failureMutex.Lock()
	if !playback.proxyFailureReady {
		playback.proxyFailure = failure
		playback.proxyFailureReady = true
	}
	result := playback.proxyFailure
	playback.failureMutex.Unlock()
	return result
}

func (playback *Playback) markProxyFailuresClosed() {
	playback.failureMutex.Lock()
	playback.failureSourceClosed = true
	playback.failureMutex.Unlock()
}

// WaitForEnd waits for media completion or player/process failure.
func (playback *Playback) WaitForEnd(ctx context.Context) error {
	if ready, failure := playback.beginProxyFailureWait(); ready {
		return failure
	}
	defer playback.endProxyFailureWait()

	playerResult := make(chan error, 1)
	go func() { playerResult <- playback.player.WaitForEnd(ctx) }()
	failures := playback.proxyFailureSource()
	for {
		select {
		case failure, open := <-failures:
			if !open {
				playback.markProxyFailuresClosed()
				failures = nil
				continue
			}
			if failure != nil {
				return playback.recordProxyFailure(failure)
			}
		case err := <-playerResult:
			select {
			case failure, open := <-failures:
				if open && failure != nil {
					return playback.recordProxyFailure(failure)
				}
				if !open {
					playback.markProxyFailuresClosed()
				}
			default:
			}
			return playbackWaitResult(ctx, err)
		case <-ctx.Done():
			return playback.playbackCancellationResult(ctx, failures, playerResult)
		}
	}
}

func (playback *Playback) playbackCancellationResult(ctx context.Context, failures <-chan error, playerResult <-chan error) error {
	for {
		select {
		case failure, open := <-failures:
			if !open {
				playback.markProxyFailuresClosed()
				failures = nil
				continue
			}
			if failure != nil {
				return playback.recordProxyFailure(failure)
			}
		case err := <-playerResult:
			select {
			case failure, open := <-failures:
				if open && failure != nil {
					return playback.recordProxyFailure(failure)
				}
				if !open {
					playback.markProxyFailuresClosed()
				}
			default:
			}
			return playbackWaitResult(ctx, err)
		}
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

// SeekAbsolute seeks to an absolute playback position in seconds.
func (playback *Playback) SeekAbsolute(ctx context.Context, seconds float64) error {
	return playback.player.SeekAbsolute(ctx, seconds)
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
