package watch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/rabesss/impartus-cli/internal/artifact"
	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/downloader"
	"github.com/rabesss/impartus-cli/internal/events"
	"github.com/rabesss/impartus-cli/internal/library"
)

type fakeSource struct {
	mu          sync.Mutex
	lectures    map[[2]int]client.Lectures
	errors      map[[2]int]error
	calls       [][2]int
	before      func([2]int)
	courses     client.Courses
	courseErr   error
	courseCalls int
}

func (source *fakeSource) GetLectures(_ context.Context, _ *config.Config, course client.Course) (client.Lectures, error) {
	key := [2]int{course.SubjectID, course.SessionID}
	if source.before != nil {
		source.before(key)
	}
	source.mu.Lock()
	defer source.mu.Unlock()
	source.calls = append(source.calls, key)
	return append(client.Lectures(nil), source.lectures[key]...), source.errors[key]
}

func (source *fakeSource) GetCourses(_ context.Context, _ *config.Config) (client.Courses, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	source.courseCalls++
	return append(client.Courses(nil), source.courses...), source.courseErr
}

type fakeProducer struct {
	cfg             *config.Config
	mu              sync.Mutex
	downloads       int
	fetchErrors     map[int]error
	downloadErrors  map[int]error
	failuresLeft    map[int]int
	downloadStarted chan struct{}
	blockDownload   bool
	beforeFetch     func()
	afterFetch      func()
}

type contextRecordingStore struct {
	*library.Store
	createContextError error
	startContextError  error
}

func (store *contextRecordingStore) CreateJob(ctx context.Context, spec library.JobSpec) error {
	store.createContextError = ctx.Err()
	return store.Store.CreateJob(ctx, spec)
}

func (store *contextRecordingStore) StartJob(ctx context.Context, jobID string) error {
	store.startContextError = ctx.Err()
	return store.Store.StartJob(ctx, jobID)
}

type recordingEmitter struct {
	mu       sync.Mutex
	events   []events.Event
	failType string
	failures int
	before   func(events.Event)
}

type completeThenFailStore struct {
	*library.Store
	err           error
	afterComplete func()
}

func (store completeThenFailStore) CompleteJob(ctx context.Context, jobID string, manifest artifact.Manifest) error {
	if err := store.Store.CompleteJob(ctx, jobID, manifest); err != nil {
		return err
	}
	if store.afterComplete != nil {
		store.afterComplete()
	}
	return store.err
}

func (emitter *recordingEmitter) Emit(event events.Event) error {
	emitter.mu.Lock()
	defer emitter.mu.Unlock()
	if emitter.before != nil {
		emitter.before(event)
	}
	emitter.events = append(emitter.events, event)
	if event.Type == emitter.failType && emitter.failures > 0 {
		emitter.failures--
		return errors.New("event sink failed")
	}
	return nil
}

func (emitter *recordingEmitter) types() []string {
	emitter.mu.Lock()
	defer emitter.mu.Unlock()
	types := make([]string, 0, len(emitter.events))
	for _, event := range emitter.events {
		types = append(types, event.Type)
	}
	return types
}

func (producer *fakeProducer) FetchLecturePlaylists(_ context.Context, lectures []client.Lecture) ([]client.ParsedPlaylist, error) {
	lecture := lectures[0]
	if producer.beforeFetch != nil {
		producer.beforeFetch()
	}
	if err := producer.fetchErrors[lecture.TTID]; err != nil {
		return nil, err
	}
	if producer.afterFetch != nil {
		producer.afterFetch()
	}
	return []client.ParsedPlaylist{{
		ID: lecture.TTID, InstituteID: lecture.InstituteID, SubjectID: lecture.SubjectID,
		SessionID: lecture.SessionID, SeqNo: lecture.SeqNo, Title: lecture.Topic,
		FirstViewURLs: []string{"left"},
	}}, nil
}

func (producer *fakeProducer) DownloadAndJoinPlaylist(ctx context.Context, playlist client.ParsedPlaylist) (downloader.JoinResult, error) {
	producer.mu.Lock()
	producer.downloads++
	producer.mu.Unlock()
	if producer.downloadStarted != nil {
		select {
		case <-producer.downloadStarted:
		default:
			close(producer.downloadStarted)
		}
	}
	if producer.blockDownload {
		<-ctx.Done()
		return downloader.JoinResult{}, ctx.Err()
	}
	if err := producer.downloadErrors[playlist.ID]; err != nil {
		if producer.failuresLeft[playlist.ID] > 0 {
			producer.failuresLeft[playlist.ID]--
			return downloader.JoinResult{}, err
		}
		if producer.failuresLeft != nil {
			delete(producer.downloadErrors, playlist.ID)
		}
	}
	if err := producer.downloadErrors[playlist.ID]; err != nil {
		return downloader.JoinResult{}, err
	}
	plan, err := downloader.PlanJoinResult(producer.cfg, playlist)
	if err != nil {
		return downloader.JoinResult{}, err
	}
	for _, path := range plan.OutputPaths() {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return downloader.JoinResult{}, err
		}
		if err := os.WriteFile(path, []byte("ID3completed media"), 0o600); err != nil {
			return downloader.JoinResult{}, err
		}
	}
	return plan, nil
}
