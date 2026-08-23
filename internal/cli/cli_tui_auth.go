package cli

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/rabesss/impartus-cli/internal/app"
	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/library"
	"github.com/rabesss/impartus-cli/internal/tuiproto"
	"github.com/rabesss/impartus-cli/internal/tuisession"
)

var errTUIAuthenticationUnavailable = errors.New("tui upstream authentication is unavailable")

type tuiAuthenticationAttemptError struct {
	err error
}

func (err *tuiAuthenticationAttemptError) Error() string {
	return errTUIAuthenticationUnavailable.Error()
}

func (err *tuiAuthenticationAttemptError) Unwrap() error {
	return err.err
}

type tuiRemoteService interface {
	Courses(context.Context) (client.Courses, error)
	Lectures(context.Context, client.Course) (client.Lectures, error)
	DownloadLecture(context.Context, client.Lecture) (app.DownloadResult, error)
	RecordPlayback(context.Context, library.PlaybackState) error
	ResumeLecture(context.Context, client.Lecture) (library.PlaybackState, bool, error)
	StartLecture(context.Context, client.Lecture, float64) (app.PlaybackStart, error)
}

type tuiArtifactStore interface {
	ListArtifacts(context.Context) ([]library.ArtifactRecord, error)
}

type tuiConfigLoader func() (*config.Config, error)
type tuiCandidateBuilder func(context.Context, *config.Config) (tuiRemoteService, error)

type tuiAuthenticationRetry struct {
	done chan struct{}
	err  error
}

// tuiAuthenticationCoordinator is a stable session dependency. Candidate
// services are constructed without holding the state lock and published only
// after complete success. Remote calls snapshot one complete candidate and
// release the lock before invoking it.
type tuiAuthenticationCoordinator struct {
	store tuiArtifactStore
	load  tuiConfigLoader
	build tuiCandidateBuilder

	retryMu     sync.Mutex
	activeRetry *tuiAuthenticationRetry
	stateMu     sync.RWMutex
	service     tuiRemoteService
}

func newTUIAuthenticationCoordinator(
	store tuiArtifactStore,
	load tuiConfigLoader,
	build tuiCandidateBuilder,
) *tuiAuthenticationCoordinator {
	return &tuiAuthenticationCoordinator{store: store, load: load, build: build}
}

func (coordinator *tuiAuthenticationCoordinator) Status() tuiproto.AuthStatus {
	coordinator.stateMu.RLock()
	ready := coordinator.service != nil
	coordinator.stateMu.RUnlock()
	if ready {
		return tuiproto.AuthStatusReady
	}
	return tuiproto.AuthStatusUnavailable
}

func (coordinator *tuiAuthenticationCoordinator) Retry(ctx context.Context) (err error) {
	coordinator.retryMu.Lock()
	if active := coordinator.activeRetry; active != nil {
		coordinator.retryMu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-active.done:
			return active.err
		}
	}
	active := &tuiAuthenticationRetry{done: make(chan struct{})}
	coordinator.activeRetry = active
	coordinator.retryMu.Unlock()

	defer func() {
		recovered := recover()
		coordinator.retryMu.Lock()
		active.err = err
		if recovered != nil {
			active.err = errTUIAuthenticationUnavailable
		}
		close(active.done)
		coordinator.activeRetry = nil
		coordinator.retryMu.Unlock()
		if recovered != nil {
			panic(recovered)
		}
	}()

	err = coordinator.retryOnce(ctx)
	return err
}

func (coordinator *tuiAuthenticationCoordinator) retryOnce(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cfg, err := coordinator.load()
	if err != nil {
		if errors.Is(err, config.ErrCredentialsRequired) {
			return err
		}
		return fmt.Errorf("%w: %v", tuisession.ErrAuthenticationConfiguration, err)
	}
	candidate, err := coordinator.build(ctx, cfg)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return context.Canceled
		}
		return &tuiAuthenticationAttemptError{err: err}
	}
	if candidate == nil {
		return errors.New("tui authentication produced no application service")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	coordinator.stateMu.Lock()
	coordinator.service = candidate
	coordinator.stateMu.Unlock()
	return nil
}

func (coordinator *tuiAuthenticationCoordinator) snapshot() (tuiRemoteService, error) {
	coordinator.stateMu.RLock()
	service := coordinator.service
	coordinator.stateMu.RUnlock()
	if service == nil {
		return nil, errTUIAuthenticationUnavailable
	}
	return service, nil
}

func (coordinator *tuiAuthenticationCoordinator) Courses(ctx context.Context) (client.Courses, error) {
	service, err := coordinator.snapshot()
	if err != nil {
		return nil, err
	}
	return service.Courses(ctx)
}

func (coordinator *tuiAuthenticationCoordinator) Lectures(ctx context.Context, course client.Course) (client.Lectures, error) {
	service, err := coordinator.snapshot()
	if err != nil {
		return nil, err
	}
	return service.Lectures(ctx, course)
}

func (coordinator *tuiAuthenticationCoordinator) Artifacts(ctx context.Context) ([]library.ArtifactRecord, error) {
	return coordinator.store.ListArtifacts(ctx)
}

func (coordinator *tuiAuthenticationCoordinator) DownloadLecture(ctx context.Context, lecture client.Lecture) (app.DownloadResult, error) {
	service, err := coordinator.snapshot()
	if err != nil {
		return app.DownloadResult{}, err
	}
	return service.DownloadLecture(ctx, lecture)
}

func (coordinator *tuiAuthenticationCoordinator) RecordPlayback(ctx context.Context, state library.PlaybackState) error {
	service, err := coordinator.snapshot()
	if err != nil {
		return err
	}
	return service.RecordPlayback(ctx, state)
}

func (coordinator *tuiAuthenticationCoordinator) ResumeLecture(ctx context.Context, lecture client.Lecture) (library.PlaybackState, bool, error) {
	service, err := coordinator.snapshot()
	if err != nil {
		return library.PlaybackState{}, false, err
	}
	return service.ResumeLecture(ctx, lecture)
}

func (coordinator *tuiAuthenticationCoordinator) StartLecture(ctx context.Context, lecture client.Lecture, position float64) (app.PlaybackStart, error) {
	service, err := coordinator.snapshot()
	if err != nil {
		return app.PlaybackStart{}, err
	}
	return service.StartLecture(ctx, lecture, position)
}

func isRecoverableTUIAuthenticationError(err error) bool {
	if errors.Is(err, config.ErrCredentialsRequired) {
		return true
	}
	var attemptErr *tuiAuthenticationAttemptError
	return errors.As(err, &attemptErr)
}
