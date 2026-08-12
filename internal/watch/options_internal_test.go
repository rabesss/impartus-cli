package watch

import (
	"testing"

	"github.com/rabesss/impartus-cli/internal/config"
)

func TestNormalizeOptionsMakesForceOneCycle(t *testing.T) {
	t.Parallel()

	got := normalizeOptions(&config.Config{}, Options{Force: true})
	if !got.Once {
		t.Fatal("normalizeOptions() left Once false for a forced run")
	}
}
