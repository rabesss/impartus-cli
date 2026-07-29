package watch

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/downloader"
	"github.com/rabesss/impartus-cli/internal/notebooklm"
)

type fakeSource struct {
	lectures client.Lectures
	err      error
}

func (f fakeSource) GetLectures(context.Context, *config.Config, client.Course) (client.Lectures, error) {
	return f.lectures, f.err
}

type fakeAudio struct {
	join     downloader.JoinResult
	fetchErr error
	joinErr  error
	calls    int
}

func (f *fakeAudio) FetchLecturePlaylists(context.Context, []client.Lecture) ([]client.ParsedPlaylist, error) {
	f.calls++
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}
	return []client.ParsedPlaylist{{ID: 1, SeqNo: 1, Title: "t"}}, nil
}

func (f *fakeAudio) DownloadAndJoinPlaylist(context.Context, client.ParsedPlaylist, any, any) (downloader.JoinResult, error) {
	return f.join, f.joinErr
}

type fakeUploader struct {
	result notebooklm.UploadResult
	err    error
	calls  int
}

func (f *fakeUploader) UploadFile(context.Context, string, string) (notebooklm.UploadResult, error) {
	f.calls++
	return f.result, f.err
}

func (f *fakeUploader) Doctor(context.Context) error { return nil }

func testCfg() *config.Config {
	cfg := &config.Config{
		Username: "u", Password: "p", BaseURL: "https://example.com",
		Quality: "144", Views: "left", AudioOnly: true, AudioFormat: "mp3",
	}
	cfg.ApplyDefaults()
	cfg.ApplyWatchMediaDefaults()
	return cfg
}

func TestRunCycleDryRunDoesNotDownload(t *testing.T) {
	store, err := LoadStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	audio := &fakeAudio{join: downloader.JoinResult{LeftOutput: "/tmp/x.mp3"}}
	uploader := &fakeUploader{}
	w := New(testCfg(), fakeSource{lectures: client.Lectures{
		{TTID: 10, SeqNo: 1, Topic: "Intro", StartTime: "2026-01-01"},
		{TTID: 11, SeqNo: 2, Topic: "No Class Today"},
	}}, audio, uploader, store, Options{
		SubjectID: 1, SessionID: 2, Once: true, DryRun: true, Log: io.Discard,
	})

	result, err := w.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle: %v", err)
	}
	if result.New != 1 || result.Downloaded != 0 || audio.calls != 0 || uploader.calls != 0 {
		t.Fatalf("unexpected dry-run result: %+v audioCalls=%d uploadCalls=%d", result, audio.calls, uploader.calls)
	}
}

func TestRunCycleDownloadsUploadsAndSkipsSeen(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "lec.mp3")
	if err := os.WriteFile(out, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := LoadStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	audio := &fakeAudio{join: downloader.JoinResult{LeftOutput: out}}
	uploader := &fakeUploader{result: notebooklm.UploadResult{SourceID: "src1", NotebookID: "nb1"}}
	lectures := client.Lectures{{TTID: 10, SeqNo: 1, Topic: "Intro", StartTime: "2026-01-01"}}
	opts := Options{SubjectID: 1, SessionID: 2, Once: true, Upload: true, NotebookID: "nb1", Log: io.Discard}

	w := New(testCfg(), fakeSource{lectures: lectures}, audio, uploader, store, opts)
	result, err := w.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("first cycle: %v", err)
	}
	if result.Downloaded != 1 || result.Uploaded != 1 || !store.Has(1, 2, 10) {
		t.Fatalf("unexpected first cycle: %+v", result)
	}

	result, err = w.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("second cycle: %v", err)
	}
	if result.Skipped != 1 || result.New != 0 || uploader.calls != 1 {
		t.Fatalf("expected skip on second cycle: %+v uploadCalls=%d", result, uploader.calls)
	}
}

func TestWithRetriesRetriesTransientNotebookLMErrors(t *testing.T) {
	attempts := 0
	fast := func(int) time.Duration { return time.Millisecond }
	err := withRetries(context.Background(), 3, fast, io.Discard, func() error {
		attempts++
		if attempts < 3 {
			return &notebooklm.Error{Kind: notebooklm.ErrTransient, Message: "temporary"}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected success after retries: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts=%d", attempts)
	}
}

func TestWithRetriesDoesNotRetryPermanent(t *testing.T) {
	attempts := 0
	err := withRetries(context.Background(), 3, func(int) time.Duration { return time.Millisecond }, io.Discard, func() error {
		attempts++
		return &notebooklm.Error{Kind: notebooklm.ErrPermanent, Message: "nope"}
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if attempts != 1 {
		t.Fatalf("attempts=%d", attempts)
	}
}

func TestRunRespectsOnce(t *testing.T) {
	store, err := LoadStore(filepath.Join(t.TempDir(), "s.json"))
	if err != nil {
		t.Fatal(err)
	}
	w := New(testCfg(), fakeSource{lectures: client.Lectures{}}, nil, nil, store, Options{
		SubjectID: 1, SessionID: 2, Once: true, DryRun: true, Interval: time.Hour, Log: io.Discard,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := w.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestRunCycleRetriesPreviouslyFailedLecture(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "lec.mp3")
	if err := os.WriteFile(out, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := LoadStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	lectures := client.Lectures{{TTID: 10, SeqNo: 1, Topic: "Intro", StartTime: "2026-01-01"}}
	opts := Options{SubjectID: 1, SessionID: 2, Once: true, Upload: true, NotebookID: "nb1", Log: io.Discard, MaxRetries: 1}

	failAudio := &fakeAudio{joinErr: errors.New("download blip")}
	w := New(testCfg(), fakeSource{lectures: lectures}, failAudio, &fakeUploader{}, store, opts)
	result, err := w.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("failing cycle: %v", err)
	}
	if result.Failed != 1 || store.Has(1, 2, 10) {
		t.Fatalf("expected failed+unseen after blip: %+v has=%v", result, store.Has(1, 2, 10))
	}

	okAudio := &fakeAudio{join: downloader.JoinResult{LeftOutput: out}}
	uploader := &fakeUploader{result: notebooklm.UploadResult{SourceID: "src1"}}
	w = New(testCfg(), fakeSource{lectures: lectures}, okAudio, uploader, store, opts)
	result, err = w.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("retry cycle: %v", err)
	}
	if result.Downloaded != 1 || result.Uploaded != 1 || !store.Has(1, 2, 10) {
		t.Fatalf("expected retry success: %+v", result)
	}
}
