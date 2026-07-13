package server

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAPIServerConstructionLeavesCleanupInactive(t *testing.T) {
	s := newAPIServer(validServerConfig())

	s.cleanupLifecycle.mu.Lock()
	state := s.cleanupLifecycle.state
	workerCount := len(s.cleanupLifecycle.stops)
	s.cleanupLifecycle.mu.Unlock()

	if state != cleanupInactive {
		t.Fatalf("constructed server cleanup state = %d, want inactive", state)
	}
	if workerCount != 0 {
		t.Fatalf("constructed server has %d active cleanup workers, want 0", workerCount)
	}

	// Closing a constructed-but-never-started lifecycle is safe and idempotent.
	s.cleanupLifecycle.Close()
	s.cleanupLifecycle.Close()
}

func TestAPIServerStartStopsCleanupExactlyOnce(t *testing.T) {
	t.Chdir(t.TempDir())
	s := NewAPIServerWithLogin("not-a-port", validServerConfig(), nil)

	var starts atomic.Int32
	var stops atomic.Int32
	s.cleanupLifecycle = newCleanupLifecycle(func() func() {
		starts.Add(1)
		return func() { stops.Add(1) }
	})

	if err := s.Start(); err == nil {
		t.Fatal("expected invalid listen port to fail")
	}
	if got := starts.Load(); got != 1 {
		t.Fatalf("cleanup starts = %d, want 1", got)
	}
	if got := stops.Load(); got != 1 {
		t.Fatalf("cleanup stops = %d, want 1", got)
	}

	// Start already closed the lifecycle; repeated closure remains a no-op.
	s.cleanupLifecycle.Close()
	s.cleanupLifecycle.Close()
	if got := stops.Load(); got != 1 {
		t.Fatalf("cleanup stops after repeated close = %d, want 1", got)
	}
}

func TestAPIServerContextShutdownStopsCleanupExactlyOnce(t *testing.T) {
	t.Chdir(t.TempDir())
	s := NewAPIServerWithLogin("0", validServerConfig(), nil)

	var starts atomic.Int32
	var stops atomic.Int32
	s.cleanupLifecycle = newCleanupLifecycle(func() func() {
		starts.Add(1)
		return func() { stops.Add(1) }
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := make(chan error, 1)
	go func() {
		result <- s.Start(ctx)
	}()

	select {
	case err := <-result:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("Start() error = %v, want http.ErrServerClosed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop after context cancellation")
	}

	if got := starts.Load(); got != 1 {
		t.Fatalf("cleanup starts = %d, want 1", got)
	}
	if got := stops.Load(); got != 1 {
		t.Fatalf("cleanup stops = %d, want 1", got)
	}
}

func TestCleanupLifecycleConcurrentCloseIsIdempotent(t *testing.T) {
	var starts atomic.Int32
	var stops atomic.Int32
	lifecycle := newCleanupLifecycle(func() func() {
		starts.Add(1)
		return func() { stops.Add(1) }
	})
	lifecycle.Start()

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lifecycle.Close()
		}()
	}
	wg.Wait()

	// Neither operation can restart a closed lifecycle.
	lifecycle.Start()
	if got := starts.Load(); got != 1 {
		t.Fatalf("cleanup starts = %d, want 1", got)
	}
	if got := stops.Load(); got != 1 {
		t.Fatalf("cleanup stops = %d, want 1", got)
	}
}

func TestCleanupLifecycleCloseWaitsForInFlightStart(t *testing.T) {
	starterEntered := make(chan struct{})
	releaseStarter := make(chan struct{})
	stopCalled := make(chan struct{})
	lifecycle := newCleanupLifecycle(func() func() {
		close(starterEntered)
		<-releaseStarter
		return func() { close(stopCalled) }
	})

	startReturned := make(chan struct{})
	go func() {
		lifecycle.Start()
		close(startReturned)
	}()
	<-starterEntered

	closeReturned := make(chan struct{})
	go func() {
		lifecycle.Close()
		close(closeReturned)
	}()

	select {
	case <-closeReturned:
		t.Fatal("Close returned while Start was still creating a worker")
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseStarter)
	select {
	case <-startReturned:
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after starter was released")
	}
	select {
	case <-closeReturned:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return after in-flight Start completed")
	}
	select {
	case <-stopCalled:
	default:
		t.Fatal("Close returned without stopping the worker created by Start")
	}
}

func TestCleanupLifecycleConcurrentCloseWaitsForTeardown(t *testing.T) {
	stopEntered := make(chan struct{})
	releaseStop := make(chan struct{})
	lifecycle := newCleanupLifecycle(func() func() {
		return func() {
			close(stopEntered)
			<-releaseStop
		}
	})
	lifecycle.Start()

	firstCloseReturned := make(chan struct{})
	go func() {
		lifecycle.Close()
		close(firstCloseReturned)
	}()
	<-stopEntered

	secondCloseReturned := make(chan struct{})
	go func() {
		lifecycle.Close()
		close(secondCloseReturned)
	}()
	select {
	case <-secondCloseReturned:
		t.Fatal("concurrent Close returned before worker teardown completed")
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseStop)
	for name, returned := range map[string]<-chan struct{}{
		"first Close":  firstCloseReturned,
		"second Close": secondCloseReturned,
	} {
		select {
		case <-returned:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s did not return after worker teardown", name)
		}
	}
}

func TestCleanupLifecycleCallbacksMayReenterStart(t *testing.T) {
	var starts atomic.Int32
	var stops atomic.Int32
	var lifecycle *cleanupLifecycle
	lifecycle = newCleanupLifecycle(func() func() {
		starts.Add(1)
		lifecycle.Start()
		return func() {
			lifecycle.Start()
			stops.Add(1)
		}
	})

	startReturned := make(chan struct{})
	go func() {
		lifecycle.Start()
		close(startReturned)
	}()
	select {
	case <-startReturned:
	case <-time.After(5 * time.Second):
		t.Fatal("starter callback reentry into Start deadlocked")
	}

	closeReturned := make(chan struct{})
	go func() {
		lifecycle.Close()
		close(closeReturned)
	}()
	select {
	case <-closeReturned:
	case <-time.After(5 * time.Second):
		t.Fatal("stop callback reentry into Start deadlocked")
	}
	if got := starts.Load(); got != 1 {
		t.Fatalf("cleanup starts = %d, want 1", got)
	}
	if got := stops.Load(); got != 1 {
		t.Fatalf("cleanup stops = %d, want 1", got)
	}
}

func TestCleanupLifecycleCloseBeforeStartPreventsWorkers(t *testing.T) {
	var starts atomic.Int32
	lifecycle := newCleanupLifecycle(func() func() {
		starts.Add(1)
		return func() {}
	})

	lifecycle.Close()
	lifecycle.Start()
	if got := starts.Load(); got != 0 {
		t.Fatalf("cleanup starts after close = %d, want 0", got)
	}
}
