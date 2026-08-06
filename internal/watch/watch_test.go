package watch

import (
	"context"
	"encoding/json"
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
	result := f.result
	if result.Outcome == "" {
		result.Outcome = notebooklm.UploadCreated
	}
	return result, f.err
}

func (f *fakeUploader) ReconcileUpload(_ context.Context, notebookID, _, _ string) (notebooklm.UploadResult, error) {
	f.reconcileCalls++
	f.reconcileNbs = append(f.reconcileNbs, notebookID)
	return f.reconcileResult, f.reconcileErr
}

func testCfg() *config.Config {
	cfg := &config.Config{
		Username: "u", Password: "p", BaseURL: "https://example.com",
		Quality: "144", Views: "left", AudioOnly: true, AudioFormat: "mp3",
	}
	cfg.ApplyDefaults()
	cfg.ApplyWatchMediaDefaults()
	return cfg
}

type uploadScenario struct {
	output   string
	store    *Store
	lectures client.Lectures
	opts     Options
}

func newUploadScenario(t *testing.T) uploadScenario {
	t.Helper()
	dir := t.TempDir()
	output := filepath.Join(dir, "lec.mp3")
	if err := os.WriteFile(output, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := LoadStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return uploadScenario{
		output:   output,
		store:    store,
		lectures: client.Lectures{{TTID: 10, SeqNo: 1, Topic: "Intro", StartTime: "2026-01-01"}},
		opts: Options{
			Targets: []config.WatchTarget{{SubjectID: 1, SessionID: 2, NotebookID: "nb1"}},
			Once:    true, Upload: true, MaxRetries: 1, Log: io.Discard,
		},
	}
}

func TestRunCycleDryRunDoesNotDownload(t *testing.T) {
	store, err := LoadStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	audio := &fakeAudio{join: downloader.JoinResult{LeftOutput: "/tmp/x.mp3"}}
	uploader := &fakeUploader{}
	var log strings.Builder
	w := New(testCfg(), fakeSource{lectures: client.Lectures{
		{TTID: 10, SeqNo: 1, Topic: "Intro", StartTime: "2026-01-01"},
		{TTID: 11, SeqNo: 2, Topic: "No Class Today"},
	}}, audio, uploader, store, Options{
		Targets: []config.WatchTarget{{SubjectID: 1, SessionID: 2}},
		Once:    true, DryRun: true, Log: &log,
	})

	result, err := w.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle: %v", err)
	}
	if result.New != 1 || result.Downloaded != 0 || audio.calls != 0 || uploader.calls != 0 {
		t.Fatalf("unexpected dry-run result: %+v audioCalls=%d uploadCalls=%d", result, audio.calls, uploader.calls)
	}
	if got := log.String(); !strings.Contains(got, "dry-run new lecture") || strings.Contains(got, "upload") {
		t.Fatalf("dry-run log misstates side effects: %q", got)
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
	scenario := newUploadScenario(t)
	audio := &fakeAudio{join: downloader.JoinResult{LeftOutput: scenario.output}}
	uploader := &fakeUploader{result: notebooklm.UploadResult{SourceID: "src1", NotebookID: "nb1"}}

	w := New(testCfg(), fakeSource{lectures: scenario.lectures}, audio, uploader, scenario.store, scenario.opts)
	result, err := w.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("first cycle: %v", err)
	}
	seen, ok := scenario.store.Get(1, 2, 10)
	if result.Downloaded != 1 || result.Uploaded != 1 || !ok || seen.Status != StatusUploaded {
		t.Fatalf("unexpected first cycle: %+v seen=%+v ok=%v", result, seen, ok)
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
	scenario := newUploadScenario(t)
	scenario.lectures = client.Lectures{
		{TTID: 10, SeqNo: 1, Topic: "A", StartTime: "2026-01-01"},
		{TTID: 11, SeqNo: 2, Topic: "B", StartTime: "2026-01-02"},
		{TTID: 12, SeqNo: 3, Topic: "C", StartTime: "2026-01-03"},
	}
	scenario.opts.MaxLecturesPerCycle = 2
	audio := &fakeAudio{join: downloader.JoinResult{LeftOutput: scenario.output}}
	uploader := &fakeUploader{result: notebooklm.UploadResult{SourceID: "src"}}
	w := New(testCfg(), fakeSource{lectures: scenario.lectures}, audio, uploader, scenario.store, scenario.opts)
	result, err := w.RunCycle(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.New != 2 || result.Uploaded != 2 {
		t.Fatalf("expected cap of 2, got %+v", result)
	}
}

func TestRunCycleResumesDownloadedWithoutRedownload(t *testing.T) {
	scenario := newUploadScenario(t)
	if markErr := scenario.store.Mark(1, 2, SeenLecture{
		Status: StatusDownloaded, SeqNo: 1, Topic: "Intro", OutputPath: scenario.output, NotebookID: "nb1",
	}, 10); markErr != nil {
		t.Fatal(markErr)
	}
	audio := &fakeAudio{join: downloader.JoinResult{LeftOutput: scenario.output}}
	uploader := &fakeUploader{result: notebooklm.UploadResult{SourceID: "src1"}}
	w := New(testCfg(), fakeSource{lectures: scenario.lectures}, audio, uploader, scenario.store, scenario.opts)
	result, err := w.RunCycle(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if audio.calls != 0 {
		t.Fatalf("expected resume without re-download, audioCalls=%d", audio.calls)
	}
	seen, ok := scenario.store.Get(1, 2, 10)
	if result.Downloaded != 0 || result.Uploaded != 1 || !ok || seen.Status != StatusUploaded {
		t.Fatalf("unexpected resume result: %+v seen=%+v ok=%v", result, seen, ok)
	}
}

func TestRunCycleUploadFailureResumesExistingAudio(t *testing.T) {
	scenario := newUploadScenario(t)
	firstAudio := &fakeAudio{join: downloader.JoinResult{LeftOutput: scenario.output}}
	firstUpload := &fakeUploader{
		result: notebooklm.UploadResult{Outcome: notebooklm.UploadRejected},
		err:    &notebooklm.Error{Kind: notebooklm.ErrPermanent, Message: "failed"},
	}
	first := New(testCfg(), fakeSource{lectures: scenario.lectures}, firstAudio, firstUpload, scenario.store, scenario.opts)
	result, err := first.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("first cycle: %v", err)
	}
	if result.Failed != 1 || firstAudio.calls != 1 {
		t.Fatalf("unexpected failed cycle: %+v downloads=%d", result, firstAudio.calls)
	}

	secondAudio := &fakeAudio{join: downloader.JoinResult{LeftOutput: scenario.output}}
	secondUpload := &fakeUploader{result: notebooklm.UploadResult{SourceID: "src"}}
	second := New(testCfg(), fakeSource{lectures: scenario.lectures}, secondAudio, secondUpload, scenario.store, scenario.opts)
	result, err = second.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("retry cycle: %v", err)
	}
	if secondAudio.calls != 0 || result.Downloaded != 0 || result.Uploaded != 1 {
		t.Fatalf("retry re-downloaded audio: result=%+v downloads=%d", result, secondAudio.calls)
	}
}

func TestRunCycleReconcilesAmbiguousUploadWithoutAnotherAdd(t *testing.T) {
	scenario := newUploadScenario(t)
	scenario.opts.MaxRetries = 3
	scenario.opts.Targets[0].NotebookID = "nb"
	uploader := &fakeUploader{
		result: notebooklm.UploadResult{Outcome: notebooklm.UploadAmbiguous},
		err:    &notebooklm.Error{Kind: notebooklm.ErrAmbiguous, Message: "outcome unknown"},
	}
	first := New(testCfg(), fakeSource{lectures: scenario.lectures},
		&fakeAudio{join: downloader.JoinResult{LeftOutput: scenario.output}}, uploader, scenario.store, scenario.opts)
	result, err := first.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("first cycle: %v", err)
	}
	if result.Failed != 1 || uploader.calls != 1 {
		t.Fatalf("expected one ambiguous add: result=%+v uploadCalls=%d", result, uploader.calls)
	}
	seen, ok := scenario.store.Get(1, 2, 10)
	if !ok || seen.Status != StatusAmbiguous {
		t.Fatalf("ambiguous outcome was not durable: %+v ok=%v", seen, ok)
	}

	downloadOnlyOpts := scenario.opts
	downloadOnlyOpts.Upload = false
	downloadOnly := New(testCfg(), fakeSource{lectures: scenario.lectures}, &fakeAudio{}, uploader, scenario.store, downloadOnlyOpts)
	result, err = downloadOnly.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("download-only cycle: %v", err)
	}
	if result.Skipped != 1 || uploader.calls != 1 || uploader.reconcileCalls != 0 {
		t.Fatalf("download-only cycle disturbed ambiguous upload: result=%+v uploadCalls=%d reconcileCalls=%d",
			result, uploader.calls, uploader.reconcileCalls)
	}
	seen, _ = scenario.store.Get(1, 2, 10)
	if seen.Status != StatusAmbiguous {
		t.Fatalf("download-only cycle cleared ambiguous state: %+v", seen)
	}

	reconcileOpts := scenario.opts
	reconcileOpts.Targets = []config.WatchTarget{{SubjectID: 1, SessionID: 2, NotebookID: "reconfigured-nb"}}
	uploader.reconcileResult = notebooklm.UploadResult{Outcome: notebooklm.UploadAmbiguous}
	uploader.reconcileErr = &notebooklm.Error{Kind: notebooklm.ErrAmbiguous, Message: "source is not ready"}
	if removeErr := os.Remove(scenario.output); removeErr != nil {
		t.Fatal(removeErr)
	}
	second := New(testCfg(), fakeSource{lectures: scenario.lectures}, nil, uploader, scenario.store, reconcileOpts)
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
	seen, _ = scenario.store.Get(1, 2, 10)
	if seen.Status != StatusAmbiguous {
		t.Fatalf("unresolved ambiguity was not preserved: %+v", seen)
	}

	uploader.reconcileResult = notebooklm.UploadResult{
		Outcome: notebooklm.UploadFound, SourceID: "src-late", NotebookID: "nb",
	}
	uploader.reconcileErr = nil
	third := New(testCfg(), fakeSource{lectures: scenario.lectures}, nil, uploader, scenario.store, reconcileOpts)
	result, err = third.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("third cycle: %v", err)
	}
	if result.Uploaded != 1 || uploader.calls != 1 || uploader.reconcileCalls != 2 {
		t.Fatalf("late source was not reconciled: result=%+v uploadCalls=%d reconcileCalls=%d",
			result, uploader.calls, uploader.reconcileCalls)
	}
	seen, _ = scenario.store.Get(1, 2, 10)
	if seen.Status != StatusUploaded || seen.SourceID != "src-late" {
		t.Fatalf("late source did not complete durable state: %+v", seen)
	}
}

func TestRunCycleKeepsAmbiguousStateWhenReconciliationFails(t *testing.T) {
	scenario := newUploadScenario(t)
	scenario.opts.Targets[0].NotebookID = "nb"
	scenario.opts.DeleteAudioAfterUpload = true
	if markErr := scenario.store.Mark(1, 2, SeenLecture{
		Status:     StatusAmbiguous,
		SeqNo:      1,
		Topic:      "Intro",
		OutputPath: scenario.output,
		NotebookID: "nb",
		UploadKey:  "impartus:1:2:10",
	}, 10); markErr != nil {
		t.Fatal(markErr)
	}
	uploader := &fakeUploader{
		reconcileResult: notebooklm.UploadResult{Outcome: notebooklm.UploadAmbiguous},
		reconcileErr:    &notebooklm.Error{Kind: notebooklm.ErrTransient, Message: "source list timed out"},
	}
	w := New(testCfg(), fakeSource{lectures: scenario.lectures}, nil, uploader, scenario.store, scenario.opts)

	result, err := w.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("reconciliation cycle: %v", err)
	}
	seen, ok := scenario.store.Get(1, 2, 10)
	if result.Failed != 1 || !ok || seen.Status != StatusAmbiguous || seen.NotebookID != "nb" {
		t.Fatalf("reconcile error lost fail-closed state: result=%+v seen=%+v ok=%v", result, seen, ok)
	}
	if uploader.calls != 0 || uploader.reconcileCalls != 1 {
		t.Fatalf("reconcile error issued an add: uploadCalls=%d reconcileCalls=%d",
			uploader.calls, uploader.reconcileCalls)
	}
	if _, statErr := os.Stat(scenario.output); statErr != nil {
		t.Fatalf("ambiguous reconciliation deleted the only local audio: %v", statErr)
	}
}

func TestRunCycleFailsClosedOnInvalidSuccessfulOutcome(t *testing.T) {
	scenario := newUploadScenario(t)
	uploader := &fakeUploader{
		result:          notebooklm.UploadResult{Outcome: notebooklm.UploadOutcome("invalid")},
		reconcileResult: notebooklm.UploadResult{Outcome: notebooklm.UploadOutcome("invalid")},
	}
	w := New(testCfg(), fakeSource{lectures: scenario.lectures},
		&fakeAudio{join: downloader.JoinResult{LeftOutput: scenario.output}},
		uploader, scenario.store, scenario.opts)
	if _, err := w.RunCycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	seen, ok := scenario.store.Get(1, 2, 10)
	if !ok || seen.Status != StatusAmbiguous || uploader.calls != 1 {
		t.Fatalf("invalid success did not fail closed: seen=%+v ok=%v addCalls=%d", seen, ok, uploader.calls)
	}
	if _, err := w.RunCycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	seen, ok = scenario.store.Get(1, 2, 10)
	if !ok || seen.ReconcileAttempts != 1 || uploader.calls != 1 || uploader.reconcileCalls != 1 {
		t.Fatalf("invalid reconciliation was not counted fail-closed: seen=%+v ok=%v addCalls=%d reconcileCalls=%d",
			seen, ok, uploader.calls, uploader.reconcileCalls)
	}
}

func TestRunCyclePausesAfterBoundedAmbiguousReconciliation(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	store, err := LoadStore(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if markErr := store.Mark(1, 2, SeenLecture{
		Status:     StatusAmbiguous,
		SeqNo:      1,
		Topic:      "Intro",
		NotebookID: "nb",
		UploadKey:  "impartus:1:2:10",
	}, 10); markErr != nil {
		t.Fatal(markErr)
	}
	uploader := &fakeUploader{
		reconcileResult: notebooklm.UploadResult{Outcome: notebooklm.UploadAmbiguous},
		reconcileErr:    &notebooklm.Error{Kind: notebooklm.ErrAmbiguous, Message: "source is not ready"},
	}
	opts := Options{
		Targets: []config.WatchTarget{{SubjectID: 1, SessionID: 2, NotebookID: "nb"}},
		Once:    true, Upload: true, Log: io.Discard,
	}

	for attempt := 1; attempt <= 3; attempt++ {
		w := New(testCfg(), fakeSource{lectures: client.Lectures{{TTID: 10, SeqNo: 1, Topic: "Intro"}}},
			nil, uploader, store, opts)
		result, cycleErr := w.RunCycle(context.Background())
		if cycleErr != nil {
			t.Fatalf("reconcile attempt %d stopped the cycle: %v", attempt, cycleErr)
		}
		if result.Failed != 1 {
			t.Fatalf("reconcile attempt %d result = %+v, want one recorded failure", attempt, result)
		}
		if attempt == 3 && (len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "manual verification")) {
			t.Fatalf("final reconcile result did not report the manual-verification pause: %+v", result)
		}
		store, err = LoadStore(statePath)
		if err != nil {
			t.Fatalf("reload state after reconcile attempt %d: %v", attempt, err)
		}
	}

	seen, ok := store.Get(1, 2, 10)
	if !ok || seen.Status != StatusAmbiguous || seen.ReconcileAttempts != 3 {
		t.Fatalf("pause must preserve fail-closed state: %+v ok=%v", seen, ok)
	}
	if store.NeedsWork(1, 2, 10, true) {
		t.Fatalf("exhausted ambiguous lecture should remain paused")
	}
	w := New(testCfg(), fakeSource{lectures: client.Lectures{{TTID: 10, SeqNo: 1, Topic: "Intro"}}},
		nil, uploader, store, opts)
	result, cycleErr := w.RunCycle(context.Background())
	if cycleErr != nil || result.Skipped != 1 || result.Failed != 0 {
		t.Fatalf("paused lecture was not skipped cleanly: result=%+v err=%v", result, cycleErr)
	}
	if uploader.calls != 0 || uploader.reconcileCalls != 3 {
		t.Fatalf("bounded reconciliation issued an add or wrong probe count: uploadCalls=%d reconcileCalls=%d",
			uploader.calls, uploader.reconcileCalls)
	}
}

func TestRunContinuesDaemonAfterAmbiguousLectureIsPaused(t *testing.T) {
	dir := t.TempDir()
	output := filepath.Join(dir, "lec.mp3")
	if writeErr := os.WriteFile(output, []byte("audio"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	store, err := LoadStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if markErr := store.Mark(1, 2, SeenLecture{
		Status:            StatusAmbiguous,
		SeqNo:             1,
		Topic:             "Intro",
		NotebookID:        "nb",
		UploadKey:         "impartus:1:2:10",
		ReconcileAttempts: 2,
	}, 10); markErr != nil {
		t.Fatal(markErr)
	}
	uploader := &fakeUploader{
		reconcileResult: notebooklm.UploadResult{Outcome: notebooklm.UploadAmbiguous},
		reconcileErr:    &notebooklm.Error{Kind: notebooklm.ErrAmbiguous, Message: "source is not ready"},
	}
	uploader.result = notebooklm.UploadResult{SourceID: "src"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	uploader.beforeUpload = cancel
	source := targetSource{lectures: map[string]client.Lectures{
		"1:2": {{TTID: 10, SeqNo: 1, Topic: "ambiguous"}},
		"3:4": {{TTID: 20, SeqNo: 1, Topic: "healthy"}},
	}}
	w := New(testCfg(), source, &fakeAudio{join: downloader.JoinResult{LeftOutput: output}},
		uploader, store, Options{
			Targets: []config.WatchTarget{
				{SubjectID: 1, SessionID: 2, NotebookID: "nb"},
				{SubjectID: 3, SessionID: 4, NotebookID: "nb-two"},
			},
			Upload: true,
			Log:    io.Discard,
		})

	result, runErr := w.Run(ctx)
	if !errors.Is(runErr, context.Canceled) {
		t.Fatalf("Run error = %v, want daemon to continue until context cancellation", runErr)
	}
	paused, pausedOK := store.Get(1, 2, 10)
	healthy, healthyOK := store.Get(3, 4, 20)
	if !pausedOK || paused.Status != StatusAmbiguous || paused.ReconcileAttempts != 3 ||
		!healthyOK || healthy.Status != StatusUploaded {
		t.Fatalf("daemon did not preserve the pause and process the healthy target: paused=%+v ok=%v healthy=%+v ok=%v",
			paused, pausedOK, healthy, healthyOK)
	}
	if result.Failed != 1 || result.Uploaded != 1 || uploader.calls != 1 || uploader.reconcileCalls != 1 {
		t.Fatalf("unexpected continuing daemon result: %+v uploadCalls=%d reconcileCalls=%d",
			result, uploader.calls, uploader.reconcileCalls)
	}
}

func TestPersistUploadErrorReportsStateWriteFailure(t *testing.T) {
	store, err := LoadStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	initial := SeenLecture{
		Status:     StatusAmbiguous,
		SeqNo:      1,
		Topic:      "Intro",
		NotebookID: "nb",
		UploadKey:  "impartus:1:2:10",
	}
	if markErr := store.Mark(1, 2, initial, 10); markErr != nil {
		t.Fatal(markErr)
	}
	persistErr := errors.New("state filesystem is read-only")
	store.writeFile = func(string, []byte, os.FileMode) error { return persistErr }
	uploadErr := errors.New("provider rejected upload")
	w := &Watcher{store: store}

	err = w.persistUploadError(
		context.Background(),
		config.WatchTarget{SubjectID: 1, SessionID: 2},
		client.Lecture{TTID: 10},
		notebooklm.UploadRejected,
		initial,
		uploadErr,
	)
	if !errors.Is(err, uploadErr) || !errors.Is(err, persistErr) {
		t.Fatalf("expected upload and persistence failures, got %v", err)
	}
	seen, ok := store.Get(1, 2, 10)
	if !ok || seen.Status != StatusAmbiguous {
		t.Fatalf("failed state write must preserve fail-closed state: %+v ok=%v", seen, ok)
	}
}

func TestRunCycleHardStopsWhenRejectedUploadCannotPersistFailedState(t *testing.T) {
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
		Status:     StatusDownloaded,
		SeqNo:      1,
		Topic:      "Intro",
		OutputPath: out,
		NotebookID: "nb",
		UploadKey:  "impartus:1:2:10",
	}, 10); markErr != nil {
		t.Fatal(markErr)
	}

	persistErr := errors.New("state filesystem is read-only")
	failedWrites := 0
	store.writeFile = func(path string, data []byte, mode os.FileMode) error {
		var snapshot State
		if err := json.Unmarshal(data, &snapshot); err != nil {
			t.Fatalf("decode attempted state write: %v", err)
		}
		seen := snapshot.Courses[CourseKey(1, 2)].SeenTTIDs["10"]
		if seen.Status == StatusFailed {
			failedWrites++
			return persistErr
		}
		return atomicWriteFile(path, data, mode)
	}
	uploadErr := &notebooklm.Error{Kind: notebooklm.ErrPermanent, Message: "provider rejected upload"}
	uploader := &fakeUploader{
		result: notebooklm.UploadResult{Outcome: notebooklm.UploadRejected},
		err:    uploadErr,
	}
	w := New(testCfg(), fakeSource{lectures: client.Lectures{{TTID: 10, SeqNo: 1, Topic: "Intro"}}},
		&fakeAudio{}, uploader, store, Options{
			Targets: []config.WatchTarget{{SubjectID: 1, SessionID: 2, NotebookID: "nb"}},
			Once:    true, Upload: true, MaxRetries: 1, Log: io.Discard,
		})

	result, cycleErr := w.RunCycle(context.Background())
	if cycleErr == nil || !strings.Contains(cycleErr.Error(), "watch stopped to preserve upload safety") {
		t.Fatalf("RunCycle error = %v, want fatal state-safety error", cycleErr)
	}
	if !errors.Is(cycleErr, uploadErr) || !errors.Is(cycleErr, persistErr) {
		t.Fatalf("fatal error must retain upload and persistence causes: %v", cycleErr)
	}
	if failedWrites != 2 {
		t.Fatalf("failed-state writes = %d, want initial attempt plus one retry", failedWrites)
	}
	if result.Failed != 1 || uploader.calls != 1 {
		t.Fatalf("unexpected fatal cycle result: %+v uploadCalls=%d", result, uploader.calls)
	}
	seen, ok := store.Get(1, 2, 10)
	if !ok || seen.Status != StatusAmbiguous {
		t.Fatalf("failed demotion must preserve fail-closed state: %+v ok=%v", seen, ok)
	}
}

func TestRunCycleRetriesFailedStatePersistenceOnce(t *testing.T) {
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
		Status:     StatusDownloaded,
		SeqNo:      1,
		Topic:      "Intro",
		OutputPath: out,
		NotebookID: "nb",
		UploadKey:  "impartus:1:2:10",
	}, 10); markErr != nil {
		t.Fatal(markErr)
	}

	failedWrites := 0
	store.writeFile = func(path string, data []byte, mode os.FileMode) error {
		var snapshot State
		if err := json.Unmarshal(data, &snapshot); err != nil {
			t.Fatalf("decode attempted state write: %v", err)
		}
		seen := snapshot.Courses[CourseKey(1, 2)].SeenTTIDs["10"]
		if seen.Status == StatusFailed {
			failedWrites++
			if failedWrites == 1 {
				return errors.New("transient state write failure")
			}
		}
		return atomicWriteFile(path, data, mode)
	}
	uploader := &fakeUploader{
		result: notebooklm.UploadResult{Outcome: notebooklm.UploadRejected},
		err:    &notebooklm.Error{Kind: notebooklm.ErrPermanent, Message: "provider rejected upload"},
	}
	w := New(testCfg(), fakeSource{lectures: client.Lectures{{TTID: 10, SeqNo: 1, Topic: "Intro"}}},
		&fakeAudio{}, uploader, store, Options{
			Targets: []config.WatchTarget{{SubjectID: 1, SessionID: 2, NotebookID: "nb"}},
			Once:    true, Upload: true, MaxRetries: 1, Log: io.Discard,
		})

	result, cycleErr := w.RunCycle(context.Background())
	if cycleErr != nil {
		t.Fatalf("retryable state write failure aborted cycle: %v", cycleErr)
	}
	if failedWrites != 2 || result.Failed != 1 {
		t.Fatalf("unexpected persistence retry result: writes=%d result=%+v", failedWrites, result)
	}
	seen, ok := store.Get(1, 2, 10)
	if !ok || seen.Status != StatusFailed {
		t.Fatalf("retry did not persist failed state: %+v ok=%v", seen, ok)
	}
}

func TestRunCyclePersistsReconciliationOnlyIntentBeforeAdd(t *testing.T) {
	scenario := newUploadScenario(t)
	var statusAtProviderCall LectureStatus
	uploader := &fakeUploader{
		result: notebooklm.UploadResult{SourceID: "src"},
		beforeUpload: func() {
			seen, _ := scenario.store.Get(1, 2, 10)
			statusAtProviderCall = seen.Status
		},
	}
	w := New(testCfg(), fakeSource{lectures: scenario.lectures},
		&fakeAudio{join: downloader.JoinResult{LeftOutput: scenario.output}},
		uploader, scenario.store, scenario.opts)
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
	scenario := newUploadScenario(t)
	source := targetSource{lectures: map[string]client.Lectures{
		"1:2": {{TTID: 10, SeqNo: 1, Topic: "one"}},
		"3:4": {{TTID: 20, SeqNo: 1, Topic: "two"}},
	}}
	uploader := &fakeUploader{result: notebooklm.UploadResult{SourceID: "src"}}
	scenario.opts.Targets = []config.WatchTarget{
		{SubjectID: 1, SessionID: 2, NotebookID: "nb-one"},
		{SubjectID: 3, SessionID: 4, NotebookID: "nb-two"},
	}
	scenario.opts.MaxLecturesPerCycle = 2
	w := New(testCfg(), source, &fakeAudio{join: downloader.JoinResult{LeftOutput: scenario.output}},
		uploader, scenario.store, scenario.opts)
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
	scenario := newUploadScenario(t)
	scenario.opts.DeleteAudioAfterUpload = true
	w := New(testCfg(), fakeSource{lectures: scenario.lectures},
		&fakeAudio{join: downloader.JoinResult{LeftOutput: scenario.output}},
		&fakeUploader{result: notebooklm.UploadResult{SourceID: "src"}},
		scenario.store, scenario.opts)
	if _, err := w.RunCycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(scenario.output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("uploaded audio was not deleted: %v", err)
	}
}

func TestRunCycleAuthFailureAborts(t *testing.T) {
	scenario := newUploadScenario(t)
	scenario.lectures = append(scenario.lectures,
		client.Lecture{TTID: 11, SeqNo: 2, Topic: "Next", StartTime: "2026-01-02"})
	scenario.opts.MaxRetries = 3
	audio := &fakeAudio{join: downloader.JoinResult{LeftOutput: scenario.output}}
	authErr := &notebooklm.Error{Kind: notebooklm.ErrAuth, Message: "re-authenticate"}
	uploader := &fakeUploader{
		result: notebooklm.UploadResult{Outcome: notebooklm.UploadAmbiguous},
		err: &notebooklm.Error{
			Kind: notebooklm.ErrAmbiguous, Message: "upload outcome is ambiguous", Err: authErr,
		},
	}
	w := New(testCfg(), fakeSource{lectures: scenario.lectures}, audio, uploader, scenario.store, scenario.opts)
	_, err := w.RunCycle(context.Background())
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

func TestWithRetriesDoesNotRetryAmbiguousNestedRateLimit(t *testing.T) {
	attempts := 0
	err := withRetries(context.Background(), 3, func(int) time.Duration { return time.Millisecond }, io.Discard, func() error {
		attempts++
		return &notebooklm.Error{
			Kind:    notebooklm.ErrAmbiguous,
			Message: "upload outcome is ambiguous",
			Err:     &notebooklm.Error{Kind: notebooklm.ErrRateLimit, Message: "retry later"},
		}
	})
	if !notebooklm.IsAmbiguous(err) {
		t.Fatalf("error = %v, want ambiguous outcome", err)
	}
	if attempts != 1 {
		t.Fatalf("ambiguous upload was retried %d times, want exactly one provider call", attempts)
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
		Targets: []config.WatchTarget{{SubjectID: 1, SessionID: 2}},
		Once:    true, DryRun: true, Interval: time.Hour, Log: io.Discard,
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
		Targets: []config.WatchTarget{{SubjectID: 1, SessionID: 2}},
		DryRun:  true, Interval: time.Millisecond, Log: io.Discard,
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
			Targets: []config.WatchTarget{{SubjectID: 1, SessionID: 2}},
			Once:    true, MaxRetries: 1, Log: io.Discard,
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
	scenario := newUploadScenario(t)

	failAudio := &fakeAudio{joinErr: errors.New("download blip")}
	w := New(testCfg(), fakeSource{lectures: scenario.lectures}, failAudio,
		&fakeUploader{}, scenario.store, scenario.opts)
	result, err := w.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("failing cycle: %v", err)
	}
	seen, ok := scenario.store.Get(1, 2, 10)
	if result.Failed != 1 || !ok || seen.Status != StatusFailed {
		t.Fatalf("expected failed state after blip: %+v seen=%+v ok=%v", result, seen, ok)
	}

	okAudio := &fakeAudio{join: downloader.JoinResult{LeftOutput: scenario.output}}
	uploader := &fakeUploader{result: notebooklm.UploadResult{SourceID: "src1"}}
	w = New(testCfg(), fakeSource{lectures: scenario.lectures}, okAudio,
		uploader, scenario.store, scenario.opts)
	result, err = w.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("retry cycle: %v", err)
	}
	seen, ok = scenario.store.Get(1, 2, 10)
	if result.Downloaded != 1 || result.Uploaded != 1 || !ok || seen.Status != StatusUploaded {
		t.Fatalf("expected retry success: %+v seen=%+v ok=%v", result, seen, ok)
	}
}
