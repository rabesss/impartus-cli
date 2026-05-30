package server

import "testing"

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
