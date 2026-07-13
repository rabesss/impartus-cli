package server

import (
	"context"
	"errors"
)

var errJobStoreClosed = errors.New("job store is closed")

// Flush waits until all mutations visible at call time have been attempted on disk.
func (js *JobStore) Flush(ctx context.Context) error {
	if js.coordinator == nil {
		return nil
	}
	js.mu.RLock()
	revision := js.revision
	js.mu.RUnlock()
	return js.coordinator.flushTo(ctx, revision)
}

// Close flushes pending persistence and stops the persistence worker. It is idempotent.
func (js *JobStore) Close(ctx context.Context) error {
	if js.coordinator == nil {
		return nil
	}
	js.lifecycleMu.Lock()
	js.closed = true
	drained := js.producersDone
	js.lifecycleMu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-drained:
		return js.coordinator.close(ctx)
	}
}

func (js *JobStore) beginMutation() error {
	js.lifecycleMu.Lock()
	if js.closed {
		js.lifecycleMu.Unlock()
		return errJobStoreClosed
	}
	if js.producers == 0 {
		js.producersDone = make(chan struct{})
	}
	js.producers++
	js.lifecycleMu.Unlock()
	js.mutationMu.Lock()
	return nil
}

func (js *JobStore) endMutation() {
	js.mutationMu.Unlock()
	js.lifecycleMu.Lock()
	js.producers--
	if js.producers == 0 {
		close(js.producersDone)
	}
	js.lifecycleMu.Unlock()
}

func closedSignal() chan struct{} {
	closed := make(chan struct{})
	close(closed)
	return closed
}
