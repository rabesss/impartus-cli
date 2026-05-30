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
