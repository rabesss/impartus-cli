package watch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreMarkHasAndPersistsAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	store, err := LoadStore(path)
	if err != nil {
		t.Fatalf("LoadStore empty: %v", err)
	}
	if store.Has(1, 2, 99) {
		t.Fatalf("expected empty store")
	}

	err = store.Mark(1, 2, SeenLecture{
		Status: StatusUploaded, SeqNo: 3, Topic: "Intro", StartTime: "2026-01-01",
		OutputPath: "/tmp/a.mp3", NotebookID: "nb1", SourceID: "src1",
	}, 99)
	if err != nil {
		t.Fatalf("Mark: %v", err)
	}
	if !store.Has(1, 2, 99) {
		t.Fatalf("expected marked lecture")
	}
	if store.NeedsWork(1, 2, 99, true) {
		t.Fatalf("uploaded lecture should not need work")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected mode 0600, got %v", info.Mode().Perm())
	}
	if _, tmpErr := os.Stat(path + ".tmp"); !os.IsNotExist(tmpErr) {
		t.Fatalf("temp file should not remain: %v", tmpErr)
	}
	if matches, globErr := filepath.Glob(filepath.Join(dir, ".state.json.tmp-*")); globErr != nil || len(matches) != 0 {
		t.Fatalf("unique temp files should not remain: %v err=%v", matches, globErr)
	}

	reloaded, err := LoadStore(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	seen, ok := reloaded.Get(1, 2, 99)
	if !ok {
		t.Fatalf("expected reloaded lecture")
	}
	if seen.Topic != "Intro" || seen.Status != StatusUploaded || seen.NotebookID != "nb1" {
		t.Fatalf("unexpected seen lecture: %+v", seen)
	}
}

func TestLoadStoreCorruptJSONFailsClosed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadStore(path); err == nil {
		t.Fatalf("corrupt state must fail closed to protect deduplication")
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "{not-json" {
		t.Fatalf("corrupt state must remain available for recovery: %q err=%v", got, err)
	}
}

func TestLoadStoreAcceptsLegacyOutputPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.json")
	raw := []byte(`{
	  "version": 1,
	  "courses": {
	    "1:2": {
	      "seenTtids": {
	        "7": {"outputPath": "/tmp/legacy.mp3", "uploaded": false}
	      }
	    }
	  }
	}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := LoadStore(path)
	if err != nil {
		t.Fatalf("LoadStore legacy: %v", err)
	}
	seen, ok := store.Get(1, 2, 7)
	if !ok || seen.OutputPath != "/tmp/legacy.mp3" || seen.Status != StatusDownloaded {
		t.Fatalf("legacy output path was not normalized: %+v ok=%v", seen, ok)
	}
}

func TestMarkWithoutStatusPreservesUploadedState(t *testing.T) {
	store, err := LoadStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Mark(1, 2, SeenLecture{
		Status: StatusUploaded, OutputPath: "/tmp/a.mp3", NotebookID: "nb", SourceID: "src",
		UploadKey: "impartus:1:2:7",
	}, 7); err != nil {
		t.Fatal(err)
	}
	if err := store.Mark(1, 2, SeenLecture{Topic: "updated"}, 7); err != nil {
		t.Fatal(err)
	}
	seen, _ := store.Get(1, 2, 7)
	if seen.Status != StatusUploaded || !seen.Uploaded || seen.SourceID != "src" ||
		seen.NotebookID != "nb" || seen.UploadKey != "impartus:1:2:7" {
		t.Fatalf("uploaded state regressed: %+v", seen)
	}
}

func TestMarkRollsBackMemoryWhenPersistenceFails(t *testing.T) {
	dir := t.TempDir()
	store, err := LoadStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if markErr := store.Mark(1, 2, SeenLecture{
		Status: StatusDownloaded, OutputPath: "/tmp/a.mp3", SourceID: "before",
	}, 7); markErr != nil {
		t.Fatal(markErr)
	}
	badPath := filepath.Join(dir, "cannot-replace-directory")
	if mkdirErr := os.Mkdir(badPath, 0o700); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	store.path = badPath
	err = store.Mark(1, 2, SeenLecture{
		Status: StatusUploaded, OutputPath: "/tmp/a.mp3", SourceID: "after",
	}, 7)
	if err == nil {
		t.Fatalf("expected persistence failure")
	}
	seen, ok := store.Get(1, 2, 7)
	if !ok || seen.Status != StatusDownloaded || seen.SourceID != "before" {
		t.Fatalf("failed Mark leaked into memory: %+v ok=%v", seen, ok)
	}
	if store.Has(1, 2, 7) {
		t.Fatalf("failed uploaded Mark must not affect deduplication")
	}
}

func TestNeedsWorkResumesDownloaded(t *testing.T) {
	store, err := LoadStore(filepath.Join(t.TempDir(), "s.json"))
	if err != nil {
		t.Fatal(err)
	}
	err = store.Mark(1, 2, SeenLecture{Status: StatusDownloaded, OutputPath: "/tmp/a.mp3"}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if !store.NeedsWork(1, 2, 7, true) {
		t.Fatalf("downloaded lecture should resume upload")
	}
	if store.NeedsWork(1, 2, 7, false) {
		t.Fatalf("download-only mode should treat downloaded as done")
	}
}

func TestCourseKeyAndSnapshot(t *testing.T) {
	if got := CourseKey(12, 34); got != "12:34" {
		t.Fatalf("CourseKey = %q", got)
	}
	store, err := LoadStore(filepath.Join(t.TempDir(), "s.json"))
	if err != nil {
		t.Fatal(err)
	}
	err = store.Mark(12, 34, SeenLecture{Status: StatusDownloaded, Topic: "T", OutputPath: "/tmp/t.mp3"}, 7)
	if err != nil {
		t.Fatal(err)
	}
	snap := store.Snapshot()
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(raw) {
		t.Fatalf("snapshot not valid JSON")
	}
	if _, ok := snap.Courses["12:34"]; !ok {
		t.Fatalf("snapshot missing course")
	}
}

func TestHasIgnoresFailedAttempts(t *testing.T) {
	store, err := LoadStore(filepath.Join(t.TempDir(), "s.json"))
	if err != nil {
		t.Fatal(err)
	}
	err = store.Mark(1, 2, SeenLecture{Status: StatusFailed, Topic: "fail", Error: "boom"}, 99)
	if err != nil {
		t.Fatal(err)
	}
	if store.Has(1, 2, 99) {
		t.Fatalf("failed attempt must not count as seen")
	}
	if !store.NeedsWork(1, 2, 99, true) {
		t.Fatalf("failed attempt must be retried")
	}
	err = store.Mark(1, 2, SeenLecture{Status: StatusUploaded, Topic: "ok", OutputPath: "/tmp/a.mp3"}, 99)
	if err != nil {
		t.Fatal(err)
	}
	if !store.Has(1, 2, 99) {
		t.Fatalf("successful attempt must count as seen")
	}
}
