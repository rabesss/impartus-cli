package watch

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/events"
)

func TestWatcherCompletesPredownloadTransitionsBeforeHonoringCancellation(t *testing.T) {
	t.Parallel()

	baseStore := openWatchStore(t)
	store := &contextRecordingStore{Store: baseStore}
	cfg := watchTestConfig(t)
	target := cfg.Watch.Targets[0]
	lecture := watchLecture(target, 39, 1, "Cancel after playlist")
	source := &fakeSource{lectures: map[[2]int]client.Lectures{{target.SubjectID, target.SessionID}: {lecture}}, errors: map[[2]int]error{}}
	ctx, cancel := context.WithCancel(context.Background())
	producer := &fakeProducer{
		cfg: cfg, fetchErrors: map[int]error{}, downloadErrors: map[int]error{},
		afterFetch: cancel, blockDownload: true,
	}

	_, err := New(cfg, source, producer, store, Options{Once: true}).Run(ctx)
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrDurableState) {
		t.Fatalf("Run() error = %v", err)
	}
	if store.createContextError != nil || store.startContextError != nil {
		t.Fatalf("pre-download contexts: create=%v start=%v", store.createContextError, store.startContextError)
	}
}

func TestWatcherCancellationBeforeJobDoesNotEmitLectureFailure(t *testing.T) {
	t.Parallel()

	store := openWatchStore(t)
	cfg := watchTestConfig(t)
	target := cfg.Watch.Targets[0]
	lecture := watchLecture(target, 71, 1, "Cancel during playlist resolution")
	source := &fakeSource{lectures: map[[2]int]client.Lectures{{target.SubjectID, target.SessionID}: {lecture}}, errors: map[[2]int]error{}}
	ctx, cancel := context.WithCancel(context.Background())
	producer := &fakeProducer{
		cfg: cfg, fetchErrors: map[int]error{lecture.TTID: context.Canceled}, downloadErrors: map[int]error{},
		beforeFetch: cancel,
	}
	emitter := &recordingEmitter{failType: events.LectureFailed, failures: 1}

	_, err := New(cfg, source, producer, store, Options{Once: true, Emitter: emitter}).Run(ctx)
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrEventDelivery) {
		t.Fatalf("Run() error = %v, want cancellation without event-delivery promotion", err)
	}
	if got := emitter.types(); !reflect.DeepEqual(got, []string{events.JobStarted, events.JobCanceled}) {
		t.Fatalf("event types = %v, want cancellation without lecture.failed", got)
	}
}
