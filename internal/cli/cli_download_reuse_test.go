package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/artifact"
	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/events"
	"github.com/rabesss/impartus-cli/internal/library"
	"github.com/rabesss/impartus-cli/internal/lockfile"
)

func TestParseDownloadFlagsReuseVerifiedBounds(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{
			name: "rejects whole-course reuse",
			args: []string{"-s", "1", "-S", "2", "--reuse-verified"},
			want: "requires --ttid or both --start and --end",
		},
		{
			name: "rejects start without end",
			args: []string{"-s", "1", "-S", "2", "--start", "1", "--reuse-verified"},
			want: "requires --ttid or both --start and --end",
		},
		{
			name: "rejects span greater than two",
			args: []string{"-s", "1", "-S", "2", "--start", "1", "--end", "3", "--reuse-verified"},
			want: "accepts at most 2 lectures",
		},
		{
			name: "rejects inverted range",
			args: []string{"-s", "1", "-S", "2", "--start", "2", "--end", "1", "--reuse-verified"},
			want: "greater than or equal to --start",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseDownloadFlags(tc.args); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("parseDownloadFlags() error = %v, want %q", err, tc.want)
			}
		})
	}

	if _, err := parseDownloadFlags([]string{"-s", "1", "-S", "2", "--ttid", "9", "--reuse-verified"}); err != nil {
		t.Fatalf("exact TTID reuse error = %v", err)
	}
	if _, err := parseDownloadFlags([]string{"-s", "1", "-S", "2", "--start", "1", "--end", "2", "--reuse-verified"}); err != nil {
		t.Fatalf("two-lecture reuse error = %v", err)
	}
}

func TestReuseVerifiedWholeCourseStopsBeforeLogin(t *testing.T) {
	_, err := executeDownloadWithDependencies(
		[]string{"-s", "1", "-S", "2", "--reuse-verified"},
		quietDownloadPresentation(),
		downloadExecutionDependencies{
			loadConfig: func() (*config.Config, error) {
				t.Fatal("whole-course reuse reached config loading")
				return nil, nil
			},
			ensureFFmpeg: func() error {
				t.Fatal("whole-course reuse reached FFmpeg")
				return nil
			},
			login: func(context.Context, *config.Config) (*client.Client, error) {
				t.Fatal("whole-course reuse reached login")
				return nil, nil
			},
		},
	)
	if err == nil || !strings.Contains(err.Error(), "requires --ttid or both --start and --end") {
		t.Fatalf("error = %v, want pre-login reuse bound", err)
	}
}

func TestReuseVerifiedUsesStoredArtifactWithoutDownload(t *testing.T) {
	fixture := setupReuseVerifiedFixture(t)
	var human bytes.Buffer
	downloaded := false
	result, err := executeDownloadWithDependenciesContext(
		context.Background(),
		[]string{"-s", "67", "-S", "8", "--ttid", "42", "--reuse-verified"},
		downloadPresentationOptions{progressOutput: &human},
		reuseVerifiedDeps(t, fixture, func(context.Context, *config.Config, *client.Client, client.Lectures, downloadPresentationOptions) (downloadResult, error) {
			downloaded = true
			return downloadResult{}, errors.New("download should not run")
		}),
	)
	if err != nil {
		t.Fatalf("executeDownloadWithDependenciesContext() error = %v", err)
	}
	if downloaded {
		t.Fatal("verified reuse invoked the downloader")
	}
	if result.Status != "completed" || result.LectureCount != 1 || !result.LibraryRecorded || len(result.Outcomes) != 1 {
		t.Fatalf("result = %+v", result)
	}
	if result.Outcomes[0].Outcome != lectureOutcomeReused || result.Outcomes[0].TTID != fixture.lecture.TTID || result.Outcomes[0].Reason != "" {
		t.Fatalf("outcome = %+v", result.Outcomes[0])
	}
	if len(result.OutputPaths) != 1 || !strings.Contains(human.String(), "reusing verified artifact") {
		t.Fatalf("paths/human = %v / %q", result.OutputPaths, human.String())
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"outcome":"reused"`) {
		t.Fatalf("JSON = %s", raw)
	}
}

func TestReuseVerifiedFallsBackAfterTruncation(t *testing.T) {
	fixture := setupReuseVerifiedFixture(t)
	if err := os.WriteFile(fixture.mediaPath, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}

	var downloaded client.Lectures
	ffmpegCalled := false
	deps := reuseVerifiedDeps(t, fixture, func(_ context.Context, _ *config.Config, _ *client.Client, lectures client.Lectures, _ downloadPresentationOptions) (downloadResult, error) {
		downloaded = append(client.Lectures(nil), lectures...)
		return fallbackDownloadResult(fixture.lecture), nil
	})
	deps.ensureFFmpeg = func() error {
		ffmpegCalled = true
		return nil
	}
	result, err := executeDownloadWithDependenciesContext(
		context.Background(),
		[]string{"-s", "67", "-S", "8", "--ttid", "42", "--reuse-verified"},
		quietDownloadPresentation(),
		deps,
	)
	if err != nil {
		t.Fatalf("executeDownloadWithDependenciesContext() error = %v", err)
	}
	if !ffmpegCalled {
		t.Fatal("fallback download skipped FFmpeg")
	}
	if len(downloaded) != 1 || downloaded[0].TTID != fixture.lecture.TTID {
		t.Fatalf("downloaded = %+v", downloaded)
	}
	if len(result.Outcomes) != 1 || result.Outcomes[0].Outcome != lectureOutcomeDownloaded || result.Outcomes[0].Reason != string(library.FileSizeMismatch) {
		t.Fatalf("outcome = %+v", result.Outcomes)
	}
}

func TestReuseVerifiedReportsVerificationFailedWhenFallbackFails(t *testing.T) {
	fixture := setupReuseVerifiedFixture(t)
	if err := os.WriteFile(fixture.mediaPath, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}

	deps := reuseVerifiedDeps(t, fixture, func(context.Context, *config.Config, *client.Client, client.Lectures, downloadPresentationOptions) (downloadResult, error) {
		return downloadResult{Status: "failed"}, errors.New("upstream failed")
	})
	deps.ensureFFmpeg = func() error { return nil }
	result, err := executeDownloadWithDependenciesContext(
		context.Background(),
		[]string{"-s", "67", "-S", "8", "--ttid", "42", "--reuse-verified"},
		quietDownloadPresentation(),
		deps,
	)
	if err == nil || !strings.Contains(err.Error(), "upstream failed") {
		t.Fatalf("error = %v, want fallback failure", err)
	}
	if result.LibraryRecorded {
		t.Fatal("failed fallback marked libraryRecorded")
	}
	if len(result.Outcomes) != 1 || result.Outcomes[0].Outcome != lectureOutcomeVerificationFailed || result.Outcomes[0].Reason != string(library.FileSizeMismatch) {
		t.Fatalf("outcome = %+v", result.Outcomes)
	}
}

func TestReuseVerifiedDownloadsWhenArtifactIsMissing(t *testing.T) {
	fixture := setupReuseVerifiedFixture(t)
	emptyPath := reuseLibraryPath(t)
	store, openErr := library.Open(context.Background(), library.Options{Path: emptyPath})
	if openErr != nil {
		t.Fatal(openErr)
	}
	if closeErr := store.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	fixture.openLibrary = func(ctx context.Context) (*library.Store, error) {
		return library.Open(ctx, library.Options{Path: emptyPath})
	}

	var downloaded client.Lectures
	deps := reuseVerifiedDeps(t, fixture, func(_ context.Context, _ *config.Config, _ *client.Client, lectures client.Lectures, _ downloadPresentationOptions) (downloadResult, error) {
		downloaded = append(client.Lectures(nil), lectures...)
		return fallbackDownloadResult(fixture.lecture), nil
	})
	deps.ensureFFmpeg = func() error { return nil }
	result, err := executeDownloadWithDependenciesContext(
		context.Background(),
		[]string{"-s", "67", "-S", "8", "--ttid", "42", "--reuse-verified"},
		quietDownloadPresentation(),
		deps,
	)
	if err != nil {
		t.Fatalf("executeDownloadWithDependenciesContext() error = %v", err)
	}
	if len(downloaded) != 1 {
		t.Fatalf("downloaded = %+v", downloaded)
	}
	if len(result.Outcomes) != 1 || result.Outcomes[0].Outcome != lectureOutcomeDownloaded || result.Outcomes[0].Reason != reuseReasonNotFound {
		t.Fatalf("outcome = %+v", result.Outcomes)
	}
}

func TestReuseVerifiedDownloadsWhenLockIsBusy(t *testing.T) {
	fixture := setupReuseVerifiedFixture(t)
	artifactID, err := downloadArtifactID(fixture.lecture, fixture.cfg)
	if err != nil {
		t.Fatal(err)
	}
	held, err := lockfile.TryAcquire(lockfile.Path(fixture.lockDir, artifactID))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := held.Close(); closeErr != nil {
			t.Errorf("Close(held lock) error = %v", closeErr)
		}
	})

	var downloaded client.Lectures
	deps := reuseVerifiedDeps(t, fixture, func(_ context.Context, _ *config.Config, _ *client.Client, lectures client.Lectures, _ downloadPresentationOptions) (downloadResult, error) {
		downloaded = append(client.Lectures(nil), lectures...)
		return fallbackDownloadResult(fixture.lecture), nil
	})
	deps.ensureFFmpeg = func() error { return nil }
	result, err := executeDownloadWithDependenciesContext(
		context.Background(),
		[]string{"-s", "67", "-S", "8", "--ttid", "42", "--reuse-verified"},
		quietDownloadPresentation(),
		deps,
	)
	if err != nil {
		t.Fatalf("executeDownloadWithDependenciesContext() error = %v", err)
	}
	if len(downloaded) != 1 {
		t.Fatalf("downloaded = %+v", downloaded)
	}
	if len(result.Outcomes) != 1 || result.Outcomes[0].Outcome != lectureOutcomeDownloaded || result.Outcomes[0].Reason != reuseReasonLockBusy {
		t.Fatalf("outcome = %+v", result.Outcomes)
	}
}

func TestReuseVerifiedReportsDownloadFailedWhenMissingFallbackFails(t *testing.T) {
	fixture := setupReuseVerifiedFixture(t)
	emptyPath := reuseLibraryPath(t)
	store, openErr := library.Open(context.Background(), library.Options{Path: emptyPath})
	if openErr != nil {
		t.Fatal(openErr)
	}
	if closeErr := store.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	fixture.openLibrary = func(ctx context.Context) (*library.Store, error) {
		return library.Open(ctx, library.Options{Path: emptyPath})
	}

	deps := reuseVerifiedDeps(t, fixture, func(context.Context, *config.Config, *client.Client, client.Lectures, downloadPresentationOptions) (downloadResult, error) {
		return downloadResult{Status: "failed"}, errors.New("upstream failed")
	})
	deps.ensureFFmpeg = func() error { return nil }
	result, err := executeDownloadWithDependenciesContext(
		context.Background(),
		[]string{"-s", "67", "-S", "8", "--ttid", "42", "--reuse-verified"},
		quietDownloadPresentation(),
		deps,
	)
	if err == nil || !strings.Contains(err.Error(), "upstream failed") {
		t.Fatalf("error = %v, want fallback failure", err)
	}
	if len(result.Outcomes) != 1 || result.Outcomes[0].Outcome != lectureOutcomeDownloadFailed || result.Outcomes[0].Reason != reuseReasonNotFound {
		t.Fatalf("outcome = %+v", result.Outcomes)
	}
}

func TestReuseVerifiedMixesHitAndDownload(t *testing.T) {
	stored := client.Lecture{InstituteID: 4, SubjectID: 67, SessionID: 8, TTID: 42, SeqNo: 1, Topic: "Stored"}
	missing := client.Lecture{InstituteID: 4, SubjectID: 67, SessionID: 8, TTID: 43, SeqNo: 2, Topic: "Missing"}
	fixture := setupReuseVerifiedCatalog(t, client.Lectures{missing, stored}, client.Lectures{stored})

	var downloaded client.Lectures
	deps := reuseVerifiedDeps(t, fixture, func(_ context.Context, _ *config.Config, _ *client.Client, lectures client.Lectures, _ downloadPresentationOptions) (downloadResult, error) {
		downloaded = append(client.Lectures(nil), lectures...)
		return fallbackDownloadResult(missing), nil
	})
	deps.ensureFFmpeg = func() error { return nil }
	result, err := executeDownloadWithDependenciesContext(
		context.Background(),
		[]string{"-s", "67", "-S", "8", "--start", "1", "--end", "2", "--reuse-verified"},
		quietDownloadPresentation(),
		deps,
	)
	if err != nil {
		t.Fatalf("executeDownloadWithDependenciesContext() error = %v", err)
	}
	if len(downloaded) != 1 || downloaded[0].TTID != missing.TTID {
		t.Fatalf("downloaded = %+v", downloaded)
	}
	if result.LectureCount != 2 || len(result.Outcomes) != 2 {
		t.Fatalf("result = %+v", result)
	}
	if result.Outcomes[0].Outcome != lectureOutcomeReused || result.Outcomes[0].TTID != stored.TTID {
		t.Fatalf("stored outcome = %+v", result.Outcomes[0])
	}
	if result.Outcomes[1].Outcome != lectureOutcomeDownloaded || result.Outcomes[1].TTID != missing.TTID || result.Outcomes[1].Reason != reuseReasonNotFound {
		t.Fatalf("missing outcome = %+v", result.Outcomes[1])
	}
}

func TestReuseVerifiedMarksUnproducedFallbackAsDownloadFailed(t *testing.T) {
	first := client.Lecture{InstituteID: 4, SubjectID: 67, SessionID: 8, TTID: 42, SeqNo: 1, Topic: "First"}
	second := client.Lecture{InstituteID: 4, SubjectID: 67, SessionID: 8, TTID: 43, SeqNo: 2, Topic: "Second"}
	fixture := setupReuseVerifiedCatalog(t, client.Lectures{second, first}, nil)

	deps := reuseVerifiedDeps(t, fixture, func(_ context.Context, _ *config.Config, _ *client.Client, lectures client.Lectures, _ downloadPresentationOptions) (downloadResult, error) {
		return fallbackDownloadResult(first), nil
	})
	deps.ensureFFmpeg = func() error { return nil }
	result, err := executeDownloadWithDependenciesContext(
		context.Background(),
		[]string{"-s", "67", "-S", "8", "--start", "1", "--end", "2", "--reuse-verified"},
		quietDownloadPresentation(),
		deps,
	)
	if err != nil {
		t.Fatalf("executeDownloadWithDependenciesContext() error = %v", err)
	}
	if len(result.Outcomes) != 2 {
		t.Fatalf("outcomes = %+v", result.Outcomes)
	}
	if result.Outcomes[0].Outcome != lectureOutcomeDownloaded || result.Outcomes[0].TTID != first.TTID {
		t.Fatalf("produced outcome = %+v", result.Outcomes[0])
	}
	if result.Outcomes[1].Outcome != lectureOutcomeDownloadFailed || result.Outcomes[1].TTID != second.TTID {
		t.Fatalf("omitted outcome = %+v", result.Outcomes[1])
	}
}

func TestReuseVerifiedRejectsDuplicateScopedLectures(t *testing.T) {
	lecture := client.Lecture{InstituteID: 4, SubjectID: 67, SessionID: 8, TTID: 42, SeqNo: 1, Topic: "Dup"}
	fixture := setupReuseVerifiedCatalog(t, client.Lectures{lecture, lecture}, nil)
	_, err := executeDownloadWithDependenciesContext(
		context.Background(),
		[]string{"-s", "67", "-S", "8", "--start", "1", "--end", "2", "--reuse-verified"},
		quietDownloadPresentation(),
		reuseVerifiedDeps(t, fixture, func(context.Context, *config.Config, *client.Client, client.Lectures, downloadPresentationOptions) (downloadResult, error) {
			return downloadResult{}, errors.New("download should not run")
		}),
	)
	if err == nil || !strings.Contains(err.Error(), "duplicate scoped lecture identity") {
		t.Fatalf("error = %v, want duplicate identity rejection", err)
	}
}

func TestReuseVerifiedEventsSkipProgressAndKeepSchema(t *testing.T) {
	fixture := setupReuseVerifiedFixture(t)
	var output bytes.Buffer
	err := runDownloadEventsWithDependenciesContext(
		context.Background(),
		[]string{"-s", "67", "-S", "8", "--ttid", "42", "--reuse-verified"},
		&output,
		reuseVerifiedDeps(t, fixture, func(context.Context, *config.Config, *client.Client, client.Lectures, downloadPresentationOptions) (downloadResult, error) {
			return downloadResult{}, errors.New("download should not run")
		}),
		func() time.Time { return time.Unix(1, 0).UTC() },
		"job-reuse",
	)
	if err != nil {
		t.Fatalf("runDownloadEventsWithDependenciesContext() error = %v", err)
	}
	decoded := decodeCLIEvents(t, output.String())
	wantTypes := []string{events.JobStarted, events.LectureStarted, events.LectureCompleted, events.JobCompleted}
	if len(decoded) != len(wantTypes) {
		t.Fatalf("events = %+v, want %v", decoded, wantTypes)
	}
	for index, wantType := range wantTypes {
		if decoded[index].Type != wantType {
			t.Fatalf("event[%d].Type = %q, want %q", index, decoded[index].Type, wantType)
		}
		if decoded[index].SchemaVersion != 1 {
			t.Fatalf("event[%d].schemaVersion = %d", index, decoded[index].SchemaVersion)
		}
	}
	details, ok := decoded[2].Details.(map[string]any)
	if !ok || details["stage"] != "artifact_reused" {
		t.Fatalf("completed details = %#v", decoded[2].Details)
	}
}

func TestDownloadJSONOmitsOutcomesWithoutReuseFlag(t *testing.T) {
	raw, err := json.Marshal(downloadResult{Status: "completed", OutputPaths: []string{"/tmp/out.mp4"}, LectureCount: 1, LibraryRecorded: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "outcomes") {
		t.Fatalf("non-reuse JSON leaked outcomes: %s", raw)
	}
}

type reuseVerifiedFixture struct {
	lecture     client.Lecture
	cfg         *config.Config
	apiClient   *client.Client
	openLibrary func(context.Context) (*library.Store, error)
	lockDir     string
	mediaPath   string
}

func setupReuseVerifiedFixture(t *testing.T) reuseVerifiedFixture {
	t.Helper()
	lecture := client.Lecture{InstituteID: 4, SubjectID: 67, SessionID: 8, TTID: 42, SeqNo: 99, Topic: "Requested exact row"}
	return setupReuseVerifiedCatalog(t, client.Lectures{lecture}, client.Lectures{lecture})
}

func setupReuseVerifiedCatalog(t *testing.T, catalog, recorded client.Lectures) reuseVerifiedFixture {
	t.Helper()
	if len(catalog) == 0 {
		t.Fatal("catalog is required")
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/subjects/67/lectures/8" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(catalog); err != nil {
			t.Errorf("encode lecture response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	cfg := &config.Config{
		BaseURL:          server.URL,
		Token:            "synthetic-test-token",
		DownloadLocation: t.TempDir(),
		Views:            "left",
		Quality:          "720",
	}
	databasePath := reuseLibraryPath(t)
	store, err := library.Open(context.Background(), library.Options{Path: databasePath})
	if err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(t.TempDir(), "lecture.mp4")
	for index, lecture := range recorded {
		path := mediaPath
		if index > 0 {
			path = filepath.Join(t.TempDir(), fmt.Sprintf("lecture-%d.mp4", lecture.TTID))
		}
		manifest := buildCLIReuseManifest(t, lecture, cfg, path)
		if err := store.RecordManifest(context.Background(), manifest); err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			mediaPath = path
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	return reuseVerifiedFixture{
		lecture:   catalog[len(catalog)-1],
		cfg:       cfg,
		apiClient: client.New(server.Client(), func() string { return "reuse-test" }),
		openLibrary: func(ctx context.Context) (*library.Store, error) {
			return library.Open(ctx, library.Options{Path: databasePath})
		},
		lockDir:   filepath.Join(t.TempDir(), "locks"),
		mediaPath: mediaPath,
	}
}

func reuseVerifiedDeps(
	t *testing.T,
	fixture reuseVerifiedFixture,
	download func(context.Context, *config.Config, *client.Client, client.Lectures, downloadPresentationOptions) (downloadResult, error),
) downloadExecutionDependencies {
	t.Helper()
	return downloadExecutionDependencies{
		ensureFFmpeg: func() error {
			t.Fatal("verified reuse reached FFmpeg")
			return nil
		},
		loadConfig: func() (*config.Config, error) { return fixture.cfg, nil },
		login:      func(context.Context, *config.Config) (*client.Client, error) { return fixture.apiClient, nil },
		downloadLectures: func(ctx context.Context, cfg *config.Config, apiClient *client.Client, lectures client.Lectures, presentation downloadPresentationOptions) (downloadResult, error) {
			return download(ctx, cfg, apiClient, lectures, presentation)
		},
		recordArtifacts: func(context.Context, []artifact.Manifest) error { return nil },
		openLibrary:     fixture.openLibrary,
		artifactLockDir: fixture.lockDir,
	}
}

func fallbackDownloadResult(lecture client.Lecture) downloadResult {
	manifest := artifact.Manifest{
		ArtifactID: "synthetic-fallback",
		Lecture: artifact.Lecture{
			InstituteID: lecture.InstituteID,
			SubjectID:   lecture.SubjectID,
			SessionID:   lecture.SessionID,
			TTID:        lecture.TTID,
		},
		Files: []artifact.File{{Path: "/tmp/fallback.mp4"}},
	}
	return downloadResult{Status: "completed", LectureCount: 1, OutputPaths: []string{"/tmp/fallback.mp4"}, Artifacts: []artifact.Manifest{manifest}, LibraryRecorded: true}
}

func reuseLibraryPath(t *testing.T) string {
	t.Helper()
	parent := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(parent, "library.db")
}

func buildCLIReuseManifest(t *testing.T, lecture client.Lecture, cfg *config.Config, path string) artifact.Manifest {
	t.Helper()
	if err := os.WriteFile(path, []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := artifact.Build(artifact.BuildInput{
		Lecture: artifact.Lecture{
			TTID:        lecture.TTID,
			InstituteID: lecture.InstituteID,
			SubjectID:   lecture.SubjectID,
			SessionID:   lecture.SessionID,
			SeqNo:       lecture.SeqNo,
			Topic:       lecture.Topic,
		},
		Selection:  artifact.Selection{Views: cfg.Views, Quality: cfg.Quality, AudioOnly: cfg.AudioOnly, AudioFormat: cfg.AudioFormat},
		Files:      []artifact.FileSpec{{Path: path, Role: "video", View: "left", Container: "mp4"}},
		ProducedAt: time.Date(2026, time.August, 9, 8, 0, 0, 0, time.UTC),
		Producer:   artifact.Producer{Name: "impartus", Version: "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}
