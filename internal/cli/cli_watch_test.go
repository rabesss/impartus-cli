package cli

import (
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/config"
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
	if !got.Watch.Upload || got.NotebookLM.NotebookID != "nb" || got.DownloadLocation != "/tmp/out" {
		t.Fatalf("upload/output not applied: watch=%+v nlm=%+v dl=%s", got.Watch, got.NotebookLM, got.DownloadLocation)
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
