package server

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/vbauerster/mpb/v8"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/downloader"
)

type playlistJoiner interface {
	DownloadAndJoinPlaylist(context.Context, client.ParsedPlaylist, *mpb.Progress, *downloader.ProgressTracker) (downloader.JoinResult, error)
}

type playlistDownloadRunner struct {
	workers int
}

type playlistDownloadWorker struct {
	ctx        context.Context
	cancel     context.CancelFunc
	downloader playlistJoiner
	tasks      <-chan client.ParsedPlaylist
	state      *playlistDownloadState
	onProgress func(done int) bool
}

type playlistDownloadState struct {
	outputsMu  sync.Mutex
	outputs    []string
	progressMu sync.Mutex
	completed  int32
	errOnce    sync.Once
	err        error
}

func newPlaylistDownloadRunner(workers int) playlistDownloadRunner {
	if workers < 1 {
		workers = 1
	}
	return playlistDownloadRunner{workers: workers}
}

func (r playlistDownloadRunner) run(ctx context.Context, cancel context.CancelFunc, d playlistJoiner, playlists []client.ParsedPlaylist, onProgress func(done int) bool) ([]string, error) {
	tasks := make(chan client.ParsedPlaylist)
	state := &playlistDownloadState{outputs: make([]string, 0)}
	var wg sync.WaitGroup
	worker := playlistDownloadWorker{
		ctx:        ctx,
		cancel:     cancel,
		downloader: d,
		tasks:      tasks,
		state:      state,
		onProgress: onProgress,
	}

	for i := 0; i < r.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			worker.run()
		}()
	}

dispatch:
	for _, playlist := range playlists {
		select {
		case <-ctx.Done():
			break dispatch
		case tasks <- playlist:
		}
	}
	close(tasks)
	wg.Wait()
	return state.result(ctx)
}

func (w playlistDownloadWorker) run() {
	for playlist := range w.tasks {
		if w.ctx.Err() != nil {
			return
		}
		result, err := w.downloader.DownloadAndJoinPlaylist(w.ctx, playlist, nil, nil)
		if err != nil && !errors.Is(err, downloader.ErrNoSelectedMedia) {
			if ctxErr := w.ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
				return
			}
			w.state.recordError(fmt.Errorf("lecture %03d: %w", playlist.SeqNo, err), w.cancel)
			return
		}
		if err == nil {
			appendOutputs(&w.state.outputsMu, &w.state.outputs, result.OutputPaths())
		}
		if !w.state.advance(w.ctx, w.onProgress, w.cancel) {
			return
		}
	}
}

func (s *playlistDownloadState) recordError(err error, cancel context.CancelFunc) {
	s.errOnce.Do(func() {
		s.err = err
		cancel()
	})
}

func (s *playlistDownloadState) advance(ctx context.Context, onProgress func(done int) bool, cancel context.CancelFunc) bool {
	s.progressMu.Lock()
	defer s.progressMu.Unlock()
	if ctx.Err() != nil {
		return false
	}
	s.completed++
	if onProgress(int(s.completed)) {
		return true
	}
	cancel()
	return false
}

func (s *playlistDownloadState) result(ctx context.Context) ([]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.outputsMu.Lock()
	defer s.outputsMu.Unlock()
	if len(s.outputs) == 0 {
		return nil, downloader.ErrNoMediaOutputs
	}
	return append([]string{}, s.outputs...), nil
}

func appendOutputs(mu *sync.Mutex, outputs *[]string, newOutputs []string) {
	if len(newOutputs) == 0 {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	*outputs = append(*outputs, newOutputs...)
}
