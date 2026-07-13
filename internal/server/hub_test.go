package server

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewWSHub(t *testing.T) {
	h := NewWSHub()
	if h == nil || h.clients == nil {
		t.Fatal("expected initialized hub")
	}
}

func TestWSHubBroadcastNoClients(t *testing.T) {
	if err := NewWSHub().Broadcast(map[string]int{"a": 1}); err != nil {
		t.Errorf("broadcast with no clients should not error: %v", err)
	}
}

func TestWSHubBroadcastMarshalError(t *testing.T) {
	// channels cannot be JSON-marshaled, so Broadcast must surface the error.
	if err := NewWSHub().Broadcast(make(chan int)); err == nil {
		t.Error("expected marshal error for unmarshalable payload")
	}
}

func TestBroadcastEventDoesNotPanic(t *testing.T) {
	broadcastEvent(NewWSHub(), map[string]int{"x": 1})
}

func TestWSHubSlowClientDoesNotBlockHealthyClient(t *testing.T) {
	hub := NewWSHub()
	slow := newWSClient(nil, 1)
	healthy := newWSClient(nil, 1)

	hub.clients[slow] = struct{}{}
	hub.clients[healthy] = struct{}{}
	slow.send <- []byte(`{"already":"queued"}`)

	returned := make(chan error, 1)
	go func() {
		returned <- hub.Broadcast(map[string]string{"state": "current"})
	}()

	select {
	case err := <-returned:
		if err != nil {
			t.Fatalf("Broadcast() failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Broadcast() blocked on a full client queue")
	}

	select {
	case <-slow.done:
		// Expected: the slow client is disconnected.
	default:
		t.Fatal("slow client was not disconnected after queue overflow")
	}

	hub.mu.RLock()
	_, slowPresent := hub.clients[slow]
	_, healthyPresent := hub.clients[healthy]
	hub.mu.RUnlock()
	if slowPresent {
		t.Fatal("slow client remains registered after queue overflow")
	}
	if !healthyPresent {
		t.Fatal("healthy client was removed with slow client")
	}

	select {
	case data := <-healthy.send:
		var got map[string]string
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("queued event is invalid JSON: %v", err)
		}
		if got["state"] != "current" {
			t.Fatalf("queued event state = %q, want current", got["state"])
		}
	default:
		t.Fatal("healthy client did not receive broadcast")
	}

	hub.Unregister(healthy)
}
