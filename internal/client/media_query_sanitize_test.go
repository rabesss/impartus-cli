package client

import (
	"errors"
	"fmt"
	"testing"

	"github.com/rabesss/impartus-cli/internal/secrets"
)

// ErrInvalidMediaQuery must keep matching ErrInvalidMediaURL after the error
// has been sanitized, as callers that report sanitized errors do.
func TestInvalidMediaQuerySurvivesSanitizing(t *testing.T) {
	err := secrets.SanitizeError(fmt.Errorf("fetch: %w", newMediaOriginError(ErrInvalidMediaQuery)))
	for _, target := range []error{ErrInvalidMediaQuery, ErrInvalidMediaURL, ErrMediaOrigin} {
		if !errors.Is(err, target) {
			t.Fatalf("errors.Is(sanitized, %v) = false, want true", target)
		}
	}
}
