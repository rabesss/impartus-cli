package main

import (
	"testing"

	"github.com/rabesss/impartus-cli/internal/buildinfo"
)

func TestBuildinfoIntegration(t *testing.T) {
	if buildinfo.Version == "" {
		t.Error("buildinfo.Version should not be empty")
	}
}

func TestMainPackageImports(t *testing.T) {
	if buildinfo.SentryRelease() == "" {
		t.Fatal("buildinfo.SentryRelease() should not be empty")
	}
}
