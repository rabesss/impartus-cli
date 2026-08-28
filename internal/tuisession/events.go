package tuisession

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/rabesss/impartus-cli/internal/tuiproto"
)

const (
	defaultEventQueueDepth = 64
	maxEventQueueDepth     = 1024
	defaultHeartbeat       = 15 * time.Second
)

type eventSubscriber struct {
	events   chan tuiproto.Event
	overflow chan tuiproto.Event
}

type hub struct {
	mu             sync.Mutex
	nextSequence   int64
	nextSubscriber uint64
	subscribers    map[uint64]*eventSubscriber
	queueDepth     int
	done           chan struct{}
	closed         bool
}

func newHub(queueDepth int) *hub {
	if queueDepth <= 0 {
		queueDepth = defaultEventQueueDepth
	}
	if queueDepth > maxEventQueueDepth {
		queueDepth = maxEventQueueDepth
	}
	return &hub{
		subscribers: make(map[uint64]*eventSubscriber),
		queueDepth:  queueDepth,
		done:        make(chan struct{}),
	}
}

func (events *hub) subscribe() (uint64, *eventSubscriber, bool) {
	events.mu.Lock()
	defer events.mu.Unlock()
	if events.closed {
		return 0, nil, false
	}
	events.nextSubscriber++
	identifier := events.nextSubscriber
	subscriber := &eventSubscriber{
		events:   make(chan tuiproto.Event, events.queueDepth),
		overflow: make(chan tuiproto.Event, 1),
	}
	events.nextSequence++
	subscriber.events <- tuiproto.Event{
		Sequence: events.nextSequence,
		Type:     tuiproto.EventTypeSessionReady,
	}
	events.subscribers[identifier] = subscriber
	return identifier, subscriber, true
}

func (events *hub) unsubscribe(identifier uint64) {
	events.mu.Lock()
	delete(events.subscribers, identifier)
	events.mu.Unlock()
}

func (events *hub) publish(event tuiproto.Event) {
	events.mu.Lock()
	defer events.mu.Unlock()
	if events.closed {
		return
	}
	events.nextSequence++
	event.Sequence = events.nextSequence
	if event.Message != nil {
		message := safePresentationText(*event.Message)
		event.Message = &message
	}
	for identifier, subscriber := range events.subscribers {
		select {
		case subscriber.events <- event:
		default:
			message := "event stream could not keep up; refresh the current snapshot"
			overflow := tuiproto.Event{
				Sequence: event.Sequence,
				Type:     tuiproto.EventTypeStreamOverflow,
				Message:  &message,
			}
			select {
			case subscriber.overflow <- overflow:
			default:
			}
			delete(events.subscribers, identifier)
		}
	}
}

func (events *hub) close() {
	events.mu.Lock()
	defer events.mu.Unlock()
	if events.closed {
		return
	}
	events.closed = true
	clear(events.subscribers)
	close(events.done)
}

func (session *Session) streamEvents(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	flusher, ok := writer.(http.Flusher)
	if !ok {
		writeProblem(writer, http.StatusInternalServerError, "stream_unavailable", "event streaming is unavailable")
		return
	}
	identifier, subscriber, ok := session.events.subscribe()
	if !ok {
		writeProblem(writer, http.StatusServiceUnavailable, "session_closed", "TUI session is closed")
		return
	}
	defer session.events.unsubscribe(identifier)

	writer.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	writer.Header().Set("Connection", "keep-alive")
	writer.WriteHeader(http.StatusOK)
	flusher.Flush()
	heartbeats := time.NewTicker(session.eventHeartbeatInterval)
	defer heartbeats.Stop()

	for {
		select {
		case event := <-subscriber.events:
			if writeSSEEvent(writer, event) != nil {
				return
			}
			flusher.Flush()
		case overflow := <-subscriber.overflow:
			if err := writeSSEEvent(writer, overflow); err != nil {
				return
			}
			flusher.Flush()
			return
		case <-heartbeats.C:
			if writeSSEHeartbeat(writer) != nil {
				return
			}
			flusher.Flush()
		case <-request.Context().Done():
			return
		case <-session.events.done:
			return
		}
	}
}

func writeSSEHeartbeat(writer io.Writer) error {
	_, err := io.WriteString(writer, ": heartbeat\n\n")
	return err
}

func writeSSEEvent(writer http.ResponseWriter, event tuiproto.Event) error {
	encoded, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(writer, "data: %s\n\n", encoded)
	return err
}
