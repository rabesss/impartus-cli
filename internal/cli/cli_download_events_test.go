package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/artifact"
	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/downloader"
	"github.com/rabesss/impartus-cli/internal/events"
)

type failWriteOnce struct {
	bytes.Buffer
	failAt int
	writes int
}

func (writer *failWriteOnce) Write(data []byte) (int, error) {
	writer.writes++
	if writer.writes == writer.failAt {
		return 0, fmt.Errorf("event write %d failed", writer.writes)
	}
	return writer.Buffer.Write(data)
}

func TestDownloadEventsEmitCommittedArtifactsAndOneTerminal(t *testing.T) {
	t.Parallel()

	manifest := artifact.Manifest{SchemaVersion: 1, ArtifactID: "impartus:v1:test", Files: []artifact.File{{Path: "/absolute/lecture.mp3"}}}
	var output bytes.Buffer
	result := downloadResult{Status: "completed", OutputPaths: []string{"lecture.mp3"}, LectureCount: 1, Artifacts: []artifact.Manifest{manifest}, LibraryRecorded: true}
	err := emitDownloadResultEvents(&output, "job-test", result, nil, func() time.Time { return time.Unix(1, 0).UTC() })
	if err != nil {
		t.Fatalf("emitDownloadResultEvents() error = %v", err)
	}
	decoded := decodeCLIEvents(t, output.String())
	if len(decoded) != 3 || decoded[0].Type != events.JobStarted || decoded[1].Type != events.ArtifactCommitted || decoded[2].Type != events.JobCompleted {
		t.Fatalf("event types = %+v", decoded)
	}
	if decoded[1].Artifact == nil || decoded[1].Artifact.ArtifactID != manifest.ArtifactID {
		t.Fatalf("artifact event = %+v", decoded[1])
	}
	if got := decoded[2].Outputs; len(got) != 1 || got[0] != "/absolute/lecture.mp3" {
		t.Fatalf("job.completed outputs = %v, want committed manifest path", got)
	}
}

func TestDownloadEventsEmitOriginalPerLectureLifecycleWithArtifactID(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	lecture := client.Lecture{InstituteID: 4, SubjectID: 67, SessionID: 8, TTID: 123, SeqNo: 5, Topic: "Lifecycle"}
	runner := &fakeLectureDownloadRunner{
		playlists: []client.ParsedPlaylist{{InstituteID: 4, SubjectID: 67, SessionID: 8, ID: 123}},
		results:   materializeJoinResults(t, outputDir, []downloader.JoinResult{{LeftOutput: "lecture.mp4"}}),
	}
	var output bytes.Buffer
	stream := newDownloadEventStream(&output, "job-lifecycle", func() time.Time { return time.Unix(1, 0).UTC() })
	if err := stream.start(); err != nil {
		t.Fatal(err)
	}
	result, err := downloadLecturesWithRunner(context.Background(), &config.Config{
		DownloadLocation: outputDir, Views: "left", Quality: "720",
	}, runner, client.Lectures{lecture}, downloadPresentationOptions{eventStream: stream})
	if err != nil {
		t.Fatalf("downloadLecturesWithRunner() error = %v", err)
	}
	result.LibraryRecorded = true
	if err := stream.finish(result, nil); err != nil {
		t.Fatalf("finish() error = %v", err)
	}

	decoded := decodeCLIEvents(t, output.String())
	wantTypes := []string{
		events.JobStarted, events.LectureStarted, events.LectureProgress, events.LectureCompleted,
		events.ArtifactCommitted, events.JobCompleted,
	}
	if len(decoded) != len(wantTypes) {
		t.Fatalf("events = %+v, want types %v", decoded, wantTypes)
	}
	for index, wantType := range wantTypes {
		if decoded[index].Type != wantType {
			t.Fatalf("event[%d].Type = %q, want %q", index, decoded[index].Type, wantType)
		}
	}
	for _, event := range decoded[1:4] {
		if event.ArtifactID == "" {
			t.Fatalf("lecture event %q has no artifactId: %+v", event.Type, event)
		}
	}
	if decoded[3].Artifact == nil || decoded[3].Artifact.ArtifactID != decoded[3].ArtifactID || len(decoded[3].Outputs) != 1 {
		t.Fatalf("lecture.completed = %+v", decoded[3])
	}
}

func TestDownloadEventsFailClosedWhenLibraryCommitDidNotComplete(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	result := downloadResult{
		Status: "completed", OutputPaths: []string{"lecture.mp3"}, LectureCount: 1,
		Artifacts:       []artifact.Manifest{{SchemaVersion: 1, ArtifactID: "impartus:v1:not-committed"}},
		LibraryRecorded: false,
	}
	err := emitDownloadResultEvents(&output, "job-test", result, nil, func() time.Time { return time.Unix(1, 0).UTC() })
	if err == nil || !strings.Contains(err.Error(), "library") {
		t.Fatalf("emitDownloadResultEvents() error = %v", err)
	}
	decoded := decodeCLIEvents(t, output.String())
	if len(decoded) != 2 || decoded[0].Type != events.JobStarted || decoded[1].Type != events.JobFailed {
		t.Fatalf("events = %+v", decoded)
	}
}

func TestDownloadEventsFailureStillEmitsOneFailedTerminal(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	cause := errors.New("download failed")
	manifest := artifact.Manifest{SchemaVersion: 1, ArtifactID: "impartus:v1:partial", Files: []artifact.File{{Path: "/absolute/partial.mp3"}}}
	result := downloadResult{Status: "failed", LectureCount: 1, Artifacts: []artifact.Manifest{manifest}, LibraryRecorded: true}
	err := emitDownloadResultEvents(&output, "job-test", result, cause, func() time.Time { return time.Unix(1, 0).UTC() })
	if !errors.Is(err, cause) {
		t.Fatalf("error = %v, want cause", err)
	}
	decoded := decodeCLIEvents(t, output.String())
	terminals := 0
	for _, event := range decoded {
		if events.IsTerminal(event.Type) {
			terminals++
			if event.Type != events.JobFailed {
				t.Fatalf("terminal = %s", event.Type)
			}
		}
	}
	if terminals != 1 {
		t.Fatalf("terminal events = %d: %s", terminals, output.String())
	}
	terminal := decoded[len(decoded)-1]
	if len(terminal.Artifacts) != 1 || terminal.Artifacts[0].ArtifactID != manifest.ArtifactID || len(terminal.Outputs) != 1 {
		t.Fatalf("failed terminal partial completion = %+v", terminal)
	}
}

func TestDownloadEventsCancellationEmitsCanceledTerminalAndExit130(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output bytes.Buffer
	deps := downloadExecutionDependencies{
		ensureFFmpeg: func() error { return nil },
		initClient: func(got context.Context) (*config.Config, *client.Client, error) {
			return nil, nil, fmt.Errorf("Authorization: Token secret-value: %w", got.Err())
		},
	}
	err := runDownloadEventsWithDependenciesContext(
		ctx,
		[]string{"--events", "-s", "1", "-S", "2"},
		&output,
		deps,
		func() time.Time { return time.Unix(1, 0).UTC() },
		"job-canceled",
	)
	if !errors.Is(err, context.Canceled) || ExitCode(downloadCommandError(err)) != 130 {
		t.Fatalf("cancellation error = %v, exit = %d", err, ExitCode(downloadCommandError(err)))
	}
	if strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("returned error leaked authorization: %v", err)
	}
	decoded := decodeCLIEvents(t, output.String())
	if len(decoded) != 2 || decoded[1].Type != events.JobCanceled {
		t.Fatalf("events = %+v", decoded)
	}
}

func TestDownloadEventsFinishLibraryCommitAfterPostMediaCancellation(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/subjects/1/lectures/2" {
			http.NotFound(writer, request)
			return
		}
		if err := json.NewEncoder(writer).Encode(client.Lectures{{
			InstituteID: 4, SubjectID: 1, SessionID: 2, TTID: 7, Topic: "Completed media", SeqNo: 1,
		}}); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	var recordContextErr error
	manifest := artifact.Manifest{SchemaVersion: 1, ArtifactID: "impartus:v1:test", Files: []artifact.File{{Path: "/absolute/lecture.mp3"}}}
	deps := downloadExecutionDependencies{
		ensureFFmpeg: func() error { return nil },
		initClient: func(context.Context) (*config.Config, *client.Client, error) {
			cfg := &config.Config{BaseURL: server.URL, Token: "test-token", DownloadLocation: t.TempDir(), Views: "left", Quality: "720"}
			return cfg, client.New(server.Client(), nil), nil
		},
		downloadLectures: func(context.Context, *config.Config, *client.Client, client.Lectures, downloadPresentationOptions) (downloadResult, error) {
			cancel()
			return downloadResult{Status: "completed", OutputPaths: []string{"lecture.mp3"}, LectureCount: 1, Artifacts: []artifact.Manifest{manifest}}, nil
		},
		recordArtifacts: func(recordCtx context.Context, _ []artifact.Manifest) error {
			recordContextErr = recordCtx.Err()
			return recordContextErr
		},
	}
	var output bytes.Buffer
	err := runDownloadEventsWithDependenciesContext(
		ctx,
		[]string{"--events", "-s", "1", "-S", "2"},
		&output,
		deps,
		func() time.Time { return time.Unix(1, 0).UTC() },
		"job-commit-after-cancel",
	)
	if err != nil {
		t.Fatalf("download event flow error = %v", err)
	}
	if ctx.Err() != context.Canceled || recordContextErr != nil {
		t.Fatalf("contexts: request=%v library-record=%v", ctx.Err(), recordContextErr)
	}
	decoded := decodeCLIEvents(t, output.String())
	if got := decoded[len(decoded)-1].Type; got != events.JobCompleted {
		t.Fatalf("terminal event = %s, want job.completed", got)
	}
}

func TestDownloadEventsAttemptFailedTerminalAfterStreamWriteFailure(t *testing.T) {
	t.Parallel()

	result := downloadResult{
		Status: "completed", OutputPaths: []string{"lecture.mp3"}, LectureCount: 1,
		Artifacts:       []artifact.Manifest{{SchemaVersion: 1, ArtifactID: "impartus:v1:test"}},
		LibraryRecorded: true,
	}
	for _, test := range []struct {
		failAt       int
		wantTerminal bool
	}{{1, true}, {2, true}, {3, false}} {
		t.Run(fmt.Sprintf("write_%d", test.failAt), func(t *testing.T) {
			t.Parallel()
			output := &failWriteOnce{failAt: test.failAt}
			err := emitDownloadResultEvents(output, fmt.Sprintf("job-test-%d", test.failAt), result, nil, func() time.Time { return time.Unix(1, 0).UTC() })
			if err == nil || !strings.Contains(err.Error(), "event write") {
				t.Fatalf("emitDownloadResultEvents() error = %v", err)
			}
			decoded := decodeCLIEvents(t, output.String())
			if test.wantTerminal && (len(decoded) == 0 || decoded[len(decoded)-1].Type != events.JobFailed) {
				t.Fatalf("events = %+v, want final job.failed", decoded)
			}
			terminals := 0
			for _, event := range decoded {
				if events.IsTerminal(event.Type) {
					terminals++
				}
			}
			wantTerminals := 0
			if test.wantTerminal {
				wantTerminals = 1
			}
			if terminals != wantTerminals {
				t.Fatalf("terminal events = %d, want %d: %+v", terminals, wantTerminals, decoded)
			}
			if !test.wantTerminal && output.writes != test.failAt {
				t.Fatalf("terminal write attempts = %d, want %d", output.writes, test.failAt)
			}
		})
	}
}

func TestDownloadEventsAndJSONAreMutuallyExclusive(t *testing.T) {
	t.Parallel()

	if _, err := runDownloadJSONWithDependencies([]string{"--events", "-s", "1", "-S", "2"}, downloadExecutionDependencies{}); err == nil || !strings.Contains(err.Error(), "cannot combine --json and --events") {
		t.Fatalf("runDownloadJSONWithDependencies() error = %v", err)
	}
}

func TestRequestedEventsMatchesGoBooleanFlagFormsAndLastValueWins(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "bare", args: []string{"--events"}, want: true},
		{name: "single dash", args: []string{"-events"}, want: true},
		{name: "single dash numeric true", args: []string{"-events=1"}, want: true},
		{name: "numeric true", args: []string{"--events=1"}, want: true},
		{name: "short true", args: []string{"--events=t"}, want: true},
		{name: "capital short true", args: []string{"--events=T"}, want: true},
		{name: "title true", args: []string{"--events=True"}, want: true},
		{name: "uppercase true", args: []string{"--events=TRUE"}, want: true},
		{name: "explicit false", args: []string{"--events=false"}, want: false},
		{name: "last false wins", args: []string{"--events", "--events=false"}, want: false},
		{name: "last true wins", args: []string{"--events=false", "--events"}, want: true},
		{name: "output consumes events token", args: []string{"--output", "--events"}, want: false},
		{name: "short output consumes events token", args: []string{"-o", "--events"}, want: false},
		{name: "positional argument stops flags", args: []string{"bogus", "--events"}, want: false},
		{name: "event before positional argument remains", args: []string{"--events", "bogus", "--events=false"}, want: true},
		{name: "double dash stops flags", args: []string{"--", "--events"}, want: false},
		{name: "event before double dash remains", args: []string{"--events", "--", "--events=false"}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := requestedEvents(test.args); got != test.want {
				t.Fatalf("requestedEvents(%v) = %v, want %v", test.args, got, test.want)
			}
		})
	}
}

func TestDownloadEventsNumericTrueAndJSONAreMutuallyExclusive(t *testing.T) {
	t.Parallel()

	if _, err := runDownloadJSONWithDependencies([]string{"--events=1", "-s", "1", "-S", "2"}, downloadExecutionDependencies{}); err == nil || !strings.Contains(err.Error(), "cannot combine --json and --events") {
		t.Fatalf("runDownloadJSONWithDependencies() error = %v", err)
	}
}

func decodeCLIEvents(t *testing.T, output string) []events.Event {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	decoded := make([]events.Event, 0, len(lines))
	for _, line := range lines {
		var event events.Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("decode event %q: %v", line, err)
		}
		decoded = append(decoded, event)
	}
	return decoded
}
