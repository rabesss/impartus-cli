package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/artifact"
	"github.com/rabesss/impartus-cli/internal/library"
)

func TestExecuteJSONLibraryListAndVerify(t *testing.T) {
	restoreCLIState(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	mediaPath := filepath.Join(t.TempDir(), "lecture.mp4")
	if err := os.WriteFile(mediaPath, []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := artifact.Build(artifact.BuildInput{
		Lecture:    artifact.Lecture{TTID: 4, InstituteID: 1, SubjectID: 2, SessionID: 3, Topic: "Library CLI"},
		Selection:  artifact.Selection{Views: "left", Quality: "720"},
		Files:      []artifact.FileSpec{{Path: mediaPath, Role: "video", View: "left", Container: "mp4"}},
		ProducedAt: time.Date(2026, time.August, 9, 11, 0, 0, 0, time.UTC),
		Producer:   artifact.Producer{Name: "impartus", Version: "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	store, err := library.Open(context.Background(), library.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if recordErr := store.RecordManifest(context.Background(), manifest); recordErr != nil {
		t.Fatal(recordErr)
	}
	if closeErr := store.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}

	os.Args = []string{"impartus", "library", "list", "--json"}
	output, err := captureStdout(t, func() error { return Execute("dev", "") })
	if err != nil {
		t.Fatalf("Execute(library list) error = %v", err)
	}
	var listed struct {
		Success bool                     `json:"success"`
		Data    []library.ArtifactRecord `json:"data"`
		Meta    jsonMeta                 `json:"meta"`
	}
	if decodeErr := json.Unmarshal([]byte(output), &listed); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if !listed.Success || listed.Meta.Command != "library.list" || len(listed.Data) != 1 || listed.Data[0].Manifest.ArtifactID != manifest.ArtifactID {
		t.Fatalf("library list envelope = %+v", listed)
	}

	os.Args = []string{"impartus", "library", "verify", "--hash", manifest.ArtifactID, "--json"}
	output, err = captureStdout(t, func() error { return Execute("dev", "") })
	if err != nil {
		t.Fatalf("Execute(library verify) error = %v", err)
	}
	var verified struct {
		Success bool                   `json:"success"`
		Data    []library.Verification `json:"data"`
		Meta    jsonMeta               `json:"meta"`
	}
	if unmarshalErr := json.Unmarshal([]byte(output), &verified); unmarshalErr != nil {
		t.Fatal(unmarshalErr)
	}
	if !verified.Success || verified.Meta.Command != "library.verify" || len(verified.Data) != 1 || !verified.Data[0].OK || verified.Data[0].Files[0].SHA256 == "" {
		t.Fatalf("library verify envelope = %+v", verified)
	}

	os.Args = []string{"impartus", "library", "verify", manifest.ArtifactID, "--hash", "--json"}
	output, err = captureStdout(t, func() error { return Execute("dev", "") })
	if err != nil {
		t.Fatalf("Execute(library verify with trailing flag) error = %v", err)
	}
	verified = struct {
		Success bool                   `json:"success"`
		Data    []library.Verification `json:"data"`
		Meta    jsonMeta               `json:"meta"`
	}{}
	if err := json.Unmarshal([]byte(output), &verified); err != nil {
		t.Fatal(err)
	}
	if !verified.Success || len(verified.Data) != 1 || !verified.Data[0].OK || verified.Data[0].Files[0].SHA256 == "" {
		t.Fatalf("library verify trailing-flag envelope = %+v", verified)
	}
}
