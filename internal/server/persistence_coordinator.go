package server

import (
	"context"
	"errors"
	"sync"
	"time"
)

const persistenceDebounce = 150 * time.Millisecond

var errPersistenceClosed = errors.New("job persistence is closed")

// persistenceCoordinator serializes and coalesces immutable job snapshots.
// Only its worker calls save, so snapshots can never be written out of order.
type persistenceCoordinator struct {
	mu sync.Mutex

	save func(map[string]persistedJob) error

	pending         map[string]persistedJob
	pendingRevision uint64
	writtenRevision uint64
	attemptRevision uint64
	lastErr         error
	changed         chan struct{}
	closed          bool

	wake  chan struct{}
	flush chan struct{}
	stop  chan struct{}
	done  chan struct{}

	closeOnce sync.Once
}

func newPersistenceCoordinator(save func(map[string]persistedJob) error) *persistenceCoordinator {
	pc := &persistenceCoordinator{
		save:    save,
		changed: make(chan struct{}),
		wake:    make(chan struct{}, 1),
		flush:   make(chan struct{}, 1),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	go pc.run()
	return pc
}

func (pc *persistenceCoordinator) publish(revision uint64, snapshot map[string]persistedJob) error {
	pc.mu.Lock()
	if pc.closed {
		pc.mu.Unlock()
		return errPersistenceClosed
	}
	if revision > pc.pendingRevision {
		pc.pendingRevision = revision
		pc.pending = snapshot
	}
	pc.mu.Unlock()

	select {
	case pc.wake <- struct{}{}:
	default:
	}
	return nil
}

func (pc *persistenceCoordinator) flushTo(ctx context.Context, revision uint64) error {
	if revision == 0 {
		return nil
	}

	for {
		pc.mu.Lock()
		if pc.attemptRevision >= revision {
			err := pc.lastErr
			pc.mu.Unlock()
			return err
		}
		changed := pc.changed
		pc.mu.Unlock()

		select {
		case pc.flush <- struct{}{}:
		default:
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-pc.done:
			pc.mu.Lock()
			err := pc.lastErr
			attempted := pc.attemptRevision
			pc.mu.Unlock()
			if attempted < revision && err == nil {
				return errPersistenceClosed
			}
			return err
		case <-changed:
		}
	}
}

func (pc *persistenceCoordinator) close(ctx context.Context) error {
	pc.closeOnce.Do(func() {
		pc.mu.Lock()
		pc.closed = true
		pc.mu.Unlock()
		close(pc.stop)
	})
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-pc.done:
		pc.mu.Lock()
		defer pc.mu.Unlock()
		return pc.lastErr
	}
}

func (pc *persistenceCoordinator) run() {
	defer close(pc.done)
	for {
		select {
		case <-pc.stop:
			pc.writeLatest()
			return
		case <-pc.flush:
			pc.writeLatest()
		case <-pc.wake:
			timer := time.NewTimer(persistenceDebounce)
			for {
				select {
				case <-pc.stop:
					stopPersistenceTimer(timer)
					pc.writeLatest()
					return
				case <-pc.flush:
					stopPersistenceTimer(timer)
					pc.writeLatest()
					goto next
				case <-pc.wake:
					stopPersistenceTimer(timer)
					timer.Reset(persistenceDebounce)
				case <-timer.C:
					pc.writeLatest()
					goto next
				}
			}
		}
	next:
	}
}

func stopPersistenceTimer(timer *time.Timer) {
	if timer.Stop() {
		return
	}
	select {
	case <-timer.C:
	default:
	}
}

func (pc *persistenceCoordinator) writeLatest() {
	pc.mu.Lock()
	revision := pc.pendingRevision
	if revision <= pc.writtenRevision || pc.pending == nil {
		pc.mu.Unlock()
		return
	}
	snapshot := pc.pending
	pc.mu.Unlock()

	err := pc.save(snapshot)

	pc.mu.Lock()
	// A newer pending snapshot may have arrived while this write ran. Record
	// only the revision actually attempted; the worker will write the newer one.
	pc.attemptRevision = revision
	pc.lastErr = err
	if err == nil && revision > pc.writtenRevision {
		pc.writtenRevision = revision
	}
	close(pc.changed)
	pc.changed = make(chan struct{})
	newerPending := pc.pendingRevision > revision
	pc.mu.Unlock()

	if newerPending {
		select {
		case pc.wake <- struct{}{}:
		default:
		}
	}
}
