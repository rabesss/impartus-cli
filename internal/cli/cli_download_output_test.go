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

	"github.com/vbauerster/mpb/v8"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/downloader"
)

type fakeLectureDownloadRunner struct {
	playlists []client.ParsedPlaylist
	results   []downloader.JoinResult
	progress  []*mpb.Progress
	next      int
}

func (f *fakeLectureDownloadRunner) FetchLecturePlaylists(context.Context, []client.Lecture) ([]client.ParsedPlaylist, error) {
	return f.playlists, nil
}

func (f *fakeLectureDownloadRunner) DownloadAndJoinPlaylist(_ context.Context, _ client.ParsedPlaylist, progress *mpb.Progress, _ *downloader.ProgressTracker) (downloader.JoinResult, error) {
	f.progress = append(f.progress, progress)
	result := f.results[f.next]
	f.next++
	return result, nil
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
			playlists := make([]client.ParsedPlaylist, len(tt.results))
			lectures := make(client.Lectures, len(tt.results))
			for i := range playlists {
				playlists[i] = client.ParsedPlaylist{ID: i + 1}
				lectures[i] = client.Lecture{TTID: i + 1}
			}
			runner := &fakeLectureDownloadRunner{playlists: playlists, results: tt.results}
			result, err := downloadLecturesWithRunner(context.Background(), &config.Config{DownloadLocation: t.TempDir()}, runner, lectures, quietDownloadPresentation())
			if err != nil {
				t.Fatalf("downloadLecturesWithRunner() error = %v", err)
			}
			if result.LectureCount != tt.wantCount {
				t.Fatalf("LectureCount = %d, want %d", result.LectureCount, tt.wantCount)
			}
			if len(result.OutputPaths) != tt.wantOutputs {
				t.Fatalf("len(OutputPaths) = %d, want %d", len(result.OutputPaths), tt.wantOutputs)
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

	runner := &fakeLectureDownloadRunner{
		playlists: []client.ParsedPlaylist{{ID: 1}},
		results:   []downloader.JoinResult{{LeftOutput: "left.mp4"}},
	}
	if _, err := downloadLecturesWithRunner(context.Background(), &config.Config{DownloadLocation: t.TempDir()}, runner, client.Lectures{{TTID: 1}}, presentation); err != nil {
		t.Fatalf("downloadLecturesWithRunner() error = %v", err)
	}
	if len(runner.progress) != 1 || runner.progress[0] == nil {
		t.Fatal("human download did not pass a progress container to the downloader")
	}
}

func TestJSONDownloadStreamContract(t *testing.T) {
	restoreCLIState(t)
	cfg, apiClient, cleanup := newJSONDownloadIntegration(t)
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
		return executeDownloadWithDependencies(args, quietDownloadPresentation(), deps)
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
			Status       string   `json:"status"`
			OutputPaths  []string `json:"outputPaths"`
			LectureCount int      `json:"lectureCount"`
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

func newJSONDownloadIntegration(t *testing.T) (*config.Config, *client.Client, func()) {
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
			if err := json.NewEncoder(w).Encode(client.Lectures{{TTID: 7, Topic: "JSON Lecture", SeqNo: 1, NoAudio: 1}}); err != nil {
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
	stdout, err := io.ReadAll(stdoutReader)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	stderr, err := io.ReadAll(stderrReader)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	if err := stdoutReader.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}
	if err := stderrReader.Close(); err != nil {
		t.Fatalf("close stderr reader: %v", err)
	}
	return string(stdout), string(stderr), runErr
}
