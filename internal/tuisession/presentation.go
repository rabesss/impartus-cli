package tuisession

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"github.com/rabesss/impartus-cli/internal/secrets"
)

// safePresentationText removes terminal syntax and invisible key-splitting
// characters before credential redaction. The returned string is the only
// human-readable text boundary exposed to the OpenTUI sidecar.
func safePresentationText(value string) string {
	value = ansi.Strip(value)
	marker := presentationSpaceMarker(value)
	var marked strings.Builder
	for _, character := range value {
		if unicode.In(character, unicode.Cf) {
			continue
		}
		if unicode.IsControl(character) || unicode.In(character, unicode.Zl, unicode.Zp) {
			if unicode.IsSpace(character) {
				marked.WriteString(marker)
			}
			continue
		}
		marked.WriteRune(character)
	}
	markedText := marked.String()
	// Probe the separator-free form so credential keys cannot be split by
	// whitespace controls. Keep the marked form only when it redacts identically.
	joined := strings.ReplaceAll(markedText, marker, "")
	joinedSafe := secrets.Scrub(joined)
	markedSafe := secrets.Scrub(markedText)
	if strings.ReplaceAll(markedSafe, marker, "") != joinedSafe {
		return strings.Join(strings.Fields(secrets.Scrub(joinedSafe)), " ")
	}
	spacedSafe := strings.ReplaceAll(markedSafe, marker, " ")
	return strings.Join(strings.Fields(secrets.Scrub(spacedSafe)), " ")
}

func presentationSpaceMarker(value string) string {
	marker := "\uE000"
	for strings.Contains(value, marker) {
		marker += "\uE001"
	}
	return marker
}
