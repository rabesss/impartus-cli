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
		if unicode.IsSpace(character) {
			marked.WriteString(marker)
			continue
		}
		if unicode.IsControl(character) {
			continue
		}
		marked.WriteRune(character)
	}
	markedText := marked.String()
	// Scrub real word boundaries first, then probe the separator-free form so a
	// credential key cannot be split by any Unicode whitespace. Use the joined
	// form only when that second pass discovers an additional credential.
	markedSafe := secrets.Scrub(markedText)
	joined := strings.ReplaceAll(markedSafe, marker, "")
	joinedSafe := secrets.Scrub(joined)
	if joinedSafe != joined {
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
