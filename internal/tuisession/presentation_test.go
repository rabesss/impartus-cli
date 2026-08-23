package tuisession

import (
	"fmt"
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

func TestSafePresentationTextRejectsCombiningMarkSplitCredentialKeys(t *testing.T) {
	const value = "to\u0301ken=hunter2"
	if got := safePresentationText(value); got != "REDACTED" {
		t.Fatalf("safePresentationText() = %q, want REDACTED", got)
	}
}

func TestSafePresentationTextPreservesSafeWhitespaceBoundaries(t *testing.T) {
	for name, test := range map[string]struct {
		value string
		want  string
	}{
		"ordinary whitespace":                   {value: "mpv missing\nrun the installer\tthen retry", want: "mpv missing run the installer then retry"},
		"credential-shaped spacing":             {value: "Q&A: key = value slide", want: "Q&A: key = REDACTED slide"},
		"mixed credential forms":                {value: "token=first keep to ken=second", want: "token=REDACTED keep token=REDACTED"},
		"quoted split credential":               {value: `"to ken": "quoted-secret"`, want: `"token": "REDACTED"`},
		"quoted key bare value":                 {value: `"to ken": bare-secret`, want: `"token": REDACTED`},
		"quoted key equals value":               {value: `"to ken"=equals-secret`, want: `"token"=REDACTED`},
		"single quoted key":                     {value: `'to ken': single-secret`, want: `'token': REDACTED`},
		"clean URL prose boundary":              {value: "see https://example.com/path?keep=1 then retry", want: "see https://example.com/path?keep=1 then retry"},
		"credential URL conservative redaction": {value: "see https://example.com/?token=url-secret then retry", want: "see https://example.com/?token=REDACTED"},
		"split URL userinfo":                    {value: "https://us er:pa ss@example.com/", want: "https://example.com/"},
		"split URL scheme":                      {value: "ht tps://user:pass@example.com/", want: "https://example.com/"},
		"split URL query key":                   {value: "https://example.com/?to ken=query-secret", want: "https://example.com/?token=REDACTED"},
		"split URL query value":                 {value: "https://example.com/?token= SECRET", want: "https://example.com/?token=REDACTED"},
		"split credential scheme":               {value: "to ken: be arer s3cr3t", want: "token: REDACTED"},
		"bare value word boundary":              {value: "token=first second; keep", want: "token=REDACTED second; keep"},
		"marker collision":                      {value: "keep\uE000this\nspacing", want: "keep\uE000this spacing"},
		"encoded marker collision":              {value: "x%EE%80%80y", want: "x%EE%80%80y"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := safePresentationText(test.value); got != test.want {
				t.Fatalf("safePresentationText() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSafePresentationTextRedactsWhitespaceAtEveryCredentialURLBoundary(t *testing.T) {
	const queryURL = "https://example.com/path?token=querysecret"
	for index := 1; index < len(queryURL); index++ {
		value := queryURL[:index] + " " + queryURL[index:]
		got := safePresentationText(value)
		compact := strings.ReplaceAll(got, " ", "")
		if strings.Contains(compact, "querysecret") || !strings.Contains(got, "REDACTED") {
			t.Fatalf("query URL split at %d = %q", index, got)
		}
	}

	const userinfoURL = "https://user:password@example.com/"
	for index := 1; index < len(userinfoURL); index++ {
		value := userinfoURL[:index] + " " + userinfoURL[index:]
		got := safePresentationText(value)
		compact := strings.ReplaceAll(got, " ", "")
		if strings.Contains(compact, "password") || strings.Contains(got, "@") {
			t.Fatalf("userinfo URL split at %d = %q", index, got)
		}
	}
}

func TestSafePresentationTextRedactsEscapeAtEveryCredentialBoundary(t *testing.T) {
	for name, value := range map[string]string{
		"assignment": "token=assignmentsecret",
		"query URL":  "https://example.com/path?token=querysecret",
		"userinfo":   "https://alice:zzzzzz@example.com/",
	} {
		t.Run(name, func(t *testing.T) {
			for index := 1; index < len(value); index++ {
				got := safePresentationText(value[:index] + "\x1b" + value[index:])
				compact := strings.ReplaceAll(got, " ", "")
				if strings.Contains(compact, "assignmentsecret") || strings.Contains(compact, "querysecret") || strings.Contains(compact, "zzz") || strings.Contains(got, "@") {
					t.Fatalf("credential split at %d = %q", index, got)
				}
				// URL userinfo is removed rather than replaced with a marker; the
				// repeated-secret assertion above catches even partial exposure.
				if name != "userinfo" && !strings.Contains(got, "REDACTED") {
					t.Fatalf("credential split at %d = %q, want redaction marker", index, got)
				}
			}
		})
	}
}

func TestSafePresentationTextRejectsCompleteEscapeSequenceCredentialSplits(t *testing.T) {
	for name, value := range map[string]string{
		"CSI final byte":        "to\x1b[ken=csi-secret",
		"CSI crosses delimiter": "token\x1b[0=assignmentsecret",
		"CSI final userinfo at": "https://alice:hunter2\x1b[/@example.com/",
		"8-bit CSI delimiter":   "token\x9b0=assignmentsecret",
		"8-bit CSI userinfo at": "https://alice:hunter2\x9b/@example.com/",
		"Unicode CSI delimiter": "token\u009b0=assignmentsecret",
		"Unicode CSI userinfo":  "https://alice:hunter2\u009b/@example.com/",
		"OSC body":              "to\x1b]k\x07en=osc-secret",
		"OSC selector split":    "to\x1b]k;0\x07en=hunter2",
		"OSC selector and body": "t\x1b]o;ke\x07n=hunter2",
		"DCS split body":        "t\x1bPo;ke\x1b\\n=hunter2",
		"APC split body":        "t\x1b_o;ke\x1b\\n=hunter2",
		"SOS split body":        "t\x1bXo;ke\x1b\\n=hunter2",
		"PM split body":         "t\x1b^o;ke\x1b\\n=hunter2",
		"8-bit OSC split body":  "t\x9do;ke\x9cn=hunter2",
		"8-bit DCS split body":  "t\x90o;ke\x9cn=hunter2",
		"8-bit APC split body":  "t\x9fo;ke\x9cn=hunter2",
		"8-bit SOS split body":  "t\x98o;ke\x9cn=hunter2",
		"8-bit PM split body":   "t\x9eo;ke\x9cn=hunter2",
		"Unicode OSC split":     "t\u009do;ke\u009cn=hunter2",
		"Unicode DCS split":     "t\u0090o;ke\u009cn=hunter2",
		"Unicode APC split":     "t\u009fo;ke\u009cn=hunter2",
		"Unicode SOS split":     "t\u0098o;ke\u009cn=hunter2",
		"Unicode PM split":      "t\u009eo;ke\u009cn=hunter2",
	} {
		t.Run(name, func(t *testing.T) {
			if got := safePresentationText(value); got != "REDACTED" {
				t.Fatalf("safePresentationText() = %q, want REDACTED", got)
			}
		})
	}
}

func TestSafePresentationTextRejectsObfuscatedCredentialAlongsideCleanCredential(t *testing.T) {
	for name, value := range map[string]string{
		"assignment":               "token=abc to\x1b[ken=hunter2",
		"userinfo":                 "token=abc https://user:pass\x1b[@example.com/",
		"stripped-only decoy":      "to\x1b[0mken=hunter2 to\x1b[ken=ev1l",
		"payload-prefix collision": "\x1b[0mtoken=abc to\x1b[ken=hunter2",
		"same-value decoy":         "to\x1b[0mken=S3CRET x/\x1b[0mtoken=S3CRET",
	} {
		t.Run(name, func(t *testing.T) {
			if got := safePresentationText(value); got != "REDACTED" {
				t.Fatalf("safePresentationText() = %q, want REDACTED", got)
			}
		})
	}
}

func TestSafePresentationTextRejectsObfuscatedCredentialContainingRedactionMarker(t *testing.T) {
	const value = "to\x1b[ken=hunter2REDACTEDc"
	if got := safePresentationText(value); got != "REDACTED" {
		t.Fatalf("safePresentationText() = %q, want REDACTED", got)
	}
}

func TestSafePresentationTextPreservesContextWhenANSISurroundsCredentialValue(t *testing.T) {
	const want = "retry token=REDACTED after refresh"
	for name, value := range map[string]string{
		"styled credential": "retry token=\x1b[31msecret\x1b[0m after refresh",
		"styled context":    "retry token=secret after \x1b[31mrefresh\x1b[0m",
	} {
		t.Run(name, func(t *testing.T) {
			if got := safePresentationText(value); got != want {
				t.Fatalf("safePresentationText() = %q, want %q", got, want)
			}
		})
	}
}

func TestSafePresentationTextPreservesContextForANSIStyledCredentialURLs(t *testing.T) {
	for name, test := range map[string]struct {
		value string
		want  string
	}{
		"query followed by prose": {
			value: "GET \x1b[1mhttps://e.com/?token=abc\x1b[0m failed",
			want:  "GET https://e.com/?token=REDACTED",
		},
		"percent-encoded query": {
			value: "fetch \x1b[1mhttps://e.com/?token=a%2Fb\x1b[0m",
			want:  "fetch https://e.com/?token=REDACTED",
		},
		"plus-encoded query": {
			value: "fetch \x1b[1mhttps://e.com/?token=a+b\x1b[0m",
			want:  "fetch https://e.com/?token=REDACTED",
		},
		"userinfo with colon": {
			value: "ok \x1b[1mhttps://alice:pass:word@e.com/\x1b[0m",
			want:  "ok https://e.com/",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := safePresentationText(test.value); got != test.want {
				t.Fatalf("safePresentationText() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSafePresentationTextPreservesContextWithAdjacentCredentialShapes(t *testing.T) {
	for name, value := range map[string]string{
		"strong assignment":   "auth: yes token: abc after \x1b[31mrefresh\x1b[0m",
		"nested quoted key":   `x secret: "token": "abc" after ` + "\x1b[31mrefresh\x1b[0m",
		"quoted marker value": `retry token: "` + "\x1b[31mREDACTED\x1b[0m" + `" after refresh`,
	} {
		t.Run(name, func(t *testing.T) {
			got := safePresentationText(value)
			if got == "REDACTED" {
				t.Fatal("safePresentationText() discarded safely redacted context")
			}
			if strings.Contains(got, "abc") || strings.Contains(got, "yes") {
				t.Fatalf("safePresentationText() exposed credential material: %q", got)
			}
		})
	}
}

func TestSafePresentationTextDoesNotExposeInternalMarkersInCredentialURLs(t *testing.T) {
	const value = "https://exa mple.com/?token=url-secret"
	const want = "https://exa mple.com/?token=REDACTED"
	if got := safePresentationText(value); got != want {
		t.Fatalf("safePresentationText() = %q, want %q", got, want)
	}
}

func TestSafePresentationTextRedactsSplitQuotedKeysWithTrailingWhitespace(t *testing.T) {
	for whitespaceName, separator := range map[string]string{
		"space":          " ",
		"no-break space": "\u00a0",
		"tab":            "\t",
		"line separator": "\u2028",
		"thin space":     "\u2009",
		"ideographic":    "\u3000",
		"vertical tab":   "\v",
	} {
		t.Run(whitespaceName, func(t *testing.T) {
			for shapeName, value := range map[string]string{
				"quoted key":       fmt.Sprintf(`"token%s": "spotleak"`, separator),
				"JSON key":         fmt.Sprintf(`{"token%s": "spotleak"}`, separator),
				"single quoted":    fmt.Sprintf(`'token%s': spotleak`, separator),
				"split quoted key": fmt.Sprintf(`"to%sken%s": "spotleak"`, separator, separator),
				"suffix key":       fmt.Sprintf(`"client_secret%s": "spotleak"`, separator),
			} {
				t.Run(shapeName, func(t *testing.T) {
					got := safePresentationText(value)
					if strings.Contains(got, "spotleak") || !strings.Contains(got, "REDACTED") {
						t.Fatalf("safePresentationText() = %q, want credential redaction", got)
					}
				})
			}
		})
	}
}
