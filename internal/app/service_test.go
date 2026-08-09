package app

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

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
	loaded      []string
	load        error
	wait        error
	waitStarted chan struct{}
	waitRelease <-chan struct{}
	closed      int
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
func (fake *fakeManagedPlayer) Events() <-chan player.Event                 { return nil }
func (fake *fakeManagedPlayer) Pause(context.Context, bool) error           { return nil }
func (fake *fakeManagedPlayer) SeekRelative(context.Context, float64) error { return nil }
func (fake *fakeManagedPlayer) SetVolume(context.Context, float64) error    { return nil }
func (fake *fakeManagedPlayer) SetMute(context.Context, bool) error         { return nil }
func (fake *fakeManagedPlayer) SetSpeed(context.Context, float64) error     { return nil }
func (fake *fakeManagedPlayer) CycleVideo(context.Context) error            { return nil }

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
