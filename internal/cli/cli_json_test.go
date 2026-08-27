package cli

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestStripGlobalJSONFlag(t *testing.T) {
	args, jsonMode := stripGlobalJSONFlag([]string{"courses", "--json", "-x"})
	if !jsonMode {
		t.Error("expected jsonMode true when --json present")
	}
	if len(args) != 2 || args[0] != "courses" || args[1] != "-x" {
		t.Errorf("unexpected filtered args: %v", args)
	}

	if _, jsonMode2 := stripGlobalJSONFlag([]string{"courses"}); jsonMode2 {
		t.Error("expected jsonMode false when --json absent")
	}

	valueSentinel, valueSentinelMode := stripGlobalJSONFlag([]string{"download", "--output", "--", "--json"})
	if !valueSentinelMode {
		t.Error("expected --json after a --output value of -- to enable JSON mode")
	}
	if len(valueSentinel) != 3 || valueSentinel[0] != "download" || valueSentinel[1] != "--output" || valueSentinel[2] != "--" {
		t.Fatalf("args with -- value = %v, want value preserved and --json stripped", valueSentinel)
	}
	jsonValue, jsonValueMode := stripGlobalJSONFlag([]string{"download", "--output", "--json"})
	if jsonValueMode {
		t.Error("expected --json consumed by --output to remain a flag value")
	}
	if len(jsonValue) != 3 || jsonValue[0] != "download" || jsonValue[1] != "--output" || jsonValue[2] != "--json" {
		t.Fatalf("args with --json value = %v, want unchanged", jsonValue)
	}

	afterSentinel, afterSentinelMode := stripGlobalJSONFlag([]string{"courses", "--", "--json"})
	if afterSentinelMode {
		t.Error("expected --json after -- to remain positional")
	}
	if len(afterSentinel) != 3 || afterSentinel[0] != "courses" || afterSentinel[1] != "--" || afterSentinel[2] != "--json" {
		t.Fatalf("args after sentinel = %v, want unchanged positional --json", afterSentinel)
	}

	mixed, mixedMode := stripGlobalJSONFlag([]string{"courses", "--json", "--", "--json"})
	if !mixedMode {
		t.Error("expected --json before -- to enable JSON mode")
	}
	if len(mixed) != 3 || mixed[0] != "courses" || mixed[1] != "--" || mixed[2] != "--json" {
		t.Fatalf("mixed args = %v, want only pre-sentinel --json stripped", mixed)
	}
}

func TestNewSuccessEnvelope(t *testing.T) {
	env := newSuccessEnvelope("courses", map[string]int{"n": 1})
	if !env.Success || env.Error != nil || env.Meta.Command != "courses" || env.Meta.Mode != "json" {
		t.Errorf("unexpected success envelope: %+v", env)
	}
}

func TestNewErrorEnvelope(t *testing.T) {
	env := newErrorEnvelope("courses", errors.New("boom"))
	if env.Success || env.Error == nil || env.Error.Message != "boom" {
		t.Errorf("unexpected error envelope: %+v", env)
	}
}

func TestNewJSONError(t *testing.T) {
	err := newJSONError("courses", errors.New("boom"))
	var env jsonEnvelope
	if uerr := json.Unmarshal([]byte(err.Error()), &env); uerr != nil {
		t.Fatalf("newJSONError did not produce JSON: %v", uerr)
	}
	if env.Success || env.Error == nil || env.Error.Message != "boom" {
		t.Errorf("unexpected decoded envelope: %+v", env)
	}
}

func TestHelpPayload(t *testing.T) {
	p := helpPayload()
	if p.Name != "impartus" || len(p.Commands) == 0 {
		t.Errorf("unexpected help payload: %+v", p)
	}
}
