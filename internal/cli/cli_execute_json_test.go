package cli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestExecuteNoArgsDefaultsToInteractive(t *testing.T) {
	restoreCLIState(t)
	called := false
	runInteractiveFn = func() error {
		called = true
		return nil
	}
	os.Args = []string{"impartus"}

	if err := Execute("dev", ""); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !called {
		t.Fatal("expected interactive mode to be used when no args are provided")
	}
}

func TestExecuteJSONNoSubcommandReturnsCapabilitiesEnvelope(t *testing.T) {
	restoreCLIState(t)
	runInteractiveFn = func() error {
		t.Fatal("interactive mode should not run in --json mode")
		return nil
	}
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
	if payload.Data.DefaultMode != "interactive" || payload.Data.Name == "" || len(payload.Data.Commands) == 0 {
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
