package watch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rabesss/impartus-cli/internal/artifact"
	"github.com/rabesss/impartus-cli/internal/buildinfo"
	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/downloader"
	"github.com/rabesss/impartus-cli/internal/events"
	"github.com/rabesss/impartus-cli/internal/library"
)

func TestDurableStateErrorDoesNotPromoteContextCancellation(t *testing.T) {
	t.Parallel()

	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		got := durableStateError("read local state", cause)
		if !errors.Is(got, cause) || errors.Is(got, ErrDurableState) {
			t.Fatalf("durableStateError(%v) = %v", cause, got)
		}
	}
	if got := durableStateError("write local state", errors.New("disk failed")); !errors.Is(got, ErrDurableState) {
		t.Fatalf("durableStateError(ordinary) = %v, want ErrDurableState", got)
	}
}

func TestTerminalEventPreservesPartialCycleForFailureAndCancellation(t *testing.T) {
	t.Parallel()

	manifest := artifact.Manifest{SchemaVersion: 1, ArtifactID: "impartus:v1:partial"}
	cycle := CycleResult{Downloaded: 1, Outputs: []string{"/absolute/partial.mp3"}, Artifacts: []artifact.Manifest{manifest}}
	for _, test := range []struct {
		name      string
		cause     error
		wantEvent string
	}{
		{name: "failure", cause: errors.Join(ErrDurableState, errors.New("commit failed")), wantEvent: events.JobFailed},
		{name: "cancellation", cause: context.Canceled, wantEvent: events.JobCanceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			event := TerminalEvent("job-test", test.cause, cycle, time.Unix(1, 0).UTC())
			if event.Type != test.wantEvent || len(event.Outputs) != 1 || len(event.Artifacts) != 1 || event.Artifacts[0].ArtifactID != manifest.ArtifactID {
				t.Fatalf("TerminalEvent() = %+v", event)
			}
			if details, ok := event.Details.(CycleResult); !ok || details.Downloaded != 1 {
				t.Fatalf("TerminalEvent() details = %#v", event.Details)
			}
		})
	}
}

func TestWatcherCompletesPredownloadTransitionsBeforeHonoringCancellation(t *testing.T) {
	t.Parallel()

	baseStore := openWatchStore(t)
	store := &contextRecordingStore{Store: baseStore}
	cfg := watchTestConfig(t)
	target := cfg.Watch.Targets[0]
	lecture := watchLecture(target, 39, 1, "Cancel after playlist")
	source := &fakeSource{lectures: map[[2]int]client.Lectures{{target.SubjectID, target.SessionID}: {lecture}}, errors: map[[2]int]error{}}
	ctx, cancel := context.WithCancel(context.Background())
	producer := &fakeProducer{
		cfg: cfg, fetchErrors: map[int]error{}, downloadErrors: map[int]error{},
		afterFetch: cancel, blockDownload: true,
	}

	_, err := New(cfg, source, producer, store, Options{Once: true}).Run(ctx)
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrDurableState) {
		t.Fatalf("Run() error = %v", err)
	}
	if store.createContextError != nil || store.startContextError != nil {
		t.Fatalf("pre-download contexts: create=%v start=%v", store.createContextError, store.startContextError)
	}
}

func TestWatcherCommitsArtifactAndSkipsItOnRepeatedCycle(t *testing.T) {
	t.Parallel()

	store := openWatchStore(t)
	cfg := watchTestConfig(t)
	target := cfg.Watch.Targets[0]
	lecture := watchLecture(target, 40, 1, "Durable lecture")
	source := &fakeSource{lectures: map[[2]int]client.Lectures{{target.SubjectID, target.SessionID}: {lecture}}, errors: map[[2]int]error{}}
	producer := &fakeProducer{cfg: cfg, fetchErrors: map[int]error{}, downloadErrors: map[int]error{}}

	first, err := New(cfg, source, producer, store, Options{Once: true}).Run(context.Background())
	if err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	if first.Downloaded != 1 || len(first.Artifacts) != 1 || first.Skipped != 0 {
		t.Fatalf("first cycle = %+v", first)
	}
	if _, getErr := store.GetArtifact(context.Background(), first.Artifacts[0].ArtifactID); getErr != nil {
		t.Fatalf("committed artifact missing: %v", getErr)
	}
	producer.fetchErrors[lecture.TTID] = errors.New("remote playlist is no longer available")

	second, err := New(cfg, source, producer, store, Options{Once: true}).Run(context.Background())
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if second.Downloaded != 0 || second.Skipped != 1 {
		t.Fatalf("second cycle = %+v", second)
	}
	producer.mu.Lock()
	downloads := producer.downloads
	producer.mu.Unlock()
	if downloads != 1 {
		t.Fatalf("download calls = %d, want 1", downloads)
	}
}

func TestWatcherEmitsRequiredLectureLifecycleWithArtifactID(t *testing.T) {
	t.Parallel()

	store := openWatchStore(t)
	cfg := watchTestConfig(t)
	target := cfg.Watch.Targets[0]
	lecture := watchLecture(target, 401, 1, "Event lifecycle")
	source := &fakeSource{lectures: map[[2]int]client.Lectures{{target.SubjectID, target.SessionID}: {lecture}}, errors: map[[2]int]error{}}
	producer := &fakeProducer{cfg: cfg, fetchErrors: map[int]error{}, downloadErrors: map[int]error{}}
	emitter := &recordingEmitter{}

	if _, err := New(cfg, source, producer, store, Options{Once: true, Emitter: emitter}).Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := []string{events.LectureStarted, events.LectureProgress, events.LectureCompleted}
	next := 0
	for _, event := range emitter.events {
		if event.Lecture != nil && event.ArtifactID == "" {
			t.Fatalf("lecture event %q has no artifactId: %+v", event.Type, event)
		}
		if next < len(want) && event.Type == want[next] {
			if event.Type == events.LectureCompleted && (event.Artifact == nil || len(event.Outputs) == 0) {
				t.Fatalf("lecture.completed lacks artifact outputs: %+v", event)
			}
			next++
		}
	}
	if next != len(want) {
		t.Fatalf("event types = %v, want ordered lifecycle %v", emitter.types(), want)
	}
}

func TestWatcherCommitsArtifactBeforePublishingMediaProgress(t *testing.T) {
	t.Parallel()

	store := openWatchStore(t)
	cfg := watchTestConfig(t)
	target := cfg.Watch.Targets[0]
	lecture := watchLecture(target, 402, 2, "Durable before progress")
	source := &fakeSource{lectures: map[[2]int]client.Lectures{{target.SubjectID, target.SessionID}: {lecture}}, errors: map[[2]int]error{}}
	producer := &fakeProducer{cfg: cfg, fetchErrors: map[int]error{}, downloadErrors: map[int]error{}}
	committed := false
	progressBeforeCommit := false
	emitter := &recordingEmitter{before: func(event events.Event) {
		if event.Type == events.LectureProgress && !committed {
			progressBeforeCommit = true
		}
	}}

	wrappedStore := completeThenFailStore{Store: store, afterComplete: func() { committed = true }}
	if _, err := New(cfg, source, producer, wrappedStore, Options{Once: true, Emitter: emitter}).Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if progressBeforeCommit || !committed {
		t.Fatalf("progressBeforeCommit=%t committed=%t, want durable commit first", progressBeforeCommit, committed)
	}
}

func TestWatcherIdlesWhenSkipNoAudioFiltersEveryLecture(t *testing.T) {
	t.Parallel()

	store := openWatchStore(t)
	cfg := watchTestConfig(t)
	cfg.SkipNoAudio = true
	target := cfg.Watch.Targets[0]
	lecture := watchLecture(target, 403, 3, "Silent lecture")
	lecture.NoAudio = 1
	source := &fakeSource{lectures: map[[2]int]client.Lectures{{target.SubjectID, target.SessionID}: {lecture}}, errors: map[[2]int]error{}}
	producer := &fakeProducer{cfg: cfg, fetchErrors: map[int]error{}, downloadErrors: map[int]error{}}

	cycle, err := New(cfg, source, producer, store, Options{Once: true}).Run(context.Background())
	if err != nil || cycle.Failed != 0 || cycle.Listed != 0 || cycle.Downloaded != 0 {
		t.Fatalf("Run() cycle = %+v, error = %v; want a successful idle cycle", cycle, err)
	}
	producer.mu.Lock()
	downloads := producer.downloads
	producer.mu.Unlock()
	if downloads != 0 {
		t.Fatalf("download calls = %d, want 0", downloads)
	}
}

func TestWatcherResolvesMissingInstituteFromCourseCatalog(t *testing.T) {
	t.Parallel()

	store := openWatchStore(t)
	cfg := watchTestConfig(t)
	target := cfg.Watch.Targets[0]
	lecture := watchLecture(target, 41, 2, "Catalog scoped")
	lecture.InstituteID = 0
	source := &fakeSource{
		lectures: map[[2]int]client.Lectures{{target.SubjectID, target.SessionID}: {lecture}},
		errors:   map[[2]int]error{},
		courses:  client.Courses{{InstituteID: 9, SubjectID: target.SubjectID, SessionID: target.SessionID}},
	}
	producer := &fakeProducer{cfg: cfg, fetchErrors: map[int]error{}, downloadErrors: map[int]error{}}

	cycle, err := New(cfg, source, producer, store, Options{Once: true}).Run(context.Background())
	if err != nil || cycle.Downloaded != 1 || len(cycle.Artifacts) != 1 {
		t.Fatalf("Run() cycle = %+v, error = %v", cycle, err)
	}
	if got := cycle.Artifacts[0].Lecture.InstituteID; got != 9 {
		t.Fatalf("manifest instituteId = %d, want 9", got)
	}
	if source.courseCalls != 1 {
		t.Fatalf("course catalog calls = %d, want 1", source.courseCalls)
	}
	producer.fetchErrors[lecture.TTID] = errors.New("remote playlist is no longer available")
	second, err := New(cfg, source, producer, store, Options{Once: true}).Run(context.Background())
	if err != nil || second.Skipped != 1 || second.Downloaded != 0 {
		t.Fatalf("second Run() cycle = %+v, error = %v", second, err)
	}
	producer.mu.Lock()
	downloads := producer.downloads
	producer.mu.Unlock()
	if downloads != 1 {
		t.Fatalf("download calls = %d, want 1", downloads)
	}
}

func TestWatcherDoesNotListLecturesWhoseScopeCannotBeResolved(t *testing.T) {
	t.Parallel()

	store := openWatchStore(t)
	cfg := watchTestConfig(t)
	target := cfg.Watch.Targets[0]
	lecture := watchLecture(target, 42, 3, "Unscoped")
	lecture.InstituteID = 0
	source := &fakeSource{
		lectures: map[[2]int]client.Lectures{{target.SubjectID, target.SessionID}: {lecture}},
		errors:   map[[2]int]error{},
	}
	producer := &fakeProducer{cfg: cfg, fetchErrors: map[int]error{}, downloadErrors: map[int]error{}}

	cycle, err := New(cfg, source, producer, store, Options{Once: true}).Run(context.Background())
	if err == nil || cycle.Listed != 0 || cycle.Failed != 1 {
		t.Fatalf("Run() cycle = %+v, error = %v", cycle, err)
	}
}

func TestWatcherScopesOutputNamesAcrossTargets(t *testing.T) {
	t.Parallel()

	store := openWatchStore(t)
	cfg := watchTestConfig(t)
	first := config.WatchTarget{SubjectID: 10, SessionID: 11, Label: "first"}
	second := config.WatchTarget{SubjectID: 20, SessionID: 21, Label: "second"}
	cfg.Watch.Targets = []config.WatchTarget{first, second}
	source := &fakeSource{
		lectures: map[[2]int]client.Lectures{
			{first.SubjectID, first.SessionID}:   {watchLecture(first, 101, 1, "Same title")},
			{second.SubjectID, second.SessionID}: {watchLecture(second, 102, 1, "Same title")},
		},
		errors: map[[2]int]error{},
	}
	producer := &fakeProducer{cfg: cfg, fetchErrors: map[int]error{}, downloadErrors: map[int]error{}}

	cycle, err := New(cfg, source, producer, store, Options{Once: true}).Run(context.Background())
	if err != nil || cycle.Downloaded != 2 || len(cycle.Outputs) != 2 {
		t.Fatalf("Run() cycle = %+v, error = %v", cycle, err)
	}
	if cycle.Outputs[0] == cycle.Outputs[1] {
		t.Fatalf("watch outputs collided: %q", cycle.Outputs)
	}
	for _, path := range cycle.Outputs {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("scoped output %q missing: %v", path, statErr)
		}
	}
}

func TestWatcherContinuesAfterTargetFailureAndEmitsOneFailedTerminal(t *testing.T) {
	t.Parallel()

	store := openWatchStore(t)
	cfg := watchTestConfig(t)
	failed := config.WatchTarget{SubjectID: 1, SessionID: 2, Label: "failed"}
	working := config.WatchTarget{SubjectID: 3, SessionID: 4, Label: "working"}
	cfg.Watch.Targets = []config.WatchTarget{failed, working}
	source := &fakeSource{
		lectures: map[[2]int]client.Lectures{{working.SubjectID, working.SessionID}: {watchLecture(working, 50, 1, "Working")}},
		errors:   map[[2]int]error{{failed.SubjectID, failed.SessionID}: errors.New("rate limited")},
	}
	producer := &fakeProducer{cfg: cfg, fetchErrors: map[int]error{}, downloadErrors: map[int]error{}}
	var output bytes.Buffer
	emitter := events.NewWriter(&output)

	cycle, err := New(cfg, source, producer, store, Options{Once: true, Emitter: emitter}).Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("Run() error = %v", err)
	}
	if cycle.Downloaded != 1 || cycle.Failed != 1 {
		t.Fatalf("cycle = %+v", cycle)
	}
	decoded := decodeEvents(t, output.String())
	terminals := 0
	for _, event := range decoded {
		if events.IsTerminal(event.Type) {
			terminals++
			if event.Type != events.JobFailed {
				t.Fatalf("terminal event = %s, want job.failed", event.Type)
			}
		}
	}
	if terminals != 1 || !emitter.TerminalEmitted() {
		t.Fatalf("terminal events = %d, stream = %s", terminals, output.String())
	}
	if got := source.calls; len(got) != 2 || got[1] != [2]int{working.SubjectID, working.SessionID} {
		t.Fatalf("target calls = %v", got)
	}
}

func TestWatcherDryRunPerformsNoDownloadOrJobMutation(t *testing.T) {
	t.Parallel()

	store := openWatchStore(t)
	cfg := watchTestConfig(t)
	target := cfg.Watch.Targets[0]
	source := &fakeSource{lectures: map[[2]int]client.Lectures{{target.SubjectID, target.SessionID}: {watchLecture(target, 60, 1, "Dry")}}, errors: map[[2]int]error{}}
	producer := &fakeProducer{cfg: cfg, fetchErrors: map[int]error{}, downloadErrors: map[int]error{}}

	cycle, err := New(cfg, source, producer, store, Options{Once: true, DryRun: true}).Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if cycle.New != 1 || cycle.Downloaded != 0 || !cycle.DryRun {
		t.Fatalf("cycle = %+v", cycle)
	}
	jobs, err := store.ListJobs(context.Background())
	if err != nil || len(jobs) != 0 {
		t.Fatalf("jobs = %+v, err = %v", jobs, err)
	}
}

func TestWatcherDryRunDoesNotRecoverOrCommitInterruptedJob(t *testing.T) {
	t.Parallel()

	store := openWatchStore(t)
	cfg := watchTestConfig(t)
	target := cfg.Watch.Targets[0]
	lecture := watchLecture(target, 61, 2, "Dry recovery")
	output := filepath.Join(cfg.DownloadLocation, "already-published.mp3")
	expected := library.ExpectedArtifact{
		Lecture:    artifactLecture(lecture),
		Selection:  artifact.Selection{Views: cfg.Views, Quality: cfg.Quality, AudioOnly: cfg.AudioOnly, AudioFormat: cfg.AudioFormat},
		Files:      []library.ExpectedFile{{Path: output, Role: "audio", View: "left", Container: "mp3"}},
		ProducedAt: time.Now().UTC(), Producer: artifact.Producer{Name: "impartus", Version: buildinfo.Version},
	}
	jobID := uuid.NewString()
	if err := store.CreateJob(context.Background(), library.JobSpec{ID: jobID, Kind: "watch", Expected: expected}); err != nil {
		t.Fatal(err)
	}
	if err := store.StartJob(context.Background(), jobID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, []byte("published before dry-run"), 0o600); err != nil {
		t.Fatal(err)
	}
	artifactID, err := artifact.NewID(expectedIdentity(expected))
	if err != nil {
		t.Fatal(err)
	}
	source := &fakeSource{lectures: map[[2]int]client.Lectures{}, errors: map[[2]int]error{}}
	producer := &fakeProducer{cfg: cfg, fetchErrors: map[int]error{}, downloadErrors: map[int]error{}}

	if _, runErr := New(cfg, source, producer, store, Options{Once: true, DryRun: true}).Run(context.Background()); runErr != nil {
		t.Fatalf("Run() error = %v", runErr)
	}
	job, err := store.Job(context.Background(), jobID)
	if err != nil || job.Status != library.JobRunning {
		t.Fatalf("dry-run job = %+v, err = %v", job, err)
	}
	if _, err := store.GetArtifact(context.Background(), artifactID); !errors.Is(err, library.ErrArtifactNotFound) {
		t.Fatalf("dry-run artifact lookup error = %v, want ErrArtifactNotFound", err)
	}
}

func TestWatcherAttemptsFailedTerminalAfterStartedEventDeliveryFails(t *testing.T) {
	t.Parallel()

	store := openWatchStore(t)
	cfg := watchTestConfig(t)
	source := &fakeSource{lectures: map[[2]int]client.Lectures{}, errors: map[[2]int]error{}}
	producer := &fakeProducer{cfg: cfg, fetchErrors: map[int]error{}, downloadErrors: map[int]error{}}
	emitter := &recordingEmitter{failType: events.JobStarted, failures: 1}

	_, err := New(cfg, source, producer, store, Options{Once: true, Emitter: emitter}).Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "event sink failed") {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := emitter.types(), []string{events.JobStarted, events.JobFailed}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
}

func TestWatcherPreservesCommittedOutcomeWhenPostMediaEventDeliveryFails(t *testing.T) {
	t.Parallel()

	for _, failType := range []string{events.LectureProgress, events.LectureCompleted} {
		t.Run(failType, func(t *testing.T) {
			t.Parallel()
			store := openWatchStore(t)
			cfg := watchTestConfig(t)
			target := cfg.Watch.Targets[0]
			lecture := watchLecture(target, 62, 3, "Committed despite event failure")
			source := &fakeSource{lectures: map[[2]int]client.Lectures{{target.SubjectID, target.SessionID}: {lecture}}, errors: map[[2]int]error{}}
			producer := &fakeProducer{cfg: cfg, fetchErrors: map[int]error{}, downloadErrors: map[int]error{}}
			emitter := &recordingEmitter{failType: failType, failures: 1}

			cycle, err := New(cfg, source, producer, store, Options{Once: true, Emitter: emitter}).Run(context.Background())
			if err == nil || !strings.Contains(err.Error(), "event sink failed") {
				t.Fatalf("Run() error = %v", err)
			}
			if cycle.Downloaded != 1 || len(cycle.Artifacts) != 1 {
				t.Fatalf("cycle = %+v", cycle)
			}
			if _, getErr := store.GetArtifact(context.Background(), cycle.Artifacts[0].ArtifactID); getErr != nil {
				t.Fatalf("committed artifact missing: %v", getErr)
			}
		})
	}
}

func TestWatcherStopsProducingAfterCompletedEventDeliveryFails(t *testing.T) {
	t.Parallel()

	store := openWatchStore(t)
	cfg := watchTestConfig(t)
	target := cfg.Watch.Targets[0]
	source := &fakeSource{
		lectures: map[[2]int]client.Lectures{{target.SubjectID, target.SessionID}: {
			watchLecture(target, 64, 4, "First"), watchLecture(target, 65, 5, "Second"),
		}},
		errors: map[[2]int]error{},
	}
	producer := &fakeProducer{cfg: cfg, fetchErrors: map[int]error{}, downloadErrors: map[int]error{}}
	emitter := &recordingEmitter{failType: events.LectureCompleted, failures: 1}

	cycle, err := New(cfg, source, producer, store, Options{Once: true, Emitter: emitter}).Run(context.Background())
	if !errors.Is(err, ErrEventDelivery) {
		t.Fatalf("Run() error = %v, want ErrEventDelivery", err)
	}
	producer.mu.Lock()
	downloads := producer.downloads
	producer.mu.Unlock()
	if downloads != 1 || cycle.Downloaded != 1 {
		t.Fatalf("downloads = %d, cycle = %+v", downloads, cycle)
	}
}

func TestWatcherStopsAfterAmbiguousDurableCommitAcknowledgment(t *testing.T) {
	t.Parallel()

	store := openWatchStore(t)
	cfg := watchTestConfig(t)
	target := cfg.Watch.Targets[0]
	lecture := watchLecture(target, 66, 6, "Committed before lost acknowledgment")
	source := &fakeSource{lectures: map[[2]int]client.Lectures{{target.SubjectID, target.SessionID}: {lecture}}, errors: map[[2]int]error{}}
	producer := &fakeProducer{cfg: cfg, fetchErrors: map[int]error{}, downloadErrors: map[int]error{}}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	cycle, err := New(cfg, source, producer, completeThenFailStore{Store: store, err: errors.New("commit acknowledgment lost")}, Options{Interval: time.Hour}).Run(ctx)
	if !errors.Is(err, ErrDurableState) || !strings.Contains(err.Error(), "commit acknowledgment lost") {
		t.Fatalf("Run() error = %v, want ErrDurableState", err)
	}
	producer.mu.Lock()
	downloads := producer.downloads
	producer.mu.Unlock()
	if downloads != 1 {
		t.Fatalf("download calls = %d, want 1", downloads)
	}
	jobs, listErr := store.ListJobs(context.Background())
	if listErr != nil || len(jobs) != 1 || jobs[0].Status != library.JobCompleted {
		t.Fatalf("jobs = %+v, error = %v", jobs, listErr)
	}
	if cycle.Downloaded != 0 {
		t.Fatalf("cycle = %+v; acknowledgment was ambiguous, so success must not be reported", cycle)
	}
}

func TestWatcherDurableFailureOverridesSimultaneousCancellation(t *testing.T) {
	t.Parallel()

	store := openWatchStore(t)
	cfg := watchTestConfig(t)
	target := cfg.Watch.Targets[0]
	lecture := watchLecture(target, 67, 7, "Ambiguous commit during cancellation")
	source := &fakeSource{lectures: map[[2]int]client.Lectures{{target.SubjectID, target.SessionID}: {lecture}}, errors: map[[2]int]error{}}
	producer := &fakeProducer{cfg: cfg, fetchErrors: map[int]error{}, downloadErrors: map[int]error{}}
	ctx, cancel := context.WithCancel(context.Background())
	emitter := &recordingEmitter{}

	_, err := New(cfg, source, producer, completeThenFailStore{
		Store: store, err: errors.New("commit acknowledgment lost"), afterComplete: cancel,
	}, Options{Once: true, Emitter: emitter}).Run(ctx)
	if !errors.Is(err, ErrDurableState) || !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want durable state plus cancellation", err)
	}
	types := emitter.types()
	if len(types) == 0 || types[len(types)-1] != events.JobFailed {
		t.Fatalf("event types = %v, want final job.failed", types)
	}
	for _, eventType := range types {
		if eventType == events.JobCanceled {
			t.Fatalf("event types = %v, durable failure was masked as cancellation", types)
		}
	}
}

func TestWatcherRedactsLectureFailureDetails(t *testing.T) {
	t.Parallel()

	store := openWatchStore(t)
	cfg := watchTestConfig(t)
	target := cfg.Watch.Targets[0]
	lecture := watchLecture(target, 68, 8, "Redacted failure")
	source := &fakeSource{lectures: map[[2]int]client.Lectures{{target.SubjectID, target.SessionID}: {lecture}}, errors: map[[2]int]error{}}
	producer := &fakeProducer{
		cfg: cfg, downloadErrors: map[int]error{},
		fetchErrors: map[int]error{lecture.TTID: errors.New(`Authorization: Digest username="alice", realm="lecture", response="digest-secret"; token=secret-value`)},
	}
	var output bytes.Buffer

	_, err := New(cfg, source, producer, store, Options{Once: true, MaxRetries: 1, Emitter: events.NewWriter(&output)}).Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil")
	}
	if strings.Contains(err.Error(), "digest-secret") || strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("Run() returned a secret-bearing error: %v", err)
	}
	for _, event := range decodeEvents(t, output.String()) {
		if event.Type != events.LectureFailed {
			continue
		}
		encoded := fmt.Sprint(event.Details)
		if strings.Contains(encoded, "digest-secret") || strings.Contains(encoded, "secret-value") {
			t.Fatalf("lecture.failed leaked secret: %s", encoded)
		}
		if !strings.Contains(encoded, "REDACTED") {
			t.Fatalf("lecture.failed did not mark redaction: %s", encoded)
		}
		return
	}
	t.Fatalf("event stream has no lecture.failed: %s", output.String())
}

func TestWatcherCancellationCancelsDurableJobAndTerminalStream(t *testing.T) {
	store := openWatchStore(t)
	cfg := watchTestConfig(t)
	target := cfg.Watch.Targets[0]
	source := &fakeSource{lectures: map[[2]int]client.Lectures{{target.SubjectID, target.SessionID}: {watchLecture(target, 70, 1, "Canceled")}}, errors: map[[2]int]error{}}
	started := make(chan struct{})
	producer := &fakeProducer{cfg: cfg, fetchErrors: map[int]error{}, downloadErrors: map[int]error{}, downloadStarted: started, blockDownload: true}
	var output bytes.Buffer
	emitter := events.NewWriter(&output)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := New(cfg, source, producer, store, Options{Once: true, Emitter: emitter}).Run(ctx)
		result <- err
	}()
	<-started
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	jobs, err := store.ListJobs(context.Background())
	if err != nil || len(jobs) != 1 || jobs[0].Status != library.JobCanceled {
		t.Fatalf("jobs = %+v, err = %v", jobs, err)
	}
	decoded := decodeEvents(t, output.String())
	if got := decoded[len(decoded)-1].Type; got != events.JobCanceled {
		t.Fatalf("terminal event = %s, want job.canceled", got)
	}
}

func TestWatcherRecoversCompletedOutputBeforeFirstNetworkCall(t *testing.T) {
	t.Parallel()

	store := openWatchStore(t)
	cfg := watchTestConfig(t)
	target := cfg.Watch.Targets[0]
	lecture := watchLecture(target, 80, 1, "Recovered")
	output := filepath.Join(cfg.DownloadLocation, "recovered.mp3")
	expected := library.ExpectedArtifact{
		Lecture:    artifactLecture(lecture),
		Selection:  artifact.Selection{Views: cfg.Views, Quality: cfg.Quality, AudioOnly: cfg.AudioOnly, AudioFormat: cfg.AudioFormat},
		Files:      []library.ExpectedFile{{Path: output, Role: "audio", View: "left", Container: "mp3"}},
		ProducedAt: time.Now().UTC(), Producer: artifact.Producer{Name: "impartus", Version: buildinfo.Version},
	}
	jobID := uuid.NewString()
	if err := store.CreateJob(context.Background(), library.JobSpec{ID: jobID, Kind: "watch", Expected: expected}); err != nil {
		t.Fatal(err)
	}
	if err := store.StartJob(context.Background(), jobID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, []byte("ID3published before crash"), 0o600); err != nil {
		t.Fatal(err)
	}
	recovery, err := store.RecoverInterruptedJobs(context.Background())
	if err != nil {
		t.Fatalf("RecoverInterruptedJobs() error = %v", err)
	}
	artifactID, err := artifact.NewID(expectedIdentity(expected))
	if err != nil {
		t.Fatal(err)
	}
	source := &fakeSource{
		lectures: map[[2]int]client.Lectures{{target.SubjectID, target.SessionID}: {lecture}}, errors: map[[2]int]error{},
		before: func([2]int) {
			if _, getErr := store.GetArtifact(context.Background(), artifactID); getErr != nil {
				t.Errorf("artifact was not recovered before network call: %v", getErr)
			}
		},
	}
	producer := &fakeProducer{cfg: cfg, fetchErrors: map[int]error{}, downloadErrors: map[int]error{}}
	var eventOutput bytes.Buffer

	if _, runErr := New(cfg, source, producer, store, Options{
		Once: true, Emitter: events.NewWriter(&eventOutput), StartupRecovery: &recovery,
	}).Run(context.Background()); runErr != nil {
		t.Fatalf("Run() error = %v", runErr)
	}
	decoded := decodeEvents(t, eventOutput.String())
	if len(decoded) < 2 || decoded[0].Type != events.JobStarted || decoded[1].Type != events.LectureCompleted {
		t.Fatalf("recovery event order = %+v, want job.started then lecture.completed", decoded)
	}
	if decoded[1].Artifact == nil || decoded[1].Artifact.ArtifactID != artifactID || len(decoded[1].Artifact.Files) != 1 || decoded[1].Artifact.Files[0].Path != output {
		t.Fatalf("recovered artifact event = %+v", decoded[1])
	}
	details, ok := decoded[1].Details.(map[string]any)
	if !ok || details["libraryJobId"] != jobID || details["recovered"] != true {
		t.Fatalf("recovered artifact details = %#v", decoded[1].Details)
	}
	job, err := store.Job(context.Background(), jobID)
	if err != nil || job.Status != library.JobCompleted {
		t.Fatalf("recovered job = %+v, err = %v", job, err)
	}
}

func TestWatcherRetriesRecoverableJobWithoutCreatingDuplicate(t *testing.T) {
	t.Parallel()

	store := openWatchStore(t)
	cfg := watchTestConfig(t)
	target := cfg.Watch.Targets[0]
	lecture := watchLecture(target, 81, 2, "Interrupted")
	playlist := client.ParsedPlaylist{
		ID: lecture.TTID, InstituteID: lecture.InstituteID, SubjectID: lecture.SubjectID,
		SessionID: lecture.SessionID, SeqNo: lecture.SeqNo, Title: watchScopedTitle(lecture, lecture.Topic),
		FirstViewURLs: []string{"left"},
	}
	plan, err := downloader.PlanJoinResult(cfg, playlist)
	if err != nil {
		t.Fatal(err)
	}
	expected := expectedArtifact(lecture, cfg, plan, time.Now().UTC())
	jobID := uuid.NewString()
	if createErr := store.CreateJob(context.Background(), library.JobSpec{ID: jobID, Kind: "watch", Expected: expected}); createErr != nil {
		t.Fatal(createErr)
	}
	if startErr := store.StartJob(context.Background(), jobID); startErr != nil {
		t.Fatal(startErr)
	}
	source := &fakeSource{lectures: map[[2]int]client.Lectures{{target.SubjectID, target.SessionID}: {lecture}}, errors: map[[2]int]error{}}
	producer := &fakeProducer{cfg: cfg, fetchErrors: map[int]error{}, downloadErrors: map[int]error{}}

	cycle, err := New(cfg, source, producer, store, Options{Once: true}).Run(context.Background())
	if err != nil || cycle.Downloaded != 1 {
		t.Fatalf("Run() cycle = %+v, error = %v", cycle, err)
	}
	jobs, err := store.ListJobs(context.Background())
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs = %+v, error = %v", jobs, err)
	}
	if jobs[0].ID != jobID || jobs[0].Status != library.JobCompleted || jobs[0].Attempts != 2 {
		t.Fatalf("retried job = %+v", jobs[0])
	}
}

func TestWatcherReconcilesEveryRetryableSiblingForOneArtifact(t *testing.T) {
	t.Parallel()

	store := openWatchStore(t)
	cfg := watchTestConfig(t)
	target := cfg.Watch.Targets[0]
	lecture := watchLecture(target, 811, 21, "Duplicate interrupted jobs")
	playlist := client.ParsedPlaylist{
		ID: lecture.TTID, InstituteID: lecture.InstituteID, SubjectID: lecture.SubjectID,
		SessionID: lecture.SessionID, SeqNo: lecture.SeqNo, Title: watchScopedTitle(lecture, lecture.Topic),
		FirstViewURLs: []string{"left"},
	}
	plan, err := downloader.PlanJoinResult(cfg, playlist)
	if err != nil {
		t.Fatal(err)
	}
	expected := expectedArtifact(lecture, cfg, plan, time.Now().UTC())
	for range 2 {
		if createErr := store.CreateJob(context.Background(), library.JobSpec{ID: uuid.NewString(), Kind: "watch", Expected: expected}); createErr != nil {
			t.Fatal(createErr)
		}
	}
	source := &fakeSource{lectures: map[[2]int]client.Lectures{{target.SubjectID, target.SessionID}: {lecture}}, errors: map[[2]int]error{}}
	producer := &fakeProducer{cfg: cfg, fetchErrors: map[int]error{}, downloadErrors: map[int]error{}}

	cycle, err := New(cfg, source, producer, store, Options{Once: true}).Run(context.Background())
	if err != nil || cycle.Downloaded != 1 {
		t.Fatalf("Run() cycle = %+v, error = %v", cycle, err)
	}
	jobs, err := store.ListJobs(context.Background())
	if err != nil || len(jobs) != 2 {
		t.Fatalf("jobs = %+v, error = %v", jobs, err)
	}
	statuses := map[library.JobStatus]int{}
	for _, job := range jobs {
		statuses[job.Status]++
	}
	if statuses[library.JobCompleted] != 1 || statuses[library.JobFailed] != 1 {
		t.Fatalf("job statuses = %#v, want one completed and one failed", statuses)
	}
}

func TestWatcherRetriesTransientDownloadWithBackoff(t *testing.T) {
	t.Parallel()

	store := openWatchStore(t)
	cfg := watchTestConfig(t)
	target := cfg.Watch.Targets[0]
	lecture := watchLecture(target, 82, 3, "Transient")
	source := &fakeSource{lectures: map[[2]int]client.Lectures{{target.SubjectID, target.SessionID}: {lecture}}, errors: map[[2]int]error{}}
	producer := &fakeProducer{
		cfg: cfg, fetchErrors: map[int]error{}, downloadErrors: map[int]error{lecture.TTID: errors.New("temporary rate limit")},
		failuresLeft: map[int]int{lecture.TTID: 2},
	}
	backoffs := make([]int, 0, 2)
	cycle, err := New(cfg, source, producer, store, Options{
		Once: true, MaxRetries: 3,
		RetryBackoff: func(attempt int) time.Duration { backoffs = append(backoffs, attempt); return 0 },
	}).Run(context.Background())
	if err != nil || cycle.Downloaded != 1 {
		t.Fatalf("Run() cycle = %+v, error = %v", cycle, err)
	}
	if !reflect.DeepEqual(backoffs, []int{1, 2}) {
		t.Fatalf("backoff attempts = %v", backoffs)
	}
	producer.mu.Lock()
	downloads := producer.downloads
	producer.mu.Unlock()
	if downloads != 3 {
		t.Fatalf("download calls = %d, want 3", downloads)
	}
}

func TestWatcherForceRedownloadsCommittedArtifact(t *testing.T) {
	t.Parallel()

	store := openWatchStore(t)
	cfg := watchTestConfig(t)
	target := cfg.Watch.Targets[0]
	lecture := watchLecture(target, 83, 4, "Forced")
	source := &fakeSource{lectures: map[[2]int]client.Lectures{{target.SubjectID, target.SessionID}: {lecture}}, errors: map[[2]int]error{}}
	producer := &fakeProducer{cfg: cfg, fetchErrors: map[int]error{}, downloadErrors: map[int]error{}}
	if _, err := New(cfg, source, producer, store, Options{Once: true}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := New(cfg, source, producer, store, Options{Once: true, Force: true}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	producer.mu.Lock()
	downloads := producer.downloads
	producer.mu.Unlock()
	if downloads != 2 {
		t.Fatalf("download calls = %d, want 2", downloads)
	}
}

func TestWatcherIgnoresMissingHistoricalPathWhenLatestManifestIsPresent(t *testing.T) {
	t.Parallel()

	store := openWatchStore(t)
	cfg := watchTestConfig(t)
	target := cfg.Watch.Targets[0]
	lecture := watchLecture(target, 84, 5, "Moved output")
	source := &fakeSource{lectures: map[[2]int]client.Lectures{{target.SubjectID, target.SessionID}: {lecture}}, errors: map[[2]int]error{}}
	producer := &fakeProducer{cfg: cfg, fetchErrors: map[int]error{}, downloadErrors: map[int]error{}}

	first, err := New(cfg, source, producer, store, Options{Once: true}).Run(context.Background())
	if err != nil || len(first.Outputs) != 1 {
		t.Fatalf("first Run() = %+v, error = %v", first, err)
	}
	oldPath := first.Outputs[0]
	cfg.DownloadLocation = filepath.Join(t.TempDir(), "new-output")
	second, err := New(cfg, source, producer, store, Options{Once: true, Force: true}).Run(context.Background())
	if err != nil || len(second.Outputs) != 1 || second.Outputs[0] == oldPath {
		t.Fatalf("second Run() = %+v, error = %v", second, err)
	}
	if removeErr := os.Remove(oldPath); removeErr != nil {
		t.Fatal(removeErr)
	}
	producer.fetchErrors[lecture.TTID] = errors.New("remote playlist should not be fetched")

	third, err := New(cfg, source, producer, store, Options{Once: true}).Run(context.Background())
	if err != nil || third.Skipped != 1 || third.Downloaded != 0 {
		t.Fatalf("third Run() = %+v, error = %v", third, err)
	}
	producer.mu.Lock()
	downloads := producer.downloads
	producer.mu.Unlock()
	if downloads != 2 {
		t.Fatalf("download calls = %d, want 2", downloads)
	}
}

func TestWatcherAppliesOneGlobalBudgetAcrossTargets(t *testing.T) {
	t.Parallel()

	store := openWatchStore(t)
	cfg := watchTestConfig(t)
	first := config.WatchTarget{SubjectID: 10, SessionID: 11, Label: "first"}
	second := config.WatchTarget{SubjectID: 20, SessionID: 21, Label: "second"}
	cfg.Watch.Targets = []config.WatchTarget{first, second}
	firstLecture := watchLecture(first, 91, 1, "First")
	secondLecture := watchLecture(second, 92, 1, "Second")
	source := &fakeSource{
		lectures: map[[2]int]client.Lectures{
			{first.SubjectID, first.SessionID}:   {firstLecture},
			{second.SubjectID, second.SessionID}: {secondLecture},
		},
		errors: map[[2]int]error{},
	}
	producer := &fakeProducer{
		cfg: cfg,
		fetchErrors: map[int]error{
			secondLecture.TTID: errors.New("over-budget playlist must not be fetched"),
		},
		downloadErrors: map[int]error{},
	}
	var output bytes.Buffer

	cycle, err := New(cfg, source, producer, store, Options{
		Once: true, MaxLecturesPerCycle: 1, Emitter: events.NewWriter(&output),
	}).Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if cycle.New != 2 || cycle.Downloaded != 1 || cycle.Skipped != 1 {
		t.Fatalf("cycle = %+v", cycle)
	}
	producer.mu.Lock()
	downloads := producer.downloads
	producer.mu.Unlock()
	if downloads != 1 {
		t.Fatalf("download calls = %d, want 1", downloads)
	}
	for _, event := range decodeEvents(t, output.String()) {
		if event.Type != events.JobStarted && event.Type != events.LectureStarted &&
			event.Type != events.LectureProgress && event.Type != events.LectureCompleted && event.Type != events.JobCompleted {
			t.Fatalf("event stream contains out-of-contract event %q: %s", event.Type, output.String())
		}
	}
}

func TestWatcherRedownloadsCommittedArtifactWhenFinalFileIsMissing(t *testing.T) {
	t.Parallel()

	store := openWatchStore(t)
	cfg := watchTestConfig(t)
	target := cfg.Watch.Targets[0]
	lecture := watchLecture(target, 93, 1, "Missing final")
	source := &fakeSource{lectures: map[[2]int]client.Lectures{{target.SubjectID, target.SessionID}: {lecture}}, errors: map[[2]int]error{}}
	producer := &fakeProducer{cfg: cfg, fetchErrors: map[int]error{}, downloadErrors: map[int]error{}}

	first, err := New(cfg, source, producer, store, Options{Once: true}).Run(context.Background())
	if err != nil || len(first.Artifacts) != 1 || len(first.Artifacts[0].Files) != 1 {
		t.Fatalf("first Run() = %+v, error = %v", first, err)
	}
	if removeErr := os.Remove(first.Artifacts[0].Files[0].Path); removeErr != nil {
		t.Fatal(removeErr)
	}
	second, err := New(cfg, source, producer, store, Options{Once: true}).Run(context.Background())
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if second.Downloaded != 1 || second.Skipped != 0 {
		t.Fatalf("second cycle = %+v", second)
	}
	producer.mu.Lock()
	downloads := producer.downloads
	producer.mu.Unlock()
	if downloads != 2 {
		t.Fatalf("download calls = %d, want 2", downloads)
	}
}

func openWatchStore(t *testing.T) *library.Store {
	t.Helper()
	state := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(state, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := library.Open(context.Background(), library.Options{Path: filepath.Join(state, "library.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return store
}

func watchTestConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := &config.Config{
		Username: "user", Password: "pass", BaseURL: "https://example.test",
		DownloadLocation: t.TempDir(), TempDirLocation: t.TempDir(),
		Watch: config.WatchConfig{Enabled: true, Targets: []config.WatchTarget{{SubjectID: 67, SessionID: 8, Label: "Target"}}},
	}
	cfg.ApplyDefaults()
	cfg.ApplyWatchMediaDefaults()
	return cfg
}

func watchLecture(target config.WatchTarget, ttid, seq int, topic string) client.Lecture {
	return client.Lecture{
		TTID: ttid, InstituteID: 4, SubjectID: target.SubjectID, SessionID: target.SessionID,
		SeqNo: seq, Topic: topic, StartTime: "2026-08-09T10:00:00Z", ActualDuration: 60,
		ProfessorName: "Professor", Institute: "Institute",
	}
}

func artifactLecture(lecture client.Lecture) artifact.Lecture {
	return artifact.Lecture{
		TTID: lecture.TTID, InstituteID: lecture.InstituteID, SubjectID: lecture.SubjectID,
		SessionID: lecture.SessionID, SeqNo: lecture.SeqNo, Topic: lecture.Topic,
		StartTime: lecture.StartTime, DurationSeconds: lecture.ActualDuration,
		Professor: lecture.ProfessorName, Institute: lecture.Institute, NoAudio: lecture.NoAudio == 1,
	}
}

func decodeEvents(t *testing.T, output string) []events.Event {
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
