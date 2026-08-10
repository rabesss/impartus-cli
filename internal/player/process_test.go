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

func TestAcceptEventPublishesEOFPropertiesWithoutEndingPlayback(t *testing.T) {
	t.Parallel()

	session := &Session{
		events:      make(chan Event, 2),
		playbackEnd: make(chan error, 1),
	}
	for _, value := range []string{"false", "true"} {
		session.acceptEvent(Event{Name: "property-change", Property: "eof-reached", Data: []byte(value)})
		assertNoPlaybackEnd(t, session.playbackEnd)
		select {
		case event := <-session.events:
			if string(event.Data) != value {
				t.Fatalf("public EOF event = %s, want %s", event.Data, value)
			}
		default:
			t.Fatalf("EOF property %s was not published", value)
		}
	}
}

func TestPlaybackTerminalResultPrefersCancellationOverCleanExit(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := playbackTerminalResult(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("playbackTerminalResult() error = %v, want context cancellation", err)
	}
}

func TestPlaybackTerminalResultPreservesPlaybackFailure(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("playback failed")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := playbackTerminalResult(ctx, sentinel); !errors.Is(err, sentinel) {
		t.Fatalf("playbackTerminalResult() error = %v, want playback failure", err)
	}
}

func TestPlaybackCancellationResultPreservesReadyFailure(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("playback failed")
	playbackEnd := make(chan error, 1)
	playbackEnd <- sentinel
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := playbackCancellationResult(ctx, playbackEnd); !errors.Is(err, sentinel) {
		t.Fatalf("playbackCancellationResult() error = %v, want playback failure", err)
	}
}

func TestPlaybackCancellationResultKeepsCancellationOverReadyCleanExit(t *testing.T) {
	t.Parallel()

	playbackEnd := make(chan error, 1)
	playbackEnd <- nil
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := playbackCancellationResult(ctx, playbackEnd); !errors.Is(err, context.Canceled) {
		t.Fatalf("playbackCancellationResult() error = %v, want context cancellation", err)
	}
}

func TestAcceptEventRemovesUntrustedPeerTextFromPublicEvents(t *testing.T) {
	t.Parallel()

	session := &Session{events: make(chan Event, 1), playbackEnd: make(chan error, 1)}
	session.acceptEvent(Event{
		Name:      "end-file",
		Reason:    "error",
		FileError: "loading http://127.0.0.1:1234/private-token/master.m3u8 failed",
	})
	event := <-session.events
	if event.Reason != "error" || event.FileError != "" {
		t.Fatalf("public terminal event = %+v, want allowlisted reason without file_error", event)
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

func TestWaitForEndPreservesInFlightFailureWhenCancellationClosesIPC(t *testing.T) {
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
	if _, err := peerConnection.Write([]byte("{\"event\":\"end-file\",\"reason\":\"error\"}\n")); err != nil {
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
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	waitDone := make(chan error, 1)
	go func() { waitDone <- session.WaitForEnd(ctx) }()
	select {
	case err := <-waitDone:
		t.Fatalf("WaitForEnd returned %v before the in-flight failure drained", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseHandler)
	select {
	case err := <-waitDone:
		if err == nil || errors.Is(err, context.Canceled) {
			t.Fatalf("WaitForEnd() error = %v, want in-flight playback failure", err)
		}
	case <-time.After(time.Second):
		t.Fatal("WaitForEnd did not return after the terminal handler drained")
	}
}
