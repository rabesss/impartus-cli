package events

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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

func TestWriterRejectsInvalidEventsBeforeOutput(t *testing.T) {
	t.Parallel()

	tests := []Event{
		{},
		{Type: "unknown", JobID: "job-1", Timestamp: time.Now()},
		{Type: JobStarted, Timestamp: time.Now()},
		{Type: JobStarted, JobID: "job-1"},
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
	} {
		got := RedactError(errors.New(input))
		if strings.Contains(got, "secret-value") || !strings.Contains(got, "REDACTED") {
			t.Fatalf("RedactError(%q) = %q", input, got)
		}
	}
}

func TestRedactErrorPreservesParserTokenDetails(t *testing.T) {
	t.Parallel()

	const message = "decode response: unexpected token: EOF"
	if got := RedactError(errors.New(message)); got != message {
		t.Fatalf("RedactError() = %q, want %q", got, message)
	}
}

func TestRedactedErrorPreservesIdentityWithoutRenderingSecrets(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("Authorization: Token secret-value")
	got := RedactedError(fmt.Errorf("request failed: %w", sentinel))
	if !errors.Is(got, sentinel) {
		t.Fatalf("RedactedError() lost original error identity: %v", got)
	}
	if strings.Contains(got.Error(), "secret-value") || !strings.Contains(got.Error(), "REDACTED") {
		t.Fatalf("RedactedError() = %q", got.Error())
	}
}
