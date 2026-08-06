package watch

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestStoreMarkHasAndPersistsAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	store, err := LoadStore(path)
	if err != nil {
		t.Fatalf("LoadStore empty: %v", err)
	}
	if _, ok := store.Get(1, 2, 99); ok {
		t.Fatalf("expected empty store")
	}

	err = store.Mark(1, 2, SeenLecture{
		Status: StatusUploaded, SeqNo: 3, Topic: "Intro", StartTime: "2026-01-01",
		OutputPath: "/tmp/a.mp3", NotebookID: "nb1", SourceID: "src1",
	}, 99)
	if err != nil {
		t.Fatalf("Mark: %v", err)
	}
	marked, ok := store.Get(1, 2, 99)
	if !ok || marked.Status != StatusUploaded {
		t.Fatalf("expected uploaded lecture, got %+v ok=%v", marked, ok)
	}
	if store.NeedsWork(1, 2, 99, true) {
		t.Fatalf("uploaded lecture should not need work")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
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

func TestLoadStoreRejectsUnsupportedVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.json")
	if err := os.WriteFile(path, []byte(`{"version":2,"courses":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadStore(path); err == nil {
		t.Fatal("unsupported state version must fail closed")
	}
}

func TestLoadStoreRejectsStateWithoutStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.json")
	raw := []byte(`{
	  "version": 1,
	  "courses": {
	    "1:2": {
	      "seenTtids": {
	        "7": {"outputPath": "/tmp/legacy.mp3"}
	      }
	    }
	  }
	}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadStore(path); err == nil {
		t.Fatalf("state without an explicit status must fail closed")
	}
}

func TestMarkRequiresStatusAndPreservesUploadedMetadata(t *testing.T) {
	store, err := LoadStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Mark(1, 2, SeenLecture{
		Status: StatusUploaded, OutputPath: "/tmp/a.mp3", NotebookID: "nb", SourceID: "src",
		UploadKey: "impartus:1:2:7", SeqNo: 7, Topic: "original", StartTime: "2026-07-29T10:00:00Z",
	}, 7); err != nil {
		t.Fatal(err)
	}
	if err := store.Mark(1, 2, SeenLecture{Topic: "updated"}, 7); err == nil {
		t.Fatal("Mark accepted a lecture without status")
	}
	if err := store.Mark(1, 2, SeenLecture{Status: StatusUploaded, Topic: "updated"}, 7); err != nil {
		t.Fatal(err)
	}
	seen, _ := store.Get(1, 2, 7)
	if seen.Status != StatusUploaded || seen.SourceID != "src" ||
		seen.NotebookID != "nb" || seen.UploadKey != "impartus:1:2:7" ||
		seen.SeqNo != 7 || seen.Topic != "updated" || seen.StartTime != "2026-07-29T10:00:00Z" {
		t.Fatalf("uploaded state regressed: %+v", seen)
	}
	if err := store.Mark(1, 2, SeenLecture{Status: StatusFailed, Error: "retry"}, 7); err != nil {
		t.Fatal(err)
	}
	seen, _ = store.Get(1, 2, 7)
	if seen.SeqNo != 7 || seen.Topic != "updated" || seen.StartTime != "2026-07-29T10:00:00Z" {
		t.Fatalf("lecture metadata regressed: %+v", seen)
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
}

func TestMarkKeepsCommittedStateWhenDirectorySyncFails(t *testing.T) {
	store, err := LoadStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if markErr := store.Mark(1, 2, SeenLecture{Status: StatusDownloaded, OutputPath: "/tmp/a.mp3"}, 7); markErr != nil {
		t.Fatal(markErr)
	}
	store.writeFile = func(string, []byte, os.FileMode) error {
		return &stateWriteError{err: errors.New("directory sync failed"), committed: true}
	}
	err = store.Mark(1, 2, SeenLecture{
		Status: StatusUploaded, OutputPath: "/tmp/a.mp3", SourceID: "src",
	}, 7)
	if err == nil {
		t.Fatal("expected committed durability warning")
	}
	seen, ok := store.Get(1, 2, 7)
	if !ok || seen.Status != StatusUploaded || seen.SourceID != "src" {
		t.Fatalf("committed state was rolled back in memory: %+v ok=%v", seen, ok)
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

func TestNeedsWorkReconcilesAmbiguousOnlyWhenUploadEnabled(t *testing.T) {
	store, err := LoadStore(filepath.Join(t.TempDir(), "s.json"))
	if err != nil {
		t.Fatal(err)
	}
	err = store.Mark(1, 2, SeenLecture{
		Status: StatusAmbiguous, OutputPath: "/tmp/a.mp3", UploadKey: "impartus:1:2:7",
	}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if !store.NeedsWork(1, 2, 7, true) {
		t.Fatalf("ambiguous lecture should be reconciled when upload is enabled")
	}
	if store.NeedsWork(1, 2, 7, false) {
		t.Fatalf("download-only mode should not process an ambiguous remote upload")
	}
	reloaded, err := LoadStore(filepath.Join(filepath.Dir(store.path), "s.json"))
	if err != nil {
		t.Fatal(err)
	}
	seen, ok := reloaded.Get(1, 2, 7)
	if !ok || seen.UploadKey != "impartus:1:2:7" {
		t.Fatalf("ambiguous upload key was not durable: %+v ok=%v", seen, ok)
	}
}

func TestUploadedStatusControlsDeduplication(t *testing.T) {
	store, err := LoadStore(filepath.Join(t.TempDir(), "s.json"))
	if err != nil {
		t.Fatal(err)
	}
	err = store.Mark(1, 2, SeenLecture{Status: StatusFailed, Topic: "fail", Error: "boom"}, 99)
	if err != nil {
		t.Fatal(err)
	}
	seen, ok := store.Get(1, 2, 99)
	if !ok || seen.Status != StatusFailed {
		t.Fatalf("failed attempt status = %+v ok=%v", seen, ok)
	}
	if !store.NeedsWork(1, 2, 99, true) {
		t.Fatalf("failed attempt must be retried")
	}
	err = store.Mark(1, 2, SeenLecture{Status: StatusUploaded, Topic: "ok", OutputPath: "/tmp/a.mp3"}, 99)
	if err != nil {
		t.Fatal(err)
	}
	seen, ok = store.Get(1, 2, 99)
	if !ok || seen.Status != StatusUploaded {
		t.Fatalf("successful attempt status = %+v ok=%v", seen, ok)
	}
}
