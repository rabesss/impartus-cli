package buildinfo

import (
	"strings"
	"testing"
)

func TestSentryRelease(t *testing.T) {
	release := SentryRelease()
	if !strings.HasPrefix(release, "impartus-cli@") {
		t.Errorf("expected release to start with 'impartus-cli@', got %s", release)
	}
	if release == "impartus-cli@" {
		t.Error("expected non-empty version in release")
	}
}

func TestVersionNotEmpty(t *testing.T) {
	if Version == "" {
		t.Error("expected non-empty Version")
	}
}

func TestDefaultValues(t *testing.T) {
	if Commit == "" {
		t.Error("expected non-empty Commit")
	}
}
