package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func startWebSocketTestServer(t *testing.T, s *APIServer) (string, http.Header) {
	t.Helper()
	testServer := httptest.NewServer(s.router)
	t.Cleanup(testServer.Close)

	token := setupAuth(t, s)
	header := make(http.Header)
	header.Set("Authorization", "Bearer "+token)
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http") + "/api/v1/ws"
	return wsURL, header
}

func dialWebSocket(t *testing.T, wsURL string, header http.Header) *websocket.Conn {
	t.Helper()
	conn, response, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		if response != nil {
			defer func() {
				if closeErr := response.Body.Close(); closeErr != nil {
					t.Logf("closing failed websocket response body: %v", closeErr)
				}
			}()
			t.Fatalf("websocket dial failed with HTTP %d: %v", response.StatusCode, err)
		}
		t.Fatalf("websocket dial failed: %v", err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Logf("closing test websocket: %v", err)
		}
	})
	return conn
}

func waitForWebSocketClients(t *testing.T, hub *WSHub, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		hub.mu.RLock()
		got := len(hub.clients)
		hub.mu.RUnlock()
		if got == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	hub.mu.RLock()
	got := len(hub.clients)
	hub.mu.RUnlock()
	t.Fatalf("registered websocket clients = %d, want %d", got, want)
}

func TestWebSocketPreservesBroadcastOrder(t *testing.T) {
	s := newAPIServer(validServerConfig())
	wsURL, header := startWebSocketTestServer(t, s)
	conn := dialWebSocket(t, wsURL, header)
	waitForWebSocketClients(t, s.wsHub, 1)

	const events = 10
	for sequence := range events {
		if err := s.wsHub.Broadcast(map[string]int{"sequence": sequence}); err != nil {
			t.Fatalf("Broadcast() failed: %v", err)
		}
	}
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline() failed: %v", err)
	}
	for want := range events {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("ReadMessage() failed at sequence %d: %v", want, err)
		}
		var event struct {
			Sequence int `json:"sequence"`
		}
		if err := json.Unmarshal(data, &event); err != nil {
			t.Fatalf("Unmarshal() failed: %v", err)
		}
		if event.Sequence != want {
			t.Fatalf("event sequence = %d, want %d", event.Sequence, want)
		}
	}
}

func TestWebSocketConcurrentBroadcastAndPing(t *testing.T) {
	s := newAPIServer(validServerConfig())
	// Exercise the centralized ping writer without making the test wait 30 seconds.
	s.wsHub.pingInterval = 2 * time.Millisecond
	wsURL, header := startWebSocketTestServer(t, s)
	conn := dialWebSocket(t, wsURL, header)
	waitForWebSocketClients(t, s.wsHub, 1)

	const (
		workers   = 4
		perWorker = 10
		total     = workers * perWorker
	)
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline() failed: %v", err)
	}

	readErr := make(chan error, 1)
	gotIDs := make(chan int, total)
	go func() {
		for range total {
			_, data, err := conn.ReadMessage()
			if err != nil {
				readErr <- err
				return
			}
			var event struct {
				ID int `json:"id"`
			}
			if err := json.Unmarshal(data, &event); err != nil {
				readErr <- err
				return
			}
			gotIDs <- event.ID
		}
		readErr <- nil
	}()

	var wg sync.WaitGroup
	for worker := range workers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for sequence := range perWorker {
				if err := s.wsHub.Broadcast(map[string]int{"id": worker*perWorker + sequence}); err != nil {
					t.Errorf("Broadcast() failed: %v", err)
					return
				}
			}
		}(worker)
	}
	wg.Wait()

	if err := <-readErr; err != nil {
		t.Fatalf("reading concurrent broadcasts failed: %v", err)
	}
	close(gotIDs)
	seen := make(map[int]bool, total)
	for id := range gotIDs {
		seen[id] = true
	}
	if len(seen) != total {
		t.Fatalf("received %d unique events, want %d", len(seen), total)
	}
}

func TestWebSocketReadLimitClosesOnlyOversizedClient(t *testing.T) {
	s := newAPIServer(validServerConfig())
	wsURL, header := startWebSocketTestServer(t, s)
	offender := dialWebSocket(t, wsURL, header)
	healthy := dialWebSocket(t, wsURL, header)
	waitForWebSocketClients(t, s.wsHub, 2)

	oversized := make([]byte, websocketReadLimit+1)
	if err := offender.WriteMessage(websocket.TextMessage, oversized); err != nil {
		t.Fatalf("writing oversized message failed before server could inspect it: %v", err)
	}
	if err := offender.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline() failed: %v", err)
	}
	if _, _, err := offender.ReadMessage(); err == nil {
		t.Fatal("oversized inbound message did not close its connection")
	}
	waitForWebSocketClients(t, s.wsHub, 1)

	if err := s.wsHub.Broadcast(map[string]string{"state": "healthy"}); err != nil {
		t.Fatalf("Broadcast() failed after oversized message: %v", err)
	}
	if err := healthy.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline() failed: %v", err)
	}
	_, data, err := healthy.ReadMessage()
	if err != nil {
		t.Fatalf("healthy client stopped receiving after peer disconnect: %v", err)
	}
	if !strings.Contains(string(data), `"state":"healthy"`) {
		t.Fatalf("healthy client received unexpected event: %s", data)
	}
}

func TestJobExecutorPanicUsesSanitizedTerminalFailure(t *testing.T) {
	s := newAPIServer(validServerConfig())
	job := s.jobStore.CreateJob(1, 1, 1, 1, validServerConfig())
	capture := newWSClient(nil, 2)
	s.wsHub.clients[capture] = struct{}{}
	t.Cleanup(func() { s.wsHub.Unregister(capture) })

	const privateSentinel = "credential-shaped-panic-value"
	s.runJob(job.ID, func() {
		panic(privateSentinel)
	})

	gotJob, ok := s.jobStore.CopyJob(job.ID)
	if !ok {
		t.Fatal("panic job disappeared from store")
	}
	if gotJob.Status != StatusFailed {
		t.Fatalf("panic job status = %q, want %q", gotJob.Status, StatusFailed)
	}
	if gotJob.Error != internalJobExecutionError {
		t.Fatalf("panic job error = %q, want generic terminal error", gotJob.Error)
	}
	jobJSON, err := json.Marshal(gotJob)
	if err != nil {
		t.Fatalf("Marshal(job) failed: %v", err)
	}
	if strings.Contains(string(jobJSON), privateSentinel) {
		t.Fatal("panic value leaked into persisted job state")
	}

	if len(capture.send) != 1 {
		t.Fatalf("failed event count = %d, want exactly 1", len(capture.send))
	}
	eventJSON := <-capture.send
	if strings.Contains(string(eventJSON), privateSentinel) {
		t.Fatal("panic value leaked into websocket event")
	}
	var event wsEvent
	if err := json.Unmarshal(eventJSON, &event); err != nil {
		t.Fatalf("Unmarshal(failed event) failed: %v", err)
	}
	if event.Type != "job.failed" || event.Status != StatusFailed || event.Error != internalJobExecutionError {
		t.Fatalf("unexpected terminal event: %+v", event)
	}

	// A recovered panic must release its concurrency slot for the next job.
	secondRan := make(chan struct{})
	go s.runJob("semaphore-reuse", func() { close(secondRan) })
	select {
	case <-secondRan:
	case <-time.After(time.Second):
		t.Fatal("job semaphore was not reusable after panic")
	}
}
