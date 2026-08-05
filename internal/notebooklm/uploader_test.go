package notebooklm

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	stdout string
	stderr string
	err    error
	last   []string
	env    []string
	calls  int
}

type runnerFunc func(context.Context, string, []string, []string) (string, string, error)

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

func (f runnerFunc) Run(ctx context.Context, name string, args []string, env []string) (string, string, error) {
	return f(ctx, name, args, env)
}

func (f *fakeRunner) Run(_ context.Context, _ string, args []string, env []string) (string, string, error) {
	f.calls++
	f.last = append([]string{}, args...)
	f.env = append([]string{}, env...)
	return f.stdout, f.stderr, f.err
}

func TestProviderEnvironmentDropsApplicationSecrets(t *testing.T) {
	filtered := filterProviderEnvironment([]string{
		"HOME=/home/example",
		"PATH=/usr/bin",
		"LC_ALL=C.UTF-8",
		"HTTPS_PROXY=http://proxy.example",
		"NOTEBOOKLM_HOME=/srv/notebooklm",
		"NOTEBOOKLM_PROFILE=work",
		"IMPARTUS_USERNAME=student",
		"IMPARTUS_PASSWORD=secret",
		"GITHUB_TOKEN=secret",
		"OPENAI_API_KEY=secret",
		"NOTEBOOKLM_AUTH_JSON=secret",
		"MALFORMED",
	})
	joined := "\n" + strings.Join(filtered, "\n") + "\n"
	for _, want := range []string{
		"\nHOME=/home/example\n",
		"\nPATH=/usr/bin\n",
		"\nLC_ALL=C.UTF-8\n",
		"\nHTTPS_PROXY=http://proxy.example\n",
		"\nNOTEBOOKLM_HOME=/srv/notebooklm\n",
		"\nNOTEBOOKLM_PROFILE=work\n",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("safe provider environment variable %q was dropped: %v", want, filtered)
		}
	}
	for _, forbidden := range []string{
		"IMPARTUS_USERNAME",
		"IMPARTUS_PASSWORD",
		"GITHUB_TOKEN",
		"OPENAI_API_KEY",
		"NOTEBOOKLM_AUTH_JSON",
	} {
		if strings.Contains(joined, forbidden+"=") {
			t.Fatalf("secret environment variable %q was forwarded: %v", forbidden, filtered)
		}
	}
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

func TestBuildUploadArgsNLMUsesConfiguredWaitTimeout(t *testing.T) {
	args, err := BuildUploadArgs(Config{
		Provider: ProviderNLM, NotebookID: "nb1", UploadTimeout: 45 * time.Minute,
	}, UploadRequest{
		FilePath: "/tmp/a.mp3", Title: "LEC 001",
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, arg := range args {
		if arg == "--wait-timeout" {
			if i+1 >= len(args) {
				t.Fatalf("nlm wait timeout value missing: args=%v", args)
			}
			if args[i+1] != "2700" {
				t.Fatalf("nlm wait timeout = %q, want 2700; args=%v", args[i+1], args)
			}
			return
		}
	}
	t.Fatalf("configured upload timeout missing from nlm args: %v", args)
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
	t.Setenv("IMPARTUS_PASSWORD", "must-not-cross-provider-boundary")
	t.Setenv("OPENAI_API_KEY", "must-not-cross-provider-boundary")
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
	childEnv := "\n" + strings.Join(runner.env, "\n") + "\n"
	for _, forbidden := range []string{"IMPARTUS_PASSWORD=", "OPENAI_API_KEY="} {
		if strings.Contains(childEnv, "\n"+forbidden) {
			t.Fatalf("provider subprocess received %s", forbidden)
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

func TestUploadDoesNotListBeforeCrossingProviderBoundary(t *testing.T) {
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
		{stdout: `{"source_id":"created","title":"` + title + `"}`},
	}}
	u := NewWithRunner(Config{CLIPath: "notebooklm"}, runner)
	result, err := u.Upload(context.Background(), UploadRequest{
		NotebookID: "routed", FilePath: file, Title: title, IdempotencyKey: "impartus:1:2:10",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceID != "created" || result.Outcome != UploadCreated || len(runner.calls) != 1 {
		t.Fatalf("unexpected add outcome: result=%+v calls=%v", result, runner.calls)
	}
	if joined := strings.Join(runner.calls[0], " "); !strings.Contains(joined, "source add") ||
		strings.Contains(joined, "source list") {
		t.Fatalf("upload command crossed an unexpected list path: %v", runner.calls[0])
	}
}

func TestUploadUsesStableIdempotencyTokenInProviderFilename(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "lecture.mp3")
	wantBody := []byte("audio")
	if err := os.WriteFile(original, wantBody, 0o600); err != nil {
		t.Fatal(err)
	}
	var providerPath string
	runner := runnerFunc(func(_ context.Context, _ string, args []string, _ []string) (string, string, error) {
		for i, arg := range args {
			if arg == "--json" && i+1 < len(args) {
				providerPath = args[i+1]
				break
			}
		}
		if providerPath == "" {
			t.Fatalf("provider upload path missing from args: %v", args)
		}
		if got := filepath.Base(providerPath); !strings.Contains(got, "[impartus-1c3e3ccb7a54c965]") {
			t.Fatalf("provider filename %q lacks stable idempotency token", got)
		}
		body, err := os.ReadFile(providerPath)
		if err != nil {
			t.Fatalf("read provider upload file: %v", err)
		}
		if string(body) != string(wantBody) {
			t.Fatalf("provider upload file body = %q, want %q", body, wantBody)
		}
		return `{"source_id":"created"}`, "", nil
	})
	u := NewWithRunner(Config{CLIPath: "notebooklm"}, runner)

	_, err := u.Upload(context.Background(), UploadRequest{
		NotebookID:     "routed",
		FilePath:       original,
		Title:          "[impartus:1:2:10] LEC 001 Intro",
		IdempotencyKey: "impartus:1:2:10",
	})
	if err != nil {
		t.Fatal(err)
	}
	if providerPath == original {
		t.Fatalf("provider received original filename without durable token")
	}
	if _, err := os.Stat(providerPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary provider upload file was not removed: %v", err)
	}
	if _, err := os.Stat(original); err != nil {
		t.Fatalf("original audio file was disturbed: %v", err)
	}
}

func TestUploadStopsStableFilenamePreparationWhenContextCanceled(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "lecture.mp3")
	if err := os.WriteFile(original, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{stdout: `{"source_id":"unexpected"}`}
	u := NewWithRunner(Config{CLIPath: "notebooklm"}, runner)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := u.Upload(ctx, UploadRequest{
		NotebookID:     "routed",
		FilePath:       original,
		Title:          "[impartus:1:2:10] LEC 001 Intro",
		IdempotencyKey: "impartus:1:2:10",
	})
	if !errors.Is(err, context.Canceled) || result.Outcome != UploadRejected {
		t.Fatalf("canceled preparation result=%+v err=%v", result, err)
	}
	if runner.calls != 0 {
		t.Fatalf("canceled preparation crossed provider boundary: calls=%d", runner.calls)
	}
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(original) {
		t.Fatalf("canceled preparation left temporary files: %v", entries)
	}
}

func TestContextReaderStopsBetweenCopyChunks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reads := 0
	source := readerFunc(func(p []byte) (int, error) {
		reads++
		if reads > 1 {
			t.Fatal("source was read again after cancellation")
		}
		p[0] = 'x'
		cancel()
		return 1, nil
	})

	written, err := io.Copy(io.Discard, contextReader{ctx: ctx, reader: source})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("copy error = %v, want context cancellation", err)
	}
	if written != 1 || reads != 1 {
		t.Fatalf("copy continued after cancellation: written=%d reads=%d", written, reads)
	}
}

func TestReconcileUploadMatchesPostIndexProviderFilename(t *testing.T) {
	runner := &fakeRunner{stdout: `{"sources":[{"id":"existing","title":"[impartus-1c3e3ccb7a54c965] LEC 001 Intro.mp3"}]}`}
	u := NewWithRunner(Config{CLIPath: "notebooklm"}, runner)

	result, found, err := u.ReconcileUpload(
		context.Background(),
		"routed",
		"[impartus:1:2:10] LEC 001 Intro",
		"impartus:1:2:10",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !found || result.SourceID != "existing" || result.Outcome != UploadFound {
		t.Fatalf("post-index filename was not reconciled: result=%+v found=%v", result, found)
	}
}

func TestFindSourceByTitleDoesNotMatchPartialIdempotencyToken(t *testing.T) {
	sources := []UploadResult{{SourceID: "wrong", Title: "[impartus:1:2:100] LEC 001 Intro"}}
	if result, ok := findSourceByTitle(
		sources,
		"[impartus:1:2:10] LEC 001 Intro",
		"impartus:1:2:10",
	); ok {
		t.Fatalf("partial idempotency token matched wrong source: %+v", result)
	}
}

func TestUploadReturnsTypedAmbiguousOutcomeWithoutSelfReconciliation(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "lec.mp3")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &seqRunner{responses: []struct {
		stdout string
		err    error
	}{
		{err: context.DeadlineExceeded},
	}}
	u := NewWithRunner(Config{CLIPath: "notebooklm"}, runner)
	result, err := u.Upload(context.Background(), UploadRequest{
		NotebookID: "routed", FilePath: file,
		Title: "[impartus:1:2:10] LEC 001 Intro", IdempotencyKey: "impartus:1:2:10",
	})
	var typed *Error
	if result.Outcome != UploadAmbiguous || !errors.As(err, &typed) ||
		typed.Kind != ErrAmbiguous || typed.Retryable() || len(runner.calls) != 1 {
		t.Fatalf("ambiguous add was not returned directly: result=%+v err=%v calls=%v", result, err, runner.calls)
	}
}

func TestNotebookLMpyRateLimitRemainsAmbiguousAcrossRemoteUploadPhases(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "lec.mp3")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &seqRunner{responses: []struct {
		stdout string
		err    error
	}{
		{err: errors.New("HTTP 429 rate limit")},
	}}
	u := NewWithRunner(Config{CLIPath: "notebooklm"}, runner)
	result, err := u.Upload(context.Background(), UploadRequest{
		NotebookID: "routed", FilePath: file,
		Title: "LEC 001 Intro [impartus:1:2:10]", IdempotencyKey: "impartus:1:2:10",
	})
	if result.Outcome != UploadAmbiguous || !IsAmbiguous(err) {
		t.Fatalf("notebooklm-py rate limit must remain fail-closed: result=%+v err=%v", result, err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("rate-limit ambiguity triggered hidden reconciliation: %v", runner.calls)
	}
}

func TestNLMRateLimitRemainsAmbiguousBecauseWaitMayFollowCreation(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "lec.mp3")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &seqRunner{responses: []struct {
		stdout string
		err    error
	}{
		{err: errors.New("HTTP 429 rate limit")},
	}}
	u := NewWithRunner(Config{Provider: ProviderNLM, CLIPath: "nlm"}, runner)
	result, err := u.Upload(context.Background(), UploadRequest{
		NotebookID: "routed", FilePath: file,
		Title: "LEC 001 Intro [impartus:1:2:10]", IdempotencyKey: "impartus:1:2:10",
	})
	if result.Outcome != UploadAmbiguous || !IsAmbiguous(err) {
		t.Fatalf("nlm rate limit during --wait must remain ambiguous: result=%+v err=%v", result, err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("nlm rate limit triggered hidden reconciliation: %v", runner.calls)
	}
}

func TestReconcileUploadDoesNotAddWhenSourceIsStillMissing(t *testing.T) {
	runner := &seqRunner{responses: []struct {
		stdout string
		err    error
	}{
		{stdout: `[]`},
	}}
	u := NewWithRunner(Config{CLIPath: "notebooklm"}, runner)
	result, found, err := u.ReconcileUpload(
		context.Background(),
		"routed",
		"[impartus:1:2:10] LEC 001 Intro",
		"impartus:1:2:10",
	)
	if err != nil {
		t.Fatal(err)
	}
	if found || result.SourceID != "" {
		t.Fatalf("missing source reported as reconciled: result=%+v found=%v", result, found)
	}
	if len(runner.calls) != 1 || !strings.Contains(strings.Join(runner.calls[0], " "), "source list") {
		t.Fatalf("reconciliation issued an unexpected provider command: %v", runner.calls)
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
