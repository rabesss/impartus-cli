package tuisession

import (
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/tuiproto"
)

func TestHubSanitizesEventMessagesBeforeDelivery(t *testing.T) {
	events := newHub(2)
	_, subscriber, ok := events.subscribe()
	if !ok {
		t.Fatal("subscribe rejected an open event hub")
	}
	<-subscriber.events // session.ready

	message := "to\x1b[31mken=event-secret"
	events.publish(tuiproto.Event{Message: &message, Type: tuiproto.EventTypeOperationProgress})
	delivered := <-subscriber.events
	if delivered.Message == nil {
		t.Fatal("event message was dropped")
	}
	if strings.Contains(*delivered.Message, "event-secret") || strings.ContainsRune(*delivered.Message, '\x1b') {
		t.Fatalf("unsafe event message = %q", *delivered.Message)
	}
	if *delivered.Message != "token=REDACTED" {
		t.Fatalf("event message = %q, want token=REDACTED", *delivered.Message)
	}
}
