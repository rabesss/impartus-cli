package tuisession_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/app"
	artifactpkg "github.com/rabesss/impartus-cli/internal/artifact"
	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/library"
	"github.com/rabesss/impartus-cli/internal/player"
	"github.com/rabesss/impartus-cli/internal/tuiproto"
	"github.com/rabesss/impartus-cli/internal/tuisession"
)

type catalogStub struct {
	courses client.Courses
	err     error
}

type authenticationStub struct {
	mu      sync.Mutex
	status  tuiproto.AuthStatus
	retries int
	retry   func(context.Context) error
}

func (stub *authenticationStub) Status() tuiproto.AuthStatus {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	return stub.status
}

func (stub *authenticationStub) Retry(ctx context.Context) error {
	stub.mu.Lock()
	stub.retries++
	retry := stub.retry
	stub.mu.Unlock()
	if retry != nil {
		return retry(ctx)
	}
	stub.mu.Lock()
	stub.status = tuiproto.AuthStatusReady
	stub.mu.Unlock()
	return nil
}

func (stub *authenticationStub) Retries() int {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	return stub.retries
}

type productProjectionStub struct {
	catalogStub
	lectures  client.Lectures
	artifacts []library.ArtifactRecord
	course    client.Course
}

type actionStub struct {
	download func(context.Context, client.Lecture) (app.DownloadResult, error)
	record   func(context.Context, library.PlaybackState) error
	resume   func(context.Context, client.Lecture) (library.PlaybackState, bool, error)
	start    func(context.Context, client.Lecture, float64) (app.PlaybackStart, error)
}

type playbackStub struct {
	events    chan player.Event
	ended     chan error
	controls  chan string
	closeOnce sync.Once
}

func newPlaybackStub() *playbackStub {
	return &playbackStub{
		events:   make(chan player.Event, 8),
		ended:    make(chan error, 1),
		controls: make(chan string, 8),
	}
}

func (stub *playbackStub) Events() <-chan player.Event { return stub.events }
func (stub *playbackStub) WaitForEnd(ctx context.Context) error {
	select {
	case err := <-stub.ended:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (stub *playbackStub) Pause(_ context.Context, value bool) error {
	stub.controls <- fmt.Sprintf("pause:%t", value)
	return nil
}
func (stub *playbackStub) SeekRelative(_ context.Context, value float64) error {
	stub.controls <- fmt.Sprintf("seek:%.0f", value)
	return nil
}
func (stub *playbackStub) SeekAbsolute(context.Context, float64) error { return nil }
func (stub *playbackStub) SetVolume(_ context.Context, value float64) error {
	stub.controls <- fmt.Sprintf("volume:%.0f", value)
	return nil
}
func (stub *playbackStub) SetMute(_ context.Context, value bool) error {
	stub.controls <- fmt.Sprintf("mute:%t", value)
	return nil
}
func (stub *playbackStub) SetSpeed(_ context.Context, value float64) error {
	stub.controls <- fmt.Sprintf("speed:%.2f", value)
	return nil
}
func (stub *playbackStub) CycleVideo(context.Context) error {
	stub.controls <- "cycle"
	return nil
}
func (stub *playbackStub) Close(context.Context) error {
	stub.closeOnce.Do(func() { close(stub.events) })
	return nil
}

func (stub actionStub) DownloadLecture(ctx context.Context, lecture client.Lecture) (app.DownloadResult, error) {
	return stub.download(ctx, lecture)
}

func (stub actionStub) RecordPlayback(ctx context.Context, state library.PlaybackState) error {
	if stub.record == nil {
		return nil
	}
	return stub.record(ctx, state)
}

func (stub actionStub) ResumeLecture(ctx context.Context, lecture client.Lecture) (library.PlaybackState, bool, error) {
	if stub.resume == nil {
		return library.PlaybackState{}, false, nil
	}
	return stub.resume(ctx, lecture)
}

func (stub actionStub) StartLecture(ctx context.Context, lecture client.Lecture, resume float64) (app.PlaybackStart, error) {
	if stub.start == nil {
		return app.PlaybackStart{}, errors.New("playback is unavailable")
	}
	return stub.start(ctx, lecture, resume)
}

func (stub *productProjectionStub) Lectures(_ context.Context, course client.Course) (client.Lectures, error) {
	stub.course = course
	return append(client.Lectures(nil), stub.lectures...), nil
}

func (stub *productProjectionStub) Artifacts(context.Context) ([]library.ArtifactRecord, error) {
	return append([]library.ArtifactRecord(nil), stub.artifacts...), nil
}

func TestSessionStreamsOneTerminalOperationLifecycle(t *testing.T) {
	session, err := tuisession.Start(t.Context(), tuisession.Options{
		Catalog:  catalogStub{},
		SelfTest: tuisession.SelfTestOptions{Steps: 3, Interval: time.Millisecond},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupSession(t, session)

	eventContext, cancelEvents := context.WithCancel(t.Context())
	defer cancelEvents()
	eventResponse := sessionRequestContext(eventContext, t, session, http.MethodGet, "/events", nil)
	defer closeResponseBody(t, eventResponse.Body)
	if eventResponse.StatusCode != http.StatusOK {
		t.Fatalf("GET events status = %d, want %d", eventResponse.StatusCode, http.StatusOK)
	}
	if got := eventResponse.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("events Content-Type = %q", got)
	}

	startResponse := sessionRequest(t, session, http.MethodPost, "/operations", tuiproto.OperationRequest{
		Kind: tuiproto.OperationKindSelftest,
	})
	defer closeResponseBody(t, startResponse.Body)
	if startResponse.StatusCode != http.StatusAccepted {
		t.Fatalf("POST operation status = %d, want %d", startResponse.StatusCode, http.StatusAccepted)
	}
	var operation tuiproto.Operation
	decodeJSON(t, startResponse, &operation)
	if operation.ID == "" || operation.Kind != tuiproto.OperationKindSelftest || operation.State != tuiproto.OperationStateRunning {
		t.Fatalf("started operation = %+v", operation)
	}

	events := readOperationEvents(t, eventResponse, operation.ID)
	if len(events) != 5 {
		t.Fatalf("operation events = %+v, want started + 3 progress + completed", events)
	}
	wantTypes := []tuiproto.EventType{
		tuiproto.EventTypeOperationStarted,
		tuiproto.EventTypeOperationProgress,
		tuiproto.EventTypeOperationProgress,
		tuiproto.EventTypeOperationProgress,
		tuiproto.EventTypeOperationCompleted,
	}
	for index, event := range events {
		if event.Type != wantTypes[index] {
			t.Fatalf("event[%d].Type = %q, want %q", index, event.Type, wantTypes[index])
		}
		if index > 0 && event.Sequence <= events[index-1].Sequence {
			t.Fatalf("event sequence did not increase: %+v", events)
		}
	}
	if events[3].Percent == nil || *events[3].Percent != 100 {
		t.Fatalf("final progress = %+v, want 100", events[3].Percent)
	}
	if events[4].State == nil || *events[4].State != tuiproto.OperationStateCompleted {
		t.Fatalf("terminal event state = %+v", events[4].State)
	}

	getResponse := sessionRequest(t, session, http.MethodGet, "/operations/"+operation.ID, nil)
	defer closeResponseBody(t, getResponse.Body)
	var completed tuiproto.Operation
	decodeJSON(t, getResponse, &completed)
	if getResponse.StatusCode != http.StatusOK || completed.State != tuiproto.OperationStateCompleted {
		t.Fatalf("completed operation = (%d, %+v)", getResponse.StatusCode, completed)
	}
}

func TestSessionCancellationProducesOneCanceledTerminal(t *testing.T) {
	session, err := tuisession.Start(t.Context(), tuisession.Options{
		Catalog:  catalogStub{},
		SelfTest: tuisession.SelfTestOptions{Steps: 2, Interval: time.Second},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupSession(t, session)

	eventContext, cancelEvents := context.WithCancel(t.Context())
	defer cancelEvents()
	eventResponse := sessionRequestContext(eventContext, t, session, http.MethodGet, "/events", nil)
	defer closeResponseBody(t, eventResponse.Body)

	startResponse := sessionRequest(t, session, http.MethodPost, "/operations", tuiproto.OperationRequest{
		Kind: tuiproto.OperationKindSelftest,
	})
	defer closeResponseBody(t, startResponse.Body)
	var operation tuiproto.Operation
	decodeJSON(t, startResponse, &operation)

	cancelResponse := sessionRequest(t, session, http.MethodDelete, "/operations/"+operation.ID, nil)
	defer closeResponseBody(t, cancelResponse.Body)
	var canceled tuiproto.Operation
	decodeJSON(t, cancelResponse, &canceled)
	if cancelResponse.StatusCode != http.StatusOK || canceled.State != tuiproto.OperationStateCanceled {
		t.Fatalf("canceled operation = (%d, %+v)", cancelResponse.StatusCode, canceled)
	}

	events := readOperationEvents(t, eventResponse, operation.ID)
	if len(events) != 2 || events[0].Type != tuiproto.EventTypeOperationStarted ||
		events[1].Type != tuiproto.EventTypeOperationCanceled {
		t.Fatalf("cancellation events = %+v, want started then canceled", events)
	}
}

func TestDownloadOperationReResolvesLectureAndProducesOneTerminal(t *testing.T) {
	backend := &productProjectionStub{lectures: client.Lectures{{
		InstituteID: 11,
		SessionID:   13,
		SubjectID:   12,
		TTID:        14,
		Topic:       "Authoritative topic",
	}}}
	downloaded := make(chan client.Lecture, 1)
	session, err := tuisession.Start(t.Context(), tuisession.Options{
		Catalog:  backend,
		Lectures: backend,
		Actions: actionStub{download: func(_ context.Context, lecture client.Lecture) (app.DownloadResult, error) {
			downloaded <- lecture
			return app.DownloadResult{LibraryRecorded: true}, nil
		}},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupSession(t, session)

	eventResponse := sessionRequest(t, session, http.MethodGet, "/events", nil)
	defer closeResponseBody(t, eventResponse.Body)
	startResponse := sessionRequest(t, session, http.MethodPost, "/operations", tuiproto.OperationRequest{
		Kind: tuiproto.OperationKindDownload,
		Lecture: &tuiproto.LectureIdentity{
			InstituteID: 11,
			SessionID:   13,
			SubjectID:   12,
			TTID:        14,
		},
	})
	defer closeResponseBody(t, startResponse.Body)
	if startResponse.StatusCode != http.StatusAccepted {
		t.Fatalf("POST download status = %d", startResponse.StatusCode)
	}
	var operation tuiproto.Operation
	decodeJSON(t, startResponse, &operation)
	if operation.Kind != tuiproto.OperationKindDownload {
		t.Fatalf("operation = %+v", operation)
	}

	select {
	case lecture := <-downloaded:
		if lecture.Topic != "Authoritative topic" || lecture.TTID != 14 {
			t.Fatalf("downloaded lecture = %+v", lecture)
		}
	case <-time.After(time.Second):
		t.Fatal("download operation did not reach the action service")
	}
	events := readOperationEvents(t, eventResponse, operation.ID)
	if len(events) != 2 || events[0].Type != tuiproto.EventTypeOperationStarted || events[1].Type != tuiproto.EventTypeOperationCompleted {
		t.Fatalf("download events = %+v", events)
	}
}

func TestDownloadOperationCancellationWinsAndDoesNotLeakActionError(t *testing.T) {
	backend := &productProjectionStub{lectures: client.Lectures{{InstituteID: 1, SessionID: 2, SubjectID: 3, TTID: 4}}}
	entered := make(chan struct{})
	session, err := tuisession.Start(t.Context(), tuisession.Options{
		Catalog:  backend,
		Lectures: backend,
		Actions: actionStub{download: func(ctx context.Context, _ client.Lecture) (app.DownloadResult, error) {
			close(entered)
			<-ctx.Done()
			return app.DownloadResult{}, errors.New("Authorization: Digest response=download-secret")
		}},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupSession(t, session)
	eventResponse := sessionRequest(t, session, http.MethodGet, "/events", nil)
	defer closeResponseBody(t, eventResponse.Body)
	startResponse := sessionRequest(t, session, http.MethodPost, "/operations", tuiproto.OperationRequest{
		Kind:    tuiproto.OperationKindDownload,
		Lecture: &tuiproto.LectureIdentity{InstituteID: 1, SessionID: 2, SubjectID: 3, TTID: 4},
	})
	defer closeResponseBody(t, startResponse.Body)
	var operation tuiproto.Operation
	decodeJSON(t, startResponse, &operation)
	<-entered
	cancelResponse := sessionRequest(t, session, http.MethodDelete, "/operations/"+operation.ID, nil)
	defer closeResponseBody(t, cancelResponse.Body)
	var canceled tuiproto.Operation
	decodeJSON(t, cancelResponse, &canceled)
	if canceled.State != tuiproto.OperationStateCanceled {
		t.Fatalf("canceled operation = %+v", canceled)
	}
	events := readOperationEvents(t, eventResponse, operation.ID)
	if len(events) != 2 || events[1].Type != tuiproto.EventTypeOperationCanceled {
		t.Fatalf("download cancellation events = %+v", events)
	}
	encoded, marshalErr := json.Marshal(events)
	if marshalErr != nil {
		t.Fatalf("marshal events: %v", marshalErr)
	}
	if strings.Contains(string(encoded), "download-secret") {
		t.Fatalf("download events leaked action error: %s", encoded)
	}
}

func TestPlaybackOperationOwnsResumeControlsTelemetryAndCompletion(t *testing.T) {
	backend := &productProjectionStub{lectures: client.Lectures{{InstituteID: 1, SessionID: 2, SubjectID: 3, TTID: 4, Topic: "Consensus"}}}
	playback := newPlaybackStub()
	recorded := make(chan library.PlaybackState, 1)
	started := make(chan float64, 1)
	session, err := tuisession.Start(t.Context(), tuisession.Options{
		Catalog:  backend,
		Lectures: backend,
		Actions: actionStub{
			download: func(context.Context, client.Lecture) (app.DownloadResult, error) {
				return app.DownloadResult{}, errors.New("not used")
			},
			record: func(_ context.Context, state library.PlaybackState) error {
				recorded <- state
				return nil
			},
			resume: func(context.Context, client.Lecture) (library.PlaybackState, bool, error) {
				return library.PlaybackState{ArtifactID: "artifact-1", PositionSeconds: 42}, true, nil
			},
			start: func(_ context.Context, _ client.Lecture, resume float64) (app.PlaybackStart, error) {
				started <- resume
				return app.PlaybackStart{
					Session:       playback,
					InitialEvents: []player.Event{{Name: "property-change", Property: "duration", Data: json.RawMessage("120")}},
				}, nil
			},
		},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupSession(t, session)
	eventResponse := sessionRequest(t, session, http.MethodGet, "/events", nil)
	defer closeResponseBody(t, eventResponse.Body)
	resume := true
	startResponse := sessionRequest(t, session, http.MethodPost, "/operations", tuiproto.OperationRequest{
		Kind:    tuiproto.OperationKindPlayback,
		Lecture: &tuiproto.LectureIdentity{InstituteID: 1, SessionID: 2, SubjectID: 3, TTID: 4},
		Resume:  &resume,
	})
	defer closeResponseBody(t, startResponse.Body)
	var operation tuiproto.Operation
	decodeJSON(t, startResponse, &operation)
	if operation.Kind != tuiproto.OperationKindPlayback {
		t.Fatalf("playback operation = %+v", operation)
	}
	select {
	case position := <-started:
		if position != 42 {
			t.Fatalf("resume position = %f, want 42", position)
		}
	case <-time.After(time.Second):
		t.Fatal("playback did not start")
	}

	paused := true
	commandResponse := sessionRequest(t, session, http.MethodPost, "/operations/"+operation.ID+"/commands", tuiproto.PlaybackCommand{
		Action: tuiproto.PlaybackCommandActionPause,
		Flag:   &paused,
	})
	defer closeResponseBody(t, commandResponse.Body)
	if commandResponse.StatusCode != http.StatusOK {
		t.Fatalf("playback command status = %d", commandResponse.StatusCode)
	}
	select {
	case command := <-playback.controls:
		if command != "pause:true" {
			t.Fatalf("playback command = %q", command)
		}
	case <-time.After(time.Second):
		t.Fatal("playback control was not delivered")
	}

	playback.events <- player.Event{Name: "property-change", Property: "time-pos", Data: json.RawMessage("119")}
	playback.events <- player.Event{Name: "end-file", Reason: "eof"}
	playback.ended <- nil
	events := readOperationEvents(t, eventResponse, operation.ID)
	if len(events) < 4 || events[len(events)-1].Type != tuiproto.EventTypeOperationCompleted {
		t.Fatalf("playback events = %+v", events)
	}
	var observedPosition bool
	for _, event := range events {
		if event.PositionSeconds != nil && *event.PositionSeconds == 119 && event.DurationSeconds != nil && *event.DurationSeconds == 120 {
			observedPosition = true
		}
	}
	if !observedPosition {
		t.Fatalf("playback telemetry did not include position/duration: %+v", events)
	}
	select {
	case state := <-recorded:
		if state.ArtifactID != "artifact-1" || state.PositionSeconds != 119 || state.DurationSeconds != 120 || !state.Completed {
			t.Fatalf("recorded playback = %+v", state)
		}
	case <-time.After(time.Second):
		t.Fatal("playback checkpoint was not recorded")
	}
}

func TestParentCancellationClosesTheSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	session, err := tuisession.Start(ctx, tuisession.Options{Catalog: catalogStub{}})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupSession(t, session)

	cancel()
	deadline := time.Now().Add(time.Second)
	for {
		request, requestErr := http.NewRequestWithContext(t.Context(), http.MethodGet, session.BaseURL()+"/health", nil)
		if requestErr != nil {
			t.Fatalf("NewRequestWithContext() error = %v", requestErr)
		}
		request.Header.Set(tuiproto.ProtocolHeader, tuiproto.ProtocolVersion)
		request.Header.Set(tuiproto.CapabilityHeader, session.Capability())
		response, doErr := (&http.Client{Timeout: 100 * time.Millisecond}).Do(request)
		if doErr != nil {
			break
		}
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Errorf("close response body: %v", closeErr)
		}
		if time.Now().After(deadline) {
			t.Fatal("session continued accepting requests after parent cancellation")
		}
		time.Sleep(time.Millisecond)
	}
	if closeErr := session.Close(); closeErr != nil {
		t.Fatalf("second Close() error = %v", closeErr)
	}
}

func TestCloseCancelsInFlightWorkAndIsIdempotent(t *testing.T) {
	session, err := tuisession.Start(context.Background(), tuisession.Options{
		Catalog:  catalogStub{},
		SelfTest: tuisession.SelfTestOptions{Steps: 2, Interval: time.Hour},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	startResponse := sessionRequest(t, session, http.MethodPost, "/operations", tuiproto.OperationRequest{
		Kind: tuiproto.OperationKindSelftest,
	})
	defer closeResponseBody(t, startResponse.Body)
	if startResponse.StatusCode != http.StatusAccepted {
		t.Fatalf("POST operation status = %d", startResponse.StatusCode)
	}

	done := make(chan error, 1)
	go func() { done <- session.Close() }()
	select {
	case closeErr := <-done:
		if closeErr != nil {
			t.Fatalf("Close() error = %v", closeErr)
		}
	case <-time.After(time.Second):
		t.Fatal("Close() did not cancel in-flight work promptly")
	}
	if closeErr := session.Close(); closeErr != nil {
		t.Fatalf("second Close() error = %v", closeErr)
	}
}

func TestSessionRejectsUnauthenticatedAndMismatchedRequests(t *testing.T) {
	session, err := tuisession.Start(t.Context(), tuisession.Options{Catalog: catalogStub{}})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupSession(t, session)

	missingCapability := rawSessionRequest(t, session, http.MethodGet, "/health", nil, map[string]string{
		tuiproto.ProtocolHeader: tuiproto.ProtocolVersion,
	})
	defer closeResponseBody(t, missingCapability.Body)
	if missingCapability.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing capability status = %d, want %d", missingCapability.StatusCode, http.StatusUnauthorized)
	}
	missingBody, err := io.ReadAll(missingCapability.Body)
	if err != nil {
		t.Fatalf("read missing-capability response: %v", err)
	}
	if strings.Contains(string(missingBody), session.Capability()) {
		t.Fatal("unauthorized response disclosed the session capability")
	}

	mismatchedProtocol := rawSessionRequest(t, session, http.MethodGet, "/health", nil, map[string]string{
		tuiproto.ProtocolHeader:   "tui/v999",
		tuiproto.CapabilityHeader: session.Capability(),
	})
	defer closeResponseBody(t, mismatchedProtocol.Body)
	if mismatchedProtocol.StatusCode != http.StatusUpgradeRequired {
		t.Fatalf("protocol mismatch status = %d, want %d", mismatchedProtocol.StatusCode, http.StatusUpgradeRequired)
	}
	if got := mismatchedProtocol.Header.Get(tuiproto.SupportedProtocolHeader); got != tuiproto.ProtocolVersion {
		t.Fatalf("supported protocol header = %q, want %q", got, tuiproto.ProtocolVersion)
	}
	var mismatch tuiproto.Problem
	decodeJSON(t, mismatchedProtocol, &mismatch)
	if mismatch.SupportedProtocol == nil || *mismatch.SupportedProtocol != tuiproto.ProtocolVersion {
		t.Fatalf("protocol mismatch problem = %+v", mismatch)
	}

	wrongMethod := sessionRequest(t, session, http.MethodPost, "/health", nil)
	defer closeResponseBody(t, wrongMethod.Body)
	if wrongMethod.StatusCode != http.StatusMethodNotAllowed || wrongMethod.Header.Get("Allow") != http.MethodGet {
		t.Fatalf("POST health = (%d, Allow %q)", wrongMethod.StatusCode, wrongMethod.Header.Get("Allow"))
	}

	unknown := sessionRequest(t, session, http.MethodGet, "/unknown", nil)
	defer closeResponseBody(t, unknown.Body)
	if unknown.StatusCode != http.StatusNotFound || !strings.HasPrefix(unknown.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("unknown route = (%d, Content-Type %q)", unknown.StatusCode, unknown.Header.Get("Content-Type"))
	}
	var notFound tuiproto.Problem
	decodeJSON(t, unknown, &notFound)
	if notFound.Code != "not_found" {
		t.Fatalf("unknown route problem = %+v", notFound)
	}
}

func (stub catalogStub) Courses(context.Context) (client.Courses, error) {
	return append(client.Courses(nil), stub.courses...), stub.err
}

func TestSessionExposesAuthenticatedHealthAndCourses(t *testing.T) {
	session, err := tuisession.Start(t.Context(), tuisession.Options{
		Catalog: catalogStub{courses: client.Courses{{
			InstituteID:   7,
			SessionID:     11,
			SubjectID:     13,
			SubjectName:   "Distributed Systems",
			SessionName:   "Monsoon 2026",
			ProfessorName: "Dr. Rao",
			VideoCount:    21,
		}}},
		Version: "test-version",
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if !strings.HasPrefix(session.Address(), "127.0.0.1:") {
		t.Fatalf("session address = %q, want an IPv4 loopback listener", session.Address())
	}
	t.Cleanup(func() {
		if closeErr := session.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})

	healthResponse := sessionRequest(t, session, http.MethodGet, "/health", nil)
	defer closeResponseBody(t, healthResponse.Body)
	if healthResponse.StatusCode != http.StatusOK {
		t.Fatalf("GET health status = %d, want %d", healthResponse.StatusCode, http.StatusOK)
	}
	if got := healthResponse.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("health Cache-Control = %q, want no-store", got)
	}
	var health tuiproto.Health
	decodeJSON(t, healthResponse, &health)
	if health.Protocol != tuiproto.ProtocolVersion || health.SessionID != session.ID() ||
		health.AuthStatus != tuiproto.AuthStatusReady || health.Status != tuiproto.HealthStatusOK || health.Version != "test-version" {
		t.Fatalf("health = %+v", health)
	}

	coursesResponse := sessionRequest(t, session, http.MethodGet, "/courses", nil)
	defer closeResponseBody(t, coursesResponse.Body)
	if coursesResponse.StatusCode != http.StatusOK {
		t.Fatalf("GET courses status = %d, want %d", coursesResponse.StatusCode, http.StatusOK)
	}
	var courses tuiproto.CourseList
	decodeJSON(t, coursesResponse, &courses)
	if len(courses.Courses) != 1 {
		t.Fatalf("courses = %+v, want one course", courses)
	}
	got := courses.Courses[0]
	if got.InstituteID != 7 || got.SessionID != 11 || got.SubjectID != 13 ||
		got.SubjectName != "Distributed Systems" || got.SessionName != "Monsoon 2026" ||
		got.ProfessorName != "Dr. Rao" || got.VideoCount != 21 {
		t.Fatalf("course projection = %+v", got)
	}
}

func TestSessionKeepsLocalSurfacesAvailableAndRetriesAuthenticationWithoutABody(t *testing.T) {
	authentication := &authenticationStub{status: tuiproto.AuthStatusUnavailable}
	backend := &productProjectionStub{
		catalogStub: catalogStub{courses: client.Courses{{InstituteID: 1, SessionID: 2, SubjectID: 3, SubjectName: "Course"}}},
		artifacts: []library.ArtifactRecord{{Manifest: artifactpkg.Manifest{
			ArtifactID: "local-artifact",
			ProducedAt: time.Date(2026, time.August, 23, 0, 0, 0, 0, time.UTC),
		}}},
	}
	session, err := tuisession.Start(t.Context(), tuisession.Options{
		Actions:        actionStub{},
		Artifacts:      backend,
		Authentication: authentication,
		Catalog:        backend,
		Lectures:       backend,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupSession(t, session)

	healthResponse := sessionRequest(t, session, http.MethodGet, "/health", nil)
	defer closeResponseBody(t, healthResponse.Body)
	var health tuiproto.Health
	decodeJSON(t, healthResponse, &health)
	if health.AuthStatus != tuiproto.AuthStatusUnavailable {
		t.Fatalf("health auth status = %q, want unavailable", health.AuthStatus)
	}

	coursesResponse := sessionRequest(t, session, http.MethodGet, "/courses", nil)
	defer closeResponseBody(t, coursesResponse.Body)
	var unavailable tuiproto.Problem
	decodeJSON(t, coursesResponse, &unavailable)
	if coursesResponse.StatusCode != http.StatusServiceUnavailable || unavailable.Code != "auth_unavailable" ||
		unavailable.Error != "upstream authentication is unavailable" {
		t.Fatalf("unavailable courses = (%d, %+v)", coursesResponse.StatusCode, unavailable)
	}

	libraryResponse := sessionRequest(t, session, http.MethodGet, "/library", nil)
	defer closeResponseBody(t, libraryResponse.Body)
	var artifacts tuiproto.ArtifactList
	decodeJSON(t, libraryResponse, &artifacts)
	if libraryResponse.StatusCode != http.StatusOK || len(artifacts.Artifacts) != 1 {
		t.Fatalf("local library = (%d, %+v)", libraryResponse.StatusCode, artifacts)
	}

	diagnosticsResponse := sessionRequest(t, session, http.MethodGet, "/diagnostics", nil)
	defer closeResponseBody(t, diagnosticsResponse.Body)
	if diagnosticsResponse.StatusCode != http.StatusOK {
		t.Fatalf("local diagnostics status = %d, want %d", diagnosticsResponse.StatusCode, http.StatusOK)
	}

	for _, kind := range []tuiproto.OperationKind{tuiproto.OperationKindDownload, tuiproto.OperationKindPlayback} {
		operationResponse := sessionRequest(t, session, http.MethodPost, "/operations", tuiproto.OperationRequest{
			Kind: kind,
			Lecture: &tuiproto.LectureIdentity{
				InstituteID: 1,
				SessionID:   2,
				SubjectID:   3,
				TTID:        4,
			},
		})
		defer closeResponseBody(t, operationResponse.Body)
		var operationProblem tuiproto.Problem
		decodeJSON(t, operationResponse, &operationProblem)
		if operationResponse.StatusCode != http.StatusServiceUnavailable || operationProblem.Code != "auth_unavailable" {
			t.Fatalf("%s operation while unavailable = (%d, %+v)", kind, operationResponse.StatusCode, operationProblem)
		}
	}

	selfTestResponse := sessionRequest(t, session, http.MethodPost, "/operations", tuiproto.OperationRequest{Kind: tuiproto.OperationKindSelftest})
	defer closeResponseBody(t, selfTestResponse.Body)
	if selfTestResponse.StatusCode != http.StatusAccepted {
		t.Fatalf("self-test status = %d, want %d", selfTestResponse.StatusCode, http.StatusAccepted)
	}

	invalidRetry := sessionRequest(t, session, http.MethodPost, "/auth/retry", map[string]string{"password": "must-not-cross"})
	defer closeResponseBody(t, invalidRetry.Body)
	if invalidRetry.StatusCode != http.StatusBadRequest || authentication.Retries() != 0 {
		t.Fatalf("retry with body = status %d, retries %d", invalidRetry.StatusCode, authentication.Retries())
	}

	retryResponse := sessionRequest(t, session, http.MethodPost, "/auth/retry", nil)
	defer closeResponseBody(t, retryResponse.Body)
	var retried tuiproto.Health
	decodeJSON(t, retryResponse, &retried)
	if retryResponse.StatusCode != http.StatusOK || retried.AuthStatus != tuiproto.AuthStatusReady || authentication.Retries() != 1 {
		t.Fatalf("retry = (%d, %+v), retries %d", retryResponse.StatusCode, retried, authentication.Retries())
	}

	readyCourses := sessionRequest(t, session, http.MethodGet, "/courses", nil)
	defer closeResponseBody(t, readyCourses.Body)
	if readyCourses.StatusCode != http.StatusOK {
		t.Fatalf("courses after retry status = %d, want %d", readyCourses.StatusCode, http.StatusOK)
	}
}

func TestSessionAuthenticationRetryFailureReturnsFixedProblem(t *testing.T) {
	secret := "raw-upstream-password=body-secret"
	authentication := &authenticationStub{
		status: tuiproto.AuthStatusUnavailable,
		retry:  func(context.Context) error { return errors.New(secret) },
	}
	session, err := tuisession.Start(t.Context(), tuisession.Options{
		Authentication: authentication,
		Catalog:        catalogStub{},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupSession(t, session)

	response := sessionRequest(t, session, http.MethodPost, "/auth/retry", nil)
	defer closeResponseBody(t, response.Body)
	body, readErr := io.ReadAll(response.Body)
	if readErr != nil {
		t.Fatalf("read retry failure: %v", readErr)
	}
	if response.StatusCode != http.StatusServiceUnavailable || strings.Contains(string(body), secret) ||
		!strings.Contains(string(body), `"code":"auth_unavailable"`) {
		t.Fatalf("retry failure = (%d, %q)", response.StatusCode, body)
	}
}

func TestSessionProjectsLecturesLibraryAndDiagnosticsWithoutTerminalOrSecretData(t *testing.T) {
	producedAt := time.Date(2026, time.August, 13, 12, 30, 0, 0, time.UTC)
	backend := &productProjectionStub{
		lectures: client.Lectures{{
			ActualDuration: 3600,
			ClassroomName:  "Room 7",
			InstituteID:    11,
			NoAudio:        0,
			ProfessorName:  "to\u200bken=professor-secret",
			SeqNo:          4,
			SessionID:      13,
			SessionName:    "Monsoon",
			StartTime:      "2026-08-13T10:00:00Z",
			SubjectID:      12,
			SubjectName:    "Distributed Systems",
			Topic:          "to\x1b[31mken=lecture-secret",
			TTID:           14,
			Views:          2,
		}},
		artifacts: []library.ArtifactRecord{{
			Manifest: artifactpkg.Manifest{
				ArtifactID: "artifact-1",
				Lecture:    artifactpkg.Lecture{SeqNo: 4, Topic: "Consensus"},
				ProducedAt: producedAt,
			},
			Files: []library.ArtifactFile{
				{Bytes: 1200, Present: true},
				{Bytes: 800, Present: false},
			},
		}},
	}
	session, err := tuisession.Start(t.Context(), tuisession.Options{
		Catalog:   backend,
		Lectures:  backend,
		Artifacts: backend,
		Diagnostics: []tuisession.Diagnostic{{
			Name:   "mpv",
			Status: "warn",
			Detail: "upstream auth=diagnostic-secret\x1b[2J is unavailable",
		}},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupSession(t, session)

	lectureResponse := sessionRequest(t, session, http.MethodGet, "/lectures?instituteId=11&subjectId=12&sessionId=13", nil)
	defer closeResponseBody(t, lectureResponse.Body)
	var lectures tuiproto.LectureList
	decodeJSON(t, lectureResponse, &lectures)
	if lectureResponse.StatusCode != http.StatusOK || len(lectures.Lectures) != 1 {
		t.Fatalf("lectures = (%d, %+v)", lectureResponse.StatusCode, lectures)
	}
	if backend.course.InstituteID != 11 || backend.course.SubjectID != 12 || backend.course.SessionID != 13 {
		t.Fatalf("requested course = %+v", backend.course)
	}
	lecture := lectures.Lectures[0]
	if lecture.TTID != 14 || lecture.Sequence != 4 || lecture.Topic != "token=REDACTED" ||
		lecture.ProfessorName != "token=REDACTED" || lecture.NoAudio {
		t.Fatalf("projected lecture = %+v", lecture)
	}

	invalidResponse := sessionRequest(t, session, http.MethodGet, "/lectures?instituteId=11&subjectId=12&sessionId=13&extra=1", nil)
	defer closeResponseBody(t, invalidResponse.Body)
	if invalidResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid lecture identity status = %d", invalidResponse.StatusCode)
	}

	libraryResponse := sessionRequest(t, session, http.MethodGet, "/library", nil)
	defer closeResponseBody(t, libraryResponse.Body)
	var artifacts tuiproto.ArtifactList
	decodeJSON(t, libraryResponse, &artifacts)
	if len(artifacts.Artifacts) != 1 {
		t.Fatalf("artifacts = %+v", artifacts)
	}
	artifact := artifacts.Artifacts[0]
	if artifact.ArtifactID != "artifact-1" || artifact.TotalBytes != 2000 || artifact.PresentFileCount != 1 || artifact.ProducedAt != producedAt.Format(time.RFC3339) {
		t.Fatalf("projected artifact = %+v", artifact)
	}

	diagnosticResponse := sessionRequest(t, session, http.MethodGet, "/diagnostics", nil)
	defer closeResponseBody(t, diagnosticResponse.Body)
	var diagnostics tuiproto.DiagnosticList
	decodeJSON(t, diagnosticResponse, &diagnostics)
	if len(diagnostics.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	detail := diagnostics.Diagnostics[0].Detail
	if strings.Contains(detail, "diagnostic-secret") || strings.ContainsRune(detail, '\x1b') || !strings.Contains(detail, "REDACTED") {
		t.Fatalf("unsafe diagnostic detail = %q", detail)
	}
}

func TestSessionRejectsInvalidBodiesWithoutLeakingUpstreamErrors(t *testing.T) {
	session, err := tuisession.Start(t.Context(), tuisession.Options{
		Catalog: catalogStub{err: errors.New("upstream Authorization: Digest response=body-secret")},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupSession(t, session)

	catalogResponse := sessionRequest(t, session, http.MethodGet, "/courses", nil)
	defer closeResponseBody(t, catalogResponse.Body)
	catalogBody, readErr := io.ReadAll(catalogResponse.Body)
	if readErr != nil {
		t.Fatalf("read catalog failure: %v", readErr)
	}
	if catalogResponse.StatusCode != http.StatusBadGateway || strings.Contains(string(catalogBody), "body-secret") {
		t.Fatalf("catalog failure = (%d, %q)", catalogResponse.StatusCode, catalogBody)
	}

	for name, requestBody := range map[string]string{
		"unknown field":  `{"kind":"selftest","extra":true}`,
		"trailing value": `{"kind":"selftest"} {"kind":"selftest"}`,
		"oversized":      `{"kind":"selftest","padding":"` + strings.Repeat("x", 2*maxTestRequestBodyBytes) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			response := rawBodySessionRequest(t, session, http.MethodPost, "/operations", requestBody, "application/json")
			defer closeResponseBody(t, response.Body)
			if response.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusBadRequest)
			}
			var problem tuiproto.Problem
			decodeJSON(t, response, &problem)
			if problem.Code != "invalid_request" || problem.Error != "invalid operation request" {
				t.Fatalf("problem = %+v", problem)
			}
		})
	}

	wrongContentType := rawBodySessionRequest(t, session, http.MethodPost, "/operations", `{"kind":"selftest"}`, "text/plain")
	defer closeResponseBody(t, wrongContentType.Body)
	if wrongContentType.StatusCode != http.StatusBadRequest {
		t.Fatalf("wrong content type status = %d, want %d", wrongContentType.StatusCode, http.StatusBadRequest)
	}
}

const maxTestRequestBodyBytes = 4 << 10

func cleanupSession(t *testing.T, session *tuisession.Session) {
	t.Helper()
	t.Cleanup(func() {
		if err := session.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
}

func closeResponseBody(t *testing.T, body io.Closer) {
	t.Helper()
	if err := body.Close(); err != nil {
		t.Errorf("close response body: %v", err)
	}
}

func TestSlowEventConsumerReceivesExplicitOverflow(t *testing.T) {
	session, err := tuisession.Start(t.Context(), tuisession.Options{
		Catalog:         catalogStub{},
		EventQueueDepth: 1,
		SelfTest:        tuisession.SelfTestOptions{Steps: 5000},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupSession(t, session)

	eventContext, cancelEvents := context.WithCancel(t.Context())
	defer cancelEvents()
	eventResponse := sessionRequestContext(eventContext, t, session, http.MethodGet, "/events", nil)
	defer closeResponseBody(t, eventResponse.Body)

	startResponse := sessionRequest(t, session, http.MethodPost, "/operations", tuiproto.OperationRequest{
		Kind: tuiproto.OperationKindSelftest,
	})
	defer closeResponseBody(t, startResponse.Body)
	if startResponse.StatusCode != http.StatusAccepted {
		t.Fatalf("POST operation status = %d", startResponse.StatusCode)
	}

	scanner := bufio.NewScanner(eventResponse.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event tuiproto.Event
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			t.Fatalf("decode SSE event: %v", err)
		}
		if event.Type == tuiproto.EventTypeStreamOverflow {
			if event.Message == nil || !strings.Contains(*event.Message, "refresh") {
				t.Fatalf("overflow event = %+v", event)
			}
			return
		}
		if event.Type == tuiproto.EventTypeOperationCompleted {
			t.Fatal("slow event consumer silently reached completion without overflow")
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read overflow event: %v", err)
	}
	t.Fatal("event stream closed without an overflow event")
}

func sessionRequest(t *testing.T, session *tuisession.Session, method, path string, body any) *http.Response {
	t.Helper()
	return rawSessionRequest(t, session, method, path, body, map[string]string{
		tuiproto.ProtocolHeader:   tuiproto.ProtocolVersion,
		tuiproto.CapabilityHeader: session.Capability(),
	})
}

func rawSessionRequest(
	t *testing.T,
	session *tuisession.Session,
	method string,
	path string,
	body any,
	headers map[string]string,
) *http.Response {
	t.Helper()
	return rawSessionRequestContext(t.Context(), t, session, method, path, body, headers)
}

func sessionRequestContext(
	ctx context.Context,
	t *testing.T,
	session *tuisession.Session,
	method string,
	path string,
	body any,
) *http.Response {
	t.Helper()
	return rawSessionRequestContext(ctx, t, session, method, path, body, map[string]string{
		tuiproto.ProtocolHeader:   tuiproto.ProtocolVersion,
		tuiproto.CapabilityHeader: session.Capability(),
	})
}

func rawSessionRequestContext(
	ctx context.Context,
	t *testing.T,
	session *tuisession.Session,
	method string,
	path string,
	body any,
	headers map[string]string,
) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		bodyReader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, session.BaseURL()+path, bodyReader)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	return response
}

func rawBodySessionRequest(
	t *testing.T,
	session *tuisession.Session,
	method string,
	path string,
	body string,
	contentType string,
) *http.Response {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), method, session.BaseURL()+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	request.Header.Set(tuiproto.ProtocolHeader, tuiproto.ProtocolVersion)
	request.Header.Set(tuiproto.CapabilityHeader, session.Capability())
	request.Header.Set("Content-Type", contentType)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	return response
}

func readOperationEvents(t *testing.T, response *http.Response, operationID string) []tuiproto.Event {
	t.Helper()
	scanner := bufio.NewScanner(response.Body)
	events := make([]tuiproto.Event, 0, 5)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event tuiproto.Event
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			t.Fatalf("decode SSE event: %v", err)
		}
		if event.OperationID == nil || *event.OperationID != operationID {
			continue
		}
		events = append(events, event)
		if event.Type == tuiproto.EventTypeOperationCompleted ||
			event.Type == tuiproto.EventTypeOperationCanceled ||
			event.Type == tuiproto.EventTypeOperationFailed {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read SSE stream: %v", err)
	}
	return events
}

func decodeJSON(t *testing.T, response *http.Response, target any) {
	t.Helper()
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}
