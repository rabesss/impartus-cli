package server

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/vbauerster/mpb/v8"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/downloader"
)

type fakePlaylistJoiner struct {
	results map[int]downloader.JoinResult
	errors  map[int]error
}

func (fake fakePlaylistJoiner) DownloadAndJoinPlaylist(_ context.Context, playlist client.ParsedPlaylist, _ *mpb.Progress, _ *downloader.ProgressTracker) (downloader.JoinResult, error) {
	return fake.results[playlist.ID], fake.errors[playlist.ID]
}

func TestPlaylistDownloadRunnerSkipsUnavailableSelectedMedia(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	joiner := fakePlaylistJoiner{
		results: map[int]downloader.JoinResult{2: {LeftOutput: "available.mp4"}},
		errors:  map[int]error{1: downloader.ErrNoSelectedMedia},
	}
	var progress []int
	outputs, err := newPlaylistDownloadRunner(1).run(ctx, cancel, joiner, []client.ParsedPlaylist{
		{ID: 1, SeqNo: 1},
		{ID: 2, SeqNo: 2},
	}, func(done int) bool {
		progress = append(progress, done)
		return true
	})
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if len(outputs) != 1 || outputs[0] != "available.mp4" {
		t.Fatalf("outputs = %v, want available media", outputs)
	}
	if len(progress) != 2 || progress[0] != 1 || progress[1] != 2 {
		t.Fatalf("progress = %v, want [1 2]", progress)
	}
}

func TestPlaylistDownloadRunnerPreservesOtherFailures(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	want := errors.New("download failed")
	_, err := newPlaylistDownloadRunner(1).run(ctx, cancel, fakePlaylistJoiner{
		errors: map[int]error{1: want},
	}, []client.ParsedPlaylist{{ID: 1, SeqNo: 1}}, func(int) bool { return true })
	if !errors.Is(err, want) {
		t.Fatalf("run() error = %v, want wrapped failure", err)
	}
}

func TestNewPlaylistDownloadRunner(t *testing.T) {
	tests := []struct {
		name    string
		workers int
		want    int
	}{
		{
			name:    "positive workers",
			workers: 5,
			want:    5,
		},
		{
			name:    "one worker",
			workers: 1,
			want:    1,
		},
		{
			name:    "zero workers defaults to one",
			workers: 0,
			want:    1,
		},
		{
			name:    "negative workers defaults to one",
			workers: -3,
			want:    1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := newPlaylistDownloadRunner(tc.workers)
			if runner.workers != tc.want {
				t.Errorf("newPlaylistDownloadRunner(%d).workers = %d, want %d", tc.workers, runner.workers, tc.want)
			}
		})
	}
}

func TestAppendOutputs(t *testing.T) {
	tests := []struct {
		name       string
		initial    []string
		newOutputs []string
		want       []string
	}{
		{
			name:       "append to empty slice",
			initial:    []string{},
			newOutputs: []string{"a", "b"},
			want:       []string{"a", "b"},
		},
		{
			name:       "append to existing slice",
			initial:    []string{"x"},
			newOutputs: []string{"a", "b"},
			want:       []string{"x", "a", "b"},
		},
		{
			name:       "append multiple to existing slice",
			initial:    []string{"x", "y", "z"},
			newOutputs: []string{"a", "b", "c"},
			want:       []string{"x", "y", "z", "a", "b", "c"},
		},
		{
			name:       "empty newOutputs does nothing",
			initial:    []string{"x", "y"},
			newOutputs: []string{},
			want:       []string{"x", "y"},
		},
		{
			name:       "nil newOutputs does nothing",
			initial:    []string{"x", "y"},
			newOutputs: nil,
			want:       []string{"x", "y"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			outputs := tc.initial
			appendOutputs(&mu, &outputs, tc.newOutputs)
			if len(outputs) != len(tc.want) {
				t.Errorf("appendOutputs() len = %d, want %d", len(outputs), len(tc.want))
				return
			}
			for i := range outputs {
				if outputs[i] != tc.want[i] {
					t.Errorf("appendOutputs()[%d] = %q, want %q", i, outputs[i], tc.want[i])
				}
			}
		})
	}
}

func TestAppendOutputsConcurrent(t *testing.T) {
	// Test that appendOutputs is safe for concurrent use
	var mu sync.Mutex
	outputs := make([]string, 0)
	numGoroutines := 10
	itemsPerGoroutine := 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(base int) {
			defer wg.Done()
			newOutputs := make([]string, itemsPerGoroutine)
			for j := 0; j < itemsPerGoroutine; j++ {
				newOutputs[j] = string(rune('a' + (base+j)%26))
			}
			appendOutputs(&mu, &outputs, newOutputs)
		}(i)
	}

	wg.Wait()

	expectedLen := numGoroutines * itemsPerGoroutine
	if len(outputs) != expectedLen {
		t.Errorf("concurrent appendOutputs: expected %d items, got %d", expectedLen, len(outputs))
	}
}
