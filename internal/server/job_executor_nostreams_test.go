package server

import (
	"fmt"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
)

// A batch in which no lecture lists a stream reports the fixed ErrNoStreams
// text instead of a generic upstream error.
func TestSanitizeUpstreamErrReportsNoStreams(t *testing.T) {
	err := fmt.Errorf("prepare download: %w", fmt.Errorf("%w (2 of 2 lectures listed none)", client.ErrNoStreams))
	if got, want := sanitizeUpstreamErr(err), client.ErrNoStreams.Error(); got != want {
		t.Fatalf("sanitizeUpstreamErr() = %q, want %q", got, want)
	}
}
