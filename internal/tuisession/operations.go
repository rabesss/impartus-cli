package tuisession

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/rabesss/impartus-cli/internal/app"
	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/library"
	"github.com/rabesss/impartus-cli/internal/player"
	"github.com/rabesss/impartus-cli/internal/tuiproto"
)

const (
	defaultSelfTestSteps = 3
	maxOperations        = 128
)

var (
	errOperationNotFound = errors.New("operation not found")
	errTooManyOperations = errors.New("too many operations")
	errRegistryStopping  = errors.New("operation registry is stopping")
)

type operationEntry struct {
	operation tuiproto.Operation
	cancel    context.CancelFunc
	done      chan struct{}
	playback  app.PlaybackSession
}

type operationRegistry struct {
	ctx     context.Context
	events  *hub
	options SelfTestOptions
	actions Actions

	mu         sync.Mutex
	operations map[string]*operationEntry
	stopping   bool
	wait       sync.WaitGroup
}

type progressiveDownloadActions interface {
	DownloadLectureWithProgress(context.Context, client.Lecture, func(float64)) (app.DownloadResult, error)
}

func newOperationRegistry(ctx context.Context, events *hub, options SelfTestOptions, actions Actions) *operationRegistry {
	if options.Steps <= 0 {
		options.Steps = defaultSelfTestSteps
	}
	return &operationRegistry{
		ctx:        ctx,
		events:     events,
		options:    options,
		actions:    actions,
		operations: make(map[string]*operationEntry),
	}
}

func (registry *operationRegistry) startSelfTest() (tuiproto.Operation, error) {
	ctx, entry, operation, err := registry.register(tuiproto.OperationKindSelftest)
	if err != nil {
		return tuiproto.Operation{}, err
	}
	go registry.runSelfTest(ctx, entry)
	return operation, nil
}

func (registry *operationRegistry) startDownload(lecture client.Lecture) (tuiproto.Operation, error) {
	if registry.actions == nil {
		return tuiproto.Operation{}, errors.New("download action is unavailable")
	}
	ctx, entry, operation, err := registry.register(tuiproto.OperationKindDownload)
	if err != nil {
		return tuiproto.Operation{}, err
	}
	go registry.runDownload(ctx, entry, lecture)
	return operation, nil
}

func (registry *operationRegistry) startPlayback(lecture client.Lecture, resume bool) (tuiproto.Operation, error) {
	if registry.actions == nil {
		return tuiproto.Operation{}, errors.New("playback action is unavailable")
	}
	ctx, entry, operation, err := registry.register(tuiproto.OperationKindPlayback)
	if err != nil {
		return tuiproto.Operation{}, err
	}
	go registry.runPlayback(ctx, entry, lecture, resume)
	return operation, nil
}

func (registry *operationRegistry) register(kind tuiproto.OperationKind) (context.Context, *operationEntry, tuiproto.Operation, error) {
	identifier, err := randomToken(16)
	if err != nil {
		return nil, nil, tuiproto.Operation{}, err
	}
	ctx, cancel := context.WithCancel(registry.ctx)
	entry := &operationEntry{
		operation: tuiproto.Operation{ID: identifier, Kind: kind, State: tuiproto.OperationStateRunning},
		cancel:    cancel,
		done:      make(chan struct{}),
	}
	registry.mu.Lock()
	if registry.stopping {
		registry.mu.Unlock()
		cancel()
		return nil, nil, tuiproto.Operation{}, errRegistryStopping
	}
	if len(registry.operations) >= maxOperations {
		registry.mu.Unlock()
		cancel()
		return nil, nil, tuiproto.Operation{}, errTooManyOperations
	}
	registry.operations[identifier] = entry
	registry.wait.Add(1)
	registry.mu.Unlock()

	state := tuiproto.OperationStateRunning
	registry.events.publish(tuiproto.Event{
		OperationID: &identifier,
		State:       &state,
		Type:        tuiproto.EventTypeOperationStarted,
	})
	return ctx, entry, entry.operation, nil
}

func (registry *operationRegistry) runSelfTest(ctx context.Context, entry *operationEntry) {
	defer registry.wait.Done()
	defer close(entry.done)
	for step := 1; step <= registry.options.Steps; step++ {
		if registry.options.Interval > 0 {
			timer := time.NewTimer(registry.options.Interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				registry.finish(entry, tuiproto.OperationStateCanceled, tuiproto.EventTypeOperationCanceled, "")
				return
			case <-timer.C:
			}
		} else {
			select {
			case <-ctx.Done():
				registry.finish(entry, tuiproto.OperationStateCanceled, tuiproto.EventTypeOperationCanceled, "")
				return
			default:
			}
		}
		percent := float64(step) / float64(registry.options.Steps) * 100
		state := tuiproto.OperationStateRunning
		identifier := entry.operation.ID
		registry.events.publish(tuiproto.Event{
			OperationID: &identifier,
			Percent:     &percent,
			State:       &state,
			Type:        tuiproto.EventTypeOperationProgress,
		})
	}
	registry.finish(entry, tuiproto.OperationStateCompleted, tuiproto.EventTypeOperationCompleted, "")
}

func (registry *operationRegistry) runDownload(ctx context.Context, entry *operationEntry, lecture client.Lecture) {
	defer registry.wait.Done()
	defer close(entry.done)
	var result app.DownloadResult
	var err error
	if actions, ok := registry.actions.(progressiveDownloadActions); ok {
		result, err = actions.DownloadLectureWithProgress(ctx, lecture, registry.downloadProgressReporter(entry))
	} else {
		result, err = registry.actions.DownloadLecture(ctx, lecture)
	}
	if ctx.Err() != nil {
		registry.finish(entry, tuiproto.OperationStateCanceled, tuiproto.EventTypeOperationCanceled, "")
		return
	}
	if err != nil {
		registry.finish(entry, tuiproto.OperationStateFailed, tuiproto.EventTypeOperationFailed, "lecture download failed")
		return
	}
	message := ""
	if result.Warning != "" || !result.LibraryRecorded {
		message = "download completed with a local library warning"
	}
	registry.finish(entry, tuiproto.OperationStateCompleted, tuiproto.EventTypeOperationCompleted, message)
}

func (registry *operationRegistry) downloadProgressReporter(entry *operationEntry) func(float64) {
	var mu sync.Mutex
	last := float64(0)
	return func(percent float64) {
		if math.IsNaN(percent) || math.IsInf(percent, 0) {
			return
		}
		percent = max(0, min(100, percent))
		mu.Lock()
		if percent <= last || (last > 0 && percent < 100 && percent-last < 2) {
			mu.Unlock()
			return
		}
		last = percent
		mu.Unlock()
		state := tuiproto.OperationStateRunning
		identifier := entry.operation.ID
		registry.events.publish(tuiproto.Event{
			OperationID: &identifier,
			Percent:     &percent,
			State:       &state,
			Type:        tuiproto.EventTypeOperationProgress,
		})
	}
}

func (registry *operationRegistry) runPlayback(ctx context.Context, entry *operationEntry, lecture client.Lecture, resume bool) {
	defer registry.wait.Done()
	defer close(entry.done)
	playbackState, err := registry.resolvePlaybackResume(ctx, lecture, resume)
	if err != nil {
		registry.finish(entry, tuiproto.OperationStateFailed, tuiproto.EventTypeOperationFailed, "resume checkpoint is unavailable")
		return
	}
	started, err := registry.actions.StartLecture(ctx, lecture, playbackState.PositionSeconds)
	if err != nil {
		registry.finishPlaybackStartError(ctx, entry)
		return
	}
	if !registry.attachPlayback(entry, started.Session) {
		return
	}

	telemetry := playbackTelemetry{volume: 100, speed: 1}
	for _, event := range started.InitialEvents {
		registry.applyPlaybackEvent(entry, &telemetry, event)
	}
	eventsDone := registry.consumePlaybackEvents(ctx, entry, started.Session, &telemetry)
	waitErr := started.Session.WaitForEnd(ctx)
	closeErr := started.Session.Close(context.Background())
	<-eventsDone
	registry.detachPlayback(entry)
	registry.recordPlayback(entry, playbackState.ArtifactID, &telemetry, ctx.Err() == nil && waitErr == nil && closeErr == nil)
	registry.finishPlayback(ctx, entry, waitErr, closeErr)
}

func (registry *operationRegistry) resolvePlaybackResume(ctx context.Context, lecture client.Lecture, resume bool) (library.PlaybackState, error) {
	state, found, err := registry.actions.ResumeLecture(ctx, lecture)
	if err != nil {
		return library.PlaybackState{}, err
	}
	if !resume || !found || state.Completed {
		state.PositionSeconds = 0
		state.DurationSeconds = 0
		state.Completed = false
	}
	return state, nil
}

func (registry *operationRegistry) attachPlayback(entry *operationEntry, playback app.PlaybackSession) bool {
	registry.mu.Lock()
	if entry.operation.State != tuiproto.OperationStateRunning {
		registry.mu.Unlock()
		if err := playback.Close(context.Background()); err != nil {
			registry.publishPlaybackWarning(entry, "playback cleanup failed")
		}
		return false
	}
	entry.playback = playback
	registry.mu.Unlock()
	return true
}

func (registry *operationRegistry) consumePlaybackEvents(ctx context.Context, entry *operationEntry, playback app.PlaybackSession, telemetry *playbackTelemetry) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case event, open := <-playback.Events():
				if !open {
					return
				}
				registry.applyPlaybackEvent(entry, telemetry, event)
			case <-ctx.Done():
				return
			}
		}
	}()
	return done
}

func (registry *operationRegistry) detachPlayback(entry *operationEntry) {
	registry.mu.Lock()
	entry.playback = nil
	registry.mu.Unlock()
}

func (registry *operationRegistry) finishPlayback(ctx context.Context, entry *operationEntry, waitErr, closeErr error) {
	if ctx.Err() != nil {
		registry.finish(entry, tuiproto.OperationStateCanceled, tuiproto.EventTypeOperationCanceled, "")
		return
	}
	if waitErr != nil || closeErr != nil {
		registry.finish(entry, tuiproto.OperationStateFailed, tuiproto.EventTypeOperationFailed, "lecture playback failed")
		return
	}
	registry.finish(entry, tuiproto.OperationStateCompleted, tuiproto.EventTypeOperationCompleted, "")
}

func (registry *operationRegistry) finishPlaybackStartError(ctx context.Context, entry *operationEntry) {
	if ctx.Err() != nil {
		registry.finish(entry, tuiproto.OperationStateCanceled, tuiproto.EventTypeOperationCanceled, "")
		return
	}
	registry.finish(entry, tuiproto.OperationStateFailed, tuiproto.EventTypeOperationFailed, "lecture playback could not start")
}

type playbackTelemetry struct {
	mu        sync.Mutex
	position  float64
	duration  float64
	volume    float64
	speed     float64
	paused    bool
	muted     bool
	completed bool
}

func (registry *operationRegistry) applyPlaybackEvent(entry *operationEntry, telemetry *playbackTelemetry, playbackEvent player.Event) {
	telemetry.mu.Lock()
	switch playbackEvent.Name {
	case "end-file":
		telemetry.completed = playbackEvent.Reason == "eof"
	case "property-change":
		updatePlaybackTelemetry(telemetry, playbackEvent)
	}
	event := telemetry.event(entry.operation.ID)
	telemetry.mu.Unlock()
	if event != nil {
		registry.events.publish(*event)
	}
}

func updatePlaybackTelemetry(telemetry *playbackTelemetry, event player.Event) {
	switch event.Property {
	case "time-pos":
		decodeFiniteNonNegative(event.Data, &telemetry.position)
	case "duration":
		decodeFiniteNonNegative(event.Data, &telemetry.duration)
	case "volume":
		decodeFiniteNonNegative(event.Data, &telemetry.volume)
	case "speed":
		decodeFiniteNonNegative(event.Data, &telemetry.speed)
	case "pause":
		decodeBoolean(event.Data, &telemetry.paused)
	case "mute":
		decodeBoolean(event.Data, &telemetry.muted)
	}
}

func decodeBoolean(data json.RawMessage, target *bool) {
	var value bool
	if err := json.Unmarshal(data, &value); err == nil {
		*target = value
	}
}

func decodeFiniteNonNegative(data json.RawMessage, target *float64) {
	var value float64
	if json.Unmarshal(data, &value) == nil && value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0) {
		*target = value
	}
}

func (telemetry *playbackTelemetry) event(identifier string) *tuiproto.Event {
	state := tuiproto.OperationStateRunning
	progress := float64(0)
	if telemetry.duration > 0 {
		progress = min(100, telemetry.position/telemetry.duration*100)
	}
	position := telemetry.position
	duration := telemetry.duration
	volume := telemetry.volume
	speed := telemetry.speed
	paused := telemetry.paused
	muted := telemetry.muted
	return &tuiproto.Event{
		DurationSeconds: &duration,
		Muted:           &muted,
		OperationID:     &identifier,
		Paused:          &paused,
		Percent:         &progress,
		PositionSeconds: &position,
		Speed:           &speed,
		State:           &state,
		Type:            tuiproto.EventTypeOperationProgress,
		Volume:          &volume,
	}
}

func (registry *operationRegistry) recordPlayback(entry *operationEntry, artifactID string, telemetry *playbackTelemetry, cleanTerminal bool) {
	if artifactID == "" {
		return
	}
	telemetry.mu.Lock()
	state := library.PlaybackState{
		ArtifactID:      artifactID,
		PositionSeconds: telemetry.position,
		DurationSeconds: telemetry.duration,
		Completed:       cleanTerminal && telemetry.completed,
		LastPlayedAt:    time.Now().UTC(),
	}
	telemetry.mu.Unlock()
	recordCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := registry.actions.RecordPlayback(recordCtx, state); err != nil {
		registry.publishPlaybackWarning(entry, "playback checkpoint could not be saved")
	}
}

func (registry *operationRegistry) publishPlaybackWarning(entry *operationEntry, message string) {
	identifier := entry.operation.ID
	registry.events.publish(tuiproto.Event{Message: &message, OperationID: &identifier, Type: tuiproto.EventTypeOperationProgress})
}

func (registry *operationRegistry) controlPlayback(ctx context.Context, identifier string, command tuiproto.PlaybackCommand) error {
	playback, err := registry.playbackForControl(identifier)
	if err != nil {
		return err
	}
	return runPlaybackCommand(ctx, playback, command)
}

func (registry *operationRegistry) playbackForControl(identifier string) (app.PlaybackSession, error) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	entry, ok := registry.operations[identifier]
	if !ok || entry.operation.Kind != tuiproto.OperationKindPlayback || entry.operation.State != tuiproto.OperationStateRunning || entry.playback == nil {
		return nil, errOperationNotFound
	}
	return entry.playback, nil
}

func runPlaybackCommand(ctx context.Context, playback app.PlaybackSession, command tuiproto.PlaybackCommand) error {
	switch command.Action {
	case tuiproto.PlaybackCommandActionPause:
		return playbackBooleanCommand(ctx, command.Flag, "pause flag is required", playback.Pause)
	case tuiproto.PlaybackCommandActionSeek:
		return playbackFiniteCommand(ctx, command.Value, "finite seek value is required", playback.SeekRelative)
	case tuiproto.PlaybackCommandActionMute:
		return playbackBooleanCommand(ctx, command.Flag, "mute flag is required", playback.SetMute)
	case tuiproto.PlaybackCommandActionVolume:
		return playbackRangeCommand(ctx, command.Value, 0, 130, "volume must be between 0 and 130", playback.SetVolume)
	case tuiproto.PlaybackCommandActionSpeed:
		return playbackRangeCommand(ctx, command.Value, 0.25, 4, "speed must be between 0.25 and 4", playback.SetSpeed)
	case tuiproto.PlaybackCommandActionCycleVideo:
		return playback.CycleVideo(ctx)
	default:
		return errors.New("unsupported playback command")
	}
}

func playbackBooleanCommand(ctx context.Context, value *bool, message string, run func(context.Context, bool) error) error {
	if value == nil {
		return errors.New(message)
	}
	return run(ctx, *value)
}

func playbackFiniteCommand(ctx context.Context, value *float64, message string, run func(context.Context, float64) error) error {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) {
		return errors.New(message)
	}
	return run(ctx, *value)
}

func playbackRangeCommand(ctx context.Context, value *float64, minimum, maximum float64, message string, run func(context.Context, float64) error) error {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) || *value < minimum || *value > maximum {
		return errors.New(message)
	}
	return run(ctx, *value)
}

func (registry *operationRegistry) finish(entry *operationEntry, state tuiproto.OperationState, eventType tuiproto.EventType, message string) {
	registry.mu.Lock()
	if entry.operation.State != tuiproto.OperationStateRunning {
		registry.mu.Unlock()
		return
	}
	entry.operation.State = state
	identifier := entry.operation.ID
	registry.mu.Unlock()
	event := tuiproto.Event{
		OperationID: &identifier,
		State:       &state,
		Type:        eventType,
	}
	if message != "" {
		event.Message = &message
	}
	registry.events.publish(event)
}

func (registry *operationRegistry) get(identifier string) (tuiproto.Operation, error) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	entry, ok := registry.operations[identifier]
	if !ok {
		return tuiproto.Operation{}, errOperationNotFound
	}
	return entry.operation, nil
}

func (registry *operationRegistry) cancelOperation(identifier string) (tuiproto.Operation, error) {
	registry.mu.Lock()
	entry, ok := registry.operations[identifier]
	if !ok {
		registry.mu.Unlock()
		return tuiproto.Operation{}, errOperationNotFound
	}
	entry.cancel()
	done := entry.done
	registry.mu.Unlock()
	<-done
	return registry.get(identifier)
}

func (registry *operationRegistry) stopAndWait() {
	registry.mu.Lock()
	registry.stopping = true
	for _, entry := range registry.operations {
		entry.cancel()
	}
	registry.mu.Unlock()
	registry.wait.Wait()
}
