package cli

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/rabesss/impartus-cli/internal/artifact"
	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/events"
)

type downloadEventStream struct {
	writer  *events.Writer
	jobID   string
	now     func() time.Time
	started bool
}

func newDownloadEventStream(output io.Writer, jobID string, now func() time.Time) *downloadEventStream {
	if strings.TrimSpace(jobID) == "" {
		jobID = "job-" + uuid.NewString()
	}
	if now == nil {
		now = time.Now
	}
	return &downloadEventStream{writer: events.NewWriter(output), jobID: jobID, now: now}
}

func (stream *downloadEventStream) start() error {
	if stream.started {
		return nil
	}
	stream.started = true
	return stream.writer.Emit(events.Event{
		Type: events.JobStarted, JobID: stream.jobID, Command: "download",
		Status: "running", Timestamp: stream.now().UTC(),
	})
}

func (stream *downloadEventStream) finish(ctx context.Context, result downloadResult, cause error) error {
	if !result.LibraryRecorded && (cause == nil || len(result.Artifacts) > 0) {
		commitErr := errors.Join(errDownloadLibraryCommit, errors.New("download completed but the local library commit did not complete"))
		return stream.failResult(ctx, result, errors.Join(cause, commitErr))
	}
	if cause != nil {
		return stream.failResult(ctx, result, cause)
	}
	if err := stream.writer.Emit(events.Event{
		Type: events.JobCompleted, JobID: stream.jobID, Command: "download",
		Status: "completed", Timestamp: stream.now().UTC(), Outputs: artifactOutputPaths(result.Artifacts),
		Artifacts: append([]artifact.Manifest(nil), result.Artifacts...),
		Details: map[string]any{
			"lectureCount": result.LectureCount, "libraryRecorded": result.LibraryRecorded,
			"filteredCount": result.FilteredCount, "totalLectures": result.TotalLectures,
		},
	}); err != nil {
		return stream.failResult(ctx, result, err)
	}
	return nil
}

func (stream *downloadEventStream) fail(ctx context.Context, cause error) error {
	return stream.failResult(ctx, downloadResult{}, cause)
}

func (stream *downloadEventStream) failResult(ctx context.Context, result downloadResult, cause error) error {
	event := events.Failure(stream.jobID, "download", cause, stream.now())
	if !errors.Is(cause, errDownloadLibraryCommit) && !errors.Is(cause, errDownloadEventDelivery) && events.IsCancellationForContext(ctx, cause) {
		event = events.Cancellation(stream.jobID, "download", cause, stream.now())
	}
	event.Outputs = artifactOutputPaths(result.Artifacts)
	event.Artifacts = append([]artifact.Manifest(nil), result.Artifacts...)
	event.Details = map[string]any{
		"lectureCount": result.LectureCount, "libraryRecorded": result.LibraryRecorded,
		"filteredCount": result.FilteredCount, "totalLectures": result.TotalLectures,
	}
	emitErr := stream.writer.Emit(event)
	if emitErr != nil {
		return errors.Join(events.RedactedError(cause), errDownloadEventDelivery, events.RedactedError(emitErr))
	}
	return events.RedactedError(cause)
}

func (stream *downloadEventStream) lecture(eventType string, lecture client.Lecture, artifactID string, manifest *artifact.Manifest, outputs []string, details any) error {
	if stream == nil {
		return nil
	}
	event := events.Event{
		Type: eventType, JobID: stream.jobID, Command: "download", Timestamp: stream.now().UTC(),
		Lecture:    &events.Lecture{TTID: lecture.TTID, SeqNo: lecture.SeqNo, Topic: lecture.Topic},
		ArtifactID: artifactID, Artifact: manifest, Outputs: append([]string(nil), outputs...), Details: details,
	}
	if err := stream.writer.Emit(event); err != nil {
		return errors.Join(errDownloadEventDelivery, events.RedactedError(err))
	}
	return nil
}

func runDownloadEventsWithDependenciesContext(ctx context.Context, args []string, output io.Writer, deps downloadExecutionDependencies, now func() time.Time, jobID string) error {
	stream := newDownloadEventStream(output, jobID, now)
	if err := stream.start(); err != nil {
		return stream.fail(ctx, err)
	}
	presentation := quietDownloadPresentation()
	presentation.eventStream = stream
	result, err := executeDownloadWithDependenciesContext(ctx, args, presentation, deps)
	return stream.finish(ctx, result, err)
}

func emitDownloadResultEvents(ctx context.Context, output io.Writer, jobID string, result downloadResult, cause error, now func() time.Time) error {
	stream := newDownloadEventStream(output, jobID, now)
	if err := stream.start(); err != nil {
		return stream.fail(ctx, err)
	}
	return stream.finish(ctx, result, cause)
}

func requestedEvents(args []string) bool {
	enabled := false
	valueFlags := map[string]bool{
		"--subject": true, "-subject": true, "-s": true,
		"--session": true, "-session": true, "-S": true,
		"--ttid": true, "-ttid": true,
		"--start": true, "-start": true, "--end": true, "-end": true,
		"--quality": true, "-quality": true, "--views": true, "-views": true,
		"--format": true, "-format": true, "--output": true, "-output": true, "-o": true,
		"--interval": true, "-interval": true,
	}
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if argument == "--" {
			break
		}
		if valueFlags[argument] {
			index++
			continue
		}
		// flag.FlagSet stops parsing immediately before the first positional
		// argument. Keep this pre-parser aligned so a later --events token cannot
		// change the output mode of a parse error the real parser never reached.
		if argument == "" || argument == "-" || !strings.HasPrefix(argument, "-") {
			break
		}
		if argument == "--events" || argument == "-events" {
			enabled = true
			continue
		}
		prefix := "--events="
		if strings.HasPrefix(argument, "-events=") && !strings.HasPrefix(argument, prefix) {
			prefix = "-events="
		}
		if !strings.HasPrefix(argument, prefix) {
			continue
		}
		value, err := strconv.ParseBool(strings.TrimPrefix(argument, prefix))
		if err == nil {
			enabled = value
		}
	}
	return enabled
}

func manifestOutputPaths(manifest artifact.Manifest) []string {
	paths := make([]string, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		paths = append(paths, file.Path)
	}
	return paths
}

func artifactOutputPaths(manifests []artifact.Manifest) []string {
	count := 0
	for _, manifest := range manifests {
		count += len(manifest.Files)
	}
	output := make([]string, 0, count)
	for _, manifest := range manifests {
		output = append(output, manifestOutputPaths(manifest)...)
	}
	return output
}
