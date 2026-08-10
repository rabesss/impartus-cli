// Package events defines the stable newline-delimited JSON lifecycle stream
// shared by CLI downloads and the generic watcher.
package events

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/rabesss/impartus-cli/internal/artifact"
	"github.com/rabesss/impartus-cli/internal/secrets"
)

// SchemaVersion is the current NDJSON event-contract version.
const SchemaVersion = 1

// Lifecycle event types are stable values consumed by local automation.
const (
	JobStarted       = "job.started"
	LectureStarted   = "lecture.started"
	LectureProgress  = "lecture.progress"
	LectureCompleted = "lecture.completed"
	LectureFailed    = "lecture.failed"
	JobCompleted     = "job.completed"
	JobFailed        = "job.failed"
	JobCanceled      = "job.canceled"
)

var (
	// ErrTerminalEvent reports a second terminal event or any event after one.
	ErrTerminalEvent = errors.New("event stream already emitted a terminal event")
	validTypes       = map[string]bool{
		JobStarted: true, LectureStarted: true, LectureProgress: true, LectureCompleted: true,
		LectureFailed: true, JobCompleted: true, JobFailed: true, JobCanceled: true,
	}
)

// Target identifies the course associated with an event.
type Target struct {
	SubjectID int    `json:"subjectId"`
	SessionID int    `json:"sessionId"`
	Label     string `json:"label,omitempty"`
}

// Lecture identifies one source lecture without embedding credentials or URLs.
type Lecture struct {
	TTID  int    `json:"ttid"`
	SeqNo int    `json:"seqNo,omitempty"`
	Topic string `json:"topic,omitempty"`
}

// Event is one self-contained NDJSON lifecycle record.
type Event struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Type          string              `json:"event"`
	JobID         string              `json:"jobId"`
	Command       string              `json:"command"`
	Status        string              `json:"status,omitempty"`
	Timestamp     time.Time           `json:"timestamp"`
	Target        *Target             `json:"target,omitempty"`
	Lecture       *Lecture            `json:"lecture,omitempty"`
	ArtifactID    string              `json:"artifactId,omitempty"`
	Artifact      *artifact.Manifest  `json:"artifact,omitempty"`
	Artifacts     []artifact.Manifest `json:"artifacts,omitempty"`
	Outputs       []string            `json:"outputs,omitempty"`
	Details       any                 `json:"details,omitempty"`
	Error         string              `json:"error,omitempty"`
}

// Emitter synchronously publishes a lifecycle event.
type Emitter interface {
	Emit(Event) error
}

// Writer serializes an event stream and enforces its single-terminal rule.
type Writer struct {
	mu                sync.Mutex
	output            io.Writer
	encoder           *json.Encoder
	terminalAttempted bool
	terminalEmitted   bool
}

// NewWriter creates a synchronous NDJSON emitter for output.
func NewWriter(output io.Writer) *Writer {
	return &Writer{output: output, encoder: json.NewEncoder(output)}
}

// Emit validates and writes one event, rejecting records after a terminal.
func (writer *Writer) Emit(event Event) error {
	if writer == nil || writer.encoder == nil {
		return errors.New("event writer is not configured")
	}
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.terminalAttempted {
		return ErrTerminalEvent
	}
	if event.SchemaVersion == 0 {
		event.SchemaVersion = SchemaVersion
	}
	if err := validate(event); err != nil {
		return err
	}
	terminal := IsTerminal(event.Type)
	if terminal {
		writer.terminalAttempted = true
	}
	if err := writer.encoder.Encode(event); err != nil {
		// json.Encoder permanently caches an output error. Recreate it so a
		// caller can still attempt a terminal failure record when the sink's
		// failure was transient or occurred before writing any bytes.
		writer.encoder = json.NewEncoder(writer.output)
		return fmt.Errorf("write event %s: %w", event.Type, err)
	}
	if terminal {
		writer.terminalEmitted = true
	}
	return nil
}

// TerminalEmitted reports whether the stream has written its terminal record.
func (writer *Writer) TerminalEmitted() bool {
	if writer == nil {
		return false
	}
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.terminalEmitted
}

// TerminalAttempted reports whether a terminal write was attempted, even if
// the underlying output rejected it. Callers use this to avoid retry loops on
// closed pipes and other permanently unwritable sinks.
func (writer *Writer) TerminalAttempted() bool {
	if writer == nil {
		return false
	}
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.terminalAttempted
}

func validate(event Event) error {
	if event.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported event schema version %d", event.SchemaVersion)
	}
	if !validTypes[event.Type] {
		return fmt.Errorf("unsupported event type %q", event.Type)
	}
	if strings.TrimSpace(event.JobID) == "" {
		return errors.New("event jobId is required")
	}
	if strings.TrimSpace(event.Command) == "" {
		return errors.New("event command is required")
	}
	if event.Timestamp.IsZero() {
		return errors.New("event timestamp is required")
	}
	if isLectureLifecycle(event.Type) && strings.TrimSpace(event.ArtifactID) == "" {
		return fmt.Errorf("event %s artifactId is required", event.Type)
	}
	return nil
}

func isLectureLifecycle(eventType string) bool {
	switch eventType {
	case LectureStarted, LectureProgress, LectureCompleted, LectureFailed:
		return true
	default:
		return false
	}
}

// IsTerminal reports whether eventType closes an event stream.
func IsTerminal(eventType string) bool {
	return eventType == JobCompleted || eventType == JobFailed || eventType == JobCanceled
}

// Failure constructs a sanitized terminal failure event.
func Failure(jobID, command string, cause error, at time.Time) Event {
	message := "operation failed"
	if cause != nil {
		message = RedactError(cause)
	}
	return Event{
		SchemaVersion: SchemaVersion, Type: JobFailed, JobID: jobID, Command: command, Status: "failed",
		Timestamp: at.UTC(), Error: message,
	}
}

// Cancellation constructs a sanitized terminal cancellation event.
func Cancellation(jobID, command string, cause error, at time.Time) Event {
	message := "operation canceled"
	if cause != nil {
		message = RedactError(cause)
	}
	return Event{
		SchemaVersion: SchemaVersion, Type: JobCanceled, JobID: jobID, Command: command, Status: "canceled",
		Timestamp: at.UTC(), Error: message,
	}
}

// RedactError returns a lifecycle-safe error message with URL credentials,
// authorization headers, and common bare secret assignments removed.
func RedactError(cause error) string {
	if cause == nil {
		return ""
	}
	return scrubFailure(cause)
}

// RedactedError returns an error whose rendered message and reachable chain are
// safe for CLI and automation output. It preserves errors.Is classification
// without exposing the original cause through errors.Unwrap or errors.As.
func RedactedError(cause error) error {
	if cause == nil {
		return nil
	}
	if _, ok := cause.(redactedError); ok {
		return cause
	}
	return redactedError{cause: cause}
}

type redactedError struct{ cause error }

func (err redactedError) Error() string { return RedactError(err.cause) }
func (err redactedError) Is(target error) bool {
	return errors.Is(err.cause, target)
}

func scrubFailure(cause error) string {
	return secrets.ScrubError(cause)
}
