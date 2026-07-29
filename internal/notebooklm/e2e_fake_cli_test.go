package notebooklm_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/notebooklm"
)

func TestUploadFileAgainstFakeCLIBinary(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "argv.log")
	bin := filepath.Join(dir, "notebooklm")
	if runtime.GOOS == "windows" {
		bin += ".bat"
	}

	script := "#!/bin/sh\n" +
		"echo \"$@\" >> \"$NOTEBOOKLM_ARGV_LOG\"\n" +
		"echo '{\"source_id\":\"src-e2e\",\"title\":\"from-fake\"}'\n"
	if runtime.GOOS == "windows" {
		script = "@echo off\r\n" +
			"echo %*>>\"%NOTEBOOKLM_ARGV_LOG%\"\r\n" +
			"echo {\"source_id\":\"src-e2e\",\"title\":\"from-fake\"}\r\n"
	}
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	audio := filepath.Join(dir, "lec.mp3")
	if err := os.WriteFile(audio, []byte("fake-mp3"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("NOTEBOOKLM_ARGV_LOG", logPath)
	u := notebooklm.New(notebooklm.Config{NotebookID: "nb-e2e", CLIPath: bin})
	result, err := u.UploadFile(context.Background(), audio, "LEC 001 E2E")
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if result.SourceID != "src-e2e" {
		t.Fatalf("source id = %q", result.SourceID)
	}

	argv, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	line := string(argv)
	for _, want := range []string{"source add", "--notebook nb-e2e", "--type file", "--json", "LEC 001 E2E", audio} {
		if !strings.Contains(line, want) {
			t.Fatalf("argv log missing %q:\n%s", want, line)
		}
	}
}
