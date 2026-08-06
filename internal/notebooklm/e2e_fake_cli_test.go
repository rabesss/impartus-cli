package notebooklm_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/notebooklm"
)

func TestUploadToNotebookAgainstFakeCLIBinary(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "argv.log")
	bin := filepath.Join(dir, "notebooklm")
	if runtime.GOOS == "windows" {
		bin += ".bat"
	}

	logPathQuoted := "'" + strings.ReplaceAll(logPath, "'", "'\"'\"'") + "'"
	script := fmt.Sprintf(
		`#!/bin/sh
echo "$@" >> %s
case "$*" in
  *"source list"*)
    echo '{"sources":[{"id":"src-e2e","title":"[impartus:e2e] LEC 001 E2E","status":"processing","status_id":1}]}'
    ;;
  *"source wait"*)
    echo '{"source_id":"src-e2e","status":"ready","status_code":2}'
    ;;
  *)
    echo '{"source":{"source_id":"src-e2e","title":"from-fake"}}'
    ;;
esac
`,
		logPathQuoted,
	)
	if runtime.GOOS == "windows" {
		logPathQuoted = `"` + strings.ReplaceAll(logPath, `"`, `""`) + `"`
		script = fmt.Sprintf(
			"@echo off\r\necho %%*>>%s\r\necho %%*| findstr /C:\"source list\" >nul\r\nif not errorlevel 1 (echo {\"sources\":[{\"id\":\"src-e2e\",\"title\":\"[impartus:e2e] LEC 001 E2E\",\"status\":\"processing\",\"status_id\":1}]}& exit /b 0)\r\necho %%*| findstr /C:\"source wait\" >nul\r\nif not errorlevel 1 (echo {\"source_id\":\"src-e2e\",\"status\":\"ready\",\"status_code\":2}& exit /b 0)\r\necho {\"source\":{\"source_id\":\"src-e2e\",\"title\":\"from-fake\"}}\r\n",
			logPathQuoted,
		)
	}
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	audio := filepath.Join(dir, "lec.mp3")
	if err := os.WriteFile(audio, []byte("fake-mp3"), 0o600); err != nil {
		t.Fatal(err)
	}

	u := notebooklm.New(notebooklm.Config{NotebookID: "nb-e2e", CLIPath: bin})
	result, err := u.UploadToNotebook(
		context.Background(), "nb-e2e", audio,
		"[impartus:e2e] LEC 001 E2E", "impartus:e2e",
	)
	if err != nil {
		t.Fatalf("UploadToNotebook: %v", err)
	}
	if result.SourceID != "src-e2e" {
		t.Fatalf("source id = %q", result.SourceID)
	}

	argv, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	line := string(argv)
	for _, want := range []string{"source add", "--notebook nb-e2e", "--type file", "--json", "[impartus:e2e] LEC 001 E2E"} {
		if !strings.Contains(line, want) {
			t.Fatalf("argv log missing %q:\n%s", want, line)
		}
	}
	if strings.Contains(line, "source list") || strings.Contains(line, "source wait") {
		t.Fatalf("normal successful add unexpectedly waited for READY:\n%s", line)
	}

	reconciled, err := u.ReconcileUpload(
		context.Background(), "nb-e2e",
		"[impartus:e2e] LEC 001 E2E", "impartus:e2e",
	)
	if err != nil || reconciled.Outcome != notebooklm.UploadFound || reconciled.SourceID != "src-e2e" {
		t.Fatalf("ReconcileUpload: result=%+v err=%v", reconciled, err)
	}
	argv, err = os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	line = string(argv)
	for _, want := range []string{"source list", "source wait src-e2e", "--timeout 1800", "--interval 1"} {
		if !strings.Contains(line, want) {
			t.Fatalf("reconciliation argv log missing %q:\n%s", want, line)
		}
	}
}
