package player

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

type testRequest struct {
	Command   []json.RawMessage `json:"command"`
	RequestID int64             `json:"request_id"`
}

func TestClientMatchesOutOfOrderReplies(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	client := NewClient(clientConn, ClientOptions{CommandTimeout: time.Second, MaxPending: 4})
	cleanupClient(t, client)

	serverDone := make(chan error, 1)
	go func() {
		decoder := json.NewDecoder(serverConn)
		requests := make(map[string]int64, 2)
		for range 2 {
			var request testRequest
			if err := decoder.Decode(&request); err != nil {
				serverDone <- err
				return
			}
			var name string
			if err := json.Unmarshal(request.Command[0], &name); err != nil {
				serverDone <- err
				return
			}
			requests[name] = request.RequestID
		}
		encoder := json.NewEncoder(serverConn)
		if err := encoder.Encode(map[string]any{"request_id": requests["second"], "error": "success", "data": "two"}); err != nil {
			serverDone <- err
			return
		}
		if err := encoder.Encode(map[string]any{"request_id": requests["first"], "error": "success", "data": "one"}); err != nil {
			serverDone <- err
			return
		}
		serverDone <- nil
	}()

	type result struct {
		data string
		err  error
	}
	results := make(map[string]result, 2)
	var mutex sync.Mutex
	var wait sync.WaitGroup
	for _, name := range []string{"first", "second"} {
		wait.Add(1)
		go func() {
			defer wait.Done()
			raw, err := client.Command(context.Background(), name)
			var value string
			if err == nil {
				err = json.Unmarshal(raw, &value)
			}
			mutex.Lock()
			results[name] = result{data: value, err: err}
			mutex.Unlock()
		}()
	}
	wait.Wait()
	if err := <-serverDone; err != nil {
		t.Fatalf("fake mpv server: %v", err)
	}
	if results["first"].err != nil || results["first"].data != "one" {
		t.Fatalf("first result = %+v", results["first"])
	}
	if results["second"].err != nil || results["second"].data != "two" {
		t.Fatalf("second result = %+v", results["second"])
	}
}

func TestClientEmitsPropertyEvent(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	client := NewClient(clientConn, ClientOptions{CommandTimeout: time.Second})
	cleanupClient(t, client)

	go func() {
		if _, err := serverConn.Write([]byte("{\"event\":\"property-change\",\"id\":7,\"name\":\"pause\",\"data\":true,\"file_error\":\"HTTP error 401\"}\n")); err != nil {
			t.Errorf("write property event: %v", err)
		}
	}()

	select {
	case event := <-client.Events():
		if event.Name != "property-change" || event.Property != "pause" || event.ID != 7 || string(event.Data) != "true" || event.FileError != "HTTP error 401" {
			t.Fatalf("event = %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for property event")
	}
}

func TestClientEventHandlerOwnsEventDelivery(t *testing.T) {
	t.Parallel()

	var handled []Event
	client := &Client{
		options: ClientOptions{eventHandler: func(event Event) {
			handled = append(handled, event)
		}},
		events: make(chan Event, 1),
	}
	want := Event{Name: "end-file", Reason: "eof"}
	client.publishEvent(want)
	if len(handled) != 1 || handled[0].Name != want.Name || handled[0].Reason != want.Reason {
		t.Fatalf("handled events = %+v, want %+v", handled, want)
	}
	select {
	case event := <-client.events:
		t.Fatalf("internal channel received duplicate event %+v", event)
	default:
	}
}

func TestClientDropsOldestEventWithoutBlockingCommands(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	client := NewClient(clientConn, ClientOptions{CommandTimeout: time.Second, EventBuffer: 2})
	cleanupClient(t, client)
	eventsWritten := make(chan struct{})
	serverDone := make(chan error, 1)
	go func() {
		for id := 1; id <= 3; id++ {
			if err := json.NewEncoder(serverConn).Encode(map[string]any{
				"event": "property-change", "id": id, "name": "time-pos", "data": id,
			}); err != nil {
				serverDone <- err
				return
			}
		}
		close(eventsWritten)
		var request testRequest
		if json.NewDecoder(serverConn).Decode(&request) == nil {
			serverDone <- json.NewEncoder(serverConn).Encode(map[string]any{
				"request_id": request.RequestID, "error": "success", "data": true,
			})
			return
		}
		serverDone <- errors.New("fake mpv did not decode command")
	}()
	<-eventsWritten
	if _, err := client.Command(context.Background(), "get_property", "pause"); err != nil {
		t.Fatalf("Command() after event overflow error = %v", err)
	}
	first := <-client.Events()
	second := <-client.Events()
	if first.ID != 2 || second.ID != 3 {
		t.Fatalf("retained event IDs = %d, %d; want newest 2, 3", first.ID, second.ID)
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("fake mpv server: %v", err)
	}
}

func TestClientRejectsOversizedInboundFrame(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	client := NewClient(clientConn, ClientOptions{})
	cleanupClient(t, client)
	writeDone := make(chan error, 1)
	go func() {
		frame := bytes.Repeat([]byte{'x'}, maxIPCFrameBytes+1)
		_, writeErr := serverConn.Write(frame)
		writeDone <- writeErr
	}()
	select {
	case <-client.Done():
		if err := client.Err(); err == nil || !strings.Contains(err.Error(), "decode mpv IPC") {
			t.Fatalf("Err() = %v, want bounded protocol failure", err)
		}
	case <-time.After(time.Second):
		t.Fatal("client did not reject oversized inbound frame")
	}
	select {
	case <-writeDone:
	case <-time.After(time.Second):
		t.Fatal("oversized frame writer did not unblock")
	}
}

func TestClientClosesOnMalformedJSON(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	client := NewClient(clientConn, ClientOptions{})
	cleanupClient(t, client)

	go func() {
		if _, err := serverConn.Write([]byte("{not-json}\n")); err != nil {
			t.Errorf("write malformed response: %v", err)
		}
	}()

	select {
	case <-client.Done():
		if err := client.Err(); err == nil || !strings.Contains(err.Error(), "decode mpv IPC") {
			t.Fatalf("Err() = %v, want protocol decode error", err)
		}
	case <-time.After(time.Second):
		t.Fatal("client did not close after malformed JSON")
	}
}

func TestClientCommandTimeoutAndCancellation(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		clientConn, serverConn := net.Pipe()
		client := NewClient(clientConn, ClientOptions{CommandTimeout: 40 * time.Millisecond})
		cleanupClient(t, client)
		go func() {
			var request testRequest
			if err := json.NewDecoder(serverConn).Decode(&request); err != nil {
				return
			}
		}()

		_, err := client.Command(context.Background(), "get_property", "pause")
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Command() error = %v, want deadline exceeded", err)
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		clientConn, serverConn := net.Pipe()
		client := NewClient(clientConn, ClientOptions{CommandTimeout: time.Second})
		cleanupClient(t, client)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := client.Command(ctx, "get_property", "pause")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Command() error = %v, want canceled", err)
		}
		if err := serverConn.Close(); err != nil {
			t.Fatalf("close server connection: %v", err)
		}
	})
}

func TestClientBoundsPendingCommands(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	client := NewClient(clientConn, ClientOptions{CommandTimeout: time.Second, MaxPending: 1})
	cleanupClient(t, client)

	requestRead := make(chan struct{})
	go func() {
		var request testRequest
		if err := json.NewDecoder(serverConn).Decode(&request); err != nil {
			return
		}
		close(requestRead)
	}()
	firstDone := make(chan error, 1)
	go func() {
		_, err := client.Command(context.Background(), "first")
		firstDone <- err
	}()
	<-requestRead

	if _, err := client.Command(context.Background(), "second"); !errors.Is(err, ErrPendingLimit) {
		t.Fatalf("second Command() error = %v, want ErrPendingLimit", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("close client: %v", err)
	}
	if err := <-firstDone; !errors.Is(err, ErrDisconnected) {
		t.Fatalf("first Command() error = %v, want ErrDisconnected", err)
	}
}

func TestClientCommandWriteHonorsTimeout(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer func() {
		if err := serverConn.Close(); err != nil {
			t.Errorf("close server connection: %v", err)
		}
	}()
	client := NewClient(clientConn, ClientOptions{CommandTimeout: 40 * time.Millisecond})
	cleanupClient(t, client)

	_, err := client.Command(context.Background(), "loadfile", strings.Repeat("x", 1<<20))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Command() error = %v, want bounded write deadline", err)
	}
}

func TestClientDoesNotTrustPeerErrorText(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	client := NewClient(clientConn, ClientOptions{CommandTimeout: time.Second})
	cleanupClient(t, client)

	const secret = "loopback-capability-secret"
	go func() {
		var request testRequest
		if json.NewDecoder(serverConn).Decode(&request) == nil {
			if err := json.NewEncoder(serverConn).Encode(map[string]any{
				"request_id": request.RequestID,
				"error":      "failed for http://127.0.0.1:1234/" + secret + "/master.m3u8",
			}); err != nil {
				t.Errorf("encode fake error: %v", err)
			}
		}
	}()

	_, err := client.Command(context.Background(), "loadfile", "http://127.0.0.1:1234/"+secret+"/master.m3u8")
	if err == nil {
		t.Fatal("expected mpv command failure")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("peer-controlled error leaked command capability: %v", err)
	}
}

func cleanupClient(t *testing.T, client *Client) {
	t.Helper()
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("close client: %v", err)
		}
	})
}
