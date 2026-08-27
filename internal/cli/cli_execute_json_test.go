package cli

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestExecuteNoArgsLaunchesTUIOnlyOnInteractiveTerminal(t *testing.T) {
	restoreCLIState(t)
	called := false
	runTUIFn = func() error {
		called = true
		return nil
	}
	isInteractiveTerminalFn = func() bool { return true }
	os.Args = []string{"impartus"}

	if err := Execute("dev", ""); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !called {
		t.Fatal("expected TUI mode to be used when no args are provided on a terminal")
	}
}

func TestExecuteNoArgsOnPipePrintsHelpAndReturnsExitTwo(t *testing.T) {
	restoreCLIState(t)
	runTUIFn = func() error {
		t.Fatal("TUI must not claim a non-interactive terminal")
		return nil
	}
	isInteractiveTerminalFn = func() bool { return false }
	os.Args = []string{"impartus"}

	stdout, stderr, err := captureOutputStreams(t, func() error { return Execute("dev", "") })
	if stdout != "" || !strings.Contains(stderr, "Usage:") {
		t.Fatalf("non-TTY output stdout=%q stderr=%q", stdout, stderr)
	}
	var exitErr interface{ ExitCode() int }
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 2 {
		t.Fatalf("non-TTY error = %v, want exit code 2", err)
	}
}

func TestExplicitTUIOnPipeReturnsExitTwoWithoutLaunching(t *testing.T) {
	restoreCLIState(t)
	runTUIFn = func() error {
		t.Fatal("explicit TUI must not claim a non-interactive terminal")
		return nil
	}
	isInteractiveTerminalFn = func() bool { return false }
	os.Args = []string{"impartus", "tui"}

	err := Execute("dev", "")
	var exitErr interface{ ExitCode() int }
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 2 {
		t.Fatalf("explicit non-TTY error = %v, want exit code 2", err)
	}
}

func TestRemovedClassicCommandIsNotAdvertisedOrDispatched(t *testing.T) {
	restoreCLIState(t)
	os.Args = []string{"impartus", "classic"}

	stdout, _, err := captureOutputStreams(t, func() error { return Execute("dev", "") })
	if err == nil || !strings.Contains(err.Error(), "unknown command: classic") {
		t.Fatalf("classic error = %v, want removed-command error", err)
	}
	if strings.Contains(stdout, "impartus classic") {
		t.Fatalf("help still advertises classic: %q", stdout)
	}
}

func TestExitCodePreservesUsageErrors(t *testing.T) {
	if got := ExitCode(nil); got != 0 {
		t.Fatalf("ExitCode(nil) = %d, want 0", got)
	}
	if got := ExitCode(errors.New("ordinary")); got != 1 {
		t.Fatalf("ExitCode(ordinary) = %d, want 1", got)
	}
	if got := ExitCode(&exitCodeError{code: 2, err: errors.New("usage")}); got != 2 {
		t.Fatalf("ExitCode(usage) = %d, want 2", got)
	}
}

func TestExecuteJSONNoSubcommandReturnsCapabilitiesEnvelope(t *testing.T) {
	restoreCLIState(t)
	os.Args = []string{"impartus", "--json"}

	output, err := captureStdout(t, func() error { return Execute("1.2.3", "2025-01-01") })
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Name        string `json:"name"`
			DefaultMode string `json:"defaultMode"`
			Flags       []string
			Commands    []struct {
				Name  string `json:"name"`
				Usage string `json:"usage"`
			} `json:"commands"`
		} `json:"data"`
		Error any `json:"error"`
		Meta  struct {
			Command string `json:"command"`
			Mode    string `json:"mode"`
		} `json:"meta"`
	}
	if unmarshalErr := json.Unmarshal([]byte(output), &payload); unmarshalErr != nil {
		t.Fatalf("failed to decode payload: %v; output=%q", unmarshalErr, output)
	}
	if !payload.Success || payload.Error != nil {
		t.Fatalf("expected successful envelope without error, got %+v", payload)
	}
	if payload.Meta.Command != "help" || payload.Meta.Mode != "json" {
		t.Fatalf("unexpected meta: %+v", payload.Meta)
	}
	if payload.Data.DefaultMode != "tui" || payload.Data.Name == "" || len(payload.Data.Commands) == 0 {
		t.Fatalf("unexpected capability payload: %+v", payload.Data)
	}
}

func TestExecuteJSONEnvelopeShapeForVersionAndErrors(t *testing.T) {
	restoreCLIState(t)
	cases := []struct {
		name       string
		args       []string
		expectErr  bool
		metaCmd    string
		errorMatch string
	}{
		{name: "json before command", args: []string{"impartus", "--json", "version"}, metaCmd: "version"},
		{name: "json after command", args: []string{"impartus", "version", "--json"}, metaCmd: "version"},
		{name: "unknown command", args: []string{"impartus", "unknown", "--json"}, expectErr: true, metaCmd: "unknown", errorMatch: "unknown command"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			os.Args = tc.args
			output, err := captureStdout(t, func() error { return Execute("v1", "d1") })

			var raw string
			if tc.expectErr {
				if err == nil {
					t.Fatal("expected error")
				}
				raw = err.Error()
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				raw = output
			}

			var envelope map[string]json.RawMessage
			if unmarshalErr := json.Unmarshal([]byte(raw), &envelope); unmarshalErr != nil {
				t.Fatalf("invalid envelope json: %v; raw=%q", unmarshalErr, raw)
			}
			for _, key := range []string{"success", "data", "error", "meta"} {
				if _, ok := envelope[key]; !ok {
					t.Fatalf("missing envelope key %q in %v", key, envelope)
				}
			}

			var meta struct {
				Command string `json:"command"`
				Mode    string `json:"mode"`
			}
			if unmarshalErr := json.Unmarshal(envelope["meta"], &meta); unmarshalErr != nil {
				t.Fatalf("failed to parse meta: %v", unmarshalErr)
			}
			if meta.Command != tc.metaCmd || meta.Mode != "json" {
				t.Fatalf("unexpected meta: %+v", meta)
			}

			if tc.expectErr {
				var errPayload struct {
					Message string `json:"message"`
				}
				if unmarshalErr := json.Unmarshal(envelope["error"], &errPayload); unmarshalErr != nil {
					t.Fatalf("failed to parse error payload: %v", unmarshalErr)
				}
				if !strings.Contains(errPayload.Message, tc.errorMatch) {
					t.Fatalf("expected error message to contain %q, got %q", tc.errorMatch, errPayload.Message)
				}
			}
		})
	}
}

func TestExecuteJSONServeReturnsDeterministicMetadata(t *testing.T) {
	restoreCLIState(t)
	os.Args = []string{"impartus", "serve", "--port", "9090", "--json"}

	output, err := captureStdout(t, func() error { return Execute("v1", "d1") })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Status string `json:"status"`
			Port   int    `json:"port"`
			Base   string `json:"baseURL"`
			Health string `json:"health"`
		} `json:"data"`
		Meta struct {
			Command string `json:"command"`
			Mode    string `json:"mode"`
		} `json:"meta"`
	}
	if unmarshalErr := json.Unmarshal([]byte(output), &payload); unmarshalErr != nil {
		t.Fatalf("failed to decode payload: %v; output=%q", unmarshalErr, output)
	}
	if !payload.Success {
		t.Fatalf("expected success payload, got %+v", payload)
	}
	if payload.Meta.Command != "serve" || payload.Meta.Mode != "json" {
		t.Fatalf("unexpected meta: %+v", payload.Meta)
	}
	if payload.Data.Status != "ready" || payload.Data.Port != 9090 {
		t.Fatalf("unexpected serve payload data: %+v", payload.Data)
	}
	if !strings.Contains(payload.Data.Base, "9090") || !strings.Contains(payload.Data.Health, "/health") {
		t.Fatalf("unexpected endpoint metadata: %+v", payload.Data)
	}
}

func TestExecuteJSONValidationAndDownloadEnvelope(t *testing.T) {
	restoreCLIState(t)
	os.Args = []string{"impartus", "download", "--json"}
	_, err := captureStdout(t, func() error { return Execute("v1", "d1") })
	if err == nil {
		t.Fatal("expected json envelope error")
	}

	var payload struct {
		Success bool `json:"success"`
		Data    any  `json:"data"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
		Meta struct {
			Command string `json:"command"`
			Mode    string `json:"mode"`
		} `json:"meta"`
	}
	if unmarshalErr := json.Unmarshal([]byte(err.Error()), &payload); unmarshalErr != nil {
		t.Fatalf("failed to decode error payload: %v; raw=%q", unmarshalErr, err.Error())
	}
	if payload.Success || payload.Data != nil {
		t.Fatalf("expected failed envelope with nil data, got %+v", payload)
	}
	if payload.Meta.Command != "download" || payload.Meta.Mode != "json" {
		t.Fatalf("unexpected meta: %+v", payload.Meta)
	}
	if !strings.Contains(payload.Error.Message, "requires --subject/-s and --session/-S") {
		t.Fatalf("unexpected error message: %+v", payload.Error)
	}
}

func TestExecuteGlobalJSONFlagDistinguishesFlagValueFromSentinel(t *testing.T) {
	t.Run("output consumes double dash before global JSON", func(t *testing.T) {
		restoreCLIState(t)
		os.Args = []string{"impartus", "download", "--output", "--", "--json"}

		stdout, stderr, err := captureOutputStreams(t, func() error { return Execute("v1", "d1") })
		if err == nil {
			t.Fatal("Execute returned nil, want JSON validation error")
		}
		var payload jsonEnvelope
		if decodeErr := json.Unmarshal([]byte(err.Error()), &payload); decodeErr != nil {
			t.Fatalf("decode JSON error: %v; error=%q", decodeErr, err)
		}
		if payload.Success || payload.Error == nil || !strings.Contains(payload.Error.Message, "download requires --subject/-s and --session/-S") {
			t.Fatalf("unexpected JSON validation envelope: %+v", payload)
		}
		if payload.Meta.Command != "download" || payload.Meta.Mode != "json" {
			t.Fatalf("unexpected JSON envelope: %+v", payload)
		}
		if stdout != "" || stderr != "" {
			t.Fatalf("Execute stdout/stderr = %q/%q, want empty", stdout, stderr)
		}
	})

	t.Run("free double dash keeps JSON positional", func(t *testing.T) {
		restoreCLIState(t)
		runDownloadJSONFn = func([]string) (downloadResult, error) {
			t.Fatal("post-sentinel --json should not enable JSON mode")
			return downloadResult{}, nil
		}
		runDownloadFn = func(args []string) error {
			_, err := parseDownloadFlags(args)
			return err
		}
		os.Args = []string{"impartus", "download", "--", "--json"}

		stdout, stderr, err := captureOutputStreams(t, func() error { return Execute("v1", "d1") })
		if err == nil || !strings.Contains(err.Error(), "download does not accept positional arguments") {
			t.Fatalf("Execute error = %v, want positional-argument error", err)
		}
		if json.Valid([]byte(err.Error())) {
			t.Fatalf("Execute returned a JSON envelope for positional --json: %v", err)
		}
		if stdout != "" || stderr != "" {
			t.Fatalf("Execute stdout/stderr = %q/%q, want empty", stdout, stderr)
		}
	})
}

func TestExecuteJSONDownloadUsesStructuredResult(t *testing.T) {
	result := downloadResult{Status: "completed", OutputPaths: []string{"/tmp/out.mp4"}, LectureCount: 1}
	payload := newSuccessEnvelope("download", result)
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded struct {
		Success bool `json:"success"`
		Data    struct {
			Status       string   `json:"status"`
			OutputPaths  []string `json:"outputPaths"`
			LectureCount int      `json:"lectureCount"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !decoded.Success || decoded.Data.Status != "completed" || len(decoded.Data.OutputPaths) != 1 || decoded.Data.LectureCount != 1 {
		t.Fatalf("unexpected decoded payload: %+v", decoded)
	}
}

func TestExecuteJSONDownloadIncludesLibraryWarningInMeta(t *testing.T) {
	restoreCLIState(t)
	runDownloadJSONFn = func([]string) (downloadResult, error) {
		return downloadResult{
			Status:          "completed",
			LectureCount:    1,
			LibraryRecorded: false,
			Warnings:        []string{"local library unavailable"},
		}, nil
	}
	os.Args = []string{"impartus", "download", "--json"}
	output, err := captureStdout(t, func() error { return Execute("dev", "") })
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Success bool `json:"success"`
		Data    struct {
			Status          string `json:"status"`
			LibraryRecorded bool   `json:"libraryRecorded"`
		} `json:"data"`
		Meta jsonMeta `json:"meta"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.Success || envelope.Data.Status != "completed" || envelope.Data.LibraryRecorded || len(envelope.Meta.Warnings) != 1 {
		t.Fatalf("download warning envelope = %+v", envelope)
	}
}

func TestExecuteJSONDownloadRedactsCredentialErrors(t *testing.T) {
	restoreCLIState(t)
	runDownloadJSONFn = func([]string) (downloadResult, error) {
		return downloadResult{}, errors.New("Proxy-Authorization: Custom proof=proxy-secret\nauth: Digest response=auth-secret\nX-Api-Key: api-secret")
	}

	err := executeJSONDownload(nil)
	if err == nil {
		t.Fatal("executeJSONDownload() error = nil")
	}
	for _, secret := range []string{"proxy-secret", "auth-secret", "api-secret"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("JSON download envelope leaked %q: %v", secret, err)
		}
	}
	if !strings.Contains(err.Error(), "REDACTED") {
		t.Fatalf("JSON download envelope omitted redaction marker: %v", err)
	}
}

func TestExecuteJSONPlayRejects(t *testing.T) {
	restoreCLIState(t)
	os.Args = []string{"impartus", "play", "--json"}

	_, err := captureStdout(t, func() error { return Execute("v1", "d1") })
	if err == nil {
		t.Fatal("expected json error for play command")
	}
	raw := err.Error()
	if !strings.Contains(raw, "play command is not supported in JSON mode") {
		t.Fatalf("unexpected error message: %v", err)
	}
	var envelope map[string]json.RawMessage
	if unmarshalErr := json.Unmarshal([]byte(raw), &envelope); unmarshalErr != nil {
		t.Fatalf("expected JSON envelope, got: %s", raw)
	}
}
