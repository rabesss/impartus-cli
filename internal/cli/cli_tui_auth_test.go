package cli

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/rabesss/impartus-cli/internal/app"
	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/library"
	"github.com/rabesss/impartus-cli/internal/tuihost"
	"github.com/rabesss/impartus-cli/internal/tuiproto"
	"github.com/rabesss/impartus-cli/internal/tuisession"
)

type tuiRemoteStub struct {
	courseName string
}

func (stub *tuiRemoteStub) Courses(context.Context) (client.Courses, error) {
	return client.Courses{{SubjectName: stub.courseName}}, nil
}

func (stub *tuiRemoteStub) Lectures(context.Context, client.Course) (client.Lectures, error) {
	return nil, nil
}

func (stub *tuiRemoteStub) DownloadLecture(context.Context, client.Lecture) (app.DownloadResult, error) {
	return app.DownloadResult{}, nil
}

func (stub *tuiRemoteStub) RecordPlayback(context.Context, library.PlaybackState) error {
	return nil
}

func (stub *tuiRemoteStub) ResumeLecture(context.Context, client.Lecture) (library.PlaybackState, bool, error) {
	return library.PlaybackState{}, false, nil
}

func (stub *tuiRemoteStub) StartLecture(context.Context, client.Lecture, float64) (app.PlaybackStart, error) {
	return app.PlaybackStart{}, nil
}

type tuiArtifactStoreStub struct {
	records []library.ArtifactRecord
}

func (stub *tuiArtifactStoreStub) ListArtifacts(context.Context) ([]library.ArtifactRecord, error) {
	return append([]library.ArtifactRecord(nil), stub.records...), nil
}

func TestTUIAuthenticationCoordinatorDoesNotBuildWithoutCredentialsAndRecoversInProcess(t *testing.T) {
	store := &tuiArtifactStoreStub{records: []library.ArtifactRecord{{}}}
	var configured atomic.Bool
	var builds atomic.Int64
	coordinator := newTUIAuthenticationCoordinator(
		store,
		func() (*config.Config, error) {
			if !configured.Load() {
				return nil, config.ErrCredentialsRequired
			}
			return &config.Config{Username: "user", Password: "password"}, nil
		},
		func(context.Context, *config.Config) (tuiRemoteService, error) {
			builds.Add(1)
			return &tuiRemoteStub{courseName: "Recovered"}, nil
		},
	)

	if err := coordinator.Retry(t.Context()); !errors.Is(err, config.ErrCredentialsRequired) {
		t.Fatalf("Retry() error = %T %q, want ErrCredentialsRequired", err, err)
	}
	if builds.Load() != 0 {
		t.Fatalf("candidate builds = %d, want zero without credentials", builds.Load())
	}
	if coordinator.Status() != tuiproto.AuthStatusUnavailable {
		t.Fatalf("Status() = %q, want unavailable", coordinator.Status())
	}
	if _, err := coordinator.Courses(t.Context()); !errors.Is(err, errTUIAuthenticationUnavailable) {
		t.Fatalf("Courses() error = %v, want unavailable identity", err)
	}
	if artifacts, err := coordinator.Artifacts(t.Context()); err != nil || len(artifacts) != 1 {
		t.Fatalf("Artifacts() = (%d, %v), want one local artifact", len(artifacts), err)
	}

	configured.Store(true)
	if err := coordinator.Retry(t.Context()); err != nil {
		t.Fatalf("Retry() after credentials error = %v", err)
	}
	if coordinator.Status() != tuiproto.AuthStatusReady || builds.Load() != 1 {
		t.Fatalf("ready state = %q, builds %d", coordinator.Status(), builds.Load())
	}
	courses, err := coordinator.Courses(t.Context())
	if err != nil || len(courses) != 1 || courses[0].SubjectName != "Recovered" {
		t.Fatalf("Courses() = (%+v, %v)", courses, err)
	}
}

func TestTUIAuthenticationCoordinatorPublishesOnlySuccessfulCandidates(t *testing.T) {
	var attempt atomic.Int64
	coordinator := newTUIAuthenticationCoordinator(
		&tuiArtifactStoreStub{},
		func() (*config.Config, error) { return &config.Config{Username: "user", Password: "password"}, nil },
		func(context.Context, *config.Config) (tuiRemoteService, error) {
			if attempt.Add(1) == 1 {
				return &tuiRemoteStub{courseName: "Original"}, nil
			}
			return nil, &client.AuthenticationError{Operation: "login", StatusCode: 401}
		},
	)

	if err := coordinator.Retry(t.Context()); err != nil {
		t.Fatalf("initial Retry() error = %v", err)
	}
	if err := coordinator.Retry(t.Context()); !errors.Is(err, client.ErrAuthentication) {
		t.Fatalf("failed replacement error = %T %q, want ErrAuthentication", err, err)
	}
	if coordinator.Status() != tuiproto.AuthStatusReady {
		t.Fatalf("failed replacement downgraded status to %q", coordinator.Status())
	}
	courses, err := coordinator.Courses(t.Context())
	if err != nil || courses[0].SubjectName != "Original" {
		t.Fatalf("published service changed after failed replacement: (%+v, %v)", courses, err)
	}
}

func TestTUIAuthenticationCoordinatorSnapshotsServicesRaceSafely(t *testing.T) {
	var generation atomic.Int64
	coordinator := newTUIAuthenticationCoordinator(
		&tuiArtifactStoreStub{},
		func() (*config.Config, error) { return &config.Config{Username: "user", Password: "password"}, nil },
		func(context.Context, *config.Config) (tuiRemoteService, error) {
			return &tuiRemoteStub{courseName: string(rune('A' + generation.Add(1) - 1))}, nil
		},
	)
	if err := coordinator.Retry(t.Context()); err != nil {
		t.Fatalf("initial Retry() error = %v", err)
	}

	var wait sync.WaitGroup
	for range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range 100 {
				courses, err := coordinator.Courses(t.Context())
				if err != nil || len(courses) != 1 || courses[0].SubjectName == "" {
					t.Errorf("Courses() = (%+v, %v)", courses, err)
					return
				}
			}
		}()
	}
	for range 8 {
		if err := coordinator.Retry(t.Context()); err != nil {
			t.Fatalf("Retry() error = %v", err)
		}
	}
	wait.Wait()
}

func TestRecoverableTUIAuthenticationErrorClassification(t *testing.T) {
	if !isRecoverableTUIAuthenticationError(config.ErrCredentialsRequired) {
		t.Fatal("missing credentials must be recoverable")
	}
	if !isRecoverableTUIAuthenticationError(&tuiAuthenticationAttemptError{err: &client.AuthenticationError{Operation: "login", StatusCode: 401}}) {
		t.Fatal("typed upstream authentication failure must be recoverable")
	}
	if !isRecoverableTUIAuthenticationError(&tuiAuthenticationAttemptError{err: errors.New("dial upstream: temporary network failure")}) {
		t.Fatal("non-cancellation login failure must be recoverable")
	}
	if isRecoverableTUIAuthenticationError(errors.New("baseUrl must be a valid HTTP(S) URL")) {
		t.Fatal("non-auth configuration validation failure must remain fatal")
	}
	if isRecoverableTUIAuthenticationError(context.Canceled) {
		t.Fatal("parent cancellation must not be classified as recoverable")
	}
	if isRecoverableTUIAuthenticationError(nil) {
		t.Fatal("nil must not be classified as recoverable")
	}
}

func TestTUIAuthenticationCoordinatorClassifiesCandidateFailuresWithoutLeakingDetails(t *testing.T) {
	secret := "upstream response body secret"
	coordinator := newTUIAuthenticationCoordinator(
		&tuiArtifactStoreStub{},
		func() (*config.Config, error) { return &config.Config{Username: "user", Password: "password"}, nil },
		func(context.Context, *config.Config) (tuiRemoteService, error) { return nil, errors.New(secret) },
	)

	err := coordinator.Retry(t.Context())
	if !isRecoverableTUIAuthenticationError(err) {
		t.Fatalf("Retry() error = %T %q, want recoverable candidate identity", err, err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("Retry() error disclosed candidate detail: %q", err)
	}
}

func TestTUIAuthenticationCoordinatorDoesNotPublishAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	coordinator := newTUIAuthenticationCoordinator(
		&tuiArtifactStoreStub{},
		func() (*config.Config, error) { return &config.Config{Username: "user", Password: "password"}, nil },
		func(context.Context, *config.Config) (tuiRemoteService, error) {
			cancel()
			return &tuiRemoteStub{courseName: "must-not-publish"}, nil
		},
	)

	if err := coordinator.Retry(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Retry() error = %v, want context.Canceled", err)
	}
	if coordinator.Status() != tuiproto.AuthStatusUnavailable {
		t.Fatalf("Status() = %q after canceled retry, want unavailable", coordinator.Status())
	}
}

func TestTUIAuthenticationCoordinatorStopsBeforeLoadWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	loads := 0
	coordinator := newTUIAuthenticationCoordinator(
		&tuiArtifactStoreStub{},
		func() (*config.Config, error) {
			loads++
			return nil, config.ErrCredentialsRequired
		},
		func(context.Context, *config.Config) (tuiRemoteService, error) { return nil, nil },
	)

	if err := coordinator.Retry(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Retry() error = %v, want context.Canceled", err)
	}
	if loads != 0 {
		t.Fatalf("config loads = %d, want zero after cancellation", loads)
	}
}

func TestRunTUIStartsUnavailableSessionForMissingCredentials(t *testing.T) {
	restoreCLIState(t)
	var opened *library.Store
	openTUILibraryFn = func(ctx context.Context, _ library.Options) (*library.Store, error) {
		stateDir := filepath.Join(t.TempDir(), "state")
		if err := os.Mkdir(stateDir, 0o700); err != nil {
			return nil, err
		}
		var err error
		opened, err = library.Open(ctx, library.Options{Path: filepath.Join(stateDir, "library.db")})
		return opened, err
	}
	getTUIDoctorReportFn = func([]string) (doctorReport, error) { return doctorReport{OK: true}, nil }
	resolveTUIExecutableFn = func(string) (string, error) { return "unused-sidecar", nil }
	loadTUIResolvedFn = func(string) (*config.Config, error) {
		return &config.Config{BaseURL: "https://example.com", Quality: "450", Views: "both"}, nil
	}
	newLoggedInFn = func(context.Context, *config.Config) (*client.Client, error) {
		t.Fatal("NewLoggedIn called without credentials")
		return nil, nil
	}
	started := false
	runTUIHostFn = func(ctx context.Context, options tuihost.Options) error {
		started = true
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, options.Session.BaseURL()+"/health", nil)
		if err != nil {
			t.Fatalf("create health request: %v", err)
		}
		request.Header.Set(tuiproto.CapabilityHeader, options.Session.Capability())
		request.Header.Set(tuiproto.ProtocolHeader, tuiproto.ProtocolVersion)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("health request: %v", err)
		}
		defer func() {
			if closeErr := response.Body.Close(); closeErr != nil {
				t.Errorf("close health response: %v", closeErr)
			}
		}()
		var health tuiproto.Health
		if err := json.NewDecoder(response.Body).Decode(&health); err != nil {
			t.Fatalf("decode health: %v", err)
		}
		if response.StatusCode != http.StatusOK || health.AuthStatus != tuiproto.AuthStatusUnavailable {
			t.Fatalf("health = (%d, %+v)", response.StatusCode, health)
		}
		return nil
	}

	if err := runTUI(); err != nil {
		t.Fatalf("runTUI() error = %v", err)
	}
	if !started {
		t.Fatal("runTUI() did not start the private session host")
	}
	if opened == nil {
		t.Fatal("runTUI() did not open the local library")
	}
	if _, err := opened.ListArtifacts(t.Context()); err == nil {
		t.Fatal("runTUI() left the local library open")
	}
}

func TestRunTUIKeepsInvalidNonCredentialConfigurationFatal(t *testing.T) {
	restoreCLIState(t)
	var opened *library.Store
	openTUILibraryFn = func(ctx context.Context, _ library.Options) (*library.Store, error) {
		stateDir := filepath.Join(t.TempDir(), "state")
		if err := os.Mkdir(stateDir, 0o700); err != nil {
			return nil, err
		}
		var err error
		opened, err = library.Open(ctx, library.Options{Path: filepath.Join(stateDir, "library.db")})
		return opened, err
	}
	getTUIDoctorReportFn = func([]string) (doctorReport, error) { return doctorReport{OK: true}, nil }
	resolveTUIExecutableFn = func(string) (string, error) { return "unused-sidecar", nil }
	wantErr := errors.New("baseUrl must be a valid HTTP(S) URL")
	loadTUIResolvedFn = func(string) (*config.Config, error) { return nil, wantErr }
	startTUISessionFn = func(context.Context, tuisession.Options) (*tuisession.Session, error) {
		t.Fatal("tuisession.Start called after invalid configuration")
		return nil, nil
	}
	runTUIHostFn = func(context.Context, tuihost.Options) error {
		t.Fatal("tuihost.Run called after invalid configuration")
		return nil
	}

	err := runTUI()
	if !errors.Is(err, tuisession.ErrAuthenticationConfiguration) || !strings.Contains(err.Error(), wantErr.Error()) {
		t.Fatalf("runTUI() error = %T %q, want safe configuration failure", err, err)
	}
	if opened == nil {
		t.Fatal("runTUI() did not open the local library")
	}
	if _, listErr := opened.ListArtifacts(t.Context()); listErr == nil {
		t.Fatal("runTUI() left the local library open after fatal configuration")
	}
}
