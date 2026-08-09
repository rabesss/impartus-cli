package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/player"
)

var errBackendUnavailable = errors.New("impartus application service is unavailable")

// Update applies one terminal or application event.
func (model Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case lifecycleCanceledMsg:
		return model.quit()
	case fatalOperationMsg:
		model.quitting = true
		if model.cancel != nil {
			model.cancel()
		}
		return model, tea.Quit
	case coursesLoadedMsg:
		return model.updateCoursesLoaded(message)
	case lecturesLoadedMsg:
		return model.updateLecturesLoaded(message)
	case resumeLoadedMsg:
		return model.updateResumeLoaded(message)
	case playbackStartedMsg:
		return model.updatePlaybackStarted(message)
	case playbackEventMsg:
		return model.updatePlaybackEvent(message)
	case playbackFinishedMsg:
		return model.updatePlaybackFinished(message)
	case downloadFinishedMsg:
		return model.updateDownloadFinished(message)
	case artifactsLoadedMsg:
		return model.updateArtifactsLoaded(message)
	case playbackControlMsg:
		return model.updatePlaybackControl(message)
	case tea.WindowSizeMsg:
		return model.updateWindowSize(message)
	case tea.KeyPressMsg:
		return model.updateKey(message)
	default:
		return model, nil
	}
}

func (model Model) updateCoursesLoaded(message coursesLoadedMsg) (tea.Model, tea.Cmd) {
	model.loading = false
	model.err = message.err
	model.courses = message.courses
	model.cursor = 0
	return model, nil
}

func (model Model) updateLecturesLoaded(message lecturesLoadedMsg) (tea.Model, tea.Cmd) {
	model.loading = false
	model.err = message.err
	if message.err == nil {
		model.screen = screenLectures
		model.course = message.course
		model.lectures = message.lectures
		model.cursor = 0
	}
	return model, nil
}

func (model Model) updateResumeLoaded(message resumeLoadedMsg) (tea.Model, tea.Cmd) {
	model.loading = false
	model.err = message.err
	if message.err != nil {
		return model, nil
	}
	model.lecture = message.lecture
	model.resume = message.state
	if message.found && message.state.PositionSeconds > 0 && !message.state.Completed {
		model.screen = screenResume
		return model, nil
	}
	model.loading = true
	message.state.PositionSeconds = 0
	message.state.Completed = false
	return model, model.startLecture(message.lecture, message.state)
}

func (model Model) updatePlaybackStarted(message playbackStartedMsg) (tea.Model, tea.Cmd) {
	model.loading = false
	model.err = message.err
	if message.err != nil {
		model.screen = screenLectures
		return model, nil
	}
	if nilPlaybackSession(message.playback) {
		model.screen = screenLectures
		model.err = errors.New("playback session was not created")
		return model, nil
	}
	model.screen = screenPlayback
	model.playbackGeneration++
	model.playbackCtx, model.playbackCancel = context.WithCancel(model.ctx)
	model.playbackFinishing = false
	model.lecture = message.lecture
	model.resume = message.resume
	model.playback = message.playback
	model.playbackLease = message.lease
	model.position = message.resume.PositionSeconds
	model.duration = message.resume.DurationSeconds
	model.status = "Playback started in mpv"
	return model, model.waitPlaybackEvent()
}

func nilPlaybackSession(playback any) bool {
	if playback == nil {
		return true
	}
	value := reflect.ValueOf(playback)
	kind := value.Kind()
	nilCapable := kind == reflect.Chan || kind == reflect.Func || kind == reflect.Interface ||
		kind == reflect.Map || kind == reflect.Pointer || kind == reflect.Slice
	return nilCapable && value.IsNil()
}

func (model Model) updatePlaybackEvent(message playbackEventMsg) (tea.Model, tea.Cmd) {
	if message.generation != model.playbackGeneration || model.playback == nil || model.playbackFinishing {
		return model, nil
	}
	if message.canceled {
		return model.beginPlaybackFinish(false)
	}
	if !message.open {
		return model.beginPlaybackFinish(true)
	}
	if model.applyPlaybackEvent(message.event) {
		return model.beginPlaybackFinish(true)
	}
	return model, model.waitPlaybackEvent()
}

func (model Model) updatePlaybackFinished(message playbackFinishedMsg) (tea.Model, tea.Cmd) {
	if message.generation != model.playbackGeneration || !model.playbackFinishing {
		return model, nil
	}
	model.loading = false
	model.playback = nil
	model.playbackLease = 0
	model.playbackCtx = nil
	model.playbackCancel = nil
	model.playbackFinishing = false
	model.screen = screenLectures
	model.err = message.err
	if message.err == nil {
		if message.state.Completed {
			model.status = "Playback completed"
		} else {
			model.status = "Playback stopped"
		}
	}
	if model.quitting {
		return model, tea.Quit
	}
	return model, nil
}

func (model Model) updateDownloadFinished(message downloadFinishedMsg) (tea.Model, tea.Cmd) {
	model.loading = false
	model.err = message.err
	if message.err != nil {
		return model, nil
	}
	model.status = "Downloaded " + message.lecture.Topic
	if message.result.Warning != "" {
		model.status += " — " + message.result.Warning
	}
	return model, nil
}

func (model Model) updateArtifactsLoaded(message artifactsLoadedMsg) (tea.Model, tea.Cmd) {
	model.loading = false
	model.err = message.err
	if message.err == nil {
		model.artifacts = message.artifacts
		model.screen = screenLibrary
		model.cursor = 0
	}
	return model, nil
}

func (model Model) updatePlaybackControl(message playbackControlMsg) (tea.Model, tea.Cmd) {
	if message.generation != model.playbackGeneration || model.playback == nil || model.playbackFinishing {
		return model, nil
	}
	model.err = message.err
	if message.err != nil {
		return model, nil
	}
	switch message.action {
	case "pause":
		model.paused = message.flag
	case "mute":
		model.muted = message.flag
	case "seek":
		model.position = max(0, model.position+message.value)
	case "volume":
		model.volume = message.value
	case "speed":
		model.speed = message.value
	case "camera":
	}
	return model, nil
}

func (model Model) updateWindowSize(message tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	model.width = max(1, message.Width)
	model.height = max(1, message.Height)
	model.help.SetWidth(model.width)
	model.filter.SetWidth(max(1, model.width-12))
	return model, nil
}

func (model *Model) applyPlaybackEvent(event player.Event) bool {
	if event.Name == "end-file" {
		return event.Reason != "redirect"
	}
	if event.Name != "property-change" {
		return false
	}
	var target any
	switch event.Property {
	case "time-pos":
		target = &model.position
	case "duration":
		target = &model.duration
	case "pause":
		target = &model.paused
	case "mute":
		target = &model.muted
	case "volume":
		target = &model.volume
	case "speed":
		target = &model.speed
	case "eof-reached":
		var reached bool
		if err := json.Unmarshal(event.Data, &reached); err != nil {
			model.err = fmt.Errorf("decode playback property %s: %w", event.Property, err)
			return false
		}
		return reached
	default:
		return false
	}
	if err := json.Unmarshal(event.Data, target); err != nil {
		model.err = fmt.Errorf("decode playback property %s: %w", event.Property, err)
	}
	return false
}

func (model Model) updateKey(message tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if model.filtering {
		return model.updateFilterKey(message)
	}
	key := message.String()
	if updated, command, handled := model.updateGlobalKey(key); handled {
		return updated, command
	}

	switch model.screen {
	case screenCourses:
		return model.updateCoursesKey(key)
	case screenLectures:
		return model.updateLecturesKey(key)
	case screenResume:
		return model.updateResumeKey(key)
	case screenPlayback:
		return model.updatePlaybackKey(key)
	case screenLibrary, screenDiagnostics, screenDetails:
		return model, nil
	}
	return model, nil
}

func (model Model) updateGlobalKey(key string) (Model, tea.Cmd, bool) {
	switch key {
	case "q", "ctrl+c":
		updated, command := model.quit()
		return updated, command, true
	}
	if model.loading {
		return model, nil, true
	}
	switch key {
	case "up", "k":
		return model.moveCursor(-1), nil, true
	case "down", "j":
		return model.moveCursor(1), nil, true
	case "/":
		updated, command := model.startFiltering()
		return updated, command, true
	case "r":
		updated, command := model.retry()
		return updated, command, true
	case "l":
		updated, command := model.openLibrary()
		return updated, command, true
	case "!":
		return model.openDiagnostics(), nil, true
	case "esc", "backspace":
		updated, command := model.goBack()
		return updated, command, true
	}
	return model, nil, false
}

func (model Model) updateFilterKey(message tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch message.String() {
	case "enter":
		model.filtering = false
		model.filter.Blur()
		model.cursor = 0
		return model, nil
	case "esc":
		model.filtering = false
		model.filter.Blur()
		return model, nil
	default:
		previous := model.filter.Value()
		filter, command := model.filter.Update(message)
		model.filter = filter
		if previous != model.filter.Value() {
			model.cursor = 0
		}
		return model, command
	}
}

func (model Model) quit() (Model, tea.Cmd) {
	model.quitting = true
	if model.playback != nil {
		if model.playbackFinishing {
			return model, nil
		}
		return model.beginPlaybackFinish(false)
	}
	if model.cancel != nil {
		model.cancel()
	}
	return model, tea.Quit
}

func (model Model) beginPlaybackFinish(completed bool) (Model, tea.Cmd) {
	if model.playback == nil || model.playbackFinishing {
		return model, nil
	}
	model.playbackFinishing = true
	if model.playbackCancel != nil {
		model.playbackCancel()
	}
	return model, model.finishPlayback(completed)
}

func (model Model) moveCursor(delta int) Model {
	next := model.cursor + delta
	if next >= 0 && next < model.itemCount() {
		model.cursor = next
	}
	return model
}

func (model Model) startFiltering() (Model, tea.Cmd) {
	if model.screen != screenCourses && model.screen != screenLectures {
		return model, nil
	}
	model.filtering = true
	return model, model.filter.Focus()
}

func (model Model) retry() (Model, tea.Cmd) {
	switch model.screen {
	case screenCourses:
		model.loading = true
		model.err = nil
		return model, model.loadCourses()
	case screenLectures:
		model.loading = true
		model.err = nil
		return model, model.loadLectures(model.course)
	case screenLibrary:
		model.loading = true
		model.err = nil
		return model, model.loadArtifacts()
	case screenResume, screenPlayback, screenDiagnostics, screenDetails:
		return model, nil
	}
	return model, nil
}

func (model Model) openLibrary() (Model, tea.Cmd) {
	if model.screen == screenPlayback || model.screen == screenResume {
		return model, nil
	}
	model = model.rememberPrimaryReturnScreen()
	model.loading = true
	model.err = nil
	return model, model.loadArtifacts()
}

func (model Model) openDiagnostics() Model {
	if model.screen == screenPlayback || model.screen == screenResume {
		return model
	}
	model = model.rememberPrimaryReturnScreen()
	model.screen = screenDiagnostics
	model.cursor = 0
	model.err = nil
	return model
}

func (model Model) rememberPrimaryReturnScreen() Model {
	if model.screen == screenCourses || model.screen == screenLectures {
		model.returnTo = model.screen
	}
	return model
}

func (model Model) goBack() (Model, tea.Cmd) {
	switch model.screen {
	case screenCourses:
		return model, nil
	case screenLectures:
		model.screen = screenCourses
		model.cursor = 0
		model.err = nil
		model.filter.Reset()
		model.filtering = false
	case screenLibrary, screenDiagnostics:
		model.screen = model.returnTo
		model.cursor = 0
		model.err = nil
	case screenResume:
		model.screen = screenLectures
		model.err = nil
	case screenPlayback:
		return model.beginPlaybackFinish(false)
	case screenDetails:
		model.screen = model.returnTo
	}
	return model, nil
}

func (model Model) updateCoursesKey(key string) (tea.Model, tea.Cmd) {
	if key != "enter" || model.loading || model.err != nil {
		return model, nil
	}
	visible := model.visibleCourses()
	if model.cursor >= len(visible) {
		return model, nil
	}
	course := visible[model.cursor]
	model.filter.Reset()
	model.filtering = false
	model.loading = true
	model.err = nil
	return model, model.loadLectures(course)
}

func (model Model) updateLecturesKey(key string) (tea.Model, tea.Cmd) {
	visible := model.visibleLectures()
	if model.loading || model.err != nil || model.cursor >= len(visible) {
		return model, nil
	}
	lecture := visible[model.cursor]
	switch key {
	case "enter":
		model.loading = true
		model.err = nil
		return model, model.loadResume(lecture)
	case "d":
		model.loading = true
		model.err = nil
		model.status = "Downloading " + lecture.Topic
		return model, model.downloadLecture(lecture)
	case "i":
		model.lecture = lecture
		model.returnTo = model.screen
		model.screen = screenDetails
	}
	return model, nil
}

func (model Model) updateResumeKey(key string) (tea.Model, tea.Cmd) {
	if model.loading || model.err != nil {
		return model, nil
	}
	switch key {
	case "enter", "y":
		model.loading = true
		return model, model.startLecture(model.lecture, model.resume)
	case "n":
		model.loading = true
		state := model.resume
		state.PositionSeconds = 0
		state.Completed = false
		return model, model.startLecture(model.lecture, state)
	default:
		return model, nil
	}
}

func (model Model) updatePlaybackKey(key string) (tea.Model, tea.Cmd) {
	if model.playback == nil || model.playbackFinishing {
		return model, nil
	}
	switch key {
	case "space":
		paused := !model.paused
		return model, model.playbackControl("pause", 0, paused, func() error {
			return model.playback.Pause(model.playbackCtx, paused)
		})
	case "left":
		return model, model.playbackControl("seek", -10, false, func() error {
			return model.playback.SeekRelative(model.playbackCtx, -10)
		})
	case "right":
		return model, model.playbackControl("seek", 10, false, func() error {
			return model.playback.SeekRelative(model.playbackCtx, 10)
		})
	case "m":
		muted := !model.muted
		return model, model.playbackControl("mute", 0, muted, func() error {
			return model.playback.SetMute(model.playbackCtx, muted)
		})
	case "+", "=":
		volume := min(130, model.volume+5)
		return model, model.playbackControl("volume", volume, false, func() error {
			return model.playback.SetVolume(model.playbackCtx, volume)
		})
	case "-":
		volume := max(0, model.volume-5)
		return model, model.playbackControl("volume", volume, false, func() error {
			return model.playback.SetVolume(model.playbackCtx, volume)
		})
	case "]":
		speed := min(4, model.speed+0.25)
		return model, model.playbackControl("speed", speed, false, func() error {
			return model.playback.SetSpeed(model.playbackCtx, speed)
		})
	case "[":
		speed := max(0.25, model.speed-0.25)
		return model, model.playbackControl("speed", speed, false, func() error {
			return model.playback.SetSpeed(model.playbackCtx, speed)
		})
	case "v":
		return model, model.playbackControl("camera", 0, false, func() error {
			return model.playback.CycleVideo(model.playbackCtx)
		})
	default:
		return model, nil
	}
}

func (model Model) itemCount() int {
	switch model.screen {
	case screenCourses:
		return len(model.visibleCourses())
	case screenLectures:
		return len(model.visibleLectures())
	case screenLibrary:
		return len(model.artifacts)
	case screenDiagnostics:
		return len(model.diagnostics)
	case screenResume, screenPlayback, screenDetails:
		return 0
	}
	return 0
}

func (model Model) visibleCourses() client.Courses {
	return filterVisible(model.courses, model.filter.Value(), func(course client.Course) string {
		return strings.Join([]string{course.SubjectName, course.ProfessorName, course.SessionName, course.Department}, " ")
	})
}

func (model Model) visibleLectures() client.Lectures {
	return filterVisible(model.lectures, model.filter.Value(), func(lecture client.Lecture) string {
		return strings.Join([]string{lecture.Topic, lecture.ProfessorName, lecture.SubjectName, lecture.StartTime}, " ")
	})
}

func filterVisible[S ~[]E, E any](items S, filter string, searchable func(E) string) S {
	query := strings.ToLower(strings.TrimSpace(filter))
	if query == "" {
		return items
	}
	visible := make(S, 0, len(items))
	for _, item := range items {
		if strings.Contains(strings.ToLower(searchable(item)), query) {
			visible = append(visible, item)
		}
	}
	return visible
}
