package player

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestAcceptEventRecordsTerminalAfterPublicEventsClose(t *testing.T) {
	t.Parallel()

	session := &Session{
		events:       make(chan Event, 1),
		playbackEnd:  make(chan error, 1),
		eventsClosed: true,
	}
	session.acceptEvent(Event{Name: "end-file", Reason: "eof"})

	select {
	case err := <-session.playbackEnd:
		if err != nil {
			t.Fatalf("playbackEnd error = %v, want clean EOF", err)
		}
	default:
		t.Fatal("terminal event was not recorded after public event stream closed")
	}
}

func TestAcceptEventIgnoresIdleEOFUntilLoadedMediaLeavesEOF(t *testing.T) {
	t.Parallel()

	session := &Session{
		events:      make(chan Event, 4),
		playbackEnd: make(chan error, 1),
	}
	session.acceptEvent(Event{Name: "property-change", Property: "eof-reached", Data: []byte("true")})
	assertNoPlaybackEnd(t, session.playbackEnd)
	assertNoPublicEvent(t, session.events)
	session.acceptEvent(Event{Name: "property-change", Property: "eof-reached", Data: []byte("false")})
	assertNoPlaybackEnd(t, session.playbackEnd)
	assertNoPublicEvent(t, session.events)

	session.eventMutex.Lock()
	session.loadStarted = true
	session.eventMutex.Unlock()
	session.acceptEvent(Event{Name: "property-change", Property: "eof-reached", Data: []byte("true")})
	assertNoPlaybackEnd(t, session.playbackEnd)
	assertNoPublicEvent(t, session.events)

	session.acceptEvent(Event{Name: "property-change", Property: "eof-reached", Data: []byte("false")})
	assertNoPlaybackEnd(t, session.playbackEnd)
	if event := <-session.events; string(event.Data) != "false" {
		t.Fatalf("armed public EOF event = %s, want false", event.Data)
	}
	session.acceptEvent(Event{Name: "property-change", Property: "eof-reached", Data: []byte("true")})
	select {
	case err := <-session.playbackEnd:
		if err != nil {
			t.Fatalf("playbackEnd error = %v", err)
		}
	default:
		t.Fatal("post-load EOF did not end playback")
	}
	if event := <-session.events; string(event.Data) != "true" {
		t.Fatalf("terminal public EOF event = %s, want true", event.Data)
	}
}

func assertNoPlaybackEnd(t *testing.T, playbackEnd <-chan error) {
	t.Helper()
	select {
	case err := <-playbackEnd:
		t.Fatalf("unexpected playback end: %v", err)
	default:
	}
}

func assertNoPublicEvent(t *testing.T, events <-chan Event) {
	t.Helper()
	select {
	case event := <-events:
		t.Fatalf("unexpected public event: %+v", event)
	default:
	}
}

func TestWaitForEndWaitsForInFlightTerminalHandlerAfterDisconnect(t *testing.T) {
	t.Parallel()

	clientConnection, peerConnection := net.Pipe()
	t.Cleanup(func() {
		_ = peerConnection.Close() //nolint:errcheck
	})
	handlerStarted := make(chan struct{})
	releaseHandler := make(chan struct{})
	session := &Session{
		events:      make(chan Event, 1),
		playbackEnd: make(chan error, 1),
		processDone: make(chan struct{}),
	}
	session.client = NewClient(clientConnection, ClientOptions{eventHandler: func(event Event) {
		close(handlerStarted)
		<-releaseHandler
		session.acceptEvent(event)
	}})
	if _, err := peerConnection.Write([]byte("{\"event\":\"end-file\",\"reason\":\"eof\"}\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-handlerStarted:
	case <-time.After(time.Second):
		t.Fatal("terminal handler did not start")
	}
	if err := session.client.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		t.Fatal(err)
	}

	waitDone := make(chan error, 1)
	go func() { waitDone <- session.WaitForEnd(context.Background()) }()
	select {
	case err := <-waitDone:
		t.Fatalf("WaitForEnd returned %v before the in-flight terminal handler drained", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseHandler)
	select {
	case err := <-waitDone:
		if err != nil {
			t.Fatalf("WaitForEnd() error = %v, want clean terminal outcome", err)
		}
	case <-time.After(time.Second):
		t.Fatal("WaitForEnd did not return after the terminal handler drained")
	}
}
