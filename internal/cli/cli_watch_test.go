package cli

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/notebooklm"
)

func TestParseWatchFlags(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr string
		check   func(*testing.T, watchFlags)
	}{
		{
			name: "happy path",
			args: []string{"-s", "1", "-S", "2", "--once", "--upload", "--notebook", "nb1", "--interval", "1m"},
			check: func(t *testing.T, f watchFlags) {
				t.Helper()
				if f.subject != 1 || f.session != 2 || !f.once || !f.upload || f.notebookID != "nb1" || f.interval != "1m" {
					t.Fatalf("unexpected flags: %+v", f)
				}
			},
		},
		{
			name:    "upload conflict",
			args:    []string{"-s", "1", "-S", "2", "--upload", "--no-upload"},
			wantErr: "cannot combine",
		},
		{
			name:    "positional rejected",
			args:    []string{"-s", "1", "-S", "2", "extra"},
			wantErr: "positional",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := parseWatchFlags(tc.args)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseWatchFlags: %v", err)
			}
			tc.check(t, f)
		})
	}
}

func TestApplyWatchFlagsForcesEnabledAndUpload(t *testing.T) {
	cfg := &config.Config{}
	cfg.ApplyDefaults()
	f := watchFlags{subject: 3, session: 4, upload: true, notebookID: "nb", output: "/tmp/out"}
	got, err := applyWatchFlags(cfg, f)
	if err != nil {
		t.Fatalf("applyWatchFlags: %v", err)
	}
	if !got.Watch.Enabled || got.Watch.SubjectID != 3 || got.Watch.SessionID != 4 {
		t.Fatalf("watch course not applied: %+v", got.Watch)
	}
	if !got.Watch.Upload || got.Watch.NotebookLM.NotebookID != "nb" || got.DownloadLocation != "/tmp/out" {
		t.Fatalf("upload/output not applied: watch=%+v dl=%s", got.Watch, got.DownloadLocation)
	}
	targets := got.ResolvedTargets()
	if len(targets) != 1 || targets[0].NotebookID != "nb" {
		t.Fatalf("expected synthesized target, got %+v", targets)
	}

	got.Watch.Upload = true
	f.noUpload = true
	f.upload = false
	got, err = applyWatchFlags(got, f)
	if err != nil {
		t.Fatal(err)
	}
	if got.Watch.Upload {
		t.Fatalf("expected --no-upload to clear upload")
	}
}

func TestApplyWatchFlagsRejectsPartialCourseOverride(t *testing.T) {
	cfg := &config.Config{
		Watch: config.WatchConfig{
			Targets: []config.WatchTarget{{SubjectID: 1, SessionID: 2}},
		},
	}
	cfg.ApplyDefaults()
	if _, err := applyWatchFlags(cfg, watchFlags{subject: 9}); err == nil ||
		!strings.Contains(err.Error(), "provided together") {
		t.Fatalf("expected partial override error, got %v", err)
	}
}

func TestApplyWatchFlagsRejectsOutputTraversal(t *testing.T) {
	cfg := &config.Config{}
	cfg.ApplyDefaults()
	_, err := applyWatchFlags(cfg, watchFlags{
		subject: 1, session: 2, output: "../escape",
	})
	if err == nil || !strings.Contains(err.Error(), "must not escape") {
		t.Fatalf("expected output traversal error, got %v", err)
	}
}

func TestDryRunAndJSONRunOnce(t *testing.T) {
	if !watchRunsOnce(watchFlags{dryRun: true}, false) {
		t.Fatalf("dry-run must not enter the daemon loop")
	}
	if !watchRunsOnce(watchFlags{}, true) {
		t.Fatalf("JSON mode must run one cycle")
	}
	if watchRunsOnce(watchFlags{}, false) {
		t.Fatalf("plain watch without --once should remain long-running")
	}
}

func TestHelpMentionsWatch(t *testing.T) {
	payload := helpPayload()
	found := false
	for _, cmd := range payload.Commands {
		if cmd.Name == "watch" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("help payload missing watch command: %+v", payload.Commands)
	}
}

func TestRunWatchCheckJSONReturnsDiagnostics(t *testing.T) {
	binDir := t.TempDir()
	ffmpeg := filepath.Join(binDir, "ffmpeg")
	body := "#!/bin/sh\nexit 0\n"
	if runtime.GOOS == "windows" {
		ffmpeg += ".bat"
		body = "@echo off\r\nexit /b 0\r\n"
	}
	if err := os.WriteFile(ffmpeg, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	cfg := &config.Config{
		Watch: config.WatchConfig{
			Targets: []config.WatchTarget{{SubjectID: 1, SessionID: 2}},
		},
	}
	cfg.ApplyDefaults()
	result, err := runWatchCheck(
		context.Background(),
		cfg,
		false,
		notebooklm.New(notebooklm.Config{}),
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Checks["ffmpeg"] != "ok" || result.Checks["targets"] != "1" ||
		result.Checks["upload"] != "false" {
		t.Fatalf("JSON diagnostics missing: %+v", result.Checks)
	}
}
