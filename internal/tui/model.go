// Package tui implements the Bubble Tea frontend for the Impartus application
// service. It owns terminal state only; network, subprocess, and persistence
// work remain behind Backend.
package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/rabesss/impartus-cli/internal/app"
	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/library"
)

// Backend is the application seam exercised by the terminal UI. Production
// uses *app.Service; tests can substitute behavior without HTTP, mpv, or SQL.
type Backend interface {
	Courses(context.Context) (client.Courses, error)
	Lectures(context.Context, client.Course) (client.Lectures, error)
	StartLecture(context.Context, client.Lecture, float64) (app.PlaybackSession, error)
	DownloadLecture(context.Context, client.Lecture) (app.DownloadResult, error)
	Artifacts(context.Context) ([]library.ArtifactRecord, error)
	ResumeLecture(context.Context, client.Lecture) (library.PlaybackState, bool, error)
	RecordPlayback(context.Context, library.PlaybackState) error
}

type screen uint8

const (
	screenCourses screen = iota
	screenLectures
	screenLibrary
	screenResume
	screenPlayback
	screenDiagnostics
	screenDetails
)

// Diagnostic is one non-blocking dependency or local-state preflight result.
type Diagnostic struct {
	Name   string
	Status string
	Detail string
}

// Options supplies presentation-only startup context.
type Options struct {
	Diagnostics []Diagnostic
}

type playbackControlRequest struct {
	action string
	value  float64
	flag   bool
	run    func() error
}

// Model is the deterministic terminal state machine.
type Model struct {
	ctx        context.Context
	cancel     context.CancelFunc
	operations *operationTracker
	playbacks  *playbackOwner
	runtime    *runtimeState
	backend    Backend

	screen      screen
	courses     client.Courses
	lectures    client.Lectures
	artifacts   []library.ArtifactRecord
	diagnostics []Diagnostic
	course      client.Course
	lecture     client.Lecture
	cursor      int
	loading     bool
	err         error
	status      string
	returnTo    screen

	playback            app.PlaybackSession
	playbackLease       uint64
	playbackCtx         context.Context
	playbackCancel      context.CancelFunc
	playbackGeneration  uint64
	playbackFinishing   bool
	resume              library.PlaybackState
	paused              bool
	muted               bool
	volume              float64
	speed               float64
	position            float64
	duration            float64
	quitting            bool
	playbackControls    []playbackControlRequest
	playbackControlBusy bool

	width  int
	height int

	filter    textinput.Model
	filtering bool
	help      help.Model
	keys      keyMap

	watchLifecycle bool
}

// New constructs a terminal model over the supplied application backend.
func New(ctx context.Context, backend Backend) Model {
	return NewWithOptions(ctx, backend, Options{})
}

// NewWithOptions constructs a model with non-blocking dependency diagnostics.
func NewWithOptions(ctx context.Context, backend Backend, options Options) Model {
	if ctx == nil {
		ctx = context.Background()
	}
	lifecycle, cancel := context.WithCancel(ctx)
	filter := textinput.New()
	filter.Prompt = "Filter: "
	filter.Placeholder = "type to narrow results"
	filter.CharLimit = 120
	helpModel := help.New()
	return Model{
		ctx:         lifecycle,
		cancel:      cancel,
		operations:  &operationTracker{},
		playbacks:   &playbackOwner{},
		runtime:     &runtimeState{},
		backend:     backend,
		diagnostics: append([]Diagnostic(nil), options.Diagnostics...),
		screen:      screenCourses,
		loading:     true,
		filter:      filter,
		help:        helpModel,
		keys:        newKeyMap(),
		width:       80,
		height:      24,
		volume:      100,
		speed:       1,
	}
}

type operationTracker struct {
	mu      sync.Mutex
	wait    sync.WaitGroup
	stopped bool
}

type runtimeState struct {
	mu  sync.Mutex
	err error
}

func (state *runtimeState) recordOperationPanic() {
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.err == nil {
		state.err = errors.New("terminal operation failed unexpectedly")
	}
}

func (state *runtimeState) Err() error {
	if state == nil {
		return nil
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.err
}

type ownedPlayback struct {
	lease    uint64
	playback app.PlaybackSession
}

type playbackOwner struct {
	mu      sync.Mutex
	next    uint64
	active  *ownedPlayback
	stopped bool
}

func (owner *playbackOwner) adopt(playback app.PlaybackSession) (uint64, error) {
	if owner == nil {
		return 0, errors.New("playback owner is unavailable")
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.stopped {
		return 0, errors.New("playback owner is stopped")
	}
	if owner.active != nil {
		return 0, errors.New("another playback session is already active")
	}
	owner.next++
	owner.active = &ownedPlayback{lease: owner.next, playback: playback}
	return owner.next, nil
}

func (owner *playbackOwner) close(lease uint64) error {
	if owner == nil || lease == 0 {
		return nil
	}
	owner.mu.Lock()
	if owner.active == nil || owner.active.lease != lease {
		owner.mu.Unlock()
		return nil
	}
	playback := owner.active.playback
	owner.active = nil
	owner.mu.Unlock()
	return playback.Close(context.Background())
}

func (owner *playbackOwner) StopAndClose() error {
	if owner == nil {
		return nil
	}
	owner.mu.Lock()
	owner.stopped = true
	active := owner.active
	owner.active = nil
	owner.mu.Unlock()
	if active == nil {
		return nil
	}
	return active.playback.Close(context.Background())
}

func (tracker *operationTracker) command(run func() tea.Msg) tea.Cmd {
	if tracker == nil {
		return run
	}
	return func() tea.Msg {
		tracker.mu.Lock()
		if tracker.stopped {
			tracker.mu.Unlock()
			return nil
		}
		tracker.wait.Add(1)
		tracker.mu.Unlock()
		defer tracker.wait.Done()
		return run()
	}
}

func (tracker *operationTracker) StopAndWait() {
	if tracker == nil {
		return
	}
	tracker.mu.Lock()
	tracker.stopped = true
	tracker.mu.Unlock()
	tracker.wait.Wait()
}

func (model Model) command(run func() tea.Msg) tea.Cmd {
	return model.operations.command(func() (message tea.Msg) {
		defer func() {
			if recover() != nil {
				model.runtime.recordOperationPanic()
				message = fatalOperationMsg{}
			}
		}()
		return run()
	})
}

func (model Model) loadResume(lecture client.Lecture) tea.Cmd {
	return model.command(func() tea.Msg {
		state, found, err := model.backend.ResumeLecture(model.ctx, lecture)
		return resumeLoadedMsg{lecture: lecture, state: state, found: found, err: err}
	})
}

func (model Model) startLecture(lecture client.Lecture, state library.PlaybackState) tea.Cmd {
	return model.command(func() tea.Msg {
		playback, err := model.backend.StartLecture(model.ctx, lecture, state.PositionSeconds)
		if nilPlaybackSession(playback) {
			return playbackStartedMsg{lecture: lecture, resume: state, playback: playback, err: err}
		}
		if err != nil {
			closeErr := playback.Close(context.Background())
			return playbackStartedMsg{lecture: lecture, resume: state, err: errors.Join(err, closeErr)}
		}
		lease, ownerErr := model.playbacks.adopt(playback)
		if ownerErr != nil {
			closeErr := playback.Close(context.Background())
			return playbackStartedMsg{lecture: lecture, resume: state, err: errors.Join(ownerErr, closeErr)}
		}
		if contextErr := model.ctx.Err(); contextErr != nil {
			closeErr := model.playbacks.close(lease)
			return playbackStartedMsg{lecture: lecture, resume: state, err: errors.Join(contextErr, closeErr)}
		}
		return playbackStartedMsg{lecture: lecture, resume: state, playback: playback, lease: lease}
	})
}

func (model Model) enqueuePlaybackControl(action string, value float64, flag bool, run func() error) (Model, tea.Cmd) {
	model.playbackControls = append(model.playbackControls, playbackControlRequest{action: action, value: value, flag: flag, run: run})
	if model.playbackControlBusy {
		return model, nil
	}
	model.playbackControlBusy = true
	return model, model.runNextPlaybackControl()
}

func (model Model) runNextPlaybackControl() tea.Cmd {
	if len(model.playbackControls) == 0 {
		return nil
	}
	request := model.playbackControls[0]
	generation := model.playbackGeneration
	return model.command(func() tea.Msg {
		return playbackControlMsg{
			generation: generation,
			action:     request.action,
			value:      request.value,
			flag:       request.flag,
			err:        request.run(),
		}
	})
}

func (model Model) pendingControlFlag(action string, fallback bool) bool {
	for index := len(model.playbackControls) - 1; index >= 0; index-- {
		if model.playbackControls[index].action == action {
			return model.playbackControls[index].flag
		}
	}
	return fallback
}

func (model Model) pendingControlValue(action string, fallback float64) float64 {
	for index := len(model.playbackControls) - 1; index >= 0; index-- {
		if model.playbackControls[index].action == action {
			return model.playbackControls[index].value
		}
	}
	return fallback
}

func (model Model) downloadLecture(lecture client.Lecture) tea.Cmd {
	return model.command(func() tea.Msg {
		result, err := model.backend.DownloadLecture(model.ctx, lecture)
		return downloadFinishedMsg{lecture: lecture, result: result, err: err}
	})
}

func (model Model) loadArtifacts() tea.Cmd {
	return model.command(func() tea.Msg {
		artifacts, err := model.backend.Artifacts(model.ctx)
		return artifactsLoadedMsg{artifacts: artifacts, err: err}
	})
}

func (model Model) waitPlaybackEvent() tea.Cmd {
	playback := model.playback
	ctx := model.playbackCtx
	generation := model.playbackGeneration
	return model.command(func() tea.Msg {
		select {
		case event, open := <-playback.Events():
			return playbackEventMsg{generation: generation, event: event, open: open}
		case <-ctx.Done():
			return playbackEventMsg{generation: generation, canceled: true}
		}
	})
}

func (model Model) finishPlayback(completed, observedTerminal bool) tea.Cmd {
	playback := model.playback
	lease := model.playbackLease
	generation := model.playbackGeneration
	state := library.PlaybackState{
		ArtifactID:      strings.TrimSpace(model.resume.ArtifactID),
		PositionSeconds: model.position,
		DurationSeconds: model.duration,
		Completed:       completed,
		LastPlayedAt:    time.Now().UTC(),
	}
	return model.command(func() tea.Msg {
		var waitErr error
		if observedTerminal {
			// The terminal may quit after mpv has already emitted its terminal
			// event. Preserve that observed completion while teardown drains.
			waitErr = playback.WaitForEnd(context.WithoutCancel(model.ctx))
		}
		state.Completed = observedTerminal && completed && waitErr == nil
		closeErr := model.playbacks.close(lease)
		if model.playbacks == nil || lease == 0 {
			closeErr = playback.Close(context.Background())
		}
		var recordErr error
		if state.ArtifactID != "" {
			recordErr = model.backend.RecordPlayback(context.Background(), state)
		}
		return playbackFinishedMsg{generation: generation, state: state, err: errors.Join(waitErr, closeErr, recordErr)}
	})
}

// Init loads the live course catalog asynchronously. Run also watches the
// caller lifecycle and translates cancellation into a graceful Bubble Tea
// quit, avoiding its abrupt external-context terminal teardown path.
func (model Model) Init() tea.Cmd {
	load := model.loadCourses()
	if !model.watchLifecycle {
		return load
	}
	return tea.Batch(load, func() tea.Msg {
		<-model.ctx.Done()
		return lifecycleCanceledMsg{}
	})
}

func (model Model) loadCourses() tea.Cmd {
	return model.command(func() tea.Msg {
		if model.backend == nil {
			return coursesLoadedMsg{err: errBackendUnavailable}
		}
		courses, err := model.backend.Courses(model.ctx)
		return coursesLoadedMsg{courses: courses, err: err}
	})
}

func (model Model) loadLectures(course client.Course) tea.Cmd {
	return model.command(func() tea.Msg {
		if model.backend == nil {
			return lecturesLoadedMsg{course: course, err: errBackendUnavailable}
		}
		lectures, err := model.backend.Lectures(model.ctx, course)
		return lecturesLoadedMsg{course: course, lectures: lectures, err: err}
	})
}
