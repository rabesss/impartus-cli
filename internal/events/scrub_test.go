package events

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/artifact"
)

func TestEmitScrubsAllStringFields(t *testing.T) {
	t.Parallel()

	manifest := artifact.Manifest{
		SchemaVersion: 1, ArtifactID: "token=secret-manifest-id",
		Lecture: artifact.Lecture{
			TTID: 123, Topic: "token=secret-manifest-topic", StartTime: "token=secret-start",
			Professor: "token=secret-professor", Institute: "token=secret-institute",
		},
		Selection: artifact.Selection{
			Views: "token=secret-views", Quality: "token=secret-quality", AudioOnly: true,
			AudioFormat: "token=secret-format",
		},
		Files: []artifact.File{{
			Path: "https://media.example.test/file?sig=secret-path", Role: "token=secret-role",
			View: "token=secret-view", Container: "token=secret-container", Bytes: 9007199254740993,
			SHA256: "token=secret-hash",
		}},
		ProducedAt: time.Unix(5, 0).UTC(),
		Producer:   artifact.Producer{Name: "token=secret-producer", Version: "token=secret-version"},
	}
	event := Event{
		Type: LectureCompleted, JobID: "token=secret-job", Command: "token=secret-command",
		Status: "token=secret-status", Timestamp: time.Unix(6, 0).UTC(),
		Target:     &Target{SubjectID: 7, SessionID: 8, Label: "https://media.example.test/x?token=secret-label"},
		Lecture:    &Lecture{TTID: 123, Topic: "Authorization: Bearer secret-topic"},
		ArtifactID: "token=secret-artifact-id", Artifact: &manifest, Artifacts: []artifact.Manifest{manifest},
		Outputs: []string{"https://media.example.test/out?sig=secret-output", "lecture.mp3"},
		Details: map[string]any{
			"url":    "https://media.example.test/seg.ts?token=secret-details",
			"note":   "Authorization: Bearer secret-details-2",
			"nested": []any{map[string]any{"token=secret-key": []string{"token=secret-nested", "clean"}}},
		},
		Error: "https://media.example.test/seg.ts?token=secret-error",
	}
	before, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if emitErr := NewWriter(&output).Emit(event); emitErr != nil {
		t.Fatalf("Emit() error = %v", emitErr)
	}
	if strings.Contains(output.String(), "secret-") {
		t.Fatalf("Emit() leaked a string field: %s", output.String())
	}
	if !strings.Contains(output.String(), "REDACTED") {
		t.Fatalf("Emit() output lacks redaction markers: %s", output.String())
	}
	after, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("Emit() mutated caller-owned data:\nbefore %s\nafter  %s", before, after)
	}
	var decoded Event
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("Emit() wrote invalid JSON: %v", err)
	}
	if decoded.Type != LectureCompleted || decoded.Artifact.Files[0].Bytes != 9007199254740993 || decoded.Outputs[1] != "lecture.mp3" {
		t.Fatalf("Emit() changed unrelated data: %+v", decoded)
	}
	var repeated bytes.Buffer
	if err := NewWriter(&repeated).Emit(decoded); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(repeated.Bytes(), output.Bytes()) {
		t.Fatalf("scrubbing is not idempotent:\nfirst  %s\nsecond %s", output.String(), repeated.String())
	}
}

type eventJSONMarshaler struct {
	json  string
	calls int
}

func (value *eventJSONMarshaler) MarshalJSON() ([]byte, error) {
	value.calls++
	return []byte(value.json), nil
}

type eventTextKey int

func (eventTextKey) MarshalText() ([]byte, error) { return []byte("token=secret-text-key"), nil }

func TestEmitScrubsSerializedDetails(t *testing.T) {
	t.Parallel()

	custom := &eventJSONMarshaler{json: `{"url":"https://host.test/x?token=secret-custom","n":9007199254740993}`}
	typed := struct {
		Nested []map[string]string `json:"nested"`
		Omit   string              `json:"omit,omitempty"`
		Hidden string              `json:"-"`
	}{Nested: []map[string]string{{"token=secret-typed-key": "token=secret-typed-value"}}, Hidden: "hidden"}
	tests := []struct {
		name    string
		details any
		want    string
	}{
		{"string", "token=secret-string", `"token=REDACTED"`},
		{"typed struct", &typed, `{"nested":[{"token=REDACTED":"token=REDACTED"}]}`},
		{"custom marshaler", custom, `{"url":"https://host.test/x?token=REDACTED","n":9007199254740993}`},
		{"text map keys", map[eventTextKey]string{1: "token=secret-text-value"}, `{"token=REDACTED":"token=REDACTED"}`},
		{"escaped JSON", json.RawMessage(`{"\u0074oken=secret-escaped-key":"https:\/\/host.test\/x?\u0074oken=secret-escaped-value"}`), `{"token=REDACTED":"https://host.test/x?token=REDACTED"}`},
		{"colliding keys", json.RawMessage(`{"token=secret-first":1,"token=secret-second":2}`), `{"token=REDACTED":1,"token=REDACTED":2}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event := Event{Type: JobStarted, JobID: "job-1", Command: "watch", Timestamp: time.Unix(1, 0).UTC(), Details: test.details}
			var output bytes.Buffer
			if err := NewWriter(&output).Emit(event); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(output.String(), "secret-") {
				t.Fatalf("Emit() leaked Details: %s", output.String())
			}
			var decoded struct {
				Details json.RawMessage `json:"details"`
			}
			if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
				t.Fatal(err)
			}
			if string(decoded.Details) != test.want {
				t.Fatalf("Details = %s, want %s", decoded.Details, test.want)
			}
		})
	}
	if custom.calls != 1 {
		t.Fatalf("MarshalJSON() called %d times, want once", custom.calls)
	}
	if typed.Nested[0]["token=secret-typed-key"] != "token=secret-typed-value" {
		t.Fatal("Emit() mutated the typed Details value")
	}
}

func TestEmitPreservesBenignJSONRepresentation(t *testing.T) {
	t.Parallel()

	type details struct {
		Path    string            `json:"path"`
		URL     string            `json:"url"`
		Escapes string            `json:"escapes"`
		Large   uint64            `json:"large"`
		Quoted  int64             `json:"quoted,string"`
		Bytes   []byte            `json:"bytes"`
		Nil     []string          `json:"nil"`
		Empty   []string          `json:"empty"`
		Omit    string            `json:"omit,omitempty"`
		Raw     json.RawMessage   `json:"raw"`
		Nested  map[int][]float64 `json:"nested"`
	}
	event := Event{
		SchemaVersion: SchemaVersion, Type: JobCompleted, JobID: "job-1", Command: "download",
		Timestamp: time.Unix(1, 123).UTC(), Outputs: []string{"/tmp/lecture.mp3"},
		Details: details{
			Path: `/home/user/lecture "1".mp3`, URL: "https://host.test/lecture?name=Graph%20Theory&sort=asc",
			Escapes: "line\n\t\"quote\\slash<>&☃", Large: math.MaxUint64, Quoted: 9007199254740993,
			Bytes: []byte{0, 127, 255}, Empty: []string{},
			Raw:    json.RawMessage(`{"escaped":"\u0061","n":1e+20,"n":-0,"nested":[true,false,null]}`),
			Nested: map[int][]float64{7: {1.5, -2, 0}},
		},
	}
	want, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := NewWriter(&output).Emit(event); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), append(want, '\n')) {
		t.Fatalf("Emit() changed benign JSON:\ngot  %s\nwant %s", output.Bytes(), want)
	}
}

type eventErrorMarshaler struct{ err error }

func (value eventErrorMarshaler) MarshalJSON() ([]byte, error) { return nil, value.err }

func TestEmitRejectsUnserializableDetailsWithoutOutput(t *testing.T) {
	t.Parallel()

	cycle := map[string]any{}
	cycle["self"] = cycle
	slice := make([]any, 1)
	slice[0] = slice
	marshalErr := errors.New("details cannot be encoded")
	tests := []struct {
		name    string
		details any
	}{
		{"channel", make(chan int)},
		{"function", func() {}},
		{"unsupported map key", map[struct{ Name string }]string{{Name: "key"}: "value"}},
		{"map cycle", cycle},
		{"slice cycle", slice},
		{"NaN", math.NaN()},
		{"infinity", math.Inf(1)},
		{"invalid raw JSON", json.RawMessage(`{"incomplete":`)},
		{"marshaler error", eventErrorMarshaler{err: marshalErr}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, eventType := range []string{JobStarted, JobCompleted} {
				var output bytes.Buffer
				writer := NewWriter(&output)
				event := Event{Type: eventType, JobID: "job-1", Command: "watch", Timestamp: time.Unix(1, 0).UTC(), Details: test.details}
				err := writer.Emit(event)
				if err == nil {
					t.Fatal("Emit() error = nil")
				}
				if test.name == "marshaler error" && !errors.Is(err, marshalErr) {
					t.Fatalf("Emit() lost marshal error identity: %v", err)
				}
				if output.Len() != 0 || writer.TerminalEmitted() || writer.TerminalAttempted() != IsTerminal(eventType) {
					t.Fatalf("failed encoding changed stream state: output=%q attempted=%v emitted=%v", output.String(), writer.TerminalAttempted(), writer.TerminalEmitted())
				}
				err = writer.Emit(Failure("job-1", "watch", errors.New("encoding failed"), time.Unix(2, 0).UTC()))
				if IsTerminal(eventType) {
					if !errors.Is(err, ErrTerminalEvent) {
						t.Fatalf("terminal retry = %v, want ErrTerminalEvent", err)
					}
				} else if err != nil {
					t.Fatalf("terminal recovery = %v", err)
				}
			}
		})
	}
	if reflect.ValueOf(cycle["self"]).Pointer() != reflect.ValueOf(cycle).Pointer() || reflect.ValueOf(slice[0]).Pointer() != reflect.ValueOf(slice).Pointer() {
		t.Fatal("Emit() mutated cyclic Details")
	}
}
