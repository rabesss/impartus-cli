package cli

import (
	"testing"

	"github.com/rabesss/impartus-cli/internal/downloader"
)

func TestAppendOutputPaths_AllEmpty(t *testing.T) {
	result := downloader.JoinResult{}
	paths := result.OutputPaths()
	if len(paths) != 0 {
		t.Errorf("expected 0 paths, got %d", len(paths))
	}
}
