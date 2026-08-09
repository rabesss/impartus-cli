package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vbauerster/mpb/v8"

	"github.com/rabesss/impartus-cli/internal/artifact"
	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/downloader"
)

type fakeLectureDownloadRunner struct {
	playlists   []client.ParsedPlaylist
	results     []downloader.JoinResult
	progress    []*mpb.Progress
	trackers    []*downloader.ProgressTracker
	fetches     int
	downloads   int
	next        int
	downloadErr error
}

func (f *fakeLectureDownloadRunner) FetchLecturePlaylists(_ context.Context, lectures []client.Lecture) ([]client.ParsedPlaylist, error) {
	f.fetches++
	playlists := append([]client.ParsedPlaylist(nil), f.playlists...)
	for index := range playlists {
		if index >= len(lectures) {
			break
		}
		if playlists[index].InstituteID == 0 {
			playlists[index].InstituteID = lectures[index].InstituteID
		}
		if playlists[index].SubjectID == 0 {
			playlists[index].SubjectID = lectures[index].SubjectID
		}
		if playlists[index].SessionID == 0 {
			playlists[index].SessionID = lectures[index].SessionID
		}
	}
	return playlists, nil
}

func (f *fakeLectureDownloadRunner) DownloadAndJoinPlaylist(_ context.Context, _ client.ParsedPlaylist, progress *mpb.Progress, tracker *downloader.ProgressTracker) (downloader.JoinResult, error) {
	f.progress = append(f.progress, progress)
	f.trackers = append(f.trackers, tracker)
	f.downloads++
	if f.downloadErr != nil {
		return downloader.JoinResult{}, f.downloadErr
	}
	result := f.results[f.next]
	f.next++
	return result, nil
}

func TestDownloadRejectsSelectedLectureWithoutPlaylistBeforeMediaWork(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	runner := &fakeLectureDownloadRunner{
		playlists: []client.ParsedPlaylist{{InstituteID: 1, SubjectID: 2, SessionID: 3, ID: 10}},
		results:   materializeJoinResults(t, outputDir, []downloader.JoinResult{{LeftOutput: "one.mp4"}}),
	}
	lectures := client.Lectures{
		{InstituteID: 1, SubjectID: 2, SessionID: 3, TTID: 10},
		{InstituteID: 1, SubjectID: 2, SessionID: 3, TTID: 11},
	}
	_, err := downloadLecturesWithRunner(context.Background(), &config.Config{
		DownloadLocation: outputDir,
		Views:            "left",
		Quality:          "720",
	}, runner, lectures, quietDownloadPresentation())
	if err == nil || !strings.Contains(err.Error(), "ttid=11") || !strings.Contains(err.Error(), "no playlist") {
		t.Fatalf("downloadLecturesWithRunner() error = %v, want missing TTID context", err)
	}
	if runner.downloads != 0 {
		t.Fatalf("downloads = %d, want no partial media work", runner.downloads)
	}
}

func TestDownloadFailureIncludesLectureScope(t *testing.T) {
	t.Parallel()

	lecture := client.Lecture{InstituteID: 1, SubjectID: 2, SessionID: 3, TTID: 10}
	runner := &fakeLectureDownloadRunner{
		playlists:   []client.ParsedPlaylist{{InstituteID: 1, SubjectID: 2, SessionID: 3, ID: 10}},
		downloadErr: errors.New("chunk failed"),
	}
	_, err := downloadLecturesWithRunner(context.Background(), &config.Config{
		DownloadLocation: t.TempDir(),
		Views:            "left",
		Quality:          "720",
	}, runner, client.Lectures{lecture}, quietDownloadPresentation())
	if err == nil || !strings.Contains(err.Error(), "ttid=10") || !strings.Contains(err.Error(), "chunk failed") {
		t.Fatalf("downloadLecturesWithRunner() error = %v, want lecture-scoped cause", err)
	}
}

func TestDownloadLectureCountTracksCompletedPlaylists(t *testing.T) {
	tests := []struct {
		name        string
		results     []downloader.JoinResult
		wantCount   int
		wantOutputs int
	}{
		{
			name:        "one lecture one output",
			results:     []downloader.JoinResult{{LeftOutput: "left.mp4"}},
			wantCount:   1,
			wantOutputs: 1,
		},
		{
			name:        "one lecture multiple outputs",
			results:     []downloader.JoinResult{{LeftOutput: "left.mp4", RightOutput: "right.mp4", BothOutput: "both.mp4"}},
			wantCount:   1,
			wantOutputs: 3,
		},
		{
			name:        "multiple lectures",
			results:     []downloader.JoinResult{{LeftOutput: "one.mp4"}, {LeftOutput: "two.mp4", RightOutput: "two-right.mp4"}},
			wantCount:   2,
			wantOutputs: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputDir := t.TempDir()
			playlists := make([]client.ParsedPlaylist, len(tt.results))
			lectures := make(client.Lectures, len(tt.results))
			for i := range playlists {
				playlists[i] = client.ParsedPlaylist{ID: i + 1}
				lectures[i] = client.Lecture{InstituteID: 4, SubjectID: 67, SessionID: 8, TTID: i + 1}
			}
			runner := &fakeLectureDownloadRunner{playlists: playlists, results: materializeJoinResults(t, outputDir, tt.results)}
			result, err := downloadLecturesWithRunner(context.Background(), &config.Config{DownloadLocation: outputDir, Views: "both", Quality: "720"}, runner, lectures, quietDownloadPresentation())
			if err != nil {
				t.Fatalf("downloadLecturesWithRunner() error = %v", err)
			}
			if result.LectureCount != tt.wantCount {
				t.Fatalf("LectureCount = %d, want %d", result.LectureCount, tt.wantCount)
			}
			if len(result.OutputPaths) != tt.wantOutputs {
				t.Fatalf("len(OutputPaths) = %d, want %d", len(result.OutputPaths), tt.wantOutputs)
			}
			if len(result.Artifacts) != tt.wantCount {
				t.Fatalf("len(Artifacts) = %d, want %d", len(result.Artifacts), tt.wantCount)
			}
			for _, progress := range runner.progress {
				if progress != nil {
					t.Fatal("quiet download passed a progress container to the downloader")
				}
			}
		})
	}
}

func TestHumanDownloadPresentationKeepsWarningsAndProgress(t *testing.T) {
	var progressOutput bytes.Buffer
	var warningOutput bytes.Buffer
	presentation := downloadPresentationOptions{
		showProgress:   true,
		progressOutput: &progressOutput,
		warningOutput:  &warningOutput,
	}
	warnNoAudioLectures(presentation.warningOutput, client.Lectures{{NoAudio: 1}}, false)
	if !strings.Contains(warningOutput.String(), "1 lecture(s)") {
		t.Fatalf("human warning output = %q", warningOutput.String())
	}

	outputDir := t.TempDir()
	runner := &fakeLectureDownloadRunner{
		playlists: []client.ParsedPlaylist{{ID: 1}},
		results:   materializeJoinResults(t, outputDir, []downloader.JoinResult{{LeftOutput: "left.mp4"}}),
	}
	cfg := &config.Config{
		DownloadLocation: outputDir,
		Quality:          "720",
		Views:            "left",
		ProgressTracking: config.ProgressConfig{
			Enabled:         true,
			ShowSpeed:       true,
			ShowETA:         true,
			UpdateInterval:  "500ms",
			SpeedWindowSize: 3,
		},
	}
	if _, err := downloadLecturesWithRunner(context.Background(), cfg, runner, client.Lectures{{InstituteID: 4, SubjectID: 67, SessionID: 8, TTID: 1}}, presentation); err != nil {
		t.Fatalf("downloadLecturesWithRunner() error = %v", err)
	}
	if len(runner.progress) != 1 || runner.progress[0] == nil {
		t.Fatal("human download did not pass a progress container to the downloader")
	}
	if humanDownloadPresentation().diagnosticOutput != nil {
		t.Fatal("human downloads should preserve the downloader's standard diagnostics")
	}
	if len(runner.trackers) != 1 || runner.trackers[0] == nil {
		t.Fatal("human download did not pass a progress tracker to the downloader")
	}
}

func TestProgressTrackingModeMatrix(t *testing.T) {
	tests := []struct {
		name         string
		presentation downloadPresentationOptions
		enabled      bool
		wantProgress bool
	}{
		{name: "human enabled", presentation: downloadPresentationOptions{showProgress: true, progressOutput: io.Discard}, enabled: true, wantProgress: true},
		{name: "human disabled", presentation: downloadPresentationOptions{showProgress: true, progressOutput: io.Discard}, enabled: false},
		{name: "json enabled", presentation: quietDownloadPresentation(), enabled: true},
		{name: "json disabled", presentation: quietDownloadPresentation(), enabled: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputDir := t.TempDir()
			runner := &fakeLectureDownloadRunner{
				playlists: []client.ParsedPlaylist{{ID: 1}},
				results:   materializeJoinResults(t, outputDir, []downloader.JoinResult{{LeftOutput: "left.mp4"}}),
			}
			cfg := &config.Config{
				DownloadLocation: outputDir,
				Quality:          "720",
				Views:            "left",
				ProgressTracking: config.ProgressConfig{
					Enabled:         tt.enabled,
					ShowSpeed:       true,
					ShowETA:         true,
					UpdateInterval:  "500ms",
					SpeedWindowSize: 3,
				},
			}
			if _, err := downloadLecturesWithRunner(context.Background(), cfg, runner, client.Lectures{{InstituteID: 4, SubjectID: 67, SessionID: 8, TTID: 1}}, tt.presentation); err != nil {
				t.Fatalf("downloadLecturesWithRunner() error = %v", err)
			}

			gotProgress := len(runner.progress) == 1 && runner.progress[0] != nil
			gotTracker := len(runner.trackers) == 1 && runner.trackers[0] != nil
			if gotProgress != tt.wantProgress {
				t.Fatalf("progress container present = %v, want %v", gotProgress, tt.wantProgress)
			}
			if gotTracker != tt.wantProgress {
				t.Fatalf("progress tracker present = %v, want %v", gotTracker, tt.wantProgress)
			}
		})
	}
}

func materializeJoinResults(t *testing.T, outputDir string, results []downloader.JoinResult) []downloader.JoinResult {
	t.Helper()
	materialized := make([]downloader.JoinResult, len(results))
	for i, result := range results {
		for view, output := range map[string]struct {
			path      *string
			container *string
		}{
			"left":  {path: &result.LeftOutput, container: &result.LeftContainer},
			"right": {path: &result.RightOutput, container: &result.RightContainer},
			"both":  {path: &result.BothOutput, container: &result.BothContainer},
		} {
			if *output.path == "" {
				continue
			}
			if *output.container == "" {
				*output.container = strings.TrimPrefix(strings.ToLower(filepath.Ext(*output.path)), ".")
			}
			*output.path = filepath.Join(outputDir, fmt.Sprintf("%d-%s-%s", i, view, filepath.Base(*output.path)))
			if err := os.WriteFile(*output.path, []byte("media"), 0o600); err != nil {
				t.Fatalf("write fake %s output: %v", view, err)
			}
		}
		materialized[i] = result
	}
	return materialized
}

func TestJSONDownloadStreamContract(t *testing.T) {
	restoreCLIState(t)
	cfg, apiClient, cleanup := newJSONDownloadIntegration(t, false)
	defer cleanup()

	deps := downloadExecutionDependencies{
		ensureFFmpeg: func() error { return nil },
		initClient: func(context.Context) (*config.Config, *client.Client, error) {
			return cfg, apiClient, nil
		},
		downloadLectures: downloadLectures,
	}
	var humanProgress bytes.Buffer
	var humanWarnings bytes.Buffer
	humanPresentation := downloadPresentationOptions{
		showProgress:   true,
		progressOutput: &humanProgress,
		warningOutput:  &humanWarnings,
	}
	if _, err := executeDownloadWithDependencies([]string{"-s", "1", "-S", "2"}, humanPresentation, deps); err != nil {
		t.Fatalf("human download returned error: %v", err)
	}
	if !strings.Contains(humanWarnings.String(), "1 lecture(s)") {
		t.Fatalf("human download did not emit its no-audio warning: %q", humanWarnings.String())
	}

	runDownloadJSONFn = func(args []string) (downloadResult, error) {
		return runDownloadJSONWithDependencies(args, deps)
	}
	os.Args = []string{"impartus", "download", "--json", "-s", "1", "-S", "2"}
	stdout, stderr, err := captureOutputStreams(t, func() error { return Execute("test", "test") })
	if err != nil {
		t.Fatalf("JSON download returned error: %v", err)
	}
	if stderr != "" {
		t.Fatalf("successful JSON download wrote stderr: %q", stderr)
	}

	decoder := json.NewDecoder(strings.NewReader(stdout))
	var envelope struct {
		Success bool `json:"success"`
		Data    struct {
			Status       string              `json:"status"`
			OutputPaths  []string            `json:"outputPaths"`
			LectureCount int                 `json:"lectureCount"`
			Artifacts    []artifact.Manifest `json:"artifacts"`
		} `json:"data"`
	}
	if decodeErr := decoder.Decode(&envelope); decodeErr != nil {
		t.Fatalf("decode JSON stdout: %v; stdout=%q", decodeErr, stdout)
	}
	if decodeErr := decoder.Decode(&struct{}{}); decodeErr != io.EOF {
		t.Fatalf("stdout contained more than one JSON value: %v; stdout=%q", decodeErr, stdout)
	}
	if !envelope.Success || envelope.Data.Status != "completed" || envelope.Data.LectureCount != 1 || len(envelope.Data.OutputPaths) != 1 {
		t.Fatalf("unexpected JSON download envelope: %+v", envelope)
	}
	if len(envelope.Data.Artifacts) != 1 {
		t.Fatalf("len(artifacts) = %d, want 1", len(envelope.Data.Artifacts))
	}
	manifest := envelope.Data.Artifacts[0]
	if manifest.SchemaVersion != 1 || manifest.Lecture.TTID != 7 || manifest.Lecture.InstituteID != 4 || manifest.Lecture.SubjectID != 1 || manifest.Lecture.SessionID != 2 {
		t.Fatalf("unexpected JSON artifact manifest: %+v", manifest)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Role != "video" || manifest.Files[0].View != "left" || manifest.Files[0].Container != "mp4" {
		t.Fatalf("unexpected JSON artifact files: %+v", manifest.Files)
	}
}

func TestJSONDownloadFailureStreamSuppressesDownloaderLogs(t *testing.T) {
	restoreCLIState(t)
	cfg, apiClient, cleanup := newJSONDownloadIntegration(t, true)
	defer cleanup()

	deps := downloadExecutionDependencies{
		ensureFFmpeg: func() error { return nil },
		initClient: func(context.Context) (*config.Config, *client.Client, error) {
			return cfg, apiClient, nil
		},
		downloadLectures: downloadLectures,
	}
	runDownloadJSONFn = func(args []string) (downloadResult, error) {
		return runDownloadJSONWithDependencies(args, deps)
	}
	os.Args = []string{"impartus", "download", "--json", "-s", "1", "-S", "2"}

	stdout, stderr, runErr := captureOutputStreams(t, func() error {
		executeErr := Execute("test", "test")
		if executeErr != nil {
			if _, writeErr := fmt.Fprintln(os.Stderr, executeErr); writeErr != nil {
				return fmt.Errorf("write JSON error envelope: %w", writeErr)
			}
		}
		return executeErr
	})

	if runErr == nil {
		t.Fatal("expected real downloader failure to return an error")
	}
	if stdout != "" {
		t.Fatalf("failed JSON download wrote stdout: %q", stdout)
	}
	decoder := json.NewDecoder(strings.NewReader(stderr))
	var envelope jsonEnvelope
	if decodeErr := decoder.Decode(&envelope); decodeErr != nil {
		t.Fatalf("decode stderr envelope: %v; stderr=%q", decodeErr, stderr)
	}
	if decodeErr := decoder.Decode(&struct{}{}); decodeErr != io.EOF {
		t.Fatalf("stderr contained downloader logs or another JSON value: %v; stderr=%q", decodeErr, stderr)
	}
	if envelope.Success || envelope.Error == nil || envelope.Meta.Command != "download" {
		t.Fatalf("unexpected failure envelope: %+v", envelope)
	}
	if !strings.Contains(envelope.Error.Message, "download incomplete") {
		t.Fatalf("failure envelope did not contain the downloader error: %+v", envelope.Error)
	}

	// A non-JSON downloader keeps diagnostics when given a human-visible writer.
	var humanLogs bytes.Buffer
	humanPresentation := downloadPresentationOptions{diagnosticOutput: &humanLogs}
	_, humanErr := executeDownloadWithDependencies([]string{"-s", "1", "-S", "2"}, humanPresentation, deps)
	if humanErr == nil {
		t.Fatal("expected human/non-JSON execution to return the downloader error")
	}
	if !strings.Contains(humanLogs.String(), "chunk 0 failed") {
		t.Fatalf("standard downloader logging was not restored outside JSON execution: %q", humanLogs.String())
	}
}

func TestConcurrentJSONAndHumanDownloadDiagnosticsStayIsolated(t *testing.T) {
	restoreCLIState(t)
	reachedFailure := make(chan struct{}, 2)
	releaseFailure := make(chan struct{})
	failureHook := func() {
		reachedFailure <- struct{}{}
		<-releaseFailure
	}
	jsonCfg, jsonClient, jsonCleanup := newJSONDownloadIntegrationWithFailureHook(t, true, failureHook)
	defer jsonCleanup()
	humanCfg, humanClient, humanCleanup := newJSONDownloadIntegrationWithFailureHook(t, true, failureHook)
	defer humanCleanup()

	jsonDeps := downloadExecutionDependencies{
		ensureFFmpeg: func() error { return nil },
		initClient: func(context.Context) (*config.Config, *client.Client, error) {
			return jsonCfg, jsonClient, nil
		},
		downloadLectures: downloadLectures,
	}
	humanDeps := downloadExecutionDependencies{
		ensureFFmpeg: func() error { return nil },
		initClient: func(context.Context) (*config.Config, *client.Client, error) {
			return humanCfg, humanClient, nil
		},
		downloadLectures: downloadLectures,
	}
	runDownloadJSONFn = func(args []string) (downloadResult, error) {
		return runDownloadJSONWithDependencies(args, jsonDeps)
	}
	os.Args = []string{"impartus", "download", "--json", "-s", "1", "-S", "2"}

	var humanLogs bytes.Buffer
	humanPresentation := downloadPresentationOptions{diagnosticOutput: &humanLogs}
	var humanErr error
	stdout, stderr, jsonErr := captureOutputStreams(t, func() error {
		jsonDone := make(chan error, 1)
		humanDone := make(chan error, 1)
		go func() {
			executeErr := Execute("test", "test")
			if executeErr != nil {
				if _, writeErr := fmt.Fprintln(os.Stderr, executeErr); writeErr != nil {
					jsonDone <- fmt.Errorf("write JSON error envelope: %w", writeErr)
					return
				}
			}
			jsonDone <- executeErr
		}()
		go func() {
			_, executeErr := executeDownloadWithDependencies([]string{"-s", "1", "-S", "2"}, humanPresentation, humanDeps)
			humanDone <- executeErr
		}()

		var barrierErr error
		for range 2 {
			select {
			case <-reachedFailure:
			case <-time.After(5 * time.Second):
				barrierErr = errors.New("concurrent downloads did not reach the failure barrier")
			}
			if barrierErr != nil {
				break
			}
		}
		close(releaseFailure)
		jsonRunErr := <-jsonDone
		humanErr = <-humanDone
		if barrierErr != nil {
			return barrierErr
		}
		return jsonRunErr
	})

	if jsonErr == nil || humanErr == nil {
		t.Fatalf("expected both downloads to fail, json=%v human=%v", jsonErr, humanErr)
	}
	if stdout != "" {
		t.Fatalf("concurrent failed JSON download wrote stdout: %q", stdout)
	}
	decoder := json.NewDecoder(strings.NewReader(stderr))
	var envelope jsonEnvelope
	if decodeErr := decoder.Decode(&envelope); decodeErr != nil {
		t.Fatalf("decode concurrent stderr envelope: %v; stderr=%q", decodeErr, stderr)
	}
	if decodeErr := decoder.Decode(&struct{}{}); decodeErr != io.EOF {
		t.Fatalf("concurrent JSON stderr contained cross-routed diagnostics: %v; stderr=%q", decodeErr, stderr)
	}
	if envelope.Success || envelope.Error == nil || envelope.Meta.Command != "download" {
		t.Fatalf("unexpected concurrent failure envelope: %+v", envelope)
	}
	if !strings.Contains(humanLogs.String(), "chunk 0 failed") {
		t.Fatalf("human diagnostics were discarded or cross-routed: %q", humanLogs.String())
	}
}

func TestJSONDownloadFailureStream(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), os.Args[0], "-test.run=^TestJSONFailureProcessHelper$")
	cmd.Env = append(os.Environ(), "IMPARTUS_JSON_FAILURE_HELPER=1")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected helper process to exit non-zero")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() == 0 {
		t.Fatalf("expected non-zero exit error, got %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("failed JSON command wrote stdout: %q", stdout.String())
	}
	decoder := json.NewDecoder(bytes.NewReader(stderr.Bytes()))
	var envelope jsonEnvelope
	if decodeErr := decoder.Decode(&envelope); decodeErr != nil {
		t.Fatalf("decode stderr envelope: %v; stderr=%q", decodeErr, stderr.String())
	}
	if decodeErr := decoder.Decode(&struct{}{}); decodeErr != io.EOF {
		t.Fatalf("stderr contained more than one JSON value: %v; stderr=%q", decodeErr, stderr.String())
	}
	if envelope.Success || envelope.Error == nil || envelope.Meta.Command != "download" {
		t.Fatalf("unexpected failure envelope: %+v", envelope)
	}
}

func TestJSONFailureProcessHelper(t *testing.T) {
	if os.Getenv("IMPARTUS_JSON_FAILURE_HELPER") != "1" {
		return
	}
	os.Args = []string{"impartus", "download", "--json"}
	if err := Execute("test", "test"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func newJSONDownloadIntegration(t *testing.T, failChunk bool) (*config.Config, *client.Client, func()) {
	return newJSONDownloadIntegrationWithFailureHook(t, failChunk, nil)
}

func newJSONDownloadIntegrationWithFailureHook(t *testing.T, failChunk bool, failureHook func()) (*config.Config, *client.Client, func()) {
	t.Helper()
	tempDir := t.TempDir()
	binDir := t.TempDir()
	ffmpegPath := filepath.Join(binDir, "ffmpeg")
	script := "#!/bin/sh\nset -eu\nfor last do :; done\nprintf 'joined output' > \"$last\"\n"
	if err := os.WriteFile(ffmpegPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake ffmpeg: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	key := []byte("1234567890123456")
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/subjects/1/lectures/2":
			// Subject/session are deliberately omitted to exercise the command's
			// authoritative request-scope fallback.
			if err := json.NewEncoder(w).Encode(client.Lectures{{InstituteID: 4, TTID: 7, Topic: "JSON Lecture", SeqNo: 1, NoAudio: 1}}); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		case "/fetchvideo":
			if _, err := fmt.Fprintln(w, server.URL+"/stream-1280x720.m3u8"); err != nil {
				return
			}
		case "/stream-1280x720.m3u8":
			if _, err := fmt.Fprintf(w, "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=%q\n#EXTINF:1,\n%s/chunk0.ts\n", server.URL+"/key", server.URL); err != nil {
				return
			}
		case "/key":
			response := append([]byte{0, 0}, reverseTestBytes(key)...)
			if _, err := w.Write(response); err != nil {
				return
			}
		case "/chunk0.ts":
			if failChunk {
				if failureHook != nil {
					failureHook()
				}
				// A successful but malformed encrypted chunk reaches the real
				// decrypt-and-log failure path without retry backoff.
				if _, err := w.Write([]byte{0}); err != nil {
					return
				}
				return
			}
			if _, err := w.Write(make([]byte, 16)); err != nil {
				return
			}
		default:
			http.NotFound(w, r)
		}
	}))

	cfg := &config.Config{
		BaseURL:          server.URL,
		Token:            "test-token",
		Quality:          "720",
		Views:            "left",
		DownloadLocation: filepath.Join(tempDir, "downloads"),
		TempDirLocation:  filepath.Join(tempDir, "temp"),
		NumWorkers:       1,
		RateLimit:        100,
		APIRateLimit:     100,
		ProgressTracking: config.ProgressConfig{Enabled: true},
	}
	return cfg, client.New(server.Client(), nil), server.Close
}

func reverseTestBytes(input []byte) []byte {
	output := make([]byte, len(input))
	for i := range input {
		output[i] = input[len(input)-1-i]
	}
	return output
}

func captureOutputStreams(t *testing.T, fn func() error) (string, string, error) {
	t.Helper()
	oldStdout, oldStderr := os.Stdout, os.Stderr
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	defer func() {
		os.Stdout, os.Stderr = oldStdout, oldStderr
		for _, stream := range []*os.File{stdoutWriter, stderrWriter, stdoutReader, stderrReader} {
			if closeErr := stream.Close(); closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
				t.Logf("cleanup captured stream: %v", closeErr)
			}
		}
	}()

	type readResult struct {
		data []byte
		err  error
	}
	stdoutCh := make(chan readResult, 1)
	stderrCh := make(chan readResult, 1)
	go func() {
		data, readErr := io.ReadAll(stdoutReader)
		stdoutCh <- readResult{data: data, err: readErr}
	}()
	go func() {
		data, readErr := io.ReadAll(stderrReader)
		stderrCh <- readResult{data: data, err: readErr}
	}()

	os.Stdout, os.Stderr = stdoutWriter, stderrWriter
	runErr := fn()
	stdoutCloseErr := stdoutWriter.Close()
	stderrCloseErr := stderrWriter.Close()
	os.Stdout, os.Stderr = oldStdout, oldStderr
	if stdoutCloseErr != nil {
		t.Fatalf("close stdout writer: %v", stdoutCloseErr)
	}
	if stderrCloseErr != nil {
		t.Fatalf("close stderr writer: %v", stderrCloseErr)
	}
	stdout := <-stdoutCh
	stderr := <-stderrCh
	if err := stdoutReader.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}
	if err := stderrReader.Close(); err != nil {
		t.Fatalf("close stderr reader: %v", err)
	}
	if stdout.err != nil {
		t.Fatalf("read stdout: %v", stdout.err)
	}
	if stderr.err != nil {
		t.Fatalf("read stderr: %v", stderr.err)
	}
	return string(stdout.data), string(stderr.data), runErr
}

func TestCaptureOutputStreamsDrainsConcurrently(t *testing.T) {
	payload := strings.Repeat("x", 1<<20)
	stdout, stderr, err := captureOutputStreams(t, func() error {
		if _, writeErr := io.WriteString(os.Stdout, payload); writeErr != nil {
			return writeErr
		}
		if _, writeErr := io.WriteString(os.Stderr, payload); writeErr != nil {
			return writeErr
		}
		return nil
	})
	if err != nil {
		t.Fatalf("captureOutputStreams() error = %v", err)
	}
	if stdout != payload || stderr != payload {
		t.Fatalf("captured lengths stdout=%d stderr=%d, want %d each", len(stdout), len(stderr), len(payload))
	}
}

func TestCaptureOutputStreamsRestoresGlobalsAfterPanic(t *testing.T) {
	oldStdout, oldStderr := os.Stdout, os.Stderr
	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Fatal("expected capture callback panic")
			}
		}()
		_, _, captureErr := captureOutputStreams(t, func() error { panic("synthetic panic") })
		if captureErr != nil {
			t.Fatalf("unexpected capture error: %v", captureErr)
		}
	}()
	if os.Stdout != oldStdout || os.Stderr != oldStderr {
		t.Fatal("captureOutputStreams did not restore process streams after panic")
	}
}
