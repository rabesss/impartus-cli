package tui_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rabesss/impartus-cli/internal/app"
	"github.com/rabesss/impartus-cli/internal/artifact"
	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/library"
	"github.com/rabesss/impartus-cli/internal/player"
	"github.com/rabesss/impartus-cli/internal/tui"
)

type fakeBackend struct {
	courses         client.Courses
	lectures        client.Lectures
	artifacts       []library.ArtifactRecord
	coursesErr      error
	lecturesErr     error
	panicCourses    bool
	panicLectures   bool
	coursesStarted  chan struct{}
	coursesRelease  chan struct{}
	coursesCanceled chan struct{}

	courseRequests   int
	lectureRequests  []client.Course
	resume           library.PlaybackState
	resumeFound      bool
	playback         *fakePlayback
	startedLecture   client.Lecture
	startedAt        float64
	startStarted     chan struct{}
	startAfterCancel bool
	recorded         []library.PlaybackState
	downloaded       []client.Lecture
	downloadResult   app.DownloadResult
	downloadStarted  chan struct{}
	downloadRelease  chan struct{}
	downloadCanceled chan struct{}
}

type fakePlayback struct {
	events       chan player.Event
	paused       []bool
	seeks        []float64
	muted        []bool
	volumes      []float64
	speeds       []float64
	cycles       int
	closed       int
	waitErr      error
	waitStarted  chan struct{}
	waitRelease  chan struct{}
	waitCanceled chan struct{}
	closeEvents  bool
}

func (backend *fakeBackend) Courses(ctx context.Context) (client.Courses, error) {
	if backend.panicCourses {
		panic("catalog panic fixture")
	}
	backend.courseRequests++
	if backend.coursesStarted != nil {
		close(backend.coursesStarted)
		select {
		case <-ctx.Done():
			if backend.coursesCanceled != nil {
				close(backend.coursesCanceled)
			}
			if backend.coursesRelease != nil {
				<-backend.coursesRelease
			}
			return nil, ctx.Err()
		case <-backend.coursesRelease:
		}
	}
	return append(client.Courses(nil), backend.courses...), backend.coursesErr
}

func (backend *fakeBackend) Lectures(_ context.Context, course client.Course) (client.Lectures, error) {
	if backend.panicLectures {
		panic("lecture panic fixture")
	}
	backend.lectureRequests = append(backend.lectureRequests, course)
	return append(client.Lectures(nil), backend.lectures...), backend.lecturesErr
}

func (backend *fakeBackend) StartLecture(ctx context.Context, lecture client.Lecture, resume float64) (app.PlaybackSession, error) {
	backend.startedLecture = lecture
	backend.startedAt = resume
	if backend.startStarted != nil {
		close(backend.startStarted)
	}
	if backend.startAfterCancel {
		<-ctx.Done()
	}
	return backend.playback, nil
}

func (backend *fakeBackend) DownloadLecture(ctx context.Context, lecture client.Lecture) (app.DownloadResult, error) {
	backend.downloaded = append(backend.downloaded, lecture)
	if backend.downloadStarted != nil {
		close(backend.downloadStarted)
	}
	if backend.downloadRelease != nil {
		select {
		case <-ctx.Done():
			if backend.downloadCanceled != nil {
				close(backend.downloadCanceled)
			}
			return app.DownloadResult{}, ctx.Err()
		case <-backend.downloadRelease:
		}
	}
	return backend.downloadResult, nil
}

func (backend *fakeBackend) Artifacts(context.Context) ([]library.ArtifactRecord, error) {
	return append([]library.ArtifactRecord(nil), backend.artifacts...), nil
}

func (backend *fakeBackend) ResumeLecture(context.Context, client.Lecture) (library.PlaybackState, bool, error) {
	return backend.resume, backend.resumeFound, nil
}

func (backend *fakeBackend) RecordPlayback(_ context.Context, state library.PlaybackState) error {
	backend.recorded = append(backend.recorded, state)
	return nil
}

func (playback *fakePlayback) Events() <-chan player.Event { return playback.events }
func (playback *fakePlayback) WaitForEnd(ctx context.Context) error {
	if playback.waitStarted != nil {
		close(playback.waitStarted)
	}
	if playback.waitRelease != nil {
		select {
		case <-ctx.Done():
			if playback.waitCanceled != nil {
				close(playback.waitCanceled)
			}
			return ctx.Err()
		case <-playback.waitRelease:
		}
	}
	return playback.waitErr
}
func (playback *fakePlayback) Pause(_ context.Context, paused bool) error {
	playback.paused = append(playback.paused, paused)
	return nil
}
func (playback *fakePlayback) SeekRelative(_ context.Context, seconds float64) error {
	playback.seeks = append(playback.seeks, seconds)
	return nil
}
func (playback *fakePlayback) SeekAbsolute(context.Context, float64) error { return nil }
func (playback *fakePlayback) SetVolume(_ context.Context, value float64) error {
	playback.volumes = append(playback.volumes, value)
	return nil
}
func (playback *fakePlayback) SetMute(_ context.Context, value bool) error {
	playback.muted = append(playback.muted, value)
	return nil
}
func (playback *fakePlayback) SetSpeed(_ context.Context, value float64) error {
	playback.speeds = append(playback.speeds, value)
	return nil
}
func (playback *fakePlayback) CycleVideo(context.Context) error {
	playback.cycles++
	return nil
}
func (playback *fakePlayback) Close(context.Context) error {
	playback.closed++
	if playback.closeEvents && playback.closed == 1 {
		close(playback.events)
	}
	return nil
}

func TestUserBrowsesFromCoursesIntoLectures(t *testing.T) {
	backend := &fakeBackend{
		courses: client.Courses{
			{SubjectName: "Distributed Systems", SubjectID: 11, SessionID: 22, InstituteID: 33},
			{SubjectName: "Linear Algebra", SubjectID: 44, SessionID: 55, InstituteID: 33},
		},
		lectures: client.Lectures{
			{Topic: "Consensus and Raft", TTID: 101, SeqNo: 7},
			{Topic: "Failure detectors", TTID: 102, SeqNo: 8},
		},
	}
	model := tui.New(context.Background(), backend)

	model = applyCommand(t, model, model.Init())
	if backend.courseRequests != 1 {
		t.Fatalf("course requests = %d, want 1", backend.courseRequests)
	}
	if got := model.View().Content; !strings.Contains(got, "Distributed Systems") || !strings.Contains(got, "Linear Algebra") {
		t.Fatalf("course view does not show the live catalog:\n%s", got)
	}

	model, command := update(t, model, key(tea.KeyEnter, ""))
	model = applyCommand(t, model, command)
	if len(backend.lectureRequests) != 1 || backend.lectureRequests[0].SubjectID != 11 {
		t.Fatalf("lecture requests = %+v, want selected course", backend.lectureRequests)
	}
	if got := model.View().Content; !strings.Contains(got, "Consensus and Raft") || !strings.Contains(got, "Failure detectors") {
		t.Fatalf("lecture view does not show selected course lectures:\n%s", got)
	}
}

func TestUserFiltersCatalogAndOpensVisibleSelection(t *testing.T) {
	backend := &fakeBackend{
		courses: client.Courses{
			{SubjectName: "Distributed Systems", SubjectID: 11, SessionID: 22},
			{SubjectName: "Linear Algebra", SubjectID: 44, SessionID: 55},
		},
		lectures: client.Lectures{{Topic: "Vector spaces", TTID: 103}},
	}
	model := applyCommand(t, tui.New(context.Background(), backend), tui.New(context.Background(), backend).Init())

	model, _ = update(t, model, key('/', "/"))
	for _, character := range "linear" {
		model, _ = update(t, model, key(character, string(character)))
	}
	view := model.View().Content
	if strings.Contains(view, "Distributed Systems") || !strings.Contains(view, "Linear Algebra") {
		t.Fatalf("filtered view =\n%s", view)
	}

	model, _ = update(t, model, key(tea.KeyEnter, ""))
	model, command := update(t, model, key(tea.KeyEnter, ""))
	model = applyCommand(t, model, command)
	if len(backend.lectureRequests) != 1 || backend.lectureRequests[0].SubjectID != 44 {
		t.Fatalf("opened course = %+v, want visible Linear Algebra course", backend.lectureRequests)
	}
	if !strings.Contains(model.View().Content, "Vector spaces") {
		t.Fatalf("lecture view =\n%s", model.View().Content)
	}
}

func TestLectureFilterDoesNotLeakBackIntoCourses(t *testing.T) {
	backend := &fakeBackend{
		courses: client.Courses{
			{SubjectName: "Distributed Systems", SubjectID: 11, SessionID: 22},
			{SubjectName: "Linear Algebra", SubjectID: 44, SessionID: 55},
		},
		lectures: client.Lectures{{Topic: "Consensus", TTID: 101}},
	}
	model := tui.New(context.Background(), backend)
	model = applyCommand(t, model, model.Init())
	model, command := update(t, model, key(tea.KeyEnter, ""))
	model = applyCommand(t, model, command)
	model, _ = update(t, model, key('/', "/"))
	for _, character := range "consensus" {
		model, _ = update(t, model, key(character, string(character)))
	}
	model, _ = update(t, model, key(tea.KeyEnter, ""))
	model, _ = update(t, model, key(tea.KeyEscape, ""))

	view := model.View().Content
	if !strings.Contains(view, "Distributed Systems") || !strings.Contains(view, "Linear Algebra") {
		t.Fatalf("lecture filter leaked into courses:\n%s", view)
	}
}

func TestUserResumesLectureAndControlsMPVWithoutSharingTheTerminal(t *testing.T) {
	playback := &fakePlayback{events: make(chan player.Event, 2)}
	backend := &fakeBackend{
		courses:     client.Courses{{SubjectName: "Distributed Systems", SubjectID: 11, SessionID: 22}},
		lectures:    client.Lectures{{Topic: "Consensus and Raft", TTID: 101, SeqNo: 7}},
		resume:      library.PlaybackState{ArtifactID: "impartus:v1:test", PositionSeconds: 42, DurationSeconds: 120},
		resumeFound: true,
		playback:    playback,
	}
	model := tui.New(context.Background(), backend)
	model = applyCommand(t, model, model.Init())
	model, command := update(t, model, key(tea.KeyEnter, ""))
	model = applyCommand(t, model, command)

	model, command = update(t, model, key(tea.KeyEnter, ""))
	model = applyCommand(t, model, command)
	if got := model.View().Content; !strings.Contains(got, "Resume") || !strings.Contains(got, "00:42") {
		t.Fatalf("resume prompt =\n%s", got)
	}

	model, command = update(t, model, key('y', "y"))
	model = applyCommand(t, model, command)
	if backend.startedLecture.TTID != 101 || backend.startedAt != 42 {
		t.Fatalf("StartLecture = (ttid=%d, resume=%v), want (101, 42)", backend.startedLecture.TTID, backend.startedAt)
	}
	view := model.View()
	if !view.AltScreen || !strings.Contains(view.Content, "Playing") {
		t.Fatalf("playback view does not exclusively own alternate screen: %+v", view)
	}

	playback.events <- player.Event{Name: "property-change", Property: "time-pos", Data: json.RawMessage("47.5")}
	model = applyCommand(t, model, commandFromUpdate(t, model, key(tea.KeySpace, "")))
	if len(playback.paused) != 1 || !playback.paused[0] {
		t.Fatalf("pause controls = %v, want [true]", playback.paused)
	}
	for _, pressed := range []tea.KeyPressMsg{
		key(tea.KeyRight, ""), key('m', "m"), key('+', "+"), key(']', "]"), key('v', "v"),
	} {
		model = applyCommand(t, model, commandFromUpdate(t, model, pressed))
	}
	if !reflect.DeepEqual(playback.seeks, []float64{10}) || !reflect.DeepEqual(playback.muted, []bool{true}) ||
		!reflect.DeepEqual(playback.volumes, []float64{105}) || !reflect.DeepEqual(playback.speeds, []float64{1.25}) || playback.cycles != 1 {
		t.Fatalf("transport controls seek=%v mute=%v volume=%v speed=%v cycles=%d", playback.seeks, playback.muted, playback.volumes, playback.speeds, playback.cycles)
	}
}

func TestPlaybackEventsUpdateViewAndPersistCompletedResumeState(t *testing.T) {
	playback := &fakePlayback{events: make(chan player.Event, 4)}
	backend := &fakeBackend{
		courses:     client.Courses{{SubjectName: "Distributed Systems", SubjectID: 11, SessionID: 22}},
		lectures:    client.Lectures{{Topic: "Consensus and Raft", TTID: 101, SeqNo: 7}},
		resume:      library.PlaybackState{ArtifactID: "impartus:v1:test", PositionSeconds: 42, DurationSeconds: 120},
		resumeFound: true,
		playback:    playback,
	}
	model := tui.New(context.Background(), backend)
	model = applyCommand(t, model, model.Init())
	model, command := update(t, model, key(tea.KeyEnter, ""))
	model = applyCommand(t, model, command)
	model, command = update(t, model, key(tea.KeyEnter, ""))
	model = applyCommand(t, model, command)
	model, command = update(t, model, key('y', "y"))
	model, command = applyCommandAndKeepNext(t, model, command)
	if command == nil {
		t.Fatal("playback start did not subscribe to mpv events")
	}

	playback.events <- player.Event{Name: "property-change", Property: "time-pos", Data: json.RawMessage("47.5")}
	model, command = applyCommandAndKeepNext(t, model, command)
	playback.events <- player.Event{Name: "property-change", Property: "duration", Data: json.RawMessage("120")}
	model, command = applyCommandAndKeepNext(t, model, command)
	if got := model.View().Content; !strings.Contains(got, "00:47 / 02:00") {
		t.Fatalf("playback telemetry view =\n%s", got)
	}
	for _, property := range []string{"time-pos", "duration"} {
		playback.events <- player.Event{Name: "property-change", Property: property, Data: json.RawMessage("null")}
		model, command = applyCommandAndKeepNext(t, model, command)
	}
	if got := model.View().Content; !strings.Contains(got, "00:47 / 02:00") {
		t.Fatalf("null playback telemetry reset durable state =\n%s", got)
	}

	playback.events <- player.Event{Name: "end-file", Reason: "eof"}
	model, command = applyCommandAndKeepNext(t, model, command)
	model = applyCommand(t, model, command)
	if playback.closed != 1 {
		t.Fatalf("playback close count = %d, want 1", playback.closed)
	}
	if len(backend.recorded) != 1 || !backend.recorded[0].Completed || backend.recorded[0].PositionSeconds != 47.5 {
		t.Fatalf("recorded playback = %+v", backend.recorded)
	}
	if !strings.Contains(model.View().Content, "Consensus and Raft") {
		t.Fatalf("completion did not return to lectures:\n%s", model.View().Content)
	}
}

func TestEarlyPlaybackTerminationPreservesResumeCheckpoint(t *testing.T) {
	for _, test := range []struct {
		name  string
		event player.Event
	}{
		{name: "stop", event: player.Event{Name: "end-file", Reason: "stop"}},
		{name: "quit", event: player.Event{Name: "end-file", Reason: "quit"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			playback := &fakePlayback{events: make(chan player.Event, 1)}
			backend := &fakeBackend{
				courses:     client.Courses{{SubjectName: "Course", SubjectID: 11, SessionID: 22}},
				lectures:    client.Lectures{{Topic: "Lecture", TTID: 101}},
				resume:      library.PlaybackState{ArtifactID: "impartus:v1:test", PositionSeconds: 42, DurationSeconds: 120},
				resumeFound: true,
				playback:    playback,
			}
			model := tui.New(context.Background(), backend)
			model = applyCommand(t, model, model.Init())
			model, command := update(t, model, key(tea.KeyEnter, ""))
			model = applyCommand(t, model, command)
			model, command = update(t, model, key(tea.KeyEnter, ""))
			model = applyCommand(t, model, command)
			model, command = update(t, model, key('y', "y"))
			model, eventCommand := applyCommandAndKeepNext(t, model, command)
			playback.events <- test.event
			model, finishCommand := applyCommandAndKeepNext(t, model, eventCommand)
			model = applyCommand(t, model, finishCommand)
			if len(backend.recorded) != 1 || backend.recorded[0].Completed || backend.recorded[0].PositionSeconds != 42 {
				t.Fatalf("early-exit checkpoint = %+v, want one incomplete resume at 42s", backend.recorded)
			}
			if got := model.View().Content; !strings.Contains(got, "Playback stopped") {
				t.Fatalf("early-exit status =\n%s", got)
			}
		})
	}
}

func TestStaleEOFPropertyDoesNotCompletePlayback(t *testing.T) {
	playback := &fakePlayback{events: make(chan player.Event, 2)}
	backend := &fakeBackend{
		courses:     client.Courses{{SubjectName: "Course", SubjectID: 11, SessionID: 22}},
		lectures:    client.Lectures{{Topic: "Lecture", TTID: 101}},
		resume:      library.PlaybackState{ArtifactID: "impartus:v1:test", PositionSeconds: 42, DurationSeconds: 120},
		resumeFound: true,
		playback:    playback,
	}
	model := tui.New(context.Background(), backend)
	model = applyCommand(t, model, model.Init())
	model, command := update(t, model, key(tea.KeyEnter, ""))
	model = applyCommand(t, model, command)
	model, command = update(t, model, key(tea.KeyEnter, ""))
	model = applyCommand(t, model, command)
	model, command = update(t, model, key('y', "y"))
	model, eventCommand := applyCommandAndKeepNext(t, model, command)

	playback.events <- player.Event{Name: "property-change", Property: "eof-reached", Data: json.RawMessage("true")}
	model, eventCommand = applyCommandAndKeepNext(t, model, eventCommand)
	if len(backend.recorded) != 0 || !strings.Contains(model.View().Content, "Playing Lecture") {
		t.Fatalf("stale EOF ended playback: recorded=%+v view=\n%s", backend.recorded, model.View().Content)
	}

	playback.events <- player.Event{Name: "end-file", Reason: "stop"}
	model, finishCommand := applyCommandAndKeepNext(t, model, eventCommand)
	applyCommand(t, model, finishCommand)
	if len(backend.recorded) != 1 || backend.recorded[0].Completed {
		t.Fatalf("verified stop checkpoint = %+v, want one incomplete record", backend.recorded)
	}
}

func TestPlaybackEventChannelClosureSurfacesWaitFailure(t *testing.T) {
	playback := &fakePlayback{events: make(chan player.Event), waitErr: errors.New("mpv IPC disconnected")}
	backend := &fakeBackend{
		courses:     client.Courses{{SubjectName: "Course", SubjectID: 11, SessionID: 22}},
		lectures:    client.Lectures{{Topic: "Lecture", TTID: 101}},
		resume:      library.PlaybackState{ArtifactID: "impartus:v1:test", PositionSeconds: 42, DurationSeconds: 120},
		resumeFound: true,
		playback:    playback,
	}
	model := tui.New(context.Background(), backend)
	model = applyCommand(t, model, model.Init())
	model, command := update(t, model, key(tea.KeyEnter, ""))
	model = applyCommand(t, model, command)
	model, command = update(t, model, key(tea.KeyEnter, ""))
	model = applyCommand(t, model, command)
	model, command = update(t, model, key('y', "y"))
	model, eventCommand := applyCommandAndKeepNext(t, model, command)
	close(playback.events)
	model, finishCommand := applyCommandAndKeepNext(t, model, eventCommand)
	model = applyCommand(t, model, finishCommand)
	if len(backend.recorded) != 1 || backend.recorded[0].Completed {
		t.Fatalf("closed-channel checkpoint = %+v, want one incomplete checkpoint", backend.recorded)
	}
	if got := model.View().Content; !strings.Contains(got, "mpv IPC disconnected") {
		t.Fatalf("closed-channel failure was hidden:\n%s", got)
	}
}

func TestCtrlCQuitsWhileFilterHasFocus(t *testing.T) {
	backend := &fakeBackend{courses: client.Courses{{SubjectName: "Course"}}}
	model := tui.New(context.Background(), backend)
	model = applyCommand(t, model, model.Init())
	model, _ = update(t, model, key('/', "/"))
	_, command := update(t, model, tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
	if command == nil {
		t.Fatal("ctrl+c while filtering did not quit")
	}
	if _, ok := command().(tea.QuitMsg); !ok {
		t.Fatal("ctrl+c while filtering did not return tea.Quit")
	}
}

func TestRapidPlaybackControlsAreSerializedFromPendingState(t *testing.T) {
	playback := &fakePlayback{events: make(chan player.Event)}
	backend := &fakeBackend{
		courses:     client.Courses{{SubjectName: "Course", SubjectID: 11, SessionID: 22}},
		lectures:    client.Lectures{{Topic: "Lecture", TTID: 101}},
		resume:      library.PlaybackState{ArtifactID: "impartus:v1:test", PositionSeconds: 1},
		resumeFound: true,
		playback:    playback,
	}
	model := tui.New(context.Background(), backend)
	model = applyCommand(t, model, model.Init())
	model, command := update(t, model, key(tea.KeyEnter, ""))
	model = applyCommand(t, model, command)
	model, command = update(t, model, key(tea.KeyEnter, ""))
	model = applyCommand(t, model, command)
	model, command = update(t, model, key('y', "y"))
	model, _ = applyCommandAndKeepNext(t, model, command)

	model, first := update(t, model, key(tea.KeySpace, ""))
	model, second := update(t, model, key(tea.KeySpace, ""))
	if first == nil || second != nil {
		t.Fatalf("rapid controls first=%v second=%v, want one active command and one queued request", first != nil, second != nil)
	}
	model, second = applyCommandAndKeepNext(t, model, first)
	if second == nil {
		t.Fatal("first control completion did not start queued control")
	}
	_ = applyCommand(t, model, second)
	if !reflect.DeepEqual(playback.paused, []bool{true, false}) {
		t.Fatalf("rapid pause requests = %v, want [true false]", playback.paused)
	}
}

func TestPlaybackFailurePersistsAnIncompleteCheckpoint(t *testing.T) {
	playback := &fakePlayback{events: make(chan player.Event, 1), waitErr: errors.New("mpv playback failed")}
	backend := &fakeBackend{
		courses:     client.Courses{{SubjectName: "Course", SubjectID: 11, SessionID: 22}},
		lectures:    client.Lectures{{Topic: "Lecture", TTID: 101}},
		resume:      library.PlaybackState{ArtifactID: "impartus:v1:test", PositionSeconds: 12, DurationSeconds: 120},
		resumeFound: true,
		playback:    playback,
	}
	model := tui.New(context.Background(), backend)
	model = applyCommand(t, model, model.Init())
	model, command := update(t, model, key(tea.KeyEnter, ""))
	model = applyCommand(t, model, command)
	model, command = update(t, model, key(tea.KeyEnter, ""))
	model = applyCommand(t, model, command)
	model, command = update(t, model, key('y', "y"))
	model, command = applyCommandAndKeepNext(t, model, command)
	playback.events <- player.Event{Name: "end-file", Reason: "error"}
	model, command = applyCommandAndKeepNext(t, model, command)
	model = applyCommand(t, model, command)
	if len(backend.recorded) != 1 || backend.recorded[0].Completed {
		t.Fatalf("failed playback checkpoint = %+v", backend.recorded)
	}
	if got := model.View().Content; !strings.Contains(got, "mpv playback failed") {
		t.Fatalf("failed playback view =\n%s", got)
	}
}

func TestPlaybackStopIsSingleShotAndIgnoresTrailingEventsAndControls(t *testing.T) {
	playback := &fakePlayback{events: make(chan player.Event), closeEvents: true}
	backend := &fakeBackend{
		courses:     client.Courses{{SubjectName: "Course", SubjectID: 11, SessionID: 22}},
		lectures:    client.Lectures{{Topic: "Lecture", TTID: 101}},
		resume:      library.PlaybackState{ArtifactID: "impartus:v1:test", PositionSeconds: 12, DurationSeconds: 120},
		resumeFound: true,
		playback:    playback,
	}
	model := tui.New(context.Background(), backend)
	model = applyCommand(t, model, model.Init())
	model, command := update(t, model, key(tea.KeyEnter, ""))
	model = applyCommand(t, model, command)
	model, command = update(t, model, key(tea.KeyEnter, ""))
	model = applyCommand(t, model, command)
	model, command = update(t, model, key('y', "y"))
	model, eventCommand := applyCommandAndKeepNext(t, model, command)

	model, finishCommand := update(t, model, key(tea.KeyEscape, ""))
	if finishCommand == nil {
		t.Fatal("escape did not begin playback teardown")
	}
	if got := model.View().Content; !strings.Contains(got, "Stopping playback") {
		t.Fatalf("playback teardown did not render progress:\n%s", got)
	}
	if _, controlCommand := update(t, model, key(tea.KeySpace, "")); controlCommand != nil {
		t.Fatal("playback control remained active during teardown")
	}
	if _, duplicateFinish := update(t, model, key(tea.KeyEscape, "")); duplicateFinish != nil {
		t.Fatal("second escape scheduled duplicate playback teardown")
	}
	model, quitCommand := update(t, model, key('q', "q"))
	if quitCommand != nil {
		t.Fatal("quit bypassed the in-flight playback checkpoint")
	}
	model, quitCommand = applyCommandAndKeepNext(t, model, finishCommand)
	if quitCommand == nil {
		t.Fatal("completed playback teardown did not exit the TUI")
	}
	if _, ok := quitCommand().(tea.QuitMsg); !ok {
		t.Fatal("completed playback teardown did not return tea.Quit")
	}
	model, staleCommand := update(t, model, eventCommand())
	if staleCommand != nil {
		t.Fatal("trailing closed-channel event scheduled duplicate playback teardown")
	}
	if playback.closed != 1 {
		t.Fatalf("playback close count = %d, want 1", playback.closed)
	}
	if len(backend.recorded) != 1 || backend.recorded[0].Completed {
		t.Fatalf("stopped playback checkpoint = %+v, want one incomplete record", backend.recorded)
	}
	view := model.View().Content
	if !strings.Contains(view, "Playback stopped") || strings.Contains(view, "Playback completed") {
		t.Fatalf("stopped playback status =\n%s", view)
	}
}

func TestNaturalCompletionRemainsCompletedWhenQuitRacesFinish(t *testing.T) {
	waitStarted := make(chan struct{})
	waitRelease := make(chan struct{})
	waitCanceled := make(chan struct{})
	playback := &fakePlayback{
		events:       make(chan player.Event, 1),
		waitStarted:  waitStarted,
		waitRelease:  waitRelease,
		waitCanceled: waitCanceled,
	}
	backend := &fakeBackend{
		courses:     client.Courses{{SubjectName: "Course", SubjectID: 11, SessionID: 22}},
		lectures:    client.Lectures{{Topic: "Lecture", TTID: 101}},
		resume:      library.PlaybackState{ArtifactID: "impartus:v1:test", PositionSeconds: 12, DurationSeconds: 120},
		resumeFound: true,
		playback:    playback,
	}
	model := tui.New(context.Background(), backend)
	model = applyCommand(t, model, model.Init())
	model, command := update(t, model, key(tea.KeyEnter, ""))
	model = applyCommand(t, model, command)
	model, command = update(t, model, key(tea.KeyEnter, ""))
	model = applyCommand(t, model, command)
	model, command = update(t, model, key('y', "y"))
	model, eventCommand := applyCommandAndKeepNext(t, model, command)
	playback.events <- player.Event{Name: "end-file", Reason: "eof"}
	model, finishCommand := applyCommandAndKeepNext(t, model, eventCommand)

	finishDone := make(chan tea.Msg, 1)
	go func() { finishDone <- finishCommand() }()
	select {
	case <-waitStarted:
	case <-time.After(time.Second):
		t.Fatal("natural completion wait did not start")
	}
	model, quitCommand := update(t, model, key('q', "q"))
	if quitCommand != nil {
		t.Fatal("quit bypassed natural completion teardown")
	}
	select {
	case <-waitCanceled:
		t.Fatal("quit canceled a natural completion already in teardown")
	case <-time.After(100 * time.Millisecond):
	}
	close(waitRelease)
	select {
	case message := <-finishDone:
		_, quitCommand = update(t, model, message)
		if quitCommand == nil {
			t.Fatal("natural completion teardown did not exit the TUI")
		}
	case <-time.After(time.Second):
		t.Fatal("natural completion did not finish")
	}
	if len(backend.recorded) != 1 || !backend.recorded[0].Completed {
		t.Fatalf("natural completion checkpoint = %+v, want completed", backend.recorded)
	}
}

func TestOverlayNavigationKeepsPrimaryReturnScreen(t *testing.T) {
	backend := &fakeBackend{courses: client.Courses{{SubjectName: "Available course"}}}
	model := tui.New(context.Background(), backend)
	model = applyCommand(t, model, model.Init())
	model, _ = update(t, model, key('!', "!"))
	model, command := update(t, model, key('l', "l"))
	model = applyCommand(t, model, command)
	model, _ = update(t, model, key(tea.KeyEscape, ""))
	model, _ = update(t, model, key(tea.KeyEscape, ""))
	if got := model.View().Content; !strings.Contains(got, "Available course") {
		t.Fatalf("overlay navigation did not return to courses:\n%s", got)
	}
}

func TestQuitCancelsInFlightDownload(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	canceled := make(chan struct{})
	t.Cleanup(func() { close(release) })
	backend := &fakeBackend{
		courses:          client.Courses{{SubjectName: "Course", SubjectID: 11, SessionID: 22}},
		lectures:         client.Lectures{{Topic: "Lecture", TTID: 101}},
		downloadStarted:  started,
		downloadRelease:  release,
		downloadCanceled: canceled,
	}
	model := tui.New(context.Background(), backend)
	model = applyCommand(t, model, model.Init())
	model, command := update(t, model, key(tea.KeyEnter, ""))
	model = applyCommand(t, model, command)
	model, downloadCommand := update(t, model, key('d', "d"))
	downloadDone := make(chan tea.Msg, 1)
	go func() { downloadDone <- downloadCommand() }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("download did not start")
	}
	_, quitCommand := update(t, model, key('q', "q"))
	if quitCommand == nil {
		t.Fatal("quit command = nil")
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("quit did not cancel in-flight download")
	}
	select {
	case <-downloadDone:
	case <-time.After(time.Second):
		t.Fatal("canceled download command did not return")
	}
}

func TestLoadingSerializesBackendOperations(t *testing.T) {
	backend := &fakeBackend{
		courses:  client.Courses{{SubjectName: "Course", SubjectID: 11, SessionID: 22}},
		lectures: client.Lectures{{Topic: "Lecture", TTID: 101}},
	}
	model := tui.New(context.Background(), backend)
	model = applyCommand(t, model, model.Init())
	model, command := update(t, model, key(tea.KeyEnter, ""))
	model = applyCommand(t, model, command)
	model, downloadCommand := update(t, model, key('d', "d"))
	if downloadCommand == nil {
		t.Fatal("download did not start")
	}

	model, libraryCommand := update(t, model, key('l', "l"))
	if libraryCommand != nil {
		t.Fatal("library load overlapped an in-flight download")
	}
	view := model.View().Content
	if !strings.Contains(view, "Loading") || strings.Contains(view, "Library") {
		t.Fatalf("in-flight download changed screen unexpectedly:\n%s", view)
	}
}

func TestQuitDuringStartClosesLatePlaybackSession(t *testing.T) {
	started := make(chan struct{})
	playback := &fakePlayback{events: make(chan player.Event)}
	backend := &fakeBackend{
		courses:          client.Courses{{SubjectName: "Course", SubjectID: 11, SessionID: 22}},
		lectures:         client.Lectures{{Topic: "Lecture", TTID: 101}},
		resume:           library.PlaybackState{ArtifactID: "impartus:v1:test", PositionSeconds: 12},
		resumeFound:      true,
		playback:         playback,
		startStarted:     started,
		startAfterCancel: true,
	}
	model := tui.New(context.Background(), backend)
	model = applyCommand(t, model, model.Init())
	model, command := update(t, model, key(tea.KeyEnter, ""))
	model = applyCommand(t, model, command)
	model, command = update(t, model, key(tea.KeyEnter, ""))
	model = applyCommand(t, model, command)
	model, startCommand := update(t, model, key('y', "y"))

	startDone := make(chan tea.Msg, 1)
	go func() { startDone <- startCommand() }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("playback start did not begin")
	}
	_, quitCommand := update(t, model, key('q', "q"))
	if quitCommand == nil {
		t.Fatal("quit while playback was starting did not exit the TUI")
	}
	select {
	case <-startDone:
	case <-time.After(time.Second):
		t.Fatal("playback start did not return after cancellation")
	}
	if playback.closed != 1 {
		t.Fatalf("late playback close count = %d, want 1", playback.closed)
	}
}

func TestRunWaitsForCanceledBackendOperationBeforeReturning(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	started := make(chan struct{})
	release := make(chan struct{})
	canceled := make(chan struct{})
	var releaseOnce sync.Once
	releaseBackend := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseBackend()
	backend := &fakeBackend{
		coursesStarted:  started,
		coursesRelease:  release,
		coursesCanceled: canceled,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- tui.Run(ctx, backend, strings.NewReader(""), io.Discard)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("TUI catalog operation did not start")
	}
	cancel()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("TUI lifecycle did not cancel catalog operation")
	}
	select {
	case err := <-result:
		t.Fatalf("Run returned before backend cleanup finished: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	releaseBackend()
	select {
	case err := <-result:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return after backend cleanup finished")
	}
}

func TestPlaybackRejectsMissingSessionFromBackend(t *testing.T) {
	backend := &fakeBackend{
		courses:  client.Courses{{SubjectName: "Course", SubjectID: 11, SessionID: 22}},
		lectures: client.Lectures{{Topic: "Lecture", TTID: 101}},
	}
	model := tui.New(context.Background(), backend)
	model = applyCommand(t, model, model.Init())
	model, command := update(t, model, key(tea.KeyEnter, ""))
	model = applyCommand(t, model, command)
	model, command = update(t, model, key(tea.KeyEnter, ""))
	model, command = applyCommandAndKeepNext(t, model, command)
	model = applyCommand(t, model, command)
	if got := model.View().Content; !strings.Contains(got, "playback session was not created") {
		t.Fatalf("missing playback session view =\n%s", got)
	}
}

func TestMalformedPlaybackTelemetryIsVisibleWithoutMutatingState(t *testing.T) {
	playback := &fakePlayback{events: make(chan player.Event, 1)}
	backend := &fakeBackend{
		courses:     client.Courses{{SubjectName: "Course", SubjectID: 11, SessionID: 22}},
		lectures:    client.Lectures{{Topic: "Lecture", TTID: 101}},
		resume:      library.PlaybackState{ArtifactID: "impartus:v1:test", PositionSeconds: 12, DurationSeconds: 120},
		resumeFound: true,
		playback:    playback,
	}
	model := tui.New(context.Background(), backend)
	model = applyCommand(t, model, model.Init())
	model, command := update(t, model, key(tea.KeyEnter, ""))
	model = applyCommand(t, model, command)
	model, command = update(t, model, key(tea.KeyEnter, ""))
	model = applyCommand(t, model, command)
	model, command = update(t, model, key('y', "y"))
	model, command = applyCommandAndKeepNext(t, model, command)
	playback.events <- player.Event{Name: "property-change", Property: "time-pos", Data: json.RawMessage("not-json")}
	model, _ = applyCommandAndKeepNext(t, model, command)
	if got := model.View().Content; !strings.Contains(got, "decode playback property time-pos") {
		t.Fatalf("malformed telemetry view =\n%s", got)
	}
}

func TestUserDownloadsLectureAndBrowsesTheLocalLibrary(t *testing.T) {
	manifest := artifact.Manifest{
		SchemaVersion: 1,
		ArtifactID:    "impartus:v1:downloaded",
		Lecture:       artifact.Lecture{TTID: 101, Topic: "Consensus and Raft", SeqNo: 7},
		Files:         []artifact.File{{Path: "/home/user/Lecture 7.mp4", Role: "video", View: "both", Container: "mp4", Bytes: 42}},
	}
	backend := &fakeBackend{
		courses:        client.Courses{{SubjectName: "Distributed Systems", SubjectID: 11, SessionID: 22}},
		lectures:       client.Lectures{{Topic: "Consensus and Raft", TTID: 101, SeqNo: 7}},
		downloadResult: app.DownloadResult{Manifest: manifest, LibraryRecorded: true},
		artifacts:      []library.ArtifactRecord{{Manifest: manifest}},
	}
	model := tui.New(context.Background(), backend)
	model = applyCommand(t, model, model.Init())
	model, command := update(t, model, key(tea.KeyEnter, ""))
	model = applyCommand(t, model, command)

	model, command = update(t, model, key('d', "d"))
	model = applyCommand(t, model, command)
	if len(backend.downloaded) != 1 || backend.downloaded[0].TTID != 101 {
		t.Fatalf("downloaded lectures = %+v", backend.downloaded)
	}
	if got := model.View().Content; !strings.Contains(got, "Downloaded Consensus and Raft") {
		t.Fatalf("download completion view =\n%s", got)
	}

	model, command = update(t, model, key('l', "l"))
	model = applyCommand(t, model, command)
	if got := model.View().Content; !strings.Contains(got, "Library") || !strings.Contains(got, "Consensus and Raft") {
		t.Fatalf("library view =\n%s", got)
	}
}

func TestResponsiveCourseViewsMatchGoldens(t *testing.T) {
	backend := &fakeBackend{courses: client.Courses{
		{SubjectName: "Distributed Systems", ProfessorName: "Leslie Lamport", VideoCount: 12},
		{SubjectName: "Linear Algebra", ProfessorName: "Gilbert Strang", VideoCount: 24},
	}}
	base := tui.New(context.Background(), backend)
	base = applyCommand(t, base, base.Init())
	for _, size := range []struct{ width, height int }{{40, 10}, {80, 24}, {140, 32}} {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			model, _ := update(t, base, tea.WindowSizeMsg{Width: size.width, Height: size.height})
			got := trimTrailingHorizontalSpace(ansi.Strip(model.View().Content))
			path := fmt.Sprintf("testdata/course_%dx%d.golden", size.width, size.height)
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}
			expected := strings.TrimSuffix(string(want), "\n")
			if got != expected {
				t.Fatalf("view mismatch for %dx%d\nGOT %q\nWANT %q", size.width, size.height, got, expected)
			}
		})
	}
}

func trimTrailingHorizontalSpace(value string) string {
	lines := strings.Split(value, "\n")
	for index := range lines {
		lines[index] = strings.TrimRight(lines[index], " \t")
	}
	return strings.Join(lines, "\n")
}

func TestCatalogErrorCanBeRetriedWithoutLeavingTheTUI(t *testing.T) {
	backend := &fakeBackend{coursesErr: errors.New("authentication expired")}
	model := tui.New(context.Background(), backend)
	model = applyCommand(t, model, model.Init())
	if got := model.View().Content; !strings.Contains(got, "authentication expired") {
		t.Fatalf("error view =\n%s", got)
	}

	backend.coursesErr = nil
	backend.courses = client.Courses{{SubjectName: "Recovered course"}}
	model, command := update(t, model, key('r', "r"))
	model = applyCommand(t, model, command)
	if got := model.View().Content; !strings.Contains(got, "Recovered course") {
		t.Fatalf("retry view =\n%s", got)
	}
}

func commandFromUpdate(t *testing.T, model tui.Model, message tea.Msg) tea.Cmd {
	t.Helper()
	_, command := update(t, model, message)
	return command
}

func update(t *testing.T, model tui.Model, message tea.Msg) (tui.Model, tea.Cmd) {
	t.Helper()
	updated, command := model.Update(message)
	next, ok := updated.(tui.Model)
	if !ok {
		t.Fatalf("Update returned %T, want tui.Model", updated)
	}
	return next, command
}

func applyCommand(t *testing.T, model tui.Model, command tea.Cmd) tui.Model {
	t.Helper()
	if command == nil {
		t.Fatal("expected command")
	}
	message := command()
	next, _ := update(t, model, message)
	return next
}

func applyCommandAndKeepNext(t *testing.T, model tui.Model, command tea.Cmd) (tui.Model, tea.Cmd) {
	t.Helper()
	if command == nil {
		t.Fatal("expected command")
	}
	return update(t, model, command())
}

func key(code rune, text string) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code, Text: text})
}
