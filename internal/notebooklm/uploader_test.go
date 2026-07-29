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
	calls  int
}

func (f *fakeRunner) Run(_ context.Context, _ string, args []string, _ []string) (string, string, error) {
	f.calls++
	f.last = append([]string{}, args...)
	return f.stdout, f.stderr, f.err
}

func TestBuildUploadArgsNotebookLMpy(t *testing.T) {
	args, err := BuildUploadArgs(Config{NotebookID: "nb1", AuthProfile: "work"}, UploadRequest{
		FilePath: "/tmp/a.mp3", Title: "LEC 001",
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"--profile work", "source add", "--notebook nb1", "--type file", "--json", "--title LEC 001", "/tmp/a.mp3"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %v", want, args)
		}
	}
}

func TestBuildUploadArgsNLM(t *testing.T) {
	args, err := BuildUploadArgs(Config{Provider: ProviderNLM, NotebookID: "nb1"}, UploadRequest{
		FilePath: "/tmp/a.mp3", Title: "LEC 001",
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"source add", "nb1", "--file /tmp/a.mp3", "--wait", "--json", "--title LEC 001"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %v", want, args)
		}
	}
}

func TestBuildAuthCheckArgsMatchesProviders(t *testing.T) {
	py := strings.Join(BuildAuthCheckArgs(Config{
		Provider: ProviderNotebookLMpy, AuthProfile: "work",
	}), " ")
	if py != "--profile work auth check --test --json" {
		t.Fatalf("notebooklm-py auth args = %q", py)
	}
	nlm := strings.Join(BuildAuthCheckArgs(Config{
		Provider: ProviderNLM, AuthProfile: "work",
	}), " ")
	if nlm != "login --check --profile work" {
		t.Fatalf("nlm auth args = %q", nlm)
	}
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

func TestUploadHonorsRequestNotebookID(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "lec.mp3")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{stdout: `{"source_id":"abc"}`}
	u := NewWithRunner(Config{NotebookID: "default", CLIPath: "notebooklm"}, runner)
	result, err := u.Upload(context.Background(), UploadRequest{
		NotebookID: "per-request", FilePath: file, Title: "Lecture",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.NotebookID != "per-request" || !strings.Contains(strings.Join(runner.last, " "), "--notebook per-request") {
		t.Fatalf("request notebook was discarded: result=%+v args=%v", result, runner.last)
	}
}

func TestUploadReusesSourceWithMatchingIdempotentTitle(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "lec.mp3")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	title := "LEC 001 Intro [impartus:1:2:10]"
	runner := &seqRunner{responses: []struct {
		stdout string
		err    error
	}{
		{stdout: `[{"id":"existing","title":"` + title + `"}]`},
	}}
	u := NewWithRunner(Config{CLIPath: "notebooklm"}, runner)
	result, err := u.Upload(context.Background(), UploadRequest{
		NotebookID: "routed", FilePath: file, Title: title, IdempotencyKey: "impartus:1:2:10",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceID != "existing" || len(runner.calls) != 1 {
		t.Fatalf("existing source was not reused: result=%+v calls=%v", result, runner.calls)
	}
	if joined := strings.Join(runner.calls[0], " "); !strings.Contains(joined, "--notebook routed") ||
		!strings.Contains(joined, "source list") {
		t.Fatalf("wrong reconciliation target: %v", runner.calls[0])
	}
}

func TestUploadReconcilesSourceAfterAmbiguousTimeout(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "lec.mp3")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	title := "LEC 001 Intro [impartus:1:2:10]"
	runner := &seqRunner{responses: []struct {
		stdout string
		err    error
	}{
		{stdout: `[]`},
		{err: context.DeadlineExceeded},
		{stdout: `[{"source_id":"created","title":"` + title + `"}]`},
	}}
	u := NewWithRunner(Config{CLIPath: "notebooklm"}, runner)
	result, err := u.Upload(context.Background(), UploadRequest{
		NotebookID: "routed", FilePath: file, Title: title, IdempotencyKey: "impartus:1:2:10",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceID != "created" || len(runner.calls) != 3 {
		t.Fatalf("ambiguous upload was not reconciled: result=%+v calls=%v", result, runner.calls)
	}
}

func TestUploadDefersAmbiguousWriteWhenReconciliationFindsNothing(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "lec.mp3")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &seqRunner{responses: []struct {
		stdout string
		err    error
	}{
		{stdout: `[]`},
		{err: context.DeadlineExceeded},
		{stdout: `[]`},
	}}
	u := NewWithRunner(Config{CLIPath: "notebooklm"}, runner)
	_, err := u.Upload(context.Background(), UploadRequest{
		NotebookID: "routed", FilePath: file,
		Title: "LEC 001 Intro [impartus:1:2:10]", IdempotencyKey: "impartus:1:2:10",
	})
	var typed *Error
	if !errors.As(err, &typed) || typed.Kind != ErrAmbiguous || typed.Retryable() {
		t.Fatalf("ambiguous add must defer instead of retrying immediately: %v", err)
	}
}

func TestUploadChecksCapOnRoutedNotebook(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "lec.mp3")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &seqRunner{responses: []struct {
		stdout string
		err    error
	}{
		{stdout: `[{"id":"1","title":"a"},{"id":"2","title":"b"}]`},
	}}
	u := NewWithRunner(Config{CLIPath: "notebooklm", MaxSourcesPerNotebook: 2}, runner)
	_, err := u.Upload(context.Background(), UploadRequest{
		NotebookID: "routed", FilePath: file,
		Title: "LEC 001 Intro [impartus:1:2:10]", IdempotencyKey: "impartus:1:2:10",
	})
	if err == nil || !strings.Contains(err.Error(), "cap") || len(runner.calls) != 1 {
		t.Fatalf("routed notebook cap was not enforced: err=%v calls=%v", err, runner.calls)
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

func TestDoctorAcceptsSuccessfulStatusVocabulary(t *testing.T) {
	for _, status := range []string{"OK", "authenticated", "valid", "success"} {
		t.Run(status, func(t *testing.T) {
			u := NewWithRunner(Config{CLIPath: os.Args[0]}, &fakeRunner{
				stdout: `{"status":"` + status + `"}`,
			})
			if err := u.Doctor(context.Background()); err != nil {
				t.Fatalf("Doctor rejected %q: %v", status, err)
			}
		})
	}
}

func TestDoctorAcceptsNLMAuthCheckOutput(t *testing.T) {
	u := NewWithRunner(Config{Provider: ProviderNLM, CLIPath: os.Args[0]}, &fakeRunner{
		stdout: "✓ Authentication valid!\n  Profile: work\n",
	})
	if err := u.Doctor(context.Background()); err != nil {
		t.Fatalf("Doctor rejected nlm auth output: %v", err)
	}
}

func TestDoctorFailsWhenSourceCapCannotBeChecked(t *testing.T) {
	seq := &seqRunner{responses: []struct {
		stdout string
		err    error
	}{
		{stdout: `{"status":"ok"}`},
		{err: errors.New("list unavailable")},
	}}
	u := NewWithRunner(Config{
		NotebookID: "nb1", CLIPath: os.Args[0], MaxSourcesPerNotebook: 300,
	}, seq)
	if err := u.Doctor(context.Background()); err == nil || !strings.Contains(err.Error(), "notebook sources") {
		t.Fatalf("expected source-count check failure, got %v", err)
	}
}

func TestDoctorEnforcesSourceCap(t *testing.T) {
	seq := &seqRunner{responses: []struct {
		stdout string
		err    error
	}{
		{stdout: `{"status":"ok"}`},
		{stdout: `{"sources":[1,2,3]}`},
	}}
	u := NewWithRunner(Config{
		NotebookID: "nb1", CLIPath: os.Args[0], MaxSourcesPerNotebook: 3,
	}, seq)
	if err := u.Doctor(context.Background()); err == nil || !strings.Contains(err.Error(), "cap") {
		t.Fatalf("expected source cap error, got %v", err)
	}
}

type seqRunner struct {
	responses []struct {
		stdout string
		err    error
	}
	i     int
	calls [][]string
}

func (s *seqRunner) Run(_ context.Context, _ string, args []string, _ []string) (string, string, error) {
	s.calls = append(s.calls, append([]string(nil), args...))
	if s.i >= len(s.responses) {
		return "", "", errors.New("unexpected call")
	}
	resp := s.responses[s.i]
	s.i++
	return resp.stdout, "", resp.err
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

func TestClassifyErrorUsesTypedTimeoutAndAuthSignals(t *testing.T) {
	timeoutErr := ClassifyError(context.DeadlineExceeded, "", "")
	var typed *Error
	if !errors.As(timeoutErr, &typed) || typed.Kind != ErrTransient || !typed.Retryable() {
		t.Fatalf("deadline was not transient: %v", timeoutErr)
	}
	authErr := ClassifyError(errors.New("exit 1"), "", "HTTP 401 unauthorized")
	if !errors.As(authErr, &typed) || typed.Kind != ErrAuth || typed.Retryable() {
		t.Fatalf("401 was not classified as auth: %v", authErr)
	}
}

func TestDoctorRejectsEmptyGarbageAndNegativeAuthOutput(t *testing.T) {
	for _, output := range []string{"", "not json", "Not authenticated", `{"status":""}`} {
		t.Run(output, func(t *testing.T) {
			u := NewWithRunner(Config{CLIPath: os.Args[0]}, &fakeRunner{stdout: output})
			if err := u.Doctor(context.Background()); err == nil {
				t.Fatalf("Doctor accepted unusable auth output %q", output)
			}
		})
	}
}

func TestDoctorChecksEveryUniqueRoutedNotebook(t *testing.T) {
	runner := &seqRunner{responses: []struct {
		stdout string
		err    error
	}{
		{stdout: `{"status":"ok"}`},
		{stdout: `[]`},
		{stdout: `[]`},
	}}
	u := NewWithRunner(Config{CLIPath: os.Args[0]}, runner)
	if err := u.DoctorNotebooks(context.Background(), []string{"nb-one", "nb-two", "nb-one"}); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 3 ||
		!strings.Contains(strings.Join(runner.calls[1], " "), "--notebook nb-one") ||
		!strings.Contains(strings.Join(runner.calls[2], " "), "--notebook nb-two") {
		t.Fatalf("routed notebooks were not checked exactly once: %v", runner.calls)
	}
}

func TestClassifyErrorScrubsStreamsAndDoesNotMatchGenerate(t *testing.T) {
	err := ClassifyError(
		errors.New("boom"),
		"",
		"failed to generate source at https://example.com/upload?token=secret",
	)
	var typed *Error
	if !errors.As(err, &typed) {
		t.Fatalf("expected typed error")
	}
	if typed.Kind != ErrPermanent {
		t.Fatalf("generate was misclassified as rate limit: %v", typed.Kind)
	}
	if strings.Contains(typed.Message, "secret") {
		t.Fatalf("secret leaked in classified error: %q", typed.Message)
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

func TestParseSourceCount(t *testing.T) {
	n, err := parseSourceCount(`[{"id":1},{"id":2}]`)
	if err != nil || n != 2 {
		t.Fatalf("array count = %d err=%v", n, err)
	}
	n, err = parseSourceCount(`{"sources":[1,2,3]}`)
	if err != nil || n != 3 {
		t.Fatalf("object count = %d err=%v", n, err)
	}
}
