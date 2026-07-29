package watch_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/downloader"
	"github.com/rabesss/impartus-cli/internal/notebooklm"
	"github.com/rabesss/impartus-cli/internal/watch"
)

// End-to-end cycle capturing mp3 path, upload args, and durable state artifacts.
func TestWatchCycleEndToEndArtifacts(t *testing.T) {
	dir := t.TempDir()
	mp3Path := filepath.Join(dir, "LEC 001 Intro LEFT VIEW.mp3")
	if err := os.WriteFile(mp3Path, []byte("ID3fakeaudio"), 0o600); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(dir, "watch-state.json")

	cfg := &config.Config{
		Username: "u", Password: "p", BaseURL: "https://example.com",
		Quality: "144", Views: "left", AudioOnly: true, AudioFormat: "mp3",
		Watch:      config.WatchConfig{SubjectID: 1, SessionID: 2, StatePath: statePath, Upload: true},
		NotebookLM: config.NotebookLMConfig{NotebookID: "nb-e2e"},
	}
	cfg.ApplyDefaults()
	cfg.ApplyWatchMediaDefaults()

	store, err := watch.LoadStore(statePath)
	if err != nil {
		t.Fatal(err)
	}

	source := fakeLectureSource{lectures: client.Lectures{
		{TTID: 101, SeqNo: 1, Topic: "Intro", StartTime: "2026-07-01T10:00:00", SubjectID: 1, SessionID: 2},
		{TTID: 102, SeqNo: 2, Topic: "No Lecture (holiday)", StartTime: "2026-07-02T10:00:00", SubjectID: 1, SessionID: 2},
	}}
	audio := &artifactAudio{join: downloader.JoinResult{LeftOutput: mp3Path}}
	uploader := &recordingUploader{result: notebooklm.UploadResult{SourceID: "src-e2e", NotebookID: "nb-e2e"}}

	w := watch.New(cfg, source, audio, uploader, store, watch.Options{
		SubjectID: 1, SessionID: 2, Once: true, Upload: true, NotebookID: "nb-e2e", Log: io.Discard,
	})
	result, err := w.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle: %v", err)
	}
	if result.Downloaded != 1 || result.Uploaded != 1 || result.New != 1 {
		t.Fatalf("unexpected cycle: %+v", result)
	}
	if len(result.Outputs) != 1 || result.Outputs[0] != mp3Path {
		t.Fatalf("outputs = %v", result.Outputs)
	}
	if uploader.lastPath != mp3Path || !strings.Contains(uploader.lastTitle, "Intro") {
		t.Fatalf("upload args path=%q title=%q", uploader.lastPath, uploader.lastTitle)
	}

	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"101"`) || !strings.Contains(string(raw), "src-e2e") {
		t.Fatalf("state artifact unexpected:\n%s", raw)
	}

	artifactDir := "/opt/cursor/artifacts"
	_ = os.MkdirAll(artifactDir, 0o755)                                                                                                      //nolint:errcheck // best-effort artifact capture
	_ = os.WriteFile(filepath.Join(artifactDir, "watch_e2e_mp3_path.txt"), []byte(mp3Path+"\n"), 0o600)                                      //nolint:errcheck // best-effort artifact capture
	_ = os.WriteFile(filepath.Join(artifactDir, "watch_e2e_upload_argv.txt"), []byte(uploader.lastPath+"\t"+uploader.lastTitle+"\n"), 0o600) //nolint:errcheck // best-effort artifact capture
	_ = os.WriteFile(filepath.Join(artifactDir, "watch_e2e_state.json"), raw, 0o600)                                                         //nolint:errcheck // best-effort artifact capture
}

type fakeLectureSource struct {
	lectures client.Lectures
}

func (f fakeLectureSource) GetLectures(context.Context, *config.Config, client.Course) (client.Lectures, error) {
	return f.lectures, nil
}

type artifactAudio struct {
	join downloader.JoinResult
}

func (a *artifactAudio) FetchLecturePlaylists(context.Context, []client.Lecture) ([]client.ParsedPlaylist, error) {
	return []client.ParsedPlaylist{{ID: 101, SeqNo: 1}}, nil
}

func (a *artifactAudio) DownloadAndJoinPlaylist(context.Context, client.ParsedPlaylist, any, any) (downloader.JoinResult, error) {
	return a.join, nil
}

type recordingUploader struct {
	result    notebooklm.UploadResult
	lastPath  string
	lastTitle string
}

func (r *recordingUploader) UploadFile(_ context.Context, filePath, title string) (notebooklm.UploadResult, error) {
	r.lastPath = filePath
	r.lastTitle = title
	return r.result, nil
}

func (r *recordingUploader) Doctor(context.Context) error { return nil }
