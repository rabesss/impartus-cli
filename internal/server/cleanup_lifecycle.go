package server

import "sync"

// cleanupStarter starts one server-owned background cleanup worker and returns
// a function that stops it. A starter or stop callback may call Start again (it
// is a no-op), but neither may call Close: Close deliberately waits for an
// in-flight Start and teardown to finish before returning.
type cleanupStarter func() func()

type cleanupLifecycleState uint8

const (
	cleanupInactive cleanupLifecycleState = iota
	cleanupStarting
	cleanupRunning
	cleanupClosing
	cleanupClosed
)

// cleanupLifecycle owns the background cleanup workers used by APIServer. A
// lifecycle is inactive when constructed, starts at most once, and closes at
// most once. Close before Start permanently prevents the workers from starting.
type cleanupLifecycle struct {
	mu        sync.Mutex
	starters  []cleanupStarter
	stops     []func()
	state     cleanupLifecycleState
	startDone chan struct{}
	closeDone chan struct{}
}

func newCleanupLifecycle(starters ...cleanupStarter) *cleanupLifecycle {
	return &cleanupLifecycle{
		starters:  starters,
		closeDone: make(chan struct{}),
	}
}

func (l *cleanupLifecycle) Start() {
	l.mu.Lock()
	if l.state != cleanupInactive {
		l.mu.Unlock()
		return
	}
	l.state = cleanupStarting
	l.startDone = make(chan struct{})
	starters := append([]cleanupStarter(nil), l.starters...)
	l.mu.Unlock()

	stops := make([]func(), 0, len(starters))
	defer func() {
		l.mu.Lock()
		l.stops = stops
		l.state = cleanupRunning
		close(l.startDone)
		l.mu.Unlock()
	}()

	for _, start := range starters {
		if start == nil {
			continue
		}
		if stop := start(); stop != nil {
			stops = append(stops, stop)
		}
	}
}

func (l *cleanupLifecycle) Close() {
	for {
		l.mu.Lock()
		switch l.state {
		case cleanupInactive:
			l.state = cleanupClosed
			close(l.closeDone)
			l.mu.Unlock()
			return
		case cleanupStarting:
			done := l.startDone
			l.mu.Unlock()
			<-done
			continue
		case cleanupRunning:
			l.state = cleanupClosing
			stops := l.stops
			l.stops = nil
			l.mu.Unlock()
			l.stopWorkers(stops)
			return
		case cleanupClosing:
			done := l.closeDone
			l.mu.Unlock()
			<-done
			return
		case cleanupClosed:
			l.mu.Unlock()
			return
		default:
			l.mu.Unlock()
			return
		}
	}
}

func (l *cleanupLifecycle) stopWorkers(stops []func()) {
	defer func() {
		l.mu.Lock()
		l.state = cleanupClosed
		close(l.closeDone)
		l.mu.Unlock()
	}()

	// Stop in reverse startup order, matching normal resource teardown.
	for i := len(stops) - 1; i >= 0; i-- {
		stops[i]()
	}
}
