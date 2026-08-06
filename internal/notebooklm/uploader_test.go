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

type runnerResponse struct {
	stdout string
	stderr string
	err    error
}

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

func TestBuildUploadArgsMatchesProviders(t *testing.T) {
	req := UploadRequest{NotebookID: "nb1", FilePath: "/tmp/a.mp3", Title: "LEC 001"}
	tests := []struct {
		name  string
		cfg   Config
		wants []string
	}{
		{
			name: "notebooklm-py",
			cfg:  Config{NotebookID: "default", AuthProfile: "work"},
			wants: []string{
				"--profile work", "source add", "--notebook nb1", "--type file",
				"--json /tmp/a.mp3", "--title LEC 001",
			},
		},
		{
			name: "nlm",
			cfg:  Config{Provider: ProviderNLM, NotebookID: "nb1", UploadTimeout: 45 * time.Minute},
			wants: []string{
				"source add nb1", "--file /tmp/a.mp3", "--wait", "--wait-timeout 2700",
				"--json", "--title LEC 001",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args, err := BuildUploadArgs(tc.cfg, req)
			if err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(args, " ")
			for _, want := range tc.wants {
				if !strings.Contains(joined, want) {
					t.Fatalf("missing %q in %v", want, args)
				}
			}
		})
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

	_, err := u.UploadToNotebook(
		context.Background(), "routed", original,
		"[impartus:1:2:10] LEC 001 Intro", "impartus:1:2:10",
	)
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

	result, err := u.UploadToNotebook(
		ctx, "routed", original,
		"[impartus:1:2:10] LEC 001 Intro", "impartus:1:2:10",
	)
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
	runner := &fakeRunner{stdout: `{"sources":[{"id":"existing","title":"[impartus-1c3e3ccb7a54c965] LEC 001 Intro.mp3","status":"READY","status_id":2}]}`}
	u := NewWithRunner(Config{CLIPath: "notebooklm"}, runner)

	result, err := u.ReconcileUpload(
		context.Background(),
		"routed",
		"[impartus:1:2:10] LEC 001 Intro",
		"impartus:1:2:10",
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceID != "existing" || result.Outcome != UploadFound || result.Status != "ready" || result.StatusID != 2 {
		t.Fatalf("post-index filename was not reconciled: result=%+v", result)
	}
}

func TestReconcileUploadWaitsForNotebookLMpyProcessingSource(t *testing.T) {
	runner := &seqRunner{responses: []runnerResponse{
		{stdout: `{"sources":[{"id":"existing","title":"[impartus:1:2:10] LEC 001 Intro","status":"processing","status_id":1}]}`},
		{stdout: `{"source_id":"existing","status_code":2}`},
	}}
	u := NewWithRunner(Config{
		CLIPath: "notebooklm", AuthProfile: "work", UploadTimeout: 1500 * time.Millisecond,
	}, runner)

	result, err := u.ReconcileUpload(
		context.Background(), "routed",
		"[impartus:1:2:10] LEC 001 Intro", "impartus:1:2:10",
	)
	if err != nil || result.Outcome != UploadFound || result.SourceID != "existing" || result.Status != "ready" {
		t.Fatalf("processing source was not reconciled through wait: result=%+v err=%v", result, err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("provider calls=%d, want list then wait: %v", len(runner.calls), runner.calls)
	}
	if got := strings.Join(runner.calls[0], " "); got != "--profile work source list --notebook routed --json" {
		t.Fatalf("source list args = %q", got)
	}
	if got := strings.Join(runner.calls[1], " "); got != "--profile work source wait existing --notebook routed --timeout 2 --interval 1 --json" {
		t.Fatalf("source wait args = %q", got)
	}
}

func TestReconcileUploadKeepsNonReadySourcesAmbiguous(t *testing.T) {
	tests := []struct {
		name      string
		provider  Provider
		responses []runnerResponse
		wantCalls int
	}{
		{
			name: "notebooklm-py wait timeout",
			responses: []runnerResponse{
				{stdout: `{"sources":[{"id":"existing","title":"[impartus:1:2:10] LEC 001 Intro","status":"processing","status_id":1}]}`},
				{stdout: `{"source_id":"existing","status":"timeout","last_status_code":3,"timeout_seconds":2,"error":"Source existing not ready after 2.0s"}`, err: errors.New("exit status 2")},
			},
			wantCalls: 2,
		},
		{
			name: "notebooklm-py non-ready wait",
			responses: []runnerResponse{
				{stdout: `{"sources":[{"id":"existing","title":"[impartus:1:2:10] LEC 001 Intro","status":"preparing","status_id":5}]}`},
				{stdout: `{"status":"processing"}`},
			},
			wantCalls: 2,
		},
		{
			name: "notebooklm-py malformed wait",
			responses: []runnerResponse{
				{stdout: `{"sources":[{"id":"existing","title":"[impartus:1:2:10] LEC 001 Intro","status":"processing","status_id":1}]}`},
				{stdout: `{}`},
			},
			wantCalls: 2,
		},
		{
			name:     "nlm processing does not invent wait",
			provider: ProviderNLM,
			responses: []runnerResponse{
				{stdout: `{"sources":[{"id":"existing","title":"[impartus:1:2:10] LEC 001 Intro","status":1}]}`},
			},
			wantCalls: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := &seqRunner{responses: tc.responses}
			u := NewWithRunner(Config{Provider: tc.provider, CLIPath: "provider"}, runner)
			result, err := u.ReconcileUpload(
				context.Background(), "routed",
				"[impartus:1:2:10] LEC 001 Intro", "impartus:1:2:10",
			)
			var typed *Error
			if result.Outcome != UploadAmbiguous || !errors.As(err, &typed) || typed.Kind != ErrAmbiguous {
				t.Fatalf("result=%+v err=%v, want ambiguous", result, err)
			}
			if len(runner.calls) != tc.wantCalls {
				t.Fatalf("provider calls=%d, want %d: %v", len(runner.calls), tc.wantCalls, runner.calls)
			}
			for _, call := range runner.calls {
				if strings.Contains(strings.Join(call, " "), "source add") {
					t.Fatalf("reconciliation issued another add: %v", runner.calls)
				}
			}
		})
	}
}

func TestReconcileUploadPreservesListAuthFailure(t *testing.T) {
	runner := &seqRunner{responses: []runnerResponse{{
		stdout: `{"error":true,"code":"AUTH_ERROR","message":"sign in again"}`,
		err:    errors.New("exit status 1"),
	}}}
	u := NewWithRunner(Config{CLIPath: "notebooklm"}, runner)
	result, err := u.ReconcileUpload(
		context.Background(), "routed",
		"[impartus:1:2:10] LEC 001 Intro", "impartus:1:2:10",
	)
	if result.Outcome != UploadAmbiguous || !IsAuth(err) || len(runner.calls) != 1 {
		t.Fatalf("result=%+v err=%v calls=%d, want one ambiguous auth failure", result, err, len(runner.calls))
	}
}

func TestReconcileUploadAcceptsReadyStatusAliases(t *testing.T) {
	tests := []struct {
		name   string
		status string
	}{
		{name: "numeric status", status: `"status":2`},
		{name: "snake status id", status: `"status_id":2`},
		{name: "snake status code", status: `"status_code":2`},
		{name: "camel status code", status: `"statusCode":2`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := &seqRunner{responses: []runnerResponse{{
				stdout: `{"sources":[{"id":"existing","title":"[impartus:1:2:10] LEC 001 Intro",` + tc.status + `}]}`,
			}}}
			u := NewWithRunner(Config{Provider: ProviderNLM, CLIPath: "nlm"}, runner)
			result, err := u.ReconcileUpload(
				context.Background(), "routed",
				"[impartus:1:2:10] LEC 001 Intro", "impartus:1:2:10",
			)
			if err != nil || result.Outcome != UploadFound || result.StatusID != 2 || len(runner.calls) != 1 {
				t.Fatalf("READY source alias was not reconciled: result=%+v err=%v calls=%v", result, err, runner.calls)
			}
		})
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

func TestReconcileUploadDoesNotAddWhenSourceIsStillMissing(t *testing.T) {
	runner := &seqRunner{responses: []runnerResponse{
		{stdout: `[]`},
	}}
	u := NewWithRunner(Config{CLIPath: "notebooklm"}, runner)
	result, err := u.ReconcileUpload(
		context.Background(),
		"routed",
		"[impartus:1:2:10] LEC 001 Intro",
		"impartus:1:2:10",
	)
	var typed *Error
	if result.Outcome != UploadAmbiguous || !errors.As(err, &typed) || typed.Kind != ErrAmbiguous || result.SourceID != "" {
		t.Fatalf("missing source result=%+v err=%v, want ambiguous", result, err)
	}
	if len(runner.calls) != 1 || !strings.Contains(strings.Join(runner.calls[0], " "), "source list") {
		t.Fatalf("reconciliation issued an unexpected provider command: %v", runner.calls)
	}
}

func TestDoctorAuthOutput(t *testing.T) {
	tests := []struct {
		name, output string
		provider     Provider
		wantErr      bool
	}{
		{name: "OK", output: `{"status":"OK"}`},
		{name: "authenticated", output: `{"status":"authenticated"}`},
		{name: "valid", output: `{"status":"valid"}`},
		{name: "success", output: `{"status":"success"}`},
		{name: "nlm text", provider: ProviderNLM, output: "✓ Authentication valid!\n  Profile: work\n"},
		{name: "error status", output: `{"status":"error"}`, wantErr: true},
		{name: "empty", wantErr: true},
		{name: "garbage", output: "not json", wantErr: true},
		{name: "negative text", output: "Not authenticated", wantErr: true},
		{name: "empty status", output: `{"status":""}`, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := NewWithRunner(Config{Provider: tc.provider, CLIPath: os.Args[0]}, &fakeRunner{stdout: tc.output})
			err := u.DoctorNotebooks(context.Background(), nil)
			if (err != nil) != tc.wantErr {
				t.Fatalf("DoctorNotebooks() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestDoctorSourceCapFailures(t *testing.T) {
	tests := []struct {
		name     string
		response runnerResponse
		cap      int
		want     string
	}{
		{name: "list unavailable", response: runnerResponse{err: errors.New("list unavailable")}, cap: 300, want: "notebook sources"},
		{name: "cap reached", response: runnerResponse{stdout: `{"sources":[1,2,3]}`}, cap: 3, want: "cap"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := &seqRunner{responses: []runnerResponse{{stdout: `{"status":"ok"}`}, tc.response}}
			u := NewWithRunner(Config{CLIPath: os.Args[0], MaxSourcesPerNotebook: tc.cap}, runner)
			err := u.DoctorNotebooks(context.Background(), []string{"nb1"})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("DoctorNotebooks() error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

type seqRunner struct {
	responses []runnerResponse
	i         int
	calls     [][]string
}

func (s *seqRunner) Run(_ context.Context, _ string, args []string, _ []string) (string, string, error) {
	s.calls = append(s.calls, append([]string(nil), args...))
	if s.i >= len(s.responses) {
		return "", "", errors.New("unexpected call")
	}
	resp := s.responses[s.i]
	s.i++
	return resp.stdout, resp.stderr, resp.err
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

func TestClassifyErrorPrefersNotebookLMpyEnvelope(t *testing.T) {
	tests := []struct {
		code  string
		kind  ErrorKind
		retry bool
	}{
		{code: "AUTH_ERROR", kind: ErrAuth},
		{code: "RATE_LIMITED", kind: ErrRateLimit, retry: true},
		{code: "NETWORK_ERROR", kind: ErrTransient, retry: true},
		{code: "TIMEOUT", kind: ErrTransient, retry: true},
		{code: "CANCELLED", kind: ErrCancelled}, //nolint:misspell // Exact upstream error code.
		{code: "VALIDATION_ERROR", kind: ErrPermanent},
		{code: "CONFIG_ERROR", kind: ErrPermanent},
		{code: "NOTEBOOK_LIMIT", kind: ErrPermanent},
		{code: "NOT_FOUND", kind: ErrPermanent},
		{code: "NOTEBOOKLM_ERROR", kind: ErrPermanent},
		{code: "UNEXPECTED_ERROR", kind: ErrPermanent},
		{code: "FUTURE_ERROR", kind: ErrPermanent},
	}
	for _, tc := range tests {
		t.Run(tc.code, func(t *testing.T) {
			stdout := `{"error":true,"code":"` + tc.code + `","message":"structured message","retry_after":4.5}`
			err := ClassifyError(errors.New("exit status 1"), stdout, "HTTP 401 stderr noise")
			var typed *Error
			if !errors.As(err, &typed) {
				t.Fatalf("expected typed error: %v", err)
			}
			if typed.Kind != tc.kind || typed.Retryable() != tc.retry {
				t.Fatalf("kind=%v retry=%v, want kind=%v retry=%v", typed.Kind, typed.Retryable(), tc.kind, tc.retry)
			}
			if typed.Message != "structured message" {
				t.Fatalf("stderr won over structured stdout: %q", typed.Message)
			}
		})
	}
}

func TestClassifyGenericEnvelopePreservesRPCAuthSignal(t *testing.T) {
	err := ClassifyError(errors.New("exit status 1"),
		`{"error":true,"code":"NOTEBOOKLM_ERROR","message":"Error: The server rejected this request (unauthenticated)."}`, "")
	if !IsAuth(err) {
		t.Fatalf("generic RPC auth response was not classified as auth: %v", err)
	}
}

func TestNLMClassificationKeepsLegacyTextFallback(t *testing.T) {
	stdout := `{"error":true,"code":"AUTH_ERROR","message":"structured auth"}`
	err := classifyProviderError(ProviderNLM, errors.New("exit status 1"), stdout, "HTTP 429 rate limit")
	if !hasErrorKind(err, ErrRateLimit) || hasErrorKind(err, ErrAuth) {
		t.Fatalf("nlm unexpectedly consumed notebooklm-py envelope: %v", err)
	}
}

func TestIsAuthFindsNestedCauseUnderAmbiguousOutcome(t *testing.T) {
	err := &Error{
		Kind:    ErrAmbiguous,
		Message: "remote mutation is ambiguous",
		Err:     &Error{Kind: ErrAuth, Message: "re-authenticate"},
	}
	if !IsAuth(err) {
		t.Fatalf("nested auth failure was hidden by ambiguous outcome: %v", err)
	}
}

func TestIdempotentPostSubprocessFailuresAreAlwaysAmbiguous(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "lec.mp3")
	if err := os.WriteFile(file, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		provider Provider
		stdout   string
		stderr   string
		err      error
		auth     bool
	}{
		{name: "auth", stdout: `{"error":true,"code":"AUTH_ERROR","message":"login expired"}`, err: errors.New("exit 1"), auth: true},
		{name: "rate limit", stdout: `{"error":true,"code":"RATE_LIMITED","message":"slow down","retry_after":2}`, err: errors.New("exit 1")},
		{name: "network", stdout: `{"error":true,"code":"NETWORK_ERROR","message":"connection failed"}`, err: errors.New("exit 1")},
		{name: "validation", stdout: `{"error":true,"code":"VALIDATION_ERROR","message":"bad input"}`, err: errors.New("exit 1")},
		{name: "provider generic", stdout: `{"error":true,"code":"NOTEBOOKLM_ERROR","message":"rpc failed"}`, err: errors.New("exit 1")},
		{name: "context timeout", err: context.DeadlineExceeded},
		{name: "malformed success", stdout: "not-json"},
		{name: "nlm rate limit", provider: ProviderNLM, stderr: "HTTP 429 rate limit", err: errors.New("exit 1")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeRunner{stdout: tc.stdout, stderr: tc.stderr, err: tc.err}
			u := NewWithRunner(Config{Provider: tc.provider, CLIPath: "provider"}, runner)
			result, err := u.UploadToNotebook(
				context.Background(), "nb", file,
				"[impartus:1:2:10] LEC 001 Intro", "impartus:1:2:10",
			)
			var typed *Error
			if result.Outcome != UploadAmbiguous || !errors.As(err, &typed) || typed.Kind != ErrAmbiguous {
				t.Fatalf("result=%+v err=%v, want ambiguous", result, err)
			}
			if runner.calls != 1 {
				t.Fatalf("provider calls=%d, want exactly one", runner.calls)
			}
			if IsAuth(err) != tc.auth {
				t.Fatalf("IsAuth=%v, want %v: %v", IsAuth(err), tc.auth, err)
			}
		})
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

func TestDoctorChecksEveryUniqueRoutedNotebook(t *testing.T) {
	runner := &seqRunner{responses: []runnerResponse{
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

func TestUploadToNotebookRequiresNotebookAndFile(t *testing.T) {
	tests := []struct {
		name, notebookID, filePath, title, key string
	}{
		{name: "missing notebook", title: "t"},
		{name: "missing file", notebookID: "nb", filePath: "/no/such/file.mp3", title: "t"},
		{name: "invalid idempotency title", notebookID: "nb", filePath: filepath.Join(t.TempDir(), "audio.mp3"), title: "t", key: "impartus:1:2:10"},
	}
	if err := os.WriteFile(tests[2].filePath, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeRunner{}
			u := NewWithRunner(Config{}, runner)
			result, err := u.UploadToNotebook(context.Background(), tc.notebookID, tc.filePath, tc.title, tc.key)
			if err == nil || result.Outcome != UploadRejected {
				t.Fatalf("result=%+v err=%v, want local rejection", result, err)
			}
			if runner.calls != 0 {
				t.Fatalf("local rejection crossed provider boundary: calls=%d", runner.calls)
			}
		})
	}
}

func TestParseSourceInventoryCount(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		count   int
		wantErr bool
	}{
		{name: "array length", payload: `[{"id":1},{"id":2}]`, count: 2},
		{name: "object array length", payload: `{"sources":[1,2,3]}`, count: 3},
		{name: "declared total exceeds page", payload: `{"sources":[1,2],"count":305}`, count: 305},
		{name: "string declared total", payload: `{"items":[1],"count":"305"}`, count: 305},
		{name: "declared total cannot undercount items", payload: `{"data":[1,2,3],"count":1}`, count: 3},
		{name: "non-numeric count only", payload: `{"count":"many"}`, wantErr: true},
		{name: "non-numeric count with items", payload: `{"sources":[1],"count":false}`, wantErr: true},
		{name: "negative count", payload: `{"sources":[1],"count":-1}`, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inventory, err := parseSourceInventory(tc.payload, "")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("malformed count parsed as valid inventory: %+v", inventory)
				}
				return
			}
			if err != nil || inventory.Count != tc.count {
				t.Fatalf("count = %d err=%v, want %d", inventory.Count, err, tc.count)
			}
		})
	}
}
