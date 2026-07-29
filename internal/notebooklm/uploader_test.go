package notebooklm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRunner struct {
	stdout string
	stderr string
	err    error
	last   []string
}

func (f *fakeRunner) Run(_ context.Context, _ string, args []string, _ []string) (string, string, error) {
	f.last = append([]string{}, args...)
	return f.stdout, f.stderr, f.err
}

func TestUploadFileBuildsExpectedArgs(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "lec.mp3")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{stdout: `{"source_id":"abc","title":"LEC 001 Intro"}`}
	u := NewWithRunner(Config{NotebookID: "nb1", CLIPath: "notebooklm", AuthProfile: "work"}, runner)

	result, err := u.UploadFile(context.Background(), file, "LEC 001 Intro")
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if result.SourceID != "abc" {
		t.Fatalf("source id = %q", result.SourceID)
	}
	joined := strings.Join(runner.last, " ")
	for _, want := range []string{"--profile work", "source add", "--notebook nb1", "--type file", "--json", "--title LEC 001 Intro", file} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args missing %q: %v", want, runner.last)
		}
	}
}

func TestDoctorRequiresOKStatus(t *testing.T) {
	u := NewWithRunner(Config{NotebookID: "nb1", CLIPath: os.Args[0]}, &fakeRunner{
		stdout: `{"status":"error"}`,
	})
	if err := u.Doctor(context.Background()); err == nil {
		t.Fatalf("expected auth failure")
	}
}

func TestClassifyErrorKinds(t *testing.T) {
	cases := []struct {
		detail string
		kind   ErrorKind
		retry  bool
	}{
		{"Authentication expired", ErrAuth, false},
		{"HTTP 429 rate limit", ErrRateLimit, true},
		{"connection reset by peer", ErrTransient, true},
		{"invalid notebook", ErrPermanent, false},
	}
	for _, tc := range cases {
		err := ClassifyError(errors.New("boom"), "", tc.detail)
		var typed *Error
		if !errors.As(err, &typed) {
			t.Fatalf("expected typed error for %q", tc.detail)
		}
		if typed.Kind != tc.kind || typed.Retryable() != tc.retry {
			t.Fatalf("%q => kind=%v retry=%v", tc.detail, typed.Kind, typed.Retryable())
		}
	}
}

func TestUploadFileRequiresNotebookAndFile(t *testing.T) {
	u := NewWithRunner(Config{}, &fakeRunner{})
	if _, err := u.UploadFile(context.Background(), "", "t"); err == nil {
		t.Fatalf("expected notebook required")
	}
	u.cfg.NotebookID = "nb"
	if _, err := u.UploadFile(context.Background(), "/no/such/file.mp3", "t"); err == nil {
		t.Fatalf("expected missing file error")
	}
}
