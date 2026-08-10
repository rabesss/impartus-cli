// Package player supervises mpv and communicates with it over JSON IPC.
package player

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

var (
	// ErrDisconnected reports that the mpv IPC connection is no longer usable.
	ErrDisconnected = errors.New("mpv IPC disconnected")
	// ErrPendingLimit reports that the bounded in-flight request set is full.
	ErrPendingLimit = errors.New("mpv IPC pending command limit reached")
)

const (
	defaultCommandTimeout = 5 * time.Second
	defaultMaxPending     = 64
	defaultEventBuffer    = 128
	maxIPCFrameBytes      = 1 << 20
)

// ClientOptions controls bounded IPC behavior.
type ClientOptions struct {
	CommandTimeout time.Duration
	MaxPending     int
	EventBuffer    int
	eventHandler   func(Event)
}

// Event is one newline-delimited event emitted by mpv.
type Event struct {
	Name      string
	Property  string
	ID        int64
	Data      json.RawMessage
	Reason    string
	FileError string
}

type commandResult struct {
	data json.RawMessage
	err  error
}

type wireMessage struct {
	RequestID int64           `json:"request_id"`
	Error     string          `json:"error"`
	Data      json.RawMessage `json:"data"`
	Event     string          `json:"event"`
	Name      string          `json:"name"`
	ID        int64           `json:"id"`
	Reason    string          `json:"reason"`
	FileError string          `json:"file_error"`
}

type wireRequest struct {
	Command   []any `json:"command"`
	RequestID int64 `json:"request_id"`
}

// Client owns exactly one JSON decoder goroutine and correlates command
// responses by monotonically increasing request_id.
type Client struct {
	connection net.Conn
	options    ClientOptions
	encoder    *json.Encoder

	writeMutex sync.Mutex
	stateMutex sync.Mutex
	pending    map[int64]chan commandResult
	nextID     int64
	closed     bool
	err        error

	events       chan Event
	done         chan struct{}
	readDone     chan struct{}
	shutdownOnce sync.Once
	closeErr     error
}

// NewClient starts a JSON IPC client over an established mpv connection.
func NewClient(connection net.Conn, options ClientOptions) *Client {
	if options.CommandTimeout <= 0 {
		options.CommandTimeout = defaultCommandTimeout
	}
	if options.MaxPending <= 0 {
		options.MaxPending = defaultMaxPending
	}
	if options.EventBuffer <= 0 {
		options.EventBuffer = defaultEventBuffer
	}
	client := &Client{
		connection: connection,
		options:    options,
		encoder:    json.NewEncoder(connection),
		pending:    make(map[int64]chan commandResult, options.MaxPending),
		events:     make(chan Event, options.EventBuffer),
		done:       make(chan struct{}),
		readDone:   make(chan struct{}),
	}
	go client.readLoop()
	return client
}

// Command sends one mpv command and waits for the matching response. Arguments
// are never included in returned errors, so capability URLs cannot leak through
// this layer.
func (client *Client) Command(ctx context.Context, name string, arguments ...any) (json.RawMessage, error) {
	if ctx == nil {
		return nil, errors.New("mpv command context is required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("mpv command name is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	commandCtx := ctx
	cancel := func() {}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		commandCtx, cancel = context.WithTimeout(ctx, client.options.CommandTimeout)
	}
	defer cancel()

	requestID, response, err := client.registerPending()
	if err != nil {
		return nil, err
	}
	if writeErr := client.writeCommand(commandCtx, requestID, name, arguments); writeErr != nil {
		client.removePending(requestID, response)
		return nil, writeErr
	}
	return client.awaitResponse(commandCtx, requestID, response)
}

func (client *Client) writeCommand(ctx context.Context, requestID int64, name string, arguments []any) error {
	command := make([]any, 0, len(arguments)+1)
	command = append(command, name)
	command = append(command, arguments...)
	deadline, hasDeadline := ctx.Deadline()

	client.writeMutex.Lock()
	writeErr := client.setWriteDeadline(hasDeadline, deadline)
	if writeErr == nil {
		writeErr = client.encoder.Encode(wireRequest{Command: command, RequestID: requestID})
	}
	if writeErr == nil {
		client.clearWriteDeadline(hasDeadline)
	}
	client.writeMutex.Unlock()
	if writeErr == nil {
		return nil
	}

	terminalErr := fmt.Errorf("%w: write mpv command %q", ErrDisconnected, name)
	client.shutdown(terminalErr)
	if err := ctx.Err(); err != nil {
		return err
	}
	if hasDeadline && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return terminalErr
}

func (client *Client) clearWriteDeadline(enabled bool) {
	if !enabled {
		return
	}
	// Once Encode succeeds, a peer may close immediately after accepting the
	// command. Failure to clear the deadline must not turn that delivered command
	// into a write failure; the response reader or command timeout owns the result.
	if err := client.connection.SetWriteDeadline(time.Time{}); err != nil {
		return
	}
}

func (client *Client) setWriteDeadline(enabled bool, deadline time.Time) error {
	if !enabled {
		return nil
	}
	return client.connection.SetWriteDeadline(deadline)
}

func (client *Client) awaitResponse(ctx context.Context, requestID int64, response chan commandResult) (json.RawMessage, error) {
	select {
	case result := <-response:
		return result.data, result.err
	case <-ctx.Done():
		client.removePending(requestID, response)
		return nil, ctx.Err()
	case <-client.done:
		select {
		case result := <-response:
			return result.data, result.err
		default:
			return nil, client.Err()
		}
	}
}

// Events returns the bounded stream of the newest mpv events.
func (client *Client) Events() <-chan Event { return client.events }

// Done closes when the connection becomes unusable.
func (client *Client) Done() <-chan struct{} { return client.done }

// Err returns the terminal connection error after Done closes.
func (client *Client) Err() error {
	client.stateMutex.Lock()
	defer client.stateMutex.Unlock()
	return client.err
}

// Close disconnects the client and fails all pending requests. It is idempotent.
func (client *Client) Close() error {
	client.shutdown(ErrDisconnected)
	return client.closeErr
}

func (client *Client) registerPending() (int64, chan commandResult, error) {
	client.stateMutex.Lock()
	defer client.stateMutex.Unlock()
	if client.closed {
		return 0, nil, client.err
	}
	if len(client.pending) >= client.options.MaxPending {
		return 0, nil, ErrPendingLimit
	}
	client.nextID++
	response := make(chan commandResult, 1)
	client.pending[client.nextID] = response
	return client.nextID, response, nil
}

func (client *Client) removePending(requestID int64, response chan commandResult) {
	client.stateMutex.Lock()
	defer client.stateMutex.Unlock()
	if current, exists := client.pending[requestID]; exists && current == response {
		delete(client.pending, requestID)
	}
}

func (client *Client) readLoop() {
	defer func() {
		close(client.events)
		close(client.readDone)
	}()
	reader := bufio.NewReaderSize(client.connection, maxIPCFrameBytes+1)
	for {
		encoded, readErr := reader.ReadSlice('\n')
		if readErr != nil || len(encoded) > maxIPCFrameBytes {
			client.shutdown(fmt.Errorf("%w: decode mpv IPC response", ErrDisconnected))
			return
		}
		var message wireMessage
		if err := json.Unmarshal(encoded, &message); err != nil {
			client.shutdown(fmt.Errorf("%w: decode mpv IPC response", ErrDisconnected))
			return
		}
		if message.Event != "" {
			client.publishEvent(Event{
				Name:      message.Event,
				Property:  message.Name,
				ID:        message.ID,
				Data:      append(json.RawMessage(nil), message.Data...),
				Reason:    message.Reason,
				FileError: message.FileError,
			})
			continue
		}
		if message.RequestID <= 0 {
			continue
		}
		client.deliverResponse(message)
	}
}

func (client *Client) deliverResponse(message wireMessage) {
	client.stateMutex.Lock()
	response, exists := client.pending[message.RequestID]
	if exists {
		delete(client.pending, message.RequestID)
	}
	client.stateMutex.Unlock()
	if !exists {
		return
	}
	if message.Error != "success" {
		response <- commandResult{err: fmt.Errorf("mpv command failed: %s", safeMPVError(message.Error))}
		return
	}
	response <- commandResult{data: append(json.RawMessage(nil), message.Data...)}
}

func safeMPVError(raw string) string {
	// mpv's documented command error values are fixed protocol strings. Never
	// pass arbitrary peer text through: a compromised or buggy peer could echo
	// the capability-bearing loadfile argument into an error.
	switch value := strings.TrimSpace(raw); value {
	case "event queue full",
		"memory allocation error",
		"command not found",
		"invalid parameter",
		"property unavailable",
		"property error",
		"loading failed",
		"error running command",
		"unknown error":
		return value
	default:
		return "unknown error"
	}
}

func (client *Client) publishEvent(event Event) {
	// The internal session handler is deliberately synchronous and non-blocking:
	// terminal state must be recorded before readLoop can close Done on a
	// following EOF. Public Client users still receive the bounded channel copy.
	if client.options.eventHandler != nil {
		client.options.eventHandler(event)
	}
	select {
	case client.events <- event:
		return
	default:
	}
	// Never block the response reader behind a slow UI. Discard the oldest event
	// so terminal state such as end-file remains observable.
	select {
	case <-client.events:
	default:
	}
	select {
	case client.events <- event:
	default:
	}
}

func (client *Client) shutdown(terminalErr error) {
	client.shutdownOnce.Do(func() {
		if terminalErr == nil {
			terminalErr = ErrDisconnected
		}
		client.stateMutex.Lock()
		client.closed = true
		client.err = terminalErr
		pending := client.pending
		client.pending = make(map[int64]chan commandResult)
		client.stateMutex.Unlock()

		for _, response := range pending {
			response <- commandResult{err: terminalErr}
		}
		close(client.done)
		client.closeErr = client.connection.Close()
	})
}
