package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/artifact"
	"github.com/rabesss/impartus-cli/internal/library"
)

func TestPrintLibraryVerificationEmptyWritesExplicitMessage(t *testing.T) {
	stdout, stderr, err := captureOutputStreams(t, func() error { return printLibraryVerification(nil) })
	if err != nil {
		t.Fatalf("printLibraryVerification(nil) error = %v", err)
	}
	if stdout != "Library is empty; nothing to verify.\n" || stderr != "" {
		t.Fatalf("printLibraryVerification(nil) stdout/stderr = %q/%q", stdout, stderr)
	}
}

func TestUnknownLibraryCommandRejectedBeforeStoreOpen(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)

	command, _, err := executeLibrary(context.Background(), []string{"vrfy"})
	if err == nil || !strings.Contains(err.Error(), "unknown library command: vrfy") {
		t.Fatalf("executeLibrary(vrfy) command/error = %q/%v", command, err)
	}
	if _, statErr := os.Stat(filepath.Join(stateHome, "impartus")); !os.IsNotExist(statErr) {
		t.Fatalf("unknown library command created state or returned unexpected stat error: %v", statErr)
	}
}

func TestExecuteEmptyLibraryVerificationHumanAndJSONContracts(t *testing.T) {
	restoreCLIState(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	for _, args := range [][]string{{"library", "verify"}, {"library", "verify", "--hash"}} {
		os.Args = append([]string{"impartus"}, args...)
		stdout, stderr, err := captureOutputStreams(t, func() error { return Execute("dev", "") })
		if err != nil {
			t.Fatalf("Execute(%v) error = %v", args, err)
		}
		if stdout != "Library is empty; nothing to verify.\n" || stderr != "" {
			t.Fatalf("Execute(%v) stdout/stderr = %q/%q", args, stdout, stderr)
		}
	}

	for _, args := range [][]string{{"library", "verify", "--json"}, {"library", "verify", "--hash", "--json"}} {
		os.Args = append([]string{"impartus"}, args...)
		stdout, stderr, err := captureOutputStreams(t, func() error { return Execute("dev", "") })
		if err != nil {
			t.Fatalf("Execute(%v) error = %v", args, err)
		}
		if stderr != "" || strings.Count(stdout, "\n") != 1 {
			t.Fatalf("Execute(%v) stdout/stderr = %q/%q, want one JSON line and empty stderr", args, stdout, stderr)
		}
		var envelope struct {
			Success bool                   `json:"success"`
			Data    []library.Verification `json:"data"`
			Error   *jsonErr               `json:"error"`
			Meta    jsonMeta               `json:"meta"`
		}
		if decodeErr := json.Unmarshal([]byte(stdout), &envelope); decodeErr != nil {
			t.Fatalf("decode Execute(%v): %v; stdout=%q", args, decodeErr, stdout)
		}
		if !envelope.Success || envelope.Data == nil || len(envelope.Data) != 0 || envelope.Error != nil || envelope.Meta.Command != "library.verify" || envelope.Meta.Mode != "json" {
			t.Fatalf("Execute(%v) envelope = %+v", args, envelope)
		}
	}
}

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
