package buildinfo

import "testing"

func TestVersionNotEmpty(t *testing.T) {
	if Version == "" {
		t.Error("expected non-empty Version")
	}
}

func TestDefaultDateValue(t *testing.T) {
	if Date != "unknown" {
		t.Errorf("expected explicit unknown Date, got %q", Date)
	}
}
