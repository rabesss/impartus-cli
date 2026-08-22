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

func TestSafePresentationTextRedactsControlSplitCredentialKeys(t *testing.T) {
	for name, value := range map[string]string{
		"c0 control":      "to\x01ken=control-secret",
		"line separator":  "to\u2028ken=separator-secret",
		"multiple spaces": "t o k e n=spaced-secret",
		"no-break space":  "to\u00a0ken=nbsp-secret",
	} {
		t.Run(name, func(t *testing.T) {
			if got := safePresentationText(value); got != "token=REDACTED" {
				t.Fatalf("safePresentationText() = %q, want token=REDACTED", got)
			}
		})
	}
}

func TestSafePresentationTextPreservesSafeWhitespaceBoundaries(t *testing.T) {
	for name, test := range map[string]struct {
		value string
		want  string
	}{
		"ordinary whitespace":       {value: "mpv missing\nrun the installer\tthen retry", want: "mpv missing run the installer then retry"},
		"credential-shaped spacing": {value: "Q&A: key = value slide", want: "Q&A: key = REDACTED slide"},
		"mixed credential forms":    {value: "token=first keep to ken=second", want: "token=REDACTED keep token=REDACTED"},
		"quoted split credential":   {value: `"to ken": "quoted-secret"`, want: `"token": "REDACTED"`},
		"bare value word boundary":  {value: "token=first second; keep", want: "token=REDACTED second; keep"},
		"marker collision":          {value: "keep\uE000this\nspacing", want: "keep\uE000this spacing"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := safePresentationText(test.value); got != test.want {
				t.Fatalf("safePresentationText() = %q, want %q", got, test.want)
			}
		})
	}
}
