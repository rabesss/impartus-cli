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
	value = strings.Map(func(character rune) rune {
		if unicode.In(character, unicode.Cf) {
			return -1
		}
		return character
	}, value)
	value = secrets.Scrub(value)
	value = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) || unicode.In(character, unicode.Zl, unicode.Zp) {
			return ' '
		}
		return character
	}, value)
	return strings.Join(strings.Fields(secrets.Scrub(value)), " ")
}
