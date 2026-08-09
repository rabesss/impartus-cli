package player

import "testing"

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
