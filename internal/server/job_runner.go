package server

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/rabesss/impartus-cli/internal/downloader"
)

type playlistDownloadRunner struct {
	workers int
}

func newPlaylistDownloadRunner(workers int) playlistDownloadRunner {
	if workers < 1 {
		workers = 1
	}
	return playlistDownloadRunner{workers: workers}
}

func (r playlistDownloadRunner) run(ctx context.Context, cancel context.CancelFunc, d *downloader.Downloader, playlists []downloader.ParsedPlaylist, onProgress func(done int) bool) ([]string, error) {
	tasks := make(chan downloader.ParsedPlaylist)
	errCh := make(chan error, 1)
	doneCh := make(chan struct{})
	outputs := make([]string, 0)
	var outputsMu sync.Mutex
	var completed int32
	var wg sync.WaitGroup

	workerFn := func() {
		defer wg.Done()
		for playlist := range tasks {
			if ctx.Err() != nil {
				return
			}
			result, err := d.DownloadAndJoinPlaylist(ctx, playlist, nil, nil)
			if err != nil {
				select {
				case errCh <- fmt.Errorf("lecture %03d: %w", playlist.SeqNo, err):
				default:
				}
				cancel()
				return
			}
			appendOutputs(&outputsMu, &outputs, extractJoinOutputs(result))
			done := int(atomic.AddInt32(&completed, 1))
			if !onProgress(done) {
				return
			}
		}
	}

	for i := 0; i < r.workers; i++ {
		wg.Add(1)
		go workerFn()
	}

	go func() {
		defer close(doneCh)
		defer wg.Wait()
		defer close(tasks)
		for _, playlist := range playlists {
			select {
			case <-ctx.Done():
				return
			case tasks <- playlist:
			}
		}
	}()

	select {
	case err := <-errCh:
		return nil, err
	case <-doneCh:
		outputsMu.Lock()
		defer outputsMu.Unlock()
		return append([]string{}, outputs...), nil
	}
}

func appendOutputs(mu *sync.Mutex, outputs *[]string, newOutputs []string) {
	if len(newOutputs) == 0 {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	*outputs = append(*outputs, newOutputs...)
}
