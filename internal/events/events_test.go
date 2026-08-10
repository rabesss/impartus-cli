package events

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
)

type failFirstWrite struct {
	bytes.Buffer
	failed bool
}

type failNthWrite struct {
	bytes.Buffer
	failAt int
	writes int
}

func (writer *failNthWrite) Write(data []byte) (int, error) {
	writer.writes++
	if writer.writes == writer.failAt {
		return 0, errors.New("terminal output failure")
	}
	return writer.Buffer.Write(data)
}

func (writer *failFirstWrite) Write(data []byte) (int, error) {
	if !writer.failed {
		writer.failed = true
		return 0, errors.New("transient output failure")
	}
	return writer.Buffer.Write(data)
}

func TestWriterEmitsValidNDJSONAndExactlyOneTerminal(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	writer := NewWriter(&output)
	started := Event{Type: JobStarted, JobID: "job-1", Command: "watch", Timestamp: time.Unix(1, 0).UTC()}
	completed := Event{Type: JobCompleted, JobID: "job-1", Command: "watch", Timestamp: time.Unix(2, 0).UTC(), Outputs: []string{"lecture.mp3"}}
	if err := writer.Emit(started); err != nil {
		t.Fatalf("Emit(started) error = %v", err)
	}
	if err := writer.Emit(completed); err != nil {
		t.Fatalf("Emit(completed) error = %v", err)
	}
	if err := writer.Emit(Event{Type: JobFailed, JobID: "job-1"}); !errors.Is(err, ErrTerminalEvent) {
		t.Fatalf("duplicate terminal error = %v, want ErrTerminalEvent", err)
	}

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("NDJSON lines = %d, want 2: %q", len(lines), output.String())
	}
	for _, line := range lines {
		var decoded Event
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			t.Fatalf("invalid NDJSON line %q: %v", line, err)
		}
	}
	if !writer.TerminalEmitted() {
		t.Fatal("TerminalEmitted() = false")
	}
}

func TestEventV1UsesOriginalWireFieldAndLectureLifecycleNames(t *testing.T) {
	t.Parallel()

	for _, eventType := range []string{LectureProgress, LectureCompleted} {
		var output bytes.Buffer
		event := Event{Type: eventType, JobID: "job-1", Command: "download", ArtifactID: "impartus:v1:test", Timestamp: time.Unix(1, 0).UTC()}
		if err := NewWriter(&output).Emit(event); err != nil {
			t.Fatalf("Emit(%s) error = %v", eventType, err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded["event"] != eventType {
			t.Fatalf("wire event = %#v, want %q", decoded["event"], eventType)
		}
		if _, exists := decoded["type"]; exists {
			t.Fatalf("event v1 unexpectedly serialized legacy type field: %s", output.String())
		}
	}
}

func TestWriterRejectsInvalidEventsBeforeOutput(t *testing.T) {
	t.Parallel()

	tests := []Event{
		{},
		{Type: "unknown", JobID: "job-1", Timestamp: time.Now()},
		{Type: JobStarted, Timestamp: time.Now()},
		{Type: JobStarted, JobID: "job-1"},
		{Type: LectureStarted, JobID: "job-1", Command: "download", Timestamp: time.Now()},
	}
	for _, event := range tests {
		var output bytes.Buffer
		if err := NewWriter(&output).Emit(event); err == nil {
			t.Fatalf("Emit(%+v) error = nil", event)
		}
		if output.Len() != 0 {
			t.Fatalf("invalid event wrote output: %q", output.String())
		}
	}
}

func TestWriterCanEmitTerminalAfterTransientOutputFailure(t *testing.T) {
	t.Parallel()

	output := &failFirstWrite{}
	writer := NewWriter(output)
	if err := writer.Emit(Event{Type: JobStarted, JobID: "job-1", Command: "watch", Timestamp: time.Unix(1, 0).UTC()}); err == nil {
		t.Fatal("Emit(started) error = nil")
	}
	if err := writer.Emit(Failure("job-1", "watch", errors.New("stream start failed"), time.Unix(2, 0).UTC())); err != nil {
		t.Fatalf("Emit(failed terminal) error = %v", err)
	}
	var decoded Event
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &decoded); err != nil {
		t.Fatalf("terminal output is not valid JSON: %v", err)
	}
	if decoded.Type != JobFailed || !writer.TerminalEmitted() {
		t.Fatalf("terminal event = %+v, TerminalEmitted = %v", decoded, writer.TerminalEmitted())
	}
}

func TestWriterDoesNotRetryAnAttemptedTerminalWrite(t *testing.T) {
	t.Parallel()

	output := &failNthWrite{failAt: 2}
	writer := NewWriter(output)
	if err := writer.Emit(Event{Type: JobStarted, JobID: "job-1", Command: "watch", Timestamp: time.Unix(1, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	terminal := Failure("job-1", "watch", errors.New("failed"), time.Unix(2, 0).UTC())
	if err := writer.Emit(terminal); err == nil {
		t.Fatal("Emit(first terminal) error = nil")
	}
	if !writer.TerminalAttempted() || writer.TerminalEmitted() {
		t.Fatalf("TerminalAttempted = %v, TerminalEmitted = %v", writer.TerminalAttempted(), writer.TerminalEmitted())
	}
	if err := writer.Emit(terminal); !errors.Is(err, ErrTerminalEvent) {
		t.Fatalf("Emit(second terminal) error = %v, want ErrTerminalEvent", err)
	}
}

func TestFailureEventScrubsSecrets(t *testing.T) {
	t.Parallel()

	event := Failure(
		"job-1",
		"watch",
		errors.New("token=secret-value refresh_token=refresh-value authToken=auth-value Authorization: Bearer abc"),
		time.Unix(3, 0),
	)
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"secret-value", "refresh-value", "auth-value", "Bearer abc"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("failure event leaks %q: %s", secret, encoded)
		}
	}
	if strings.Contains(string(encoded), "refresh_token=REDACTED") {
		// JSON escapes do not affect this literal, so this also proves the key is
		// retained for diagnostics while only its value is removed.
	} else {
		t.Fatalf("failure event leaks secret: %s", encoded)
	}
	canceled := Cancellation("job-2", "watch", errors.New("password=hunter2 Authorization: Basic abc"), time.Unix(4, 0))
	encoded, err = json.Marshal(canceled)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "hunter2") || strings.Contains(string(encoded), "Basic abc") {
		t.Fatalf("cancellation event leaks secret: %s", encoded)
	}
}

func TestRedactErrorCoversAnyAuthorizationScheme(t *testing.T) {
	t.Parallel()

	for _, input := range []string{
		"Authorization: Token secret-value",
		"authorization: ApiKey secret-value",
		"Authorization: OAuth secret-value",
		`Authorization: Digest username="alice", realm="lecture", response="secret-value"`,
		"Authorization: Negotiate secret-value",
		"authorization=AWS4-HMAC-SHA256 Credential=secret-value SignedHeaders=host",
		"Authorization: Custom secret-value",
	} {
		got := RedactError(errors.New(input))
		if strings.Contains(got, "secret-value") || !strings.Contains(got, "REDACTED") {
			t.Fatalf("RedactError(%q) = %q", input, got)
		}
	}
}

func TestRedactErrorPreservesParserContextWhileRedactingTokenValue(t *testing.T) {
	t.Parallel()

	const message = "decode response: unexpected token: EOF"
	if got := RedactError(errors.New(message)); got != "decode response: unexpected token: REDACTED" {
		t.Fatalf("RedactError() = %q, want parser context with redacted token value", got)
	}
}

func TestRedactedErrorPreservesClassificationWithoutExposingRawCause(t *testing.T) {
	t.Parallel()

	raw := &url.Error{Op: "Get", URL: "https://example.test/chunk?token=secret-value", Err: context.Canceled}
	got := RedactedError(fmt.Errorf("request failed: %w", raw))
	if !errors.Is(got, context.Canceled) {
		t.Fatalf("RedactedError() lost cancellation classification: %v", got)
	}
	if strings.Contains(got.Error(), "secret-value") || !strings.Contains(got.Error(), "REDACTED") {
		t.Fatalf("RedactedError() = %q", got.Error())
	}
	if unwrapped := errors.Unwrap(got); unwrapped != nil {
		t.Fatalf("RedactedError() exposed raw cause through Unwrap: %v", unwrapped)
	}
	var recovered *url.Error
	if errors.As(got, &recovered) {
		t.Fatalf("RedactedError() exposed raw cause through errors.As: %v", recovered)
	}
}
