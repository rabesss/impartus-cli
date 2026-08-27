package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

func TestExecuteCoursesHelpReturnsBeforeCommandDispatch(t *testing.T) {
	restoreCLIState(t)
	runCoursesFn = func([]string) error {
		return errors.New("courses runner must not be called for help")
	}
	loadResolvedFn = func(string) (*config.Config, error) {
		return nil, errors.New("config must not be loaded for help")
	}
	newLoggedInFn = func(context.Context, *config.Config) (*client.Client, error) {
		return nil, errors.New("login must not run for help")
	}
	os.Args = []string{"impartus", "courses", "--help"}

	stdout, stderr, err := captureOutputStreams(t, func() error { return Execute("v1", "d1") })
	if err != nil {
		t.Fatalf("Execute(courses --help) error = %v", err)
	}
	if stderr != "" {
		t.Fatalf("Execute(courses --help) stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "Usage:\n  impartus courses") {
		t.Fatalf("Execute(courses --help) stdout = %q, want command-specific usage", stdout)
	}
}

func TestExecuteLibraryHelpReturnsBeforeStoreCreation(t *testing.T) {
	restoreCLIState(t)
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	runLibraryFn = func([]string) error {
		return errors.New("library runner must not be called for help")
	}
	os.Args = []string{"impartus", "library", "--help"}

	stdout, stderr, err := captureOutputStreams(t, func() error { return Execute("v1", "d1") })
	if err != nil {
		t.Fatalf("Execute(library --help) error = %v", err)
	}
	if stderr != "" {
		t.Fatalf("Execute(library --help) stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "Usage:\n  impartus library list") {
		t.Fatalf("Execute(library --help) stdout = %q, want library usage", stdout)
	}
	if _, statErr := os.Stat(filepath.Join(stateHome, "impartus")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("library help created state directory or returned unexpected stat error: %v", statErr)
	}
}

func TestExecuteLibraryVerifyHelpIsNestedAndSideEffectFree(t *testing.T) {
	restoreCLIState(t)
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	runLibraryFn = func([]string) error {
		return errors.New("library runner must not be called for nested help")
	}
	os.Args = []string{"impartus", "library", "verify", "artifact-id", "--help"}

	stdout, stderr, err := captureOutputStreams(t, func() error { return Execute("v1", "d1") })
	if err != nil {
		t.Fatalf("Execute(library verify --help) error = %v", err)
	}
	if stderr != "" {
		t.Fatalf("Execute(library verify --help) stderr = %q, want empty", stderr)
	}
	want := "Impartus Video Downloader\nVersion: v1\nBuild Date: d1\n\nVerify recorded library artifacts and files.\n\nUsage:\n  impartus library verify [--hash] [artifact-id]\n"
	if stdout != want {
		t.Fatalf("Execute(library verify --help) stdout = %q, want %q", stdout, want)
	}
	if _, statErr := os.Stat(filepath.Join(stateHome, "impartus")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("nested library help created state directory or returned unexpected stat error: %v", statErr)
	}
}

func TestExecuteLibraryVerifyExplicitHelpIsNestedAndSideEffectFree(t *testing.T) {
	restoreCLIState(t)
	installHelpDispatchSentinels(t)
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)

	os.Args = []string{"impartus", "help", "library", "verify"}
	stdout, stderr, err := captureOutputStreams(t, func() error { return Execute("v1", "d1") })
	if err != nil || stderr != "" {
		t.Fatalf("Execute(help library verify) stdout/stderr/error = %q/%q/%v", stdout, stderr, err)
	}
	if !strings.Contains(stdout, "Usage:\n  impartus library verify [--hash] [artifact-id]") {
		t.Fatalf("Execute(help library verify) stdout = %q, want nested verify usage", stdout)
	}

	os.Args = []string{"impartus", "help", "library", "verify", "--json"}
	stdout, stderr, err = captureOutputStreams(t, func() error { return Execute("v1", "d1") })
	if err != nil || stderr != "" {
		t.Fatalf("Execute(help library verify --json) stdout/stderr/error = %q/%q/%v", stdout, stderr, err)
	}
	var envelope struct {
		Success bool               `json:"success"`
		Data    commandHelpPayload `json:"data"`
		Error   *jsonErr           `json:"error"`
		Meta    jsonMeta           `json:"meta"`
	}
	if decodeErr := json.Unmarshal([]byte(stdout), &envelope); decodeErr != nil {
		t.Fatalf("decode nested help: %v; stdout=%q", decodeErr, stdout)
	}
	if !envelope.Success || envelope.Error != nil || envelope.Meta.Command != "help" || envelope.Data.Command != "library.verify" {
		t.Fatalf("nested help envelope = %+v", envelope)
	}
	if _, statErr := os.Stat(filepath.Join(stateHome, "impartus")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("explicit nested help created state directory or returned unexpected stat error: %v", statErr)
	}
}

func TestExecuteHumanCommandHelpMatrix(t *testing.T) {
	restoreCLIState(t)
	installHelpDispatchSentinels(t)
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)

	tests := []struct {
		name        string
		args        []string
		usage       string
		description string
	}{
		{name: "root long", args: []string{"--help"}, usage: "impartus [command] [flags]"},
		{name: "root short", args: []string{"-h"}, usage: "impartus [command] [flags]"},
		{name: "help command", args: []string{"help"}, usage: "impartus [command] [flags]"},
		{name: "help download", args: []string{"help", "download"}, usage: "impartus download --subject <id> --session <id>"},
		{name: "help command short", args: []string{"help", "-h"}, usage: "impartus [command] [flags]"},
		{name: "help command long", args: []string{"help", "--help"}, usage: "impartus [command] [flags]"},
		{name: "courses", args: []string{"courses", "--help"}, usage: "impartus courses"},
		{name: "courses short", args: []string{"courses", "-h"}, usage: "impartus courses"},
		{name: "courses positional before help", args: []string{"courses", "extra", "--help"}, usage: "impartus courses"},
		{name: "library", args: []string{"library", "--help"}, usage: "impartus library list", description: "Inspect and verify the local lecture library."},
		{name: "library short", args: []string{"library", "-h"}, usage: "impartus library list", description: "Inspect and verify the local lecture library."},
		{name: "library invalid flag before help", args: []string{"library", "--bogus", "--help"}, usage: "impartus library list", description: "Inspect and verify the local lecture library."},
		{name: "library verify", args: []string{"library", "verify", "--help"}, usage: "impartus library verify [--hash] [artifact-id]"},
		{name: "library verify short", args: []string{"library", "verify", "-h"}, usage: "impartus library verify [--hash] [artifact-id]"},
		{name: "library verify flag before help", args: []string{"library", "verify", "--hash", "--help"}, usage: "impartus library verify [--hash] [artifact-id]"},
		{name: "version", args: []string{"version", "--help"}, usage: "impartus version"},
		{name: "version short", args: []string{"version", "-h"}, usage: "impartus version"},
		{name: "long version alias help", args: []string{"--version", "--help"}, usage: "impartus version"},
		{name: "single-dash version alias help", args: []string{"-version", "-h"}, usage: "impartus version"},
		{name: "short version alias help", args: []string{"-v", "-h"}, usage: "impartus version"},
		{name: "lectures", args: []string{"lectures", "--help"}, usage: "impartus lectures --subject <id> --session <id>"},
		{name: "lectures short", args: []string{"lectures", "-h"}, usage: "impartus lectures --subject <id> --session <id>"},
		{name: "download", args: []string{"download", "--help"}, usage: "impartus download --subject <id> --session <id>"},
		{name: "download short", args: []string{"download", "-h"}, usage: "impartus download --subject <id> --session <id>"},
		{name: "download invalid before help", args: []string{"download", "--start", "bad", "--help"}, usage: "impartus download --subject <id> --session <id>"},
		{name: "play", args: []string{"play", "--help"}, usage: "impartus play [--subject <id> --session <id>]"},
		{name: "play short", args: []string{"play", "-h"}, usage: "impartus play [--subject <id> --session <id>]"},
		{name: "doctor", args: []string{"doctor", "--help"}, usage: "impartus doctor"},
		{name: "doctor short", args: []string{"doctor", "-h"}, usage: "impartus doctor"},
		{name: "library list", args: []string{"library", "list", "--help"}, usage: "impartus library list", description: "List recorded library artifacts."},
		{name: "library list short", args: []string{"library", "list", "-h"}, usage: "impartus library list", description: "List recorded library artifacts."},
		{name: "library show", args: []string{"library", "show", "--help"}, usage: "impartus library show <artifact-id>", description: "Show one recorded library artifact."},
		{name: "library show short", args: []string{"library", "show", "-h"}, usage: "impartus library show <artifact-id>", description: "Show one recorded library artifact."},
		{name: "watch", args: []string{"watch", "--help"}, usage: "impartus watch [--subject <id> --session <id>]"},
		{name: "watch short", args: []string{"watch", "-h"}, usage: "impartus watch [--subject <id> --session <id>]"},
		{name: "serve", args: []string{"serve", "--help"}, usage: "impartus serve [--port <port>]"},
		{name: "serve short", args: []string{"serve", "-h"}, usage: "impartus serve [--port <port>]"},
		{name: "serve invalid before help", args: []string{"serve", "--port", "bad", "--help"}, usage: "impartus serve [--port <port>]"},
		{name: "tui", args: []string{"tui", "--help"}, usage: "impartus tui"},
		{name: "tui short", args: []string{"tui", "-h"}, usage: "impartus tui"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var first string
			for attempt := 0; attempt < 2; attempt++ {
				os.Args = append([]string{"impartus"}, test.args...)
				stdout, stderr, err := captureOutputStreams(t, func() error { return Execute("v1", "d1") })
				if err != nil {
					t.Fatalf("Execute(%v) error = %v", test.args, err)
				}
				if stderr != "" {
					t.Fatalf("Execute(%v) stderr = %q, want empty", test.args, stderr)
				}
				if !strings.Contains(stdout, test.usage) {
					t.Fatalf("Execute(%v) stdout = %q, want usage %q", test.args, stdout, test.usage)
				}
				if test.description != "" && !strings.Contains(stdout, test.description) {
					t.Fatalf("Execute(%v) stdout = %q, want description %q", test.args, stdout, test.description)
				}
				if attempt == 0 {
					first = stdout
				} else if stdout != first {
					t.Fatalf("Execute(%v) output changed across invocations:\nfirst=%q\nsecond=%q", test.args, first, stdout)
				}
			}
			if _, statErr := os.Stat(filepath.Join(stateHome, "impartus")); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("Execute(%v) created state directory or returned unexpected stat error: %v", test.args, statErr)
			}
		})
	}
}

func TestExecuteDownloadHelpFormsMatchAndListFlags(t *testing.T) {
	restoreCLIState(t)
	installHelpDispatchSentinels(t)
	outputs := make([]string, 0, 4)
	for _, args := range [][]string{
		{"help", "download"},
		{"help", "download", "--help"},
		{"--help", "download"},
		{"download", "--help"},
	} {
		os.Args = append([]string{"impartus"}, args...)
		stdout, stderr, err := captureOutputStreams(t, func() error { return Execute("v1", "d1") })
		if err != nil || stderr != "" {
			t.Fatalf("Execute(%v) stdout/stderr/error = %q/%q/%v", args, stdout, stderr, err)
		}
		for _, want := range []string{"Flags:", "--subject,-s", "--session,-S", "--start", "--quality", "--json"} {
			if !strings.Contains(stdout, want) {
				t.Fatalf("Execute(%v) output omitted %q: %q", args, want, stdout)
			}
		}
		outputs = append(outputs, stdout)
	}
	for index, output := range outputs[1:] {
		if outputs[0] != output {
			t.Fatalf("download help form %d differs from help download:\nhelp=%q\nother=%q", index+1, outputs[0], output)
		}
	}
}

func TestExecuteJSONHelpDownloadListsFlags(t *testing.T) {
	restoreCLIState(t)
	installHelpDispatchSentinels(t)
	var first string
	for _, args := range [][]string{
		{"help", "download", "--json"},
		{"help", "download", "--help", "--json"},
		{"--help", "download", "--json"},
		{"download", "--help", "--json"},
	} {
		os.Args = append([]string{"impartus"}, args...)
		stdout, stderr, err := captureOutputStreams(t, func() error { return Execute("v1", "d1") })
		if err != nil || stderr != "" {
			t.Fatalf("Execute(%v) stdout/stderr/error = %q/%q/%v", args, stdout, stderr, err)
		}
		var envelope struct {
			Data struct {
				Command string   `json:"command"`
				Flags   []string `json:"flags"`
			} `json:"data"`
		}
		if decodeErr := json.Unmarshal([]byte(stdout), &envelope); decodeErr != nil {
			t.Fatalf("decode command help: %v; stdout=%q", decodeErr, stdout)
		}
		if envelope.Data.Command != "download" || !slices.ContainsFunc(envelope.Data.Flags, func(flag string) bool {
			return strings.HasPrefix(flag, "--subject,-s")
		}) || !slices.ContainsFunc(envelope.Data.Flags, func(flag string) bool {
			return strings.HasPrefix(flag, "--json")
		}) {
			t.Fatalf("JSON download help = %+v, want command and flags", envelope.Data)
		}
		if first == "" {
			first = stdout
		} else if stdout != first {
			t.Fatalf("Execute(%v) JSON differs from help download:\nfirst=%q\ncurrent=%q", args, first, stdout)
		}
	}
}

func TestRootHelpListsJSONAsGlobalFlag(t *testing.T) {
	var output strings.Builder
	if err := showHelpTo(&output, "v1", "d1"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Global Flags:\n  --json") {
		t.Fatalf("root help does not list --json as a global flag: %q", output.String())
	}
}

func installHelpDispatchSentinels(t *testing.T) {
	t.Helper()
	unreachable := func(name string) error { return errors.New(name + " must not run for help") }
	runTUIFn = func() error { return unreachable("TUI") }
	isInteractiveTerminalFn = func() bool {
		t.Error("TTY detection must not run for explicit help")
		return false
	}
	runCoursesFn = func([]string) error { return unreachable("courses") }
	runLecturesFn = func([]string) error { return unreachable("lectures") }
	runDownloadFn = func([]string) error { return unreachable("download") }
	runDownloadJSONFn = func([]string) (downloadResult, error) { return downloadResult{}, unreachable("JSON download") }
	runServeFn = func([]string, string) error { return unreachable("serve") }
	runPlayFn = func([]string) error { return unreachable("play") }
	runDoctorFn = func([]string) error { return unreachable("doctor") }
	runLibraryFn = func([]string) error { return unreachable("library") }
	runWatchFn = func([]string) error { return unreachable("watch") }
	runWatchJSONFn = func([]string) (watchResult, error) { return watchResult{}, unreachable("JSON watch") }
	loadResolvedFn = func(string) (*config.Config, error) { return nil, unreachable("config") }
	newLoggedInFn = func(context.Context, *config.Config) (*client.Client, error) { return nil, unreachable("login") }
	startAPIServerFn = func(context.Context, string, *config.Config) error { return unreachable("server") }
}

func TestExecuteJSONCoursesHelpIsOneDeterministicEnvelopeInEitherOrder(t *testing.T) {
	restoreCLIState(t)
	installHelpDispatchSentinels(t)
	want := "{\"success\":true,\"data\":{\"command\":\"courses\",\"description\":\"List available courses as JSON.\",\"usage\":[\"impartus courses\"]},\"error\":null,\"meta\":{\"command\":\"help\",\"mode\":\"json\"}}\n"

	for _, args := range [][]string{
		{"courses", "--help", "--json"},
		{"courses", "--json", "--help"},
	} {
		os.Args = append([]string{"impartus"}, args...)
		stdout, stderr, err := captureOutputStreams(t, func() error { return Execute("v1", "d1") })
		if err != nil {
			t.Fatalf("Execute(%v) error = %v", args, err)
		}
		if stderr != "" {
			t.Fatalf("Execute(%v) stderr = %q, want empty", args, stderr)
		}
		if stdout != want {
			t.Fatalf("Execute(%v) stdout = %q, want %q", args, stdout, want)
		}
	}
}

func TestExecuteJSONRootHelpKeepsCapabilitiesInEitherOrder(t *testing.T) {
	restoreCLIState(t)
	installHelpDispatchSentinels(t)
	for _, args := range [][]string{
		{"--json", "--help"},
		{"--help", "--json"},
		{"--json", "-h"},
		{"-h", "--json"},
		{"help", "--json"},
		{"help", "--help", "--json"},
		{"help", "--json", "--help"},
	} {
		os.Args = append([]string{"impartus"}, args...)
		stdout, stderr, err := captureOutputStreams(t, func() error { return Execute("v1", "d1") })
		if err != nil {
			t.Fatalf("Execute(%v) error = %v", args, err)
		}
		if stderr != "" || strings.Count(stdout, "\n") != 1 {
			t.Fatalf("Execute(%v) stdout/stderr = %q/%q, want one JSON line and empty stderr", args, stdout, stderr)
		}
		var envelope struct {
			Success bool              `json:"success"`
			Data    capabilityPayload `json:"data"`
			Error   *jsonErr          `json:"error"`
			Meta    jsonMeta          `json:"meta"`
		}
		if decodeErr := json.Unmarshal([]byte(stdout), &envelope); decodeErr != nil {
			t.Fatalf("decode Execute(%v): %v; stdout=%q", args, decodeErr, stdout)
		}
		if !envelope.Success || envelope.Error != nil || envelope.Meta.Command != "help" || envelope.Meta.Mode != "json" || envelope.Data.Name != "impartus" || len(envelope.Data.Commands) == 0 {
			t.Fatalf("Execute(%v) root help envelope = %+v", args, envelope)
		}
	}
}

func TestExecuteJSONCommandHelpMatrix(t *testing.T) {
	restoreCLIState(t)
	installHelpDispatchSentinels(t)
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)

	tests := []struct {
		name        string
		commandArgs []string
		command     string
		description string
		usage       []string
	}{
		{name: "version", commandArgs: []string{"version"}, command: "version", description: "Show version and build date.", usage: []string{"impartus version"}},
		{name: "courses", commandArgs: []string{"courses"}, command: "courses", description: "List available courses as JSON.", usage: []string{"impartus courses"}},
		{name: "lectures", commandArgs: []string{"lectures"}, command: "lectures", description: "List lectures for one subject and session as JSON.", usage: []string{"impartus lectures --subject <id> --session <id>"}},
		{name: "download", commandArgs: []string{"download"}, command: "download", description: "Download lectures and record completed media in the local library.", usage: []string{"impartus download --subject <id> --session <id> [--ttid <id> | --start <n> --end <n>] [flags]"}},
		{name: "play", commandArgs: []string{"play"}, command: "play", description: "Play lectures in mpv.", usage: []string{"impartus play [--subject <id> --session <id>] [flags]"}},
		{name: "doctor", commandArgs: []string{"doctor"}, command: "doctor", description: "Check local dependencies and private paths.", usage: []string{"impartus doctor"}},
		{name: "library", commandArgs: []string{"library"}, command: "library", description: "Inspect and verify the local lecture library.", usage: []string{"impartus library list", "impartus library show <artifact-id>", "impartus library verify [--hash] [artifact-id]"}},
		{name: "library invalid flag before help", commandArgs: []string{"library", "--bogus"}, command: "library", description: "Inspect and verify the local lecture library.", usage: []string{"impartus library list", "impartus library show <artifact-id>", "impartus library verify [--hash] [artifact-id]"}},
		{name: "library list", commandArgs: []string{"library", "list"}, command: "library.list", description: "List recorded library artifacts.", usage: []string{"impartus library list"}},
		{name: "library show", commandArgs: []string{"library", "show"}, command: "library.show", description: "Show one recorded library artifact.", usage: []string{"impartus library show <artifact-id>"}},
		{name: "library verify", commandArgs: []string{"library", "verify"}, command: "library.verify", description: "Verify recorded library artifacts and files.", usage: []string{"impartus library verify [--hash] [artifact-id]"}},
		{name: "library verify flag before help", commandArgs: []string{"library", "verify", "--hash"}, command: "library.verify", description: "Verify recorded library artifacts and files.", usage: []string{"impartus library verify [--hash] [artifact-id]"}},
		{name: "watch", commandArgs: []string{"watch"}, command: "watch", description: "Poll and durably download new lectures.", usage: []string{"impartus watch [--subject <id> --session <id>] [--once] [--dry-run] [flags]"}},
		{name: "serve", commandArgs: []string{"serve"}, command: "serve", description: "Start the HTTP API server.", usage: []string{"impartus serve [--port <port>]"}},
		{name: "tui", commandArgs: []string{"tui"}, command: "tui", description: "Launch the interactive terminal workspace.", usage: []string{"impartus tui"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			orders := [][]string{
				append(append([]string(nil), test.commandArgs...), "--help", "--json"),
				append(append([]string(nil), test.commandArgs...), "--json", "--help"),
			}
			for _, args := range orders {
				os.Args = append([]string{"impartus"}, args...)
				stdout, stderr, err := captureOutputStreams(t, func() error { return Execute("v1", "d1") })
				if err != nil {
					t.Fatalf("Execute(%v) error = %v", args, err)
				}
				if stderr != "" || strings.Count(stdout, "\n") != 1 {
					t.Fatalf("Execute(%v) stdout/stderr = %q/%q, want one JSON line and empty stderr", args, stdout, stderr)
				}
				var envelope struct {
					Success bool                       `json:"success"`
					Data    map[string]json.RawMessage `json:"data"`
					Error   *jsonErr                   `json:"error"`
					Meta    jsonMeta                   `json:"meta"`
				}
				if decodeErr := json.Unmarshal([]byte(stdout), &envelope); decodeErr != nil {
					t.Fatalf("decode Execute(%v): %v; stdout=%q", args, decodeErr, stdout)
				}
				if !envelope.Success || envelope.Error != nil || envelope.Meta.Command != "help" || envelope.Meta.Mode != "json" || len(envelope.Data) < 3 || len(envelope.Data) > 4 {
					t.Fatalf("Execute(%v) envelope = %+v", args, envelope)
				}
				var command, description string
				var usage []string
				if decodeErr := json.Unmarshal(envelope.Data["command"], &command); decodeErr != nil {
					t.Fatal(decodeErr)
				}
				if decodeErr := json.Unmarshal(envelope.Data["description"], &description); decodeErr != nil {
					t.Fatal(decodeErr)
				}
				if decodeErr := json.Unmarshal(envelope.Data["usage"], &usage); decodeErr != nil {
					t.Fatal(decodeErr)
				}
				if command != test.command || description != test.description || !slices.Equal(usage, test.usage) {
					t.Fatalf("Execute(%v) data = command %q description %q usage %v", args, command, description, usage)
				}
			}
		})
	}
	if _, statErr := os.Stat(filepath.Join(stateHome, "impartus")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("JSON help matrix created state directory or returned unexpected stat error: %v", statErr)
	}
}

func TestExecuteNonHelpCompatibilityMatrix(t *testing.T) {
	restoreCLIState(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "version positional", args: []string{"version", "extra"}, want: "version does not accept positional arguments"},
		{name: "courses positional", args: []string{"courses", "extra"}, want: "courses does not accept positional arguments"},
		{name: "lectures positional", args: []string{"lectures", "--subject", "1", "--session", "2", "extra"}, want: "lectures does not accept positional arguments"},
		{name: "download missing selectors", args: []string{"download"}, want: "download requires --subject/-s and --session/-S"},
		{name: "download nonpositive TTID", args: []string{"download", "--subject", "1", "--session", "2", "--ttid", "0"}, want: "download --ttid must be positive"},
		{name: "download conflicting selectors", args: []string{"download", "--subject", "1", "--session", "2", "--ttid", "3", "--start", "1"}, want: "download --ttid cannot be combined with --start/--end"},
		{name: "play positional", args: []string{"play", "extra"}, want: "play does not accept positional arguments"},
		{name: "doctor positional", args: []string{"doctor", "extra"}, want: "doctor does not accept arguments"},
		{name: "library unknown", args: []string{"library", "unknown"}, want: "unknown library command: unknown"},
		{name: "library list arity", args: []string{"library", "list", "extra"}, want: "library list does not accept arguments"},
		{name: "library show arity", args: []string{"library", "show"}, want: "library show requires exactly one artifact ID"},
		{name: "library verify arity", args: []string{"library", "verify", "one", "two"}, want: "library verify accepts at most one artifact ID"},
		{name: "library verify unknown flag", args: []string{"library", "verify", "--unknown"}, want: "flag provided but not defined: -unknown"},
		{name: "watch incomplete target", args: []string{"watch", "--subject", "1"}, want: "--subject/-s and --session/-S must be provided together"},
		{name: "watch positional", args: []string{"watch", "extra"}, want: "watch does not accept positional arguments"},
		{name: "serve invalid port", args: []string{"serve", "--port", "0"}, want: "port must be between 1 and 65535"},
		{name: "serve positional", args: []string{"serve", "extra"}, want: "serve does not accept positional arguments"},
		{name: "tui positional", args: []string{"tui", "extra"}, want: "tui does not accept positional arguments"},
		{name: "courses help after sentinel", args: []string{"courses", "--", "--help"}, want: "courses does not accept positional arguments"},
		{name: "download help after sentinel", args: []string{"download", "--subject", "1", "--session", "2", "--", "--help"}, want: "download does not accept positional arguments"},
		{name: "JSON courses help after sentinel", args: []string{"courses", "--json", "--", "--help"}, want: "courses does not accept positional arguments"},
		{name: "library verify help after sentinel", args: []string{"library", "verify", "--", "--help"}, want: "library artifact not found"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			os.Args = append([]string{"impartus"}, test.args...)
			stdout, stderr, err := captureOutputStreams(t, func() error { return Execute("v1", "d1") })
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Execute(%v) error = %v, want containing %q", test.args, err, test.want)
			}
			if ExitCode(err) != 1 {
				t.Fatalf("Execute(%v) exit = %d, want 1", test.args, ExitCode(err))
			}
			if stdout != "" || stderr != "" {
				t.Fatalf("Execute(%v) stdout/stderr = %q/%q, want empty", test.args, stdout, stderr)
			}
		})
	}
}

func TestCommandHelpDefinitionsMatchAdvertisedSurface(t *testing.T) {
	advertised := make(map[string]struct{})
	for _, command := range helpPayload().Commands {
		if command.Name == "help" {
			continue
		}
		advertised[command.Name] = struct{}{}
		if _, ok := commandHelpByName[command.Name]; !ok {
			t.Errorf("advertised command %q has no command-help definition", command.Name)
		}
	}
	for name := range commandHelpByName {
		if strings.Contains(name, ".") {
			continue
		}
		if _, ok := advertised[name]; !ok {
			t.Errorf("top-level command-help definition %q is not advertised", name)
		}
	}

	var rootHelp strings.Builder
	if err := showHelpTo(&rootHelp, "v1", "d1"); err != nil {
		t.Fatalf("showHelpTo() error = %v", err)
	}
	rootHelpLines := strings.Split(rootHelp.String(), "\n")
	for name := range advertised {
		prefix := "  " + name
		if !slices.ContainsFunc(rootHelpLines, func(line string) bool {
			return line == prefix || strings.HasPrefix(line, prefix+" ")
		}) {
			t.Errorf("root human help does not advertise %q", name)
		}
	}
	for _, nested := range []string{"library.list", "library.show", "library.verify"} {
		if _, ok := commandHelpByName[nested]; !ok {
			t.Errorf("nested advertised command %q has no command-help definition", nested)
		}
	}
}

func TestUnknownCommandHelpDoesNotBecomeSuccessfulHelp(t *testing.T) {
	restoreCLIState(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	os.Args = []string{"impartus", "bogus", "--help"}
	stdout, stderr, err := captureOutputStreams(t, func() error { return Execute("v1", "d1") })
	if err == nil || !strings.Contains(err.Error(), "unknown command: bogus") || ExitCode(err) != 1 {
		t.Fatalf("Execute(bogus --help) error = %v, want unknown-command exit 1", err)
	}
	if !strings.Contains(stdout, "Usage:") || stderr != "" {
		t.Fatalf("Execute(bogus --help) stdout/stderr = %q/%q, want root help and empty stderr", stdout, stderr)
	}

	os.Args = []string{"impartus", "bogus", "--help", "--json"}
	stdout, stderr, err = captureOutputStreams(t, func() error { return Execute("v1", "d1") })
	if err == nil || ExitCode(err) != 1 || stdout != "" || stderr != "" {
		t.Fatalf("Execute(bogus --help --json) stdout/stderr/error = %q/%q/%v", stdout, stderr, err)
	}
	var envelope struct {
		Success bool     `json:"success"`
		Error   jsonErr  `json:"error"`
		Meta    jsonMeta `json:"meta"`
	}
	if decodeErr := json.Unmarshal([]byte(err.Error()), &envelope); decodeErr != nil {
		t.Fatalf("decode unknown-command JSON error: %v; raw=%q", decodeErr, err)
	}
	if envelope.Success || !strings.Contains(envelope.Error.Message, "unknown command: bogus") || envelope.Meta.Command != "bogus" || envelope.Meta.Mode != "json" {
		t.Fatalf("unknown-command JSON envelope = %+v", envelope)
	}

	for _, args := range [][]string{
		{"library", "vrfy", "--help"},
		{"library", "vrfy", "--help", "--json"},
	} {
		os.Args = append([]string{"impartus"}, args...)
		stdout, stderr, err = captureOutputStreams(t, func() error { return Execute("v1", "d1") })
		if err == nil || ExitCode(err) != 1 || !strings.Contains(err.Error(), "unknown library command: vrfy") {
			t.Fatalf("Execute(%v) stdout/stderr/error = %q/%q/%v, want unknown nested command", args, stdout, stderr, err)
		}
		if stdout != "" || stderr != "" {
			t.Fatalf("Execute(%v) stdout/stderr = %q/%q, want empty Execute streams", args, stdout, stderr)
		}
	}
}

func TestUnknownExplicitHelpTargetsRemainErrors(t *testing.T) {
	restoreCLIState(t)
	installHelpDispatchSentinels(t)
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "top level", args: []string{"help", "bogus"}, want: "unknown command: bogus"},
		{name: "nested", args: []string{"help", "library", "vrfy"}, want: "unknown library command: vrfy"},
	}
	for _, test := range tests {
		t.Run(test.name+" human", func(t *testing.T) {
			os.Args = append([]string{"impartus"}, test.args...)
			stdout, stderr, err := captureOutputStreams(t, func() error { return Execute("v1", "d1") })
			if err == nil || ExitCode(err) != 1 || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Execute(%v) stdout/stderr/error = %q/%q/%v, want %q", test.args, stdout, stderr, err, test.want)
			}
			if stdout != "" || stderr != "" {
				t.Fatalf("Execute(%v) stdout/stderr = %q/%q, want empty", test.args, stdout, stderr)
			}
		})

		t.Run(test.name+" JSON", func(t *testing.T) {
			args := append(append([]string{"impartus"}, test.args...), "--json")
			os.Args = args
			stdout, stderr, err := captureOutputStreams(t, func() error { return Execute("v1", "d1") })
			if err == nil || ExitCode(err) != 1 || stdout != "" || stderr != "" {
				t.Fatalf("Execute(%v) stdout/stderr/error = %q/%q/%v", args[1:], stdout, stderr, err)
			}
			var envelope struct {
				Success bool     `json:"success"`
				Error   jsonErr  `json:"error"`
				Meta    jsonMeta `json:"meta"`
			}
			if decodeErr := json.Unmarshal([]byte(err.Error()), &envelope); decodeErr != nil {
				t.Fatalf("decode Execute(%v): %v; raw=%q", args[1:], decodeErr, err)
			}
			if envelope.Success || !strings.Contains(envelope.Error.Message, test.want) || envelope.Meta.Command != "help" || envelope.Meta.Mode != "json" {
				t.Fatalf("Execute(%v) envelope = %+v, want %q", args[1:], envelope, test.want)
			}
		})
	}
	if _, statErr := os.Stat(filepath.Join(stateHome, "impartus")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("unknown explicit help created state directory or returned unexpected stat error: %v", statErr)
	}
}

func TestPlayHelpReflectsSelectorFreeInteractiveMode(t *testing.T) {
	if err := validatePlayFlags(playFlags{}); err != nil {
		t.Fatalf("selector-free play flags error = %v, want interactive course selection", err)
	}
	restoreCLIState(t)
	installHelpDispatchSentinels(t)
	os.Args = []string{"impartus", "play", "--help"}
	stdout, stderr, err := captureOutputStreams(t, func() error { return Execute("v1", "d1") })
	if err != nil || stderr != "" {
		t.Fatalf("Execute(play --help) stdout/stderr/error = %q/%q/%v", stdout, stderr, err)
	}
	if !strings.Contains(stdout, "impartus play [--subject <id> --session <id>] [flags]") {
		t.Fatalf("play help does not document optional direct selectors: %q", stdout)
	}
}

func TestRootHelpExplainsTTYAndNonTTYDefaultBehavior(t *testing.T) {
	restoreCLIState(t)
	os.Args = []string{"impartus", "--help"}
	stdout, stderr, err := captureOutputStreams(t, func() error { return Execute("v1", "d1") })
	if err != nil {
		t.Fatalf("Execute(--help) error = %v", err)
	}
	want := "No arguments launch the TUI only when both stdin and stdout are terminals; otherwise, help is printed to stderr and the command exits 2."
	if !strings.Contains(stdout, want) || stderr != "" {
		t.Fatalf("Execute(--help) stdout/stderr = %q/%q, want wording %q on stdout", stdout, stderr, want)
	}
}
