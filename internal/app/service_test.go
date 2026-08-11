package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/artifact"
	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/downloader"
	"github.com/rabesss/impartus-cli/internal/library"
	"github.com/rabesss/impartus-cli/internal/player"
)

type fakePlaybackHistory struct {
	state library.PlaybackState
	found bool
	err   error
}

func (fake *fakePlaybackHistory) Playback(context.Context, string) (library.PlaybackState, bool, error) {
	return fake.state, fake.found, fake.err
}

func (fake *fakePlaybackHistory) RecordPlayback(_ context.Context, state library.PlaybackState) error {
	fake.state = state
	fake.found = true
	return fake.err
}

func TestServiceExposesLibraryResumeSeam(t *testing.T) {
	history := &fakePlaybackHistory{state: library.PlaybackState{ArtifactID: "artifact", PositionSeconds: 42}, found: true}
	service := newServiceWithHistory(&config.Config{}, &fakeCatalog{}, &fakeStreams{}, nil, player.Options{}, history)
	state, found, err := service.Resume(context.Background(), "artifact")
	if err != nil || !found || state.PositionSeconds != 42 {
		t.Fatalf("Resume() = (%+v, %t, %v)", state, found, err)
	}
	want := library.PlaybackState{ArtifactID: "artifact", PositionSeconds: 75}
	if err := service.RecordPlayback(context.Background(), want); err != nil {
		t.Fatalf("RecordPlayback() error = %v", err)
	}
	if history.state.PositionSeconds != 75 {
		t.Fatalf("history state = %+v", history.state)
	}
}

func TestResumeLectureUsesCanonicalArtifactIdentity(t *testing.T) {
	identity := artifact.Identity{
		InstituteID: 4,
		SubjectID:   67,
		SessionID:   8,
		TTID:        12345,
		Views:       "both",
		Quality:     "720",
	}
	artifactID, err := artifact.NewID(identity)
	if err != nil {
		t.Fatalf("NewID() error = %v", err)
	}
	history := &fakePlaybackHistory{state: library.PlaybackState{ArtifactID: artifactID, PositionSeconds: 42}, found: true}
	service := newServiceWithHistory(
		&config.Config{Views: "both", Quality: "720"},
		&fakeCatalog{},
		&fakeStreams{},
		nil,
		player.Options{},
		history,
	)

	state, found, err := service.ResumeLecture(context.Background(), client.Lecture{
		InstituteID: 4,
		SubjectID:   67,
		SessionID:   8,
		TTID:        12345,
	})
	if err != nil || !found || state.ArtifactID != artifactID {
		t.Fatalf("ResumeLecture() = (%+v, %t, %v), want artifact %s", state, found, err, artifactID)
	}
}

func TestResumeLectureReturnsArtifactIdentityBeforeFirstCheckpoint(t *testing.T) {
	identity := artifact.Identity{InstituteID: 4, SubjectID: 67, SessionID: 8, TTID: 12345, Views: "both", Quality: "720"}
	artifactID, err := artifact.NewID(identity)
	if err != nil {
		t.Fatalf("NewID() error = %v", err)
	}
	service := &Service{
		config:  &config.Config{Views: "both", Quality: "720"},
		history: &fakePlaybackHistory{},
		library: &fakeArtifactStore{recorded: []artifact.Manifest{{ArtifactID: artifactID}}},
	}

	state, found, err := service.ResumeLecture(context.Background(), client.Lecture{
		InstituteID: 4, SubjectID: 67, SessionID: 8, TTID: 12345,
	})
	if err != nil || found || state.ArtifactID != artifactID || state.PositionSeconds != 0 {
		t.Fatalf("ResumeLecture() = (%+v, %t, %v), want first checkpoint identity %s", state, found, err, artifactID)
	}
}

type fakeCatalog struct {
	courses  client.Courses
	lectures client.Lectures
}

func (fake *fakeCatalog) GetCourses(context.Context, *config.Config) (client.Courses, error) {
	return fake.courses, nil
}

func (fake *fakeCatalog) GetLectures(context.Context, *config.Config, client.Course) (client.Lectures, error) {
	return fake.lectures, nil
}

type fakeStreams struct {
	playlists []client.ParsedPlaylist
	cleanups  int
	starts    []int
	failures  chan error
}

func (fake *fakeStreams) FetchLecturePlaylists(context.Context, []client.Lecture) ([]client.ParsedPlaylist, error) {
	return fake.playlists, nil
}

func (fake *fakeStreams) StartPlaybackStream(_ context.Context, playlist client.ParsedPlaylist) (downloader.PlaybackStream, error) {
	fake.starts = append(fake.starts, playlist.ID)
	return downloader.PlaybackStream{
		URL:      "http://127.0.0.1:1234/token/master.m3u8",
		Failures: fake.failures,
		Cleanup:  func() { fake.cleanups++ },
	}, nil
}

type fakeManagedPlayer struct {
	loaded        []string
	seeks         []float64
	absoluteSeeks []float64
	events        chan player.Event
	load          error
	wait          error
	waitStarted   chan struct{}
	waitRelease   <-chan struct{}
	closed        int
}

func (fake *fakeManagedPlayer) Load(_ context.Context, playbackURL string) error {
	fake.loaded = append(fake.loaded, playbackURL)
	return fake.load
}

func TestServiceCleansProxyWhenPlayerStartFails(t *testing.T) {
	sentinel := errors.New("player start failed")
	streams := &fakeStreams{playlists: []client.ParsedPlaylist{{ID: 1}}}
	service := newService(&config.Config{}, &fakeCatalog{}, streams, func(context.Context, player.Options) (managedPlayer, error) {
		return nil, sentinel
	})

	err := service.PlaySequential(context.Background(), client.Lectures{{TTID: 1}}, nil)
	if !errors.Is(err, sentinel) {
		t.Fatalf("PlaySequential() error = %v, want sentinel", err)
	}
	if streams.cleanups != 1 {
		t.Fatalf("player start failure leaked proxy: cleanups=%d", streams.cleanups)
	}
}

func TestPlaybackWaitResultPrefersCancellationOverCleanExit(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := playbackWaitResult(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("playbackWaitResult() error = %v, want context cancellation", err)
	}
}

func TestPlaybackWaitResultPreservesPlayerFailure(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("player failed")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := playbackWaitResult(ctx, sentinel); !errors.Is(err, sentinel) {
		t.Fatalf("playbackWaitResult() error = %v, want player failure", err)
	}
}

func TestPlaybackCancellationResultPreservesReadyFailures(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	proxyFailure := errors.New("proxy failed")
	playerFailure := errors.New("player failed")

	tests := []struct {
		name         string
		proxyFailure error
		playerResult error
		want         error
	}{
		{name: "proxy", proxyFailure: proxyFailure, playerResult: playerFailure, want: proxyFailure},
		{name: "player", playerResult: playerFailure, want: playerFailure},
		{name: "clean player", want: context.Canceled},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			failures := make(chan error, 1)
			playerResult := make(chan error, 1)
			if test.proxyFailure != nil {
				failures <- test.proxyFailure
			}
			playerResult <- test.playerResult

			if err := playbackCancellationResult(ctx, failures, playerResult); !errors.Is(err, test.want) {
				t.Fatalf("playbackCancellationResult() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestPlaybackWaitForEndDrainsInFlightPlayerFailureAfterCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sentinel := errors.New("player failed during cancellation")
	started := make(chan struct{})
	release := make(chan struct{})
	fakePlayer := &fakeManagedPlayer{
		wait:        sentinel,
		waitStarted: started,
		waitRelease: release,
	}
	playback := &Playback{player: fakePlayer}
	result := make(chan error, 1)
	go func() { result <- playback.WaitForEnd(ctx) }()

	<-started
	cancel()
	select {
	case err := <-result:
		t.Fatalf("WaitForEnd() returned before the in-flight player result: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	if err := <-result; !errors.Is(err, sentinel) {
		t.Fatalf("WaitForEnd() error = %v, want player failure", err)
	}
}

func TestServicePassesExplicitPlayerOptions(t *testing.T) {
	streams := &fakeStreams{playlists: []client.ParsedPlaylist{{ID: 1}}}
	fakePlayer := &fakeManagedPlayer{}
	want := player.Options{VideoOutput: "gpu-next"}
	var got player.Options
	service := newService(&config.Config{}, &fakeCatalog{}, streams, func(_ context.Context, options player.Options) (managedPlayer, error) {
		got = options
		return fakePlayer, nil
	}, want)
	playback, err := service.StartPlayback(context.Background(), client.ParsedPlaylist{ID: 1})
	if err != nil {
		t.Fatalf("StartPlayback() error = %v", err)
	}
	if closeErr := playback.Close(context.Background()); closeErr != nil {
		t.Fatalf("Close() error = %v", closeErr)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("player options = %+v, want %+v", got, want)
	}
}

func TestServiceCleansPlayerAndProxyWhenLoadFails(t *testing.T) {
	sentinel := errors.New("load failed")
	streams := &fakeStreams{playlists: []client.ParsedPlaylist{{ID: 1}}}
	fakePlayer := &fakeManagedPlayer{load: sentinel}
	service := newService(&config.Config{}, &fakeCatalog{}, streams, func(context.Context, player.Options) (managedPlayer, error) {
		return fakePlayer, nil
	})

	err := service.PlaySequential(context.Background(), client.Lectures{{TTID: 1}}, nil)
	if !errors.Is(err, sentinel) {
		t.Fatalf("PlaySequential() error = %v, want sentinel", err)
	}
	if streams.cleanups != 1 || fakePlayer.closed != 1 {
		t.Fatalf("load failure leaked resources: proxy=%d player=%d", streams.cleanups, fakePlayer.closed)
	}
}

func (fake *fakeManagedPlayer) WaitForEnd(context.Context) error {
	if fake.waitStarted != nil {
		close(fake.waitStarted)
	}
	if fake.waitRelease != nil {
		<-fake.waitRelease
	}
	return fake.wait
}
func (fake *fakeManagedPlayer) Close(context.Context) error {
	fake.closed++
	return nil
}
func (fake *fakeManagedPlayer) Events() <-chan player.Event       { return fake.events }
func (fake *fakeManagedPlayer) Pause(context.Context, bool) error { return nil }
func (fake *fakeManagedPlayer) SeekRelative(_ context.Context, seconds float64) error {
	fake.seeks = append(fake.seeks, seconds)
	return nil
}
func (fake *fakeManagedPlayer) SeekAbsolute(_ context.Context, seconds float64) error {
	fake.absoluteSeeks = append(fake.absoluteSeeks, seconds)
	return nil
}
func (fake *fakeManagedPlayer) SetVolume(context.Context, float64) error { return nil }
func (fake *fakeManagedPlayer) SetMute(context.Context, bool) error      { return nil }
func (fake *fakeManagedPlayer) SetSpeed(context.Context, float64) error  { return nil }
func (fake *fakeManagedPlayer) CycleVideo(context.Context) error         { return nil }

func TestServiceCatalogDelegatesToClient(t *testing.T) {
	catalog := &fakeCatalog{
		courses:  client.Courses{{SubjectID: 1}},
		lectures: client.Lectures{{TTID: 2}},
	}
	service := newService(&config.Config{}, catalog, &fakeStreams{}, nil)
	courses, err := service.Courses(context.Background())
	if err != nil || len(courses) != 1 || courses[0].SubjectID != 1 {
		t.Fatalf("Courses() = %+v, %v", courses, err)
	}
	lectures, err := service.Lectures(context.Background(), client.Course{SubjectID: 1, SessionID: 3})
	if err != nil || len(lectures) != 1 || lectures[0].TTID != 2 {
		t.Fatalf("Lectures() = %+v, %v", lectures, err)
	}
}

func TestServiceCatalogHonorsSkipNoAudio(t *testing.T) {
	catalog := &fakeCatalog{lectures: client.Lectures{
		{TTID: 1, Topic: "Audio", NoAudio: 0},
		{TTID: 2, Topic: "Silent", NoAudio: 1},
	}}
	service := newService(&config.Config{SkipNoAudio: true}, catalog, &fakeStreams{}, nil)
	lectures, err := service.Lectures(context.Background(), client.Course{SubjectID: 1, SessionID: 2})
	if err != nil {
		t.Fatalf("Lectures() error = %v", err)
	}
	if len(lectures) != 1 || lectures[0].TTID != 1 {
		t.Fatalf("Lectures() = %+v, want only audio-capable lecture", lectures)
	}
}

func TestStartLectureResolvesOnePlaylistAndAppliesResume(t *testing.T) {
	streams := &fakeStreams{playlists: []client.ParsedPlaylist{{ID: 91}}}
	fakePlayer := &fakeManagedPlayer{events: make(chan player.Event, 10)}
	wantEvents := []player.Event{
		{Name: "property-change", Property: "volume", Data: []byte("90")},
		{Name: "property-change", Property: "duration", Data: []byte("120")},
	}
	for _, event := range []player.Event{
		{Name: "property-change", Property: "time-pos", Data: []byte("null")},
		{Name: "property-change", Property: "duration", Data: []byte("null")},
		{Name: "property-change", Property: "time-pos", Data: []byte("0")},
		{Name: "property-change", Property: "volume", Data: []byte("80")},
		{Name: "property-change", Property: "pause", Data: []byte("null")},
		{Name: "property-change", Property: "volume", Data: []byte("90")},
		{Name: "property-change", Property: "duration", Data: []byte("120")},
	} {
		fakePlayer.events <- event
	}
	service := newService(&config.Config{}, &fakeCatalog{}, streams, func(context.Context, player.Options) (managedPlayer, error) {
		return fakePlayer, nil
	})

	started, err := service.StartLecture(context.Background(), client.Lecture{TTID: 91}, 42.5)
	if err != nil {
		t.Fatalf("StartLecture() error = %v", err)
	}
	playback := started.Session
	if len(streams.starts) != 1 || streams.starts[0] != 91 {
		t.Fatalf("started playlists = %v, want [91]", streams.starts)
	}
	if len(fakePlayer.seeks) != 0 || !reflect.DeepEqual(fakePlayer.absoluteSeeks, []float64{42.5}) {
		t.Fatalf("resume relative=%v absolute=%v, want readiness-gated absolute [42.5]", fakePlayer.seeks, fakePlayer.absoluteSeeks)
	}
	if !reflect.DeepEqual(started.InitialEvents, wantEvents) {
		t.Fatalf("initial events = %+v, want %+v", started.InitialEvents, wantEvents)
	}
	select {
	case got := <-playback.Events():
		t.Fatalf("unexpected stale pre-seek event left on live stream: %+v", got)
	default:
	}
	if closeErr := playback.Close(context.Background()); closeErr != nil {
		t.Fatalf("Close() error = %v", closeErr)
	}
}

func TestStartLectureSurfacesFailureBeforeMediaReady(t *testing.T) {
	sentinel := errors.New("mpv IPC disconnected")
	streams := &fakeStreams{playlists: []client.ParsedPlaylist{{ID: 91}}}
	events := make(chan player.Event)
	close(events)
	fakePlayer := &fakeManagedPlayer{events: events, wait: sentinel}
	service := newService(&config.Config{}, &fakeCatalog{}, streams, func(context.Context, player.Options) (managedPlayer, error) {
		return fakePlayer, nil
	})

	started, err := service.StartLecture(context.Background(), client.Lecture{TTID: 91}, 42.5)
	if started.Session != nil || !errors.Is(err, sentinel) {
		t.Fatalf("StartLecture() = (%v, %v), want readiness failure", started, err)
	}
	if fakePlayer.closed != 1 || streams.cleanups != 1 {
		t.Fatalf("readiness failure cleanup player=%d proxy=%d, want one each", fakePlayer.closed, streams.cleanups)
	}
}

func TestWaitForPlaybackReadyTimesOut(t *testing.T) {
	t.Parallel()

	playback := &fakeManagedPlayer{events: make(chan player.Event)}
	started := time.Now()
	_, err := waitForPlaybackReady(context.Background(), playback, 10*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waitForPlaybackReady() error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("waitForPlaybackReady() elapsed = %v, want bounded readiness wait", elapsed)
	}
}

func TestPlaybackReadinessIgnoresStaleEOFProperty(t *testing.T) {
	t.Parallel()

	ready, terminal, err := playbackReadiness(player.Event{
		Name:     "property-change",
		Property: "eof-reached",
		Data:     []byte("true"),
	})
	if err != nil || ready || terminal {
		t.Fatalf("playbackReadiness(stale EOF) = (%t, %t, %v), want non-terminal", ready, terminal, err)
	}
}

func TestServicePlaysSequentiallyAndCleansEveryBoundary(t *testing.T) {
	streams := &fakeStreams{playlists: []client.ParsedPlaylist{{ID: 1}, {ID: 2}}}
	players := []*fakeManagedPlayer{{}, {}}
	next := 0
	service := newService(&config.Config{}, &fakeCatalog{}, streams, func(context.Context, player.Options) (managedPlayer, error) {
		current := players[next]
		next++
		return current, nil
	})
	started := make([]int, 0, 2)
	err := service.PlaySequential(context.Background(), client.Lectures{{TTID: 1}, {TTID: 2}}, func(playlist client.ParsedPlaylist) {
		started = append(started, playlist.ID)
	})
	if err != nil {
		t.Fatalf("PlaySequential() error = %v", err)
	}
	if !reflect.DeepEqual(started, []int{1, 2}) || !reflect.DeepEqual(streams.starts, []int{1, 2}) {
		t.Fatalf("order: callbacks=%v streams=%v", started, streams.starts)
	}
	if streams.cleanups != 2 || players[0].closed != 1 || players[1].closed != 1 {
		t.Fatalf("cleanup counts: proxy=%d players=%d/%d", streams.cleanups, players[0].closed, players[1].closed)
	}
	for _, fake := range players {
		if len(fake.loaded) != 1 || fake.loaded[0] != "http://127.0.0.1:1234/token/master.m3u8" {
			t.Fatalf("loaded URLs = %v", fake.loaded)
		}
	}
}

func TestServiceClosesPlayerAndProxyWhenPlaybackFails(t *testing.T) {
	sentinel := errors.New("playback failed")
	streams := &fakeStreams{playlists: []client.ParsedPlaylist{{ID: 1}, {ID: 2}}}
	fakePlayer := &fakeManagedPlayer{wait: sentinel}
	service := newService(&config.Config{}, &fakeCatalog{}, streams, func(context.Context, player.Options) (managedPlayer, error) {
		return fakePlayer, nil
	})

	err := service.PlaySequential(context.Background(), client.Lectures{{TTID: 1}}, nil)
	if !errors.Is(err, sentinel) {
		t.Fatalf("PlaySequential() error = %v, want sentinel", err)
	}
	if streams.cleanups != 1 || fakePlayer.closed != 1 || len(streams.starts) != 1 {
		t.Fatalf("failed playback leaked resources: proxy=%d player=%d starts=%v", streams.cleanups, fakePlayer.closed, streams.starts)
	}
}

func TestPlaybackWaitSurfacesProxyAuthorizationFailure(t *testing.T) {
	failures := make(chan error, 1)
	failures <- downloader.ErrPlaybackAuthorization
	streams := &fakeStreams{failures: failures}
	fakePlayer := &fakeManagedPlayer{wait: errors.New("mpv playback failed")}
	service := newService(&config.Config{}, &fakeCatalog{}, streams, func(context.Context, player.Options) (managedPlayer, error) {
		return fakePlayer, nil
	})

	playback, err := service.StartPlayback(context.Background(), client.ParsedPlaylist{ID: 1})
	if err != nil {
		t.Fatalf("StartPlayback() error = %v", err)
	}
	defer func() {
		closeErr := playback.Close(context.Background())
		_ = closeErr
	}()
	if err := playback.WaitForEnd(context.Background()); !errors.Is(err, downloader.ErrPlaybackAuthorization) {
		t.Fatalf("WaitForEnd() error = %v, want ErrPlaybackAuthorization", err)
	}
}

type fakeLectureDownloader struct {
	playlists   []client.ParsedPlaylist
	joined      downloader.JoinResult
	fetchErr    error
	downloadErr error
	afterJoin   func()
}

func (fake *fakeLectureDownloader) FetchLecturePlaylists(context.Context, []client.Lecture) ([]client.ParsedPlaylist, error) {
	return fake.playlists, fake.fetchErr
}

func (fake *fakeLectureDownloader) DownloadAndJoin(context.Context, client.ParsedPlaylist) (downloader.JoinResult, error) {
	if fake.afterJoin != nil {
		fake.afterJoin()
	}
	return fake.joined, fake.downloadErr
}

type fakeArtifactStore struct {
	recorded         []artifact.Manifest
	record           error
	recordContextErr error
	created          []library.JobSpec
	started          []string
	failed           []string
	canceled         []string
	completed        []string
}

func (store *fakeArtifactStore) CreateJob(_ context.Context, spec library.JobSpec) error {
	store.created = append(store.created, spec)
	return nil
}

func (store *fakeArtifactStore) StartJob(_ context.Context, jobID string) error {
	store.started = append(store.started, jobID)
	return nil
}

func (store *fakeArtifactStore) FailJob(_ context.Context, jobID string, _ error) error {
	store.failed = append(store.failed, jobID)
	return nil
}

func (store *fakeArtifactStore) CancelJob(_ context.Context, jobID string) error {
	store.canceled = append(store.canceled, jobID)
	return nil
}

func (store *fakeArtifactStore) CompleteJob(ctx context.Context, jobID string, manifest artifact.Manifest) error {
	store.recordContextErr = ctx.Err()
	store.completed = append(store.completed, jobID)
	store.recorded = append(store.recorded, manifest)
	return store.record
}

func TestDownloadLectureFinishesArtifactCommitAfterPublishedMedia(t *testing.T) {
	output := filepath.Join(t.TempDir(), "lecture.mp4")
	if err := os.WriteFile(output, []byte("media"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	downloads := &fakeLectureDownloader{
		playlists: []client.ParsedPlaylist{{ID: 12345, SeqNo: 1, Title: "Published", FirstViewURLs: []string{"left"}}},
		joined:    downloader.JoinResult{LeftOutput: output, LeftContainer: "mp4"},
		afterJoin: cancel,
	}
	store := &fakeArtifactStore{}
	service := &Service{
		config:    &config.Config{DownloadLocation: t.TempDir(), Views: "left", Quality: "720"},
		downloads: downloads,
		library:   store,
	}
	result, err := service.DownloadLecture(ctx, client.Lecture{
		InstituteID: 1, SubjectID: 2, SessionID: 3, TTID: 12345, Topic: "Published",
	})
	if err != nil {
		t.Fatalf("DownloadLecture() error = %v", err)
	}
	if !result.LibraryRecorded || len(store.recorded) != 1 || store.recordContextErr != nil {
		t.Fatalf("published download result=%+v recorded=%d recordContextErr=%v", result, len(store.recorded), store.recordContextErr)
	}
}

func (store *fakeArtifactStore) ListArtifacts(context.Context) ([]library.ArtifactRecord, error) {
	records := make([]library.ArtifactRecord, 0, len(store.recorded))
	for _, manifest := range store.recorded {
		records = append(records, library.ArtifactRecord{Manifest: manifest})
	}
	return records, nil
}

func (store *fakeArtifactStore) GetArtifact(_ context.Context, artifactID string) (library.ArtifactRecord, error) {
	for _, manifest := range store.recorded {
		if manifest.ArtifactID == artifactID {
			return library.ArtifactRecord{Manifest: manifest}, nil
		}
	}
	return library.ArtifactRecord{}, library.ErrArtifactNotFound
}

func TestDownloadLectureBuildsAndCompletesOneArtifact(t *testing.T) {
	output := filepath.Join(t.TempDir(), "lecture.mp4")
	if err := os.WriteFile(output, []byte("media"), 0o600); err != nil {
		t.Fatalf("write output: %v", err)
	}
	downloads := &fakeLectureDownloader{
		playlists: []client.ParsedPlaylist{{
			ID: 12345, InstituteID: 4, SubjectID: 67, SessionID: 8, SeqNo: 7, Title: "Consensus",
			FirstViewURLs: []string{"left"},
		}},
		joined: downloader.JoinResult{LeftOutput: output, LeftContainer: "mp4"},
	}
	store := &fakeArtifactStore{}
	service := &Service{
		config: &config.Config{
			DownloadLocation: t.TempDir(),
			Views:            "left",
			Quality:          "720",
			AudioFormat:      "mp3",
		},
		downloads: downloads,
		library:   store,
	}
	lecture := client.Lecture{
		InstituteID: 4,
		SubjectID:   67,
		SessionID:   8,
		TTID:        12345,
		Topic:       "Consensus",
		SeqNo:       7,
	}

	result, err := service.DownloadLecture(context.Background(), lecture)
	if err != nil {
		t.Fatalf("DownloadLecture() error = %v", err)
	}
	if !result.LibraryRecorded || result.Warning != "" || len(store.recorded) != 1 {
		t.Fatalf("download result = %+v, recorded=%d", result, len(store.recorded))
	}
	if len(store.created) != 1 || len(store.started) != 1 || len(store.completed) != 1 ||
		store.created[0].ID != store.started[0] || store.started[0] != store.completed[0] {
		t.Fatalf("durable job lifecycle: created=%+v started=%v completed=%v", store.created, store.started, store.completed)
	}
	if store.created[0].Expected.Selection.AudioFormat != "" || result.Manifest.Selection.AudioFormat != "" {
		t.Fatalf("video selection retained irrelevant audio format: expected=%+v manifest=%+v", store.created[0].Expected.Selection, result.Manifest.Selection)
	}
	if result.Manifest.Lecture.TTID != 12345 || result.Manifest.Files[0].Path != output || result.Manifest.Files[0].View != "left" {
		t.Fatalf("manifest = %+v", result.Manifest)
	}
	records, err := service.Artifacts(context.Background())
	if err != nil || len(records) != 1 || records[0].Manifest.ArtifactID != result.Manifest.ArtifactID {
		t.Fatalf("Artifacts() = %+v, %v", records, err)
	}
}

func TestDownloadLectureCompletionFailureTerminalizesDurableJob(t *testing.T) {
	output := filepath.Join(t.TempDir(), "lecture.mp4")
	if err := os.WriteFile(output, []byte("media"), 0o600); err != nil {
		t.Fatal(err)
	}
	commitErr := errors.New("library commit failed")
	store := &fakeArtifactStore{record: commitErr}
	service := &Service{
		config: &config.Config{DownloadLocation: t.TempDir(), Views: "left", Quality: "720"},
		downloads: &fakeLectureDownloader{
			playlists: []client.ParsedPlaylist{{ID: 12345, SeqNo: 1, Title: "Commit failure", FirstViewURLs: []string{"left"}}},
			joined:    downloader.JoinResult{LeftOutput: output, LeftContainer: "mp4"},
		},
		library: store,
	}

	result, err := service.DownloadLecture(context.Background(), client.Lecture{
		InstituteID: 1, SubjectID: 2, SessionID: 3, TTID: 12345, Topic: "Commit failure",
	})
	if err != nil {
		t.Fatalf("DownloadLecture() error = %v", err)
	}
	if result.LibraryRecorded || result.Warning == "" {
		t.Fatalf("DownloadLecture() result = %+v, want soft commit warning", result)
	}
	if len(store.failed) != 1 || len(store.started) != 1 || store.failed[0] != store.started[0] {
		t.Fatalf("durable job lifecycle: started=%v failed=%v", store.started, store.failed)
	}
}

func TestDownloadLectureFinishesDurableJobOnDownloadFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		cause      error
		wantFailed int
		wantCancel int
	}{
		{name: "failure", cause: errors.New("download failed"), wantFailed: 1},
		{name: "cancellation", cause: context.Canceled, wantCancel: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			store := &fakeArtifactStore{}
			service := &Service{
				config: &config.Config{DownloadLocation: t.TempDir(), Views: "left", Quality: "720"},
				downloads: &fakeLectureDownloader{
					playlists:   []client.ParsedPlaylist{{ID: 12345, SeqNo: 1, Title: "Failure", FirstViewURLs: []string{"left"}}},
					downloadErr: test.cause,
				},
				library: store,
			}

			_, err := service.DownloadLecture(context.Background(), client.Lecture{
				InstituteID: 1, SubjectID: 2, SessionID: 3, TTID: 12345, Topic: "Failure",
			})
			if !errors.Is(err, test.cause) {
				t.Fatalf("DownloadLecture() error = %v, want %v", err, test.cause)
			}
			if len(store.created) != 1 || len(store.started) != 1 || len(store.failed) != test.wantFailed || len(store.canceled) != test.wantCancel {
				t.Fatalf("durable job lifecycle: created=%d started=%d failed=%d canceled=%d", len(store.created), len(store.started), len(store.failed), len(store.canceled))
			}
		})
	}
}
