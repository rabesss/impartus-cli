package watch

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rabesss/impartus-cli/internal/artifact"
	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/events"
)

func (watcher *Watcher) finish(cause error, result CycleResult) error {
	safeCause := events.RedactedError(cause)
	if watcher.options.DeferTerminal {
		return safeCause
	}
	return errors.Join(safeCause, watcher.emit(TerminalEvent(watcher.options.JobID, cause, result, watcher.options.Now())))
}

// TerminalEvent builds the single aggregate watch terminal for both package
// and CLI ownership paths without duplicating failure classification or losing
// partial committed artifacts.
func TerminalEvent(jobID string, cause error, result CycleResult, at time.Time) events.Event {
	event := events.Event{
		Type: events.JobCompleted, JobID: jobID, Command: "watch", Status: "completed", Timestamp: at.UTC(),
		Outputs: append([]string(nil), result.Outputs...), Artifacts: append([]artifact.Manifest(nil), result.Artifacts...), Details: result,
	}
	if cause == nil {
		return event
	}
	if (errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded)) && !isFatalCycleError(cause) {
		event = events.Cancellation(jobID, "watch", cause, at)
	} else {
		event = events.Failure(jobID, "watch", cause, at)
	}
	event.Outputs = append([]string(nil), result.Outputs...)
	event.Artifacts = append([]artifact.Manifest(nil), result.Artifacts...)
	event.Details = result
	return event
}

func (watcher *Watcher) emit(event events.Event) error {
	event.SchemaVersion = events.SchemaVersion
	event.JobID = watcher.options.JobID
	event.Command = "watch"
	if event.Timestamp.IsZero() {
		event.Timestamp = watcher.options.Now().UTC()
	}
	if err := watcher.options.Emitter.Emit(event); err != nil {
		return errors.Join(ErrEventDelivery, events.RedactedError(err))
	}
	return nil
}

func (watcher *Watcher) emitLecture(eventType string, target config.WatchTarget, lecture client.Lecture, artifactID string, details any) error {
	event := events.Event{
		Type:       eventType,
		Target:     &events.Target{SubjectID: target.SubjectID, SessionID: target.SessionID, Label: target.Label},
		Lecture:    &events.Lecture{TTID: lecture.TTID, SeqNo: lecture.SeqNo, Topic: lecture.Topic},
		ArtifactID: artifactID,
		Details:    details,
	}
	return watcher.emit(event)
}

func (watcher *Watcher) lectureFailure(target config.WatchTarget, lecture client.Lecture, jobID string, cause error) error {
	details := map[string]any{"error": events.RedactError(cause)}
	if jobID != "" {
		details["libraryJobId"] = jobID
	}
	artifactID, identityErr := watcher.artifactIDForLecture(target, lecture)
	if identityErr != nil {
		return events.RedactedError(cause)
	}
	if err := watcher.emitLecture(events.LectureFailed, target, lecture, artifactID, details); err != nil {
		return errors.Join(events.RedactedError(cause), err)
	}
	return events.RedactedError(cause)
}

func (watcher *Watcher) logf(format string, values ...any) {
	_, _ = fmt.Fprintf(watcher.options.Log, format+"\n", values...) //nolint:errcheck // diagnostics are best-effort
}
