package watch

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/client"
)

// A lecture that lists no stream is reported at once: retrying cannot help.
func TestWatcherDoesNotRetryLectureWithoutStreams(t *testing.T) {
	t.Parallel()

	store := openWatchStore(t)
	cfg := watchTestConfig(t)
	target := cfg.Watch.Targets[0]
	lecture := watchLecture(target, 84, 5, "No streams")
	source := &fakeSource{lectures: map[[2]int]client.Lectures{{target.SubjectID, target.SessionID}: {lecture}}, errors: map[[2]int]error{}}
	producer := &fakeProducer{
		cfg: cfg, downloadErrors: map[int]error{},
		fetchErrors: map[int]error{lecture.TTID: fmt.Errorf("%w (1 of 1 lectures listed none)", client.ErrNoStreams)},
	}
	backoffs := 0
	_, err := New(cfg, source, producer, store, Options{
		Once: true, MaxRetries: 3,
		RetryBackoff: func(int) time.Duration { backoffs++; return 0 },
	}).Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want the no-streams failure")
	}
	producer.mu.Lock()
	fetches := producer.fetches
	producer.mu.Unlock()
	if fetches != 1 || backoffs != 0 {
		t.Fatalf("fetches = %d, backoffs = %d, want 1 fetch and no retry", fetches, backoffs)
	}
}
