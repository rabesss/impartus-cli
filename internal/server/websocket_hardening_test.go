package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
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

func TestWebSocketConcurrentBroadcastHasConsistentClientOrder(t *testing.T) {
	s := newAPIServer(validServerConfig())
	wsURL, header := startWebSocketTestServer(t, s)
	first := dialWebSocket(t, wsURL, header)
	second := dialWebSocket(t, wsURL, header)
	waitForWebSocketClients(t, s.wsHub, 2)

	const (
		workers   = 4
		perWorker = 10
		total     = workers * perWorker
	)
	for _, conn := range []*websocket.Conn{first, second} {
		if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Fatalf("SetReadDeadline() failed: %v", err)
		}
	}

	type readResult struct {
		ids []int
		err error
	}
	readSequences := func(conn *websocket.Conn) <-chan readResult {
		result := make(chan readResult, 1)
		go func() {
			ids := make([]int, 0, total)
			for range total {
				_, data, err := conn.ReadMessage()
				if err != nil {
					result <- readResult{err: err}
					return
				}
				var event struct {
					ID int `json:"id"`
				}
				if err := json.Unmarshal(data, &event); err != nil {
					result <- readResult{err: err}
					return
				}
				ids = append(ids, event.ID)
			}
			result <- readResult{ids: ids}
		}()
		return result
	}
	firstResult := readSequences(first)
	secondResult := readSequences(second)

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

	gotFirst := <-firstResult
	if gotFirst.err != nil {
		t.Fatalf("first client read failed: %v", gotFirst.err)
	}
	gotSecond := <-secondResult
	if gotSecond.err != nil {
		t.Fatalf("second client read failed: %v", gotSecond.err)
	}
	if !slices.Equal(gotFirst.ids, gotSecond.ids) {
		t.Fatalf("clients observed different event orders:\nfirst:  %v\nsecond: %v", gotFirst.ids, gotSecond.ids)
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

func TestWebSocketHubCloseDisconnectsClients(t *testing.T) {
	s := newAPIServer(validServerConfig())
	wsURL, header := startWebSocketTestServer(t, s)
	conn := dialWebSocket(t, wsURL, header)
	waitForWebSocketClients(t, s.wsHub, 1)

	s.wsHub.Close()
	waitForWebSocketClients(t, s.wsHub, 0)
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline() failed: %v", err)
	}
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("hub shutdown left upgraded connection open")
	}
	if client := s.wsHub.Register(conn); client != nil {
		t.Fatal("closed hub accepted a new client")
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

func TestJobExecutorPanicDoesNotPublishBeforeDurableFailureCommit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	store := newTestPersistentStore(t, path)
	job := store.CreateJob(1, 1, 1, 1, validServerConfig())
	if job == nil {
		t.Fatal("CreateJob() failed")
	}

	realSync := store.persistence.syncFile
	var attempts atomic.Int64
	store.persistence.mu.Lock()
	store.persistence.syncFile = func(file *os.File) error {
		if attempts.Add(1) == 1 {
			return errors.New("injected terminal sync failure")
		}
		return realSync(file)
	}
	store.persistence.mu.Unlock()

	s := newAPIServer(validServerConfig())
	s.jobStore = store
	capture := newWSClient(nil, 1)
	s.wsHub.clients[capture] = struct{}{}
	t.Cleanup(func() { s.wsHub.Unregister(capture) })

	s.runJob(job.ID, func() {
		panic("private-durability-panic-value")
	})

	gotJob, ok := store.CopyJob(job.ID)
	if !ok {
		t.Fatal("panic job disappeared after persistence rollback")
	}
	if gotJob.Status != StatusPending || gotJob.Progress != 0 || gotJob.Error != "" {
		t.Fatalf("failed persistence leaked terminal state: status=%q progress=%v error=%q", gotJob.Status, gotJob.Progress, gotJob.Error)
	}
	if len(capture.send) != 0 {
		t.Fatalf("failed persistence emitted %d terminal events, want 0", len(capture.send))
	}
	persisted := readPersistedJobs(t, path)[job.ID]
	if persisted.Status != StatusPending || persisted.Progress != 0 || persisted.Error != "" {
		t.Fatalf("failed persistence reached disk: status=%q progress=%v error=%q", persisted.Status, persisted.Progress, persisted.Error)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("sync attempts = %d, want rejected transition plus synchronous rollback", got)
	}
}

func TestJobExecutorPanicDoesNotOverwriteCanceledTerminalState(t *testing.T) {
	s := newAPIServer(validServerConfig())
	job := s.jobStore.CreateJob(1, 1, 1, 1, validServerConfig())
	capture := newWSClient(nil, 1)
	s.wsHub.clients[capture] = struct{}{}
	t.Cleanup(func() { s.wsHub.Unregister(capture) })

	if _, err := s.jobStore.CancelJob(job.ID); err != nil {
		t.Fatalf("CancelJob() failed: %v", err)
	}
	s.runJob(job.ID, func() {
		panic("private-canceled-panic-value")
	})

	gotJob, ok := s.jobStore.CopyJob(job.ID)
	if !ok {
		t.Fatal("canceled panic job disappeared from store")
	}
	if gotJob.Status != StatusCanceled || gotJob.Error != "" {
		t.Fatalf("panic overwrote canceled terminal state: status=%q error=%q", gotJob.Status, gotJob.Error)
	}
	if len(capture.send) != 0 {
		t.Fatalf("panic after cancellation emitted %d failed events, want 0", len(capture.send))
	}
}

func TestJobExecutorPanicDoesNotOverwriteCompletedTerminalState(t *testing.T) {
	s := newAPIServer(validServerConfig())
	job := s.jobStore.CreateJob(1, 1, 1, 1, validServerConfig())
	s.jobStore.UpdateJob(job.ID, StatusCompleted, 100, "")
	capture := newWSClient(nil, 1)
	s.wsHub.clients[capture] = struct{}{}
	t.Cleanup(func() { s.wsHub.Unregister(capture) })

	s.runJob(job.ID, func() {
		panic("private-completed-panic-value")
	})

	gotJob, ok := s.jobStore.CopyJob(job.ID)
	if !ok {
		t.Fatal("completed panic job disappeared from store")
	}
	if gotJob.Status != StatusCompleted || gotJob.Progress != 100 || gotJob.Error != "" {
		t.Fatalf("panic overwrote completed terminal state: status=%q progress=%v error=%q", gotJob.Status, gotJob.Progress, gotJob.Error)
	}
	if len(capture.send) != 0 {
		t.Fatalf("panic after completion emitted %d failed events, want 0", len(capture.send))
	}
}

func TestConcurrentProgressRemainsMonotonicInStateAndEvents(t *testing.T) {
	s := newAPIServer(validServerConfig())
	job := s.jobStore.CreateJob(1, 1, 1, 100, validServerConfig())
	capture := newWSClient(nil, 128)
	s.wsHub.clients[capture] = struct{}{}
	t.Cleanup(func() { s.wsHub.Unregister(capture) })

	start := make(chan struct{})
	var wg sync.WaitGroup
	for progress := 1; progress <= 100; progress++ {
		wg.Add(1)
		go func(progress float64) {
			defer wg.Done()
			<-start
			s.updateRunningProgress(job.ID, progress, "downloading", nil)
		}(float64(progress))
	}
	close(start)
	wg.Wait()

	gotJob, ok := s.jobStore.CopyJob(job.ID)
	if !ok {
		t.Fatal("progress job disappeared")
	}
	if gotJob.Status != StatusRunning || gotJob.Progress != 100 {
		t.Fatalf("final progress state = %q/%v, want running/100", gotJob.Status, gotJob.Progress)
	}

	last := float64(-1)
	for len(capture.send) > 0 {
		var event wsEvent
		if err := json.Unmarshal(<-capture.send, &event); err != nil {
			t.Fatalf("Unmarshal(progress event) failed: %v", err)
		}
		if event.Progress < last {
			t.Fatalf("progress event regressed from %v to %v", last, event.Progress)
		}
		last = event.Progress
	}
	if last != gotJob.Progress {
		t.Fatalf("last progress event = %v, final state = %v", last, gotJob.Progress)
	}
}

func TestConcurrentCancellationIsTerminalForStateAndEvents(t *testing.T) {
	s := newAPIServer(validServerConfig())
	job := s.jobStore.CreateJob(1, 1, 1, 100, validServerConfig())
	capture := newWSClient(nil, 128)
	s.wsHub.clients[capture] = struct{}{}
	t.Cleanup(func() { s.wsHub.Unregister(capture) })

	start := make(chan struct{})
	var wg sync.WaitGroup
	for progress := 1; progress <= 100; progress++ {
		wg.Add(1)
		go func(progress float64) {
			defer wg.Done()
			<-start
			s.updateRunningProgress(job.ID, progress, "downloading", nil)
		}(float64(progress))
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		if err := s.cancelJob(job.ID); err != nil {
			t.Errorf("cancelJob() failed: %v", err)
		}
	}()
	close(start)
	wg.Wait()

	gotJob, ok := s.jobStore.CopyJob(job.ID)
	if !ok {
		t.Fatal("canceled job disappeared")
	}
	if gotJob.Status != StatusCanceled {
		t.Fatalf("concurrent progress resurrected status %q, want canceled", gotJob.Status)
	}

	canceled := false
	for len(capture.send) > 0 {
		var event wsEvent
		if err := json.Unmarshal(<-capture.send, &event); err != nil {
			t.Fatalf("Unmarshal(terminal event) failed: %v", err)
		}
		if canceled {
			t.Fatalf("event %q followed terminal cancellation", event.Type)
		}
		if event.Type == jobCancelledEventType {
			canceled = true
		}
	}
	if !canceled {
		t.Fatal("cancellation event was not emitted")
	}

	s.completeJob(job.ID, []string{"late.mp4"})
	s.failJob(job.ID, "late failure")
	after, _ := s.jobStore.CopyJob(job.ID)
	if after.Status != StatusCanceled || len(after.Outputs) != 0 || after.Error != "" {
		t.Fatalf("late terminal transition overwrote cancellation: %+v", after)
	}
	if len(capture.send) != 0 {
		t.Fatalf("late terminal transition emitted %d events, want 0", len(capture.send))
	}
}

func TestConcurrentLectureProgressDoesNotRegress(t *testing.T) {
	store := NewJobStore()
	job := store.CreateJob(1, 1, 1, 100, validServerConfig())
	start := make(chan struct{})
	var wg sync.WaitGroup
	for completed := 1; completed <= 100; completed++ {
		wg.Add(1)
		go func(completed int) {
			defer wg.Done()
			<-start
			store.SetLectureProgress(job.ID, completed, 100)
		}(completed)
	}
	close(start)
	wg.Wait()
	got, ok := store.CopyJob(job.ID)
	if !ok || got.CompletedLectures != 100 || got.TotalLectures != 100 {
		t.Fatalf("final lecture progress = %+v, want 100/100", got)
	}
}
