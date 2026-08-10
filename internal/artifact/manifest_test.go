package artifact

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildManifestStatsFileAndNormalizesSelection(t *testing.T) {
	outputPath := filepath.Join(t.TempDir(), "lecture.mp4")
	if err := os.WriteFile(outputPath, []byte("media"), 0o600); err != nil {
		t.Fatalf("write output fixture: %v", err)
	}
	producedAt := time.Date(2026, time.August, 8, 4, 5, 6, 0, time.UTC)

	manifest, err := Build(BuildInput{
		Lecture: Lecture{
			TTID:            12345,
			InstituteID:     4,
			SubjectID:       67,
			SessionID:       8,
			SeqNo:           12,
			Topic:           "Topic",
			StartTime:       "upstream value",
			DurationSeconds: 3600,
			Professor:       "Name",
			Institute:       "Institute",
		},
		Selection:  Selection{Views: "first", Quality: "720"},
		Files:      []FileSpec{{Path: outputPath, Role: "video", View: "left", Container: "mp4"}},
		ProducedAt: producedAt,
		Producer:   Producer{Name: "impartus", Version: "0.1.20"},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if manifest.SchemaVersion != 1 {
		t.Fatalf("SchemaVersion = %d, want 1", manifest.SchemaVersion)
	}
	if manifest.ArtifactID == "" {
		t.Fatal("ArtifactID is empty")
	}
	if manifest.Selection.Views != "left" || manifest.Selection.AudioFormat != "" {
		t.Fatalf("Selection = %+v, want normalized left video selection", manifest.Selection)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Path != outputPath || manifest.Files[0].Bytes != 5 {
		t.Fatalf("Files = %+v, want absolute five-byte output", manifest.Files)
	}
	if !manifest.ProducedAt.Equal(producedAt) {
		t.Fatalf("ProducedAt = %s, want %s", manifest.ProducedAt, producedAt)
	}

	recomputedID, err := NewID(manifest.Identity())
	if err != nil {
		t.Fatalf("NewID(manifest.Identity()) error = %v", err)
	}
	if recomputedID != manifest.ArtifactID {
		t.Fatalf("recomputed ID = %q, want %q", recomputedID, manifest.ArtifactID)
	}
}

func TestManifestJSONGolden(t *testing.T) {
	outputPath := filepath.Join(t.TempDir(), "lecture.mkv")
	if err := os.WriteFile(outputPath, []byte("media"), 0o600); err != nil {
		t.Fatalf("write output fixture: %v", err)
	}
	manifest, err := Build(BuildInput{
		Lecture: Lecture{
			TTID:            12345,
			InstituteID:     4,
			SubjectID:       67,
			SessionID:       8,
			SeqNo:           12,
			Topic:           "Topic",
			StartTime:       "upstream value",
			DurationSeconds: 3600,
			Professor:       "Name",
			Institute:       "Institute",
		},
		Selection:  Selection{Views: "both", Quality: "720"},
		Files:      []FileSpec{{Path: outputPath, Role: "video", View: "both", Container: "mkv"}},
		ProducedAt: time.Date(2026, time.August, 8, 4, 5, 6, 0, time.UTC),
		Producer:   Producer{Name: "impartus", Version: "0.1.20"},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	pathJSON, err := json.Marshal(outputPath)
	if err != nil {
		t.Fatalf("marshal path: %v", err)
	}
	want := `{"schemaVersion":1,"artifactId":"impartus:v1:CmQ1iLsQw_Aarxg3Rp4svvDdKX4sJ6R0KFWXn3keTn4","lecture":{"ttid":12345,"instituteId":4,"subjectId":67,"sessionId":8,"seqNo":12,"topic":"Topic","startTime":"upstream value","durationSeconds":3600,"professor":"Name","institute":"Institute","noAudio":false},"selection":{"views":"both","quality":"720","audioOnly":false,"audioFormat":""},"files":[{"path":` + string(pathJSON) + `,"role":"video","view":"both","container":"mkv","bytes":5}],"producedAt":"2026-08-08T04:05:06Z","producer":{"name":"impartus","version":"0.1.20"}}`
	if string(encoded) != want {
		t.Fatalf("manifest JSON mismatch\n got: %s\nwant: %s", encoded, want)
	}
}

func TestBuildRejectsIncompleteOutputs(t *testing.T) {
	base := BuildInput{
		Lecture:    Lecture{TTID: 4, InstituteID: 1, SubjectID: 2, SessionID: 3},
		Selection:  Selection{Views: "left", Quality: "720"},
		ProducedAt: time.Now(),
		Producer:   Producer{Name: "impartus", Version: "test"},
	}
	tests := []struct {
		name    string
		prepare func(*testing.T) string
		want    string
	}{
		{name: "missing", prepare: func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing.mp4") }, want: "stat output"},
		{name: "partial", prepare: func(t *testing.T) string {
			path := filepath.Join(t.TempDir(), "lecture.mp4.part")
			if err := os.WriteFile(path, []byte("partial"), 0o600); err != nil {
				t.Fatal(err)
			}
			return path
		}, want: "still partial"},
		{name: "empty", prepare: func(t *testing.T) string {
			path := filepath.Join(t.TempDir(), "lecture.mp4")
			if err := os.WriteFile(path, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			return path
		}, want: "is empty"},
		{name: "symlink", prepare: func(t *testing.T) string {
			directory := t.TempDir()
			target := filepath.Join(directory, "target.mp4")
			if err := os.WriteFile(target, []byte("media"), 0o600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, "lecture.mp4")
			if err := os.Symlink(target, path); err != nil {
				t.Skipf("create output symlink: %v", err)
			}
			return path
		}, want: "is a symlink"},
		{name: "non-regular", prepare: func(t *testing.T) string { return t.TempDir() }, want: "not a regular file"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := base
			input.Files = []FileSpec{{Path: test.prepare(t), Role: "video", View: "left", Container: "mp4"}}
			_, err := Build(input)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Build() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestBuildRejectsInvalidManifestMetadata(t *testing.T) {
	outputPath := filepath.Join(t.TempDir(), "lecture.mp4")
	if err := os.WriteFile(outputPath, []byte("media"), 0o600); err != nil {
		t.Fatal(err)
	}
	valid := BuildInput{
		Lecture:    Lecture{TTID: 4, InstituteID: 1, SubjectID: 2, SessionID: 3},
		Selection:  Selection{Views: "left", Quality: "720"},
		Files:      []FileSpec{{Path: outputPath, Role: "video", View: "left", Container: "mp4"}},
		ProducedAt: time.Now(),
		Producer:   Producer{Name: "impartus", Version: "test"},
	}
	tests := []struct {
		name   string
		change func(*BuildInput)
		want   string
	}{
		{name: "no files", change: func(input *BuildInput) { input.Files = nil }, want: "at least one"},
		{name: "zero producedAt", change: func(input *BuildInput) { input.ProducedAt = time.Time{} }, want: "producedAt"},
		{name: "empty producer name", change: func(input *BuildInput) { input.Producer.Name = "" }, want: "producer"},
		{name: "empty producer version", change: func(input *BuildInput) { input.Producer.Version = "" }, want: "producer"},
		{name: "duplicate output", change: func(input *BuildInput) { input.Files = append(input.Files, input.Files[0]) }, want: "duplicate output"},
		{name: "malformed sha256", change: func(input *BuildInput) { input.Files[0].SHA256 = "not-a-digest" }, want: "sha256 must"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := valid
			input.Files = append([]FileSpec(nil), valid.Files...)
			test.change(&input)
			_, err := Build(input)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Build() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestBuildVerifiesProvidedSHA256(t *testing.T) {
	outputPath := filepath.Join(t.TempDir(), "lecture.mp4")
	contents := []byte("known media")
	if err := os.WriteFile(outputPath, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	base := BuildInput{
		Lecture:    Lecture{TTID: 4, InstituteID: 1, SubjectID: 2, SessionID: 3},
		Selection:  Selection{Views: "left", Quality: "720"},
		ProducedAt: time.Now(),
		Producer:   Producer{Name: "impartus", Version: "test"},
	}

	t.Run("matching", func(t *testing.T) {
		digest := sha256.Sum256(contents)
		input := base
		input.Files = []FileSpec{{Path: outputPath, Role: "video", View: "left", Container: "mp4", SHA256: fmt.Sprintf("%x", digest)}}
		manifest, err := Build(input)
		if err != nil {
			t.Fatalf("Build() error = %v", err)
		}
		if len(manifest.Files) != 1 || manifest.Files[0].Bytes != int64(len(contents)) || manifest.Files[0].SHA256 != fmt.Sprintf("%x", digest) {
			t.Fatalf("Files = %+v, want one digest-verified %d-byte snapshot", manifest.Files, len(contents))
		}
	})

	t.Run("mismatched", func(t *testing.T) {
		input := base
		input.Files = []FileSpec{{Path: outputPath, Role: "video", View: "left", Container: "mp4", SHA256: strings.Repeat("ab", sha256.Size)}}
		_, err := Build(input)
		if err == nil || !strings.Contains(err.Error(), "does not match") {
			t.Fatalf("Build() error = %v, want digest mismatch", err)
		}
	})
}

func TestBuildRejectsFileViewOutsideSelection(t *testing.T) {
	outputPath := filepath.Join(t.TempDir(), "lecture.mp4")
	if err := os.WriteFile(outputPath, []byte("media"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		selection string
		fileView  string
	}{
		{selection: "left", fileView: "right"},
		{selection: "left", fileView: "both"},
		{selection: "right", fileView: "left"},
		{selection: "right", fileView: "both"},
	} {
		t.Run(test.selection+" rejects "+test.fileView, func(t *testing.T) {
			_, err := Build(BuildInput{
				Lecture:    Lecture{TTID: 4, InstituteID: 1, SubjectID: 2, SessionID: 3},
				Selection:  Selection{Views: test.selection, Quality: "720"},
				Files:      []FileSpec{{Path: outputPath, Role: "video", View: test.fileView, Container: "mp4"}},
				ProducedAt: time.Now(),
				Producer:   Producer{Name: "impartus", Version: "test"},
			})
			if err == nil || !strings.Contains(err.Error(), "outside selected views") {
				t.Fatalf("Build() error = %v, want view mismatch", err)
			}
		})
	}
}
