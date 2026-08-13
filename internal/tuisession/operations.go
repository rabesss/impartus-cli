package tuisession

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/rabesss/impartus-cli/internal/tuiproto"
)

const (
	defaultSelfTestSteps = 3
	maxOperations        = 128
)

var (
	errOperationNotFound = errors.New("operation not found")
	errTooManyOperations = errors.New("too many operations")
	errRegistryStopping  = errors.New("operation registry is stopping")
)

type operationEntry struct {
	operation tuiproto.Operation
	cancel    context.CancelFunc
	done      chan struct{}
}

type operationRegistry struct {
	ctx     context.Context
	events  *hub
	options SelfTestOptions

	mu         sync.Mutex
	operations map[string]*operationEntry
	stopping   bool
	wait       sync.WaitGroup
}

func newOperationRegistry(ctx context.Context, events *hub, options SelfTestOptions) *operationRegistry {
	if options.Steps <= 0 {
		options.Steps = defaultSelfTestSteps
	}
	return &operationRegistry{
		ctx:        ctx,
		events:     events,
		options:    options,
		operations: make(map[string]*operationEntry),
	}
}

func (registry *operationRegistry) start(kind tuiproto.OperationKind) (tuiproto.Operation, error) {
	if kind != tuiproto.OperationKindSelftest {
		return tuiproto.Operation{}, errors.New("unsupported operation kind")
	}
	identifier, err := randomToken(16)
	if err != nil {
		return tuiproto.Operation{}, err
	}
	ctx, cancel := context.WithCancel(registry.ctx)
	entry := &operationEntry{
		operation: tuiproto.Operation{ID: identifier, Kind: kind, State: tuiproto.OperationStateRunning},
		cancel:    cancel,
		done:      make(chan struct{}),
	}
	registry.mu.Lock()
	if registry.stopping {
		registry.mu.Unlock()
		cancel()
		return tuiproto.Operation{}, errRegistryStopping
	}
	if len(registry.operations) >= maxOperations {
		registry.mu.Unlock()
		cancel()
		return tuiproto.Operation{}, errTooManyOperations
	}
	registry.operations[identifier] = entry
	registry.wait.Add(1)
	registry.mu.Unlock()

	state := tuiproto.OperationStateRunning
	registry.events.publish(tuiproto.Event{
		OperationID: &identifier,
		State:       &state,
		Type:        tuiproto.EventTypeOperationStarted,
	})
	operation := entry.operation
	go registry.runSelfTest(ctx, entry)
	return operation, nil
}

func (registry *operationRegistry) runSelfTest(ctx context.Context, entry *operationEntry) {
	defer registry.wait.Done()
	defer close(entry.done)
	for step := 1; step <= registry.options.Steps; step++ {
		if registry.options.Interval > 0 {
			timer := time.NewTimer(registry.options.Interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				registry.finish(entry, tuiproto.OperationStateCanceled, tuiproto.EventTypeOperationCanceled)
				return
			case <-timer.C:
			}
		} else {
			select {
			case <-ctx.Done():
				registry.finish(entry, tuiproto.OperationStateCanceled, tuiproto.EventTypeOperationCanceled)
				return
			default:
			}
		}
		percent := float64(step) / float64(registry.options.Steps) * 100
		state := tuiproto.OperationStateRunning
		identifier := entry.operation.ID
		registry.events.publish(tuiproto.Event{
			OperationID: &identifier,
			Percent:     &percent,
			State:       &state,
			Type:        tuiproto.EventTypeOperationProgress,
		})
	}
	registry.finish(entry, tuiproto.OperationStateCompleted, tuiproto.EventTypeOperationCompleted)
}

func (registry *operationRegistry) finish(entry *operationEntry, state tuiproto.OperationState, eventType tuiproto.EventType) {
	registry.mu.Lock()
	if entry.operation.State != tuiproto.OperationStateRunning {
		registry.mu.Unlock()
		return
	}
	entry.operation.State = state
	identifier := entry.operation.ID
	registry.mu.Unlock()
	registry.events.publish(tuiproto.Event{
		OperationID: &identifier,
		State:       &state,
		Type:        eventType,
	})
}

func (registry *operationRegistry) get(identifier string) (tuiproto.Operation, error) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	entry, ok := registry.operations[identifier]
	if !ok {
		return tuiproto.Operation{}, errOperationNotFound
	}
	return entry.operation, nil
}

func (registry *operationRegistry) cancelOperation(identifier string) (tuiproto.Operation, error) {
	registry.mu.Lock()
	entry, ok := registry.operations[identifier]
	if !ok {
		registry.mu.Unlock()
		return tuiproto.Operation{}, errOperationNotFound
	}
	entry.cancel()
	done := entry.done
	registry.mu.Unlock()
	<-done
	return registry.get(identifier)
}

func (registry *operationRegistry) stopAndWait() {
	registry.mu.Lock()
	registry.stopping = true
	for _, entry := range registry.operations {
		entry.cancel()
	}
	registry.mu.Unlock()
	registry.wait.Wait()
}
