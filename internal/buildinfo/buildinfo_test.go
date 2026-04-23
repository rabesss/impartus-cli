package buildinfo

import "testing"

func TestVersionNotEmpty(t *testing.T) {
	if Version == "" {
		t.Error("expected non-empty Version")
	}
}

func TestDefaultDateValue(t *testing.T) {
	if Date != "" {
		t.Errorf("expected default empty Date, got %q", Date)
	}
}
