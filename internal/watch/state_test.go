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

	if err := store.Mark(1, 2, SeenLecture{
		SeqNo: 3, Topic: "Intro", StartTime: "2026-01-01", OutputPath: "/tmp/a.mp3", Uploaded: true, NotebookID: "nb1",
	}, 99); err != nil {
		t.Fatalf("Mark: %v", err)
	}
	if !store.Has(1, 2, 99) {
		t.Fatalf("expected marked lecture")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected mode 0600, got %v", info.Mode().Perm())
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temp file should not remain: %v", err)
	}

	reloaded, err := LoadStore(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	seen, ok := reloaded.Get(1, 2, 99)
	if !ok {
		t.Fatalf("expected reloaded lecture")
	}
	if seen.Topic != "Intro" || !seen.Uploaded || seen.NotebookID != "nb1" {
		t.Fatalf("unexpected seen lecture: %+v", seen)
	}
}

func TestLoadStoreRejectsCorruptJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadStore(path); err == nil {
		t.Fatalf("expected corrupt JSON error")
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
	_ = store.Mark(12, 34, SeenLecture{Topic: "T"}, 7)
	snap := store.Snapshot()
	raw, _ := json.Marshal(snap)
	if !json.Valid(raw) {
		t.Fatalf("snapshot not valid JSON")
	}
	if _, ok := snap.Courses["12:34"]; !ok {
		t.Fatalf("snapshot missing course")
	}
}
