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
