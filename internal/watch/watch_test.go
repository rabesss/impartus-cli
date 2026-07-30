package watch

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	result          notebooklm.UploadResult
	err             error
	calls           int
	notebookIDs     []string
	paths           []string
	titles          []string
	uploadKeys      []string
	reconcileResult notebooklm.UploadResult
	reconcileFound  bool
	reconcileErr    error
	reconcileCalls  int
	reconcileNbs    []string
	beforeUpload    func()
}

func (f *fakeUploader) UploadToNotebook(_ context.Context, notebookID, path, title, uploadKey string) (notebooklm.UploadResult, error) {
	if f.beforeUpload != nil {
		f.beforeUpload()
	}
	f.calls++
	f.notebookIDs = append(f.notebookIDs, notebookID)
	f.paths = append(f.paths, path)
	f.titles = append(f.titles, title)
	f.uploadKeys = append(f.uploadKeys, uploadKey)
	return f.result, f.err
}

func (f *fakeUploader) ReconcileUpload(_ context.Context, notebookID, _, _ string) (notebooklm.UploadResult, bool, error) {
	f.reconcileCalls++
	f.reconcileNbs = append(f.reconcileNbs, notebookID)
	return f.reconcileResult, f.reconcileFound, f.reconcileErr
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

func TestFilterEmptyLecturesHandlesBoundedPlaceholderVariants(t *testing.T) {
	lectures := client.Lectures{
		{TTID: 1, Topic: "No class!"},
		{TTID: 2, Topic: "No Class - Holiday"},
		{TTID: 3, Topic: "There will be no class."},
		{TTID: 4, Topic: "No lecture—holiday"},
		{TTID: 5, Topic: "No class — holiday"},
		{TTID: 6, Topic: "No class! - Holiday"},
		{TTID: 7, Topic: "No class:Holiday"},
		{TTID: 8, Topic: "Discussion: why there was no class"},
		{TTID: 9, Topic: "No classroom available: remote lecture"},
	}
	filtered := filterEmptyLectures(lectures)
	if len(filtered) != 2 {
		t.Fatalf("expected only legitimate lecture titles to remain, got %+v", filtered)
	}
	if filtered[0].TTID != 8 || filtered[1].TTID != 9 {
		t.Fatalf("bounded placeholder matching removed legitimate lectures: %+v", filtered)
	}
}

func TestLectureTitlePrefixesStableUploadToken(t *testing.T) {
	title := lectureTitle(client.Lecture{SeqNo: 7, Topic: "Distributed Systems"}, "impartus:1:2:10")
	if title != "[impartus:1:2:10] LEC 007 Distributed Systems" {
		t.Fatalf("lecture title = %q", title)
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

func TestRunCycleRespectsMaxLecturesPerCycle(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "lec.mp3")
	if err := os.WriteFile(out, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := LoadStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	lectures := client.Lectures{
		{TTID: 10, SeqNo: 1, Topic: "A", StartTime: "2026-01-01"},
		{TTID: 11, SeqNo: 2, Topic: "B", StartTime: "2026-01-02"},
		{TTID: 12, SeqNo: 3, Topic: "C", StartTime: "2026-01-03"},
	}
	audio := &fakeAudio{join: downloader.JoinResult{LeftOutput: out}}
	uploader := &fakeUploader{result: notebooklm.UploadResult{SourceID: "src"}}
	w := New(testCfg(), fakeSource{lectures: lectures}, audio, uploader, store, Options{
		SubjectID: 1, SessionID: 2, Once: true, Upload: true, NotebookID: "nb",
		MaxLecturesPerCycle: 2, Log: io.Discard,
	})
	result, err := w.RunCycle(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.New != 2 || result.Uploaded != 2 {
		t.Fatalf("expected cap of 2, got %+v", result)
	}
}

func TestRunCycleResumesDownloadedWithoutRedownload(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "lec.mp3")
	if err := os.WriteFile(out, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := LoadStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if markErr := store.Mark(1, 2, SeenLecture{
		Status: StatusDownloaded, SeqNo: 1, Topic: "Intro", OutputPath: out, NotebookID: "nb1",
	}, 10); markErr != nil {
		t.Fatal(markErr)
	}
	audio := &fakeAudio{join: downloader.JoinResult{LeftOutput: out}}
	uploader := &fakeUploader{result: notebooklm.UploadResult{SourceID: "src1"}}
	w := New(testCfg(), fakeSource{lectures: client.Lectures{
		{TTID: 10, SeqNo: 1, Topic: "Intro", StartTime: "2026-01-01"},
	}}, audio, uploader, store, Options{
		SubjectID: 1, SessionID: 2, Once: true, Upload: true, NotebookID: "nb1", Log: io.Discard,
	})
	result, err := w.RunCycle(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if audio.calls != 0 {
		t.Fatalf("expected resume without re-download, audioCalls=%d", audio.calls)
	}
	if result.Downloaded != 0 || result.Uploaded != 1 || !store.Has(1, 2, 10) {
		t.Fatalf("unexpected resume result: %+v", result)
	}
}

func TestRunCycleUploadFailureResumesExistingAudio(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "lec.mp3")
	if err := os.WriteFile(out, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := LoadStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	lectures := client.Lectures{{TTID: 10, SeqNo: 1, Topic: "Intro"}}
	opts := Options{
		SubjectID: 1, SessionID: 2, Once: true, Upload: true, NotebookID: "nb",
		MaxRetries: 1, Log: io.Discard,
	}
	firstAudio := &fakeAudio{join: downloader.JoinResult{LeftOutput: out}}
	firstUpload := &fakeUploader{err: &notebooklm.Error{Kind: notebooklm.ErrPermanent, Message: "failed"}}
	first := New(testCfg(), fakeSource{lectures: lectures}, firstAudio, firstUpload, store, opts)
	result, err := first.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("first cycle: %v", err)
	}
	if result.Failed != 1 || firstAudio.calls != 1 {
		t.Fatalf("unexpected failed cycle: %+v downloads=%d", result, firstAudio.calls)
	}

	secondAudio := &fakeAudio{join: downloader.JoinResult{LeftOutput: out}}
	secondUpload := &fakeUploader{result: notebooklm.UploadResult{SourceID: "src"}}
	second := New(testCfg(), fakeSource{lectures: lectures}, secondAudio, secondUpload, store, opts)
	result, err = second.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("retry cycle: %v", err)
	}
	if secondAudio.calls != 0 || result.Downloaded != 0 || result.Uploaded != 1 {
		t.Fatalf("retry re-downloaded audio: result=%+v downloads=%d", result, secondAudio.calls)
	}
}

func TestRunCycleReconcilesAmbiguousUploadWithoutAnotherAdd(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "lec.mp3")
	if err := os.WriteFile(out, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := LoadStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	lectures := client.Lectures{{TTID: 10, SeqNo: 1, Topic: "Intro"}}
	uploader := &fakeUploader{
		err: &notebooklm.Error{Kind: notebooklm.ErrAmbiguous, Message: "outcome unknown"},
	}
	opts := Options{
		SubjectID: 1, SessionID: 2, Once: true, Upload: true, NotebookID: "nb",
		MaxRetries: 1, Log: io.Discard,
	}
	first := New(testCfg(), fakeSource{lectures: lectures},
		&fakeAudio{join: downloader.JoinResult{LeftOutput: out}}, uploader, store, opts)
	result, err := first.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("first cycle: %v", err)
	}
	if result.Failed != 1 || uploader.calls != 1 {
		t.Fatalf("expected one ambiguous add: result=%+v uploadCalls=%d", result, uploader.calls)
	}
	seen, ok := store.Get(1, 2, 10)
	if !ok || seen.Status != StatusAmbiguous {
		t.Fatalf("ambiguous outcome was not durable: %+v ok=%v", seen, ok)
	}

	downloadOnlyOpts := opts
	downloadOnlyOpts.Upload = false
	downloadOnly := New(testCfg(), fakeSource{lectures: lectures}, &fakeAudio{}, uploader, store, downloadOnlyOpts)
	result, err = downloadOnly.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("download-only cycle: %v", err)
	}
	if result.Skipped != 1 || uploader.calls != 1 || uploader.reconcileCalls != 0 {
		t.Fatalf("download-only cycle disturbed ambiguous upload: result=%+v uploadCalls=%d reconcileCalls=%d",
			result, uploader.calls, uploader.reconcileCalls)
	}
	seen, _ = store.Get(1, 2, 10)
	if seen.Status != StatusAmbiguous {
		t.Fatalf("download-only cycle cleared ambiguous state: %+v", seen)
	}

	reconcileOpts := opts
	reconcileOpts.NotebookID = "reconfigured-nb"
	if removeErr := os.Remove(out); removeErr != nil {
		t.Fatal(removeErr)
	}
	second := New(testCfg(), fakeSource{lectures: lectures}, nil, uploader, store, reconcileOpts)
	result, err = second.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("second cycle: %v", err)
	}
	if result.Failed != 1 || uploader.calls != 1 || uploader.reconcileCalls != 1 {
		t.Fatalf("missing source triggered another add: result=%+v uploadCalls=%d reconcileCalls=%d",
			result, uploader.calls, uploader.reconcileCalls)
	}
	if len(uploader.reconcileNbs) != 1 || uploader.reconcileNbs[0] != "nb" {
		t.Fatalf("reconfiguration redirected ambiguous reconciliation: %v", uploader.reconcileNbs)
	}
	seen, _ = store.Get(1, 2, 10)
	if seen.Status != StatusAmbiguous {
		t.Fatalf("unresolved ambiguity was not preserved: %+v", seen)
	}

	uploader.reconcileFound = true
	uploader.reconcileResult = notebooklm.UploadResult{SourceID: "src-late", NotebookID: "nb"}
	third := New(testCfg(), fakeSource{lectures: lectures}, nil, uploader, store, reconcileOpts)
	result, err = third.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("third cycle: %v", err)
	}
	if result.Uploaded != 1 || uploader.calls != 1 || uploader.reconcileCalls != 2 {
		t.Fatalf("late source was not reconciled: result=%+v uploadCalls=%d reconcileCalls=%d",
			result, uploader.calls, uploader.reconcileCalls)
	}
	seen, _ = store.Get(1, 2, 10)
	if seen.Status != StatusUploaded || seen.SourceID != "src-late" {
		t.Fatalf("late source did not complete durable state: %+v", seen)
	}
}

func TestRunCycleRetriesConfirmedRateLimitInsteadOfReconciling(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "lec.mp3")
	if err := os.WriteFile(out, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := LoadStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	lectures := client.Lectures{{TTID: 10, SeqNo: 1, Topic: "Intro"}}
	uploader := &fakeUploader{
		err: &notebooklm.Error{Kind: notebooklm.ErrRateLimit, Message: "HTTP 429 rate limit"},
	}
	opts := Options{
		SubjectID: 1, SessionID: 2, Once: true, Upload: true, NotebookID: "nb",
		MaxRetries: 1, Log: io.Discard,
	}
	first := New(testCfg(), fakeSource{lectures: lectures},
		&fakeAudio{join: downloader.JoinResult{LeftOutput: out}}, uploader, store, opts)
	result, err := first.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("rate-limited cycle: %v", err)
	}
	seen, ok := store.Get(1, 2, 10)
	if result.Failed != 1 || !ok || seen.Status != StatusFailed {
		t.Fatalf("rate limit was persisted as ambiguous: result=%+v seen=%+v ok=%v", result, seen, ok)
	}

	uploader.err = nil
	uploader.result = notebooklm.UploadResult{SourceID: "src-retried", NotebookID: "nb"}
	second := New(testCfg(), fakeSource{lectures: lectures}, &fakeAudio{}, uploader, store, opts)
	result, err = second.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("retry cycle: %v", err)
	}
	if result.Uploaded != 1 || uploader.calls != 2 || uploader.reconcileCalls != 0 {
		t.Fatalf("confirmed rejection did not retry add: result=%+v uploadCalls=%d reconcileCalls=%d",
			result, uploader.calls, uploader.reconcileCalls)
	}
}

func TestRunCyclePersistsReconciliationOnlyIntentBeforeAdd(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "lec.mp3")
	if err := os.WriteFile(out, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := LoadStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var statusAtProviderCall LectureStatus
	uploader := &fakeUploader{
		result: notebooklm.UploadResult{SourceID: "src"},
		beforeUpload: func() {
			seen, _ := store.Get(1, 2, 10)
			statusAtProviderCall = seen.Status
		},
	}
	w := New(testCfg(), fakeSource{lectures: client.Lectures{{TTID: 10, SeqNo: 1, Topic: "Intro"}}},
		&fakeAudio{join: downloader.JoinResult{LeftOutput: out}}, uploader, store, Options{
			SubjectID: 1, SessionID: 2, Once: true, Upload: true, NotebookID: "nb",
			MaxRetries: 1, Log: io.Discard,
		})
	result, err := w.RunCycle(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if statusAtProviderCall != StatusAmbiguous {
		t.Fatalf("provider add began before reconciliation-only intent was durable: status=%q", statusAtProviderCall)
	}
	if result.Uploaded != 1 {
		t.Fatalf("successful upload did not complete after intent persistence: %+v", result)
	}
}

type targetSource struct {
	lectures map[string]client.Lectures
	errs     map[string]error
}

func (s targetSource) GetLectures(_ context.Context, _ *config.Config, course client.Course) (client.Lectures, error) {
	key := CourseKey(course.SubjectID, course.SessionID)
	return s.lectures[key], s.errs[key]
}

func TestRunCycleContinuesAfterTargetFailure(t *testing.T) {
	store, err := LoadStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	source := targetSource{
		lectures: map[string]client.Lectures{
			"3:4": {{TTID: 20, SeqNo: 1, Topic: "healthy"}},
		},
		errs: map[string]error{"1:2": errors.New("temporary upstream failure")},
	}
	w := New(testCfg(), source, nil, nil, store, Options{
		Targets: []config.WatchTarget{
			{SubjectID: 1, SessionID: 2},
			{SubjectID: 3, SessionID: 4},
		},
		Once: true, DryRun: true, Log: io.Discard,
	})
	result, err := w.RunCycle(context.Background())
	if err == nil {
		t.Fatalf("once mode should report the target error")
	}
	if result.New != 1 || result.Failed != 1 || len(result.Errors) != 1 {
		t.Fatalf("later target was not processed: %+v", result)
	}
}

func TestRunCycleUsesPerTargetNotebookAndSharedBudget(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "lec.mp3")
	if err := os.WriteFile(out, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := LoadStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	source := targetSource{lectures: map[string]client.Lectures{
		"1:2": {{TTID: 10, SeqNo: 1, Topic: "one"}},
		"3:4": {{TTID: 20, SeqNo: 1, Topic: "two"}},
	}}
	uploader := &fakeUploader{result: notebooklm.UploadResult{SourceID: "src"}}
	w := New(testCfg(), source, &fakeAudio{join: downloader.JoinResult{LeftOutput: out}}, uploader, store, Options{
		Targets: []config.WatchTarget{
			{SubjectID: 1, SessionID: 2, NotebookID: "nb-one"},
			{SubjectID: 3, SessionID: 4, NotebookID: "nb-two"},
		},
		Once: true, Upload: true, MaxLecturesPerCycle: 2, Log: io.Discard,
	})
	result, err := w.RunCycle(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Uploaded != 2 || len(uploader.notebookIDs) != 2 ||
		uploader.notebookIDs[0] != "nb-one" || uploader.notebookIDs[1] != "nb-two" {
		t.Fatalf("per-target routing failed: result=%+v notebooks=%v", result, uploader.notebookIDs)
	}
	if uploader.uploadKeys[0] != "impartus:1:2:10" || uploader.uploadKeys[1] != "impartus:3:4:20" ||
		!strings.Contains(uploader.titles[0], "[impartus:1:2:10]") ||
		!strings.Contains(uploader.titles[1], "[impartus:3:4:20]") {
		t.Fatalf("durable upload identities missing: titles=%v keys=%v", uploader.titles, uploader.uploadKeys)
	}
}

func TestRunCycleDeletesAudioOnlyAfterUpload(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "lec.mp3")
	if err := os.WriteFile(out, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := LoadStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	w := New(testCfg(), fakeSource{lectures: client.Lectures{{TTID: 10, SeqNo: 1}}},
		&fakeAudio{join: downloader.JoinResult{LeftOutput: out}},
		&fakeUploader{result: notebooklm.UploadResult{SourceID: "src"}},
		store, Options{
			SubjectID: 1, SessionID: 2, NotebookID: "nb", Once: true, Upload: true,
			DeleteAudioAfterUpload: true, Log: io.Discard,
		})
	if _, err := w.RunCycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("uploaded audio was not deleted: %v", err)
	}
}

func TestRunCycleAuthFailureAborts(t *testing.T) {
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
	uploader := &fakeUploader{err: &notebooklm.Error{Kind: notebooklm.ErrAuth, Message: "re-authenticate"}}
	w := New(testCfg(), fakeSource{lectures: client.Lectures{
		{TTID: 10, SeqNo: 1, Topic: "Intro", StartTime: "2026-01-01"},
		{TTID: 11, SeqNo: 2, Topic: "Next", StartTime: "2026-01-02"},
	}}, audio, uploader, store, Options{
		SubjectID: 1, SessionID: 2, Once: true, Upload: true, NotebookID: "nb1",
		MaxRetries: 3, Log: io.Discard,
	})
	_, err = w.RunCycle(context.Background())
	if !notebooklm.IsAuth(err) {
		t.Fatalf("expected auth abort, got %v", err)
	}
	if uploader.calls != 1 {
		t.Fatalf("auth should not burn retries across lectures, calls=%d", uploader.calls)
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

type retryingSource struct {
	calls  int
	cancel context.CancelFunc
}

func (s *retryingSource) GetLectures(context.Context, *config.Config, client.Course) (client.Lectures, error) {
	s.calls++
	if s.calls == 1 {
		return nil, errors.New("temporary poll failure")
	}
	s.cancel()
	return nil, nil
}

func TestRunRetriesPollFailureInDaemonMode(t *testing.T) {
	store, err := LoadStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	source := &retryingSource{cancel: cancel}
	w := New(testCfg(), source, nil, nil, store, Options{
		SubjectID: 1, SessionID: 2, DryRun: true, Interval: time.Millisecond, Log: io.Discard,
	})
	if _, err := w.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context cancellation", err)
	}
	if source.calls != 2 {
		t.Fatalf("daemon exited instead of retrying poll: calls=%d", source.calls)
	}
}

type cancelAudio struct {
	cancel context.CancelFunc
}

func (a cancelAudio) FetchLecturePlaylists(ctx context.Context, _ []client.Lecture) ([]client.ParsedPlaylist, error) {
	a.cancel()
	return nil, ctx.Err()
}

func (cancelAudio) DownloadAndJoinPlaylist(context.Context, client.ParsedPlaylist, any, any) (downloader.JoinResult, error) {
	return downloader.JoinResult{}, errors.New("unexpected join")
}

func TestRunCycleCancellationDoesNotRecordFailure(t *testing.T) {
	store, err := LoadStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := New(testCfg(), fakeSource{lectures: client.Lectures{{TTID: 10, SeqNo: 1}}},
		cancelAudio{cancel: cancel}, nil, store, Options{
			SubjectID: 1, SessionID: 2, Once: true, MaxRetries: 1, Log: io.Discard,
		})
	result, err := w.RunCycle(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunCycle error = %v, want context cancellation", err)
	}
	if result.Failed != 0 {
		t.Fatalf("cancellation was counted as failure: %+v", result)
	}
	seen, ok := store.Get(1, 2, 10)
	if !ok || seen.Status != StatusPending || seen.Error != "" {
		t.Fatalf("cancellation should leave resumable pending state: %+v ok=%v", seen, ok)
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
