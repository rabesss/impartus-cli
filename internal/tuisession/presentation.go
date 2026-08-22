package tuisession

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"github.com/rabesss/impartus-cli/internal/secrets"
)

var spacedAssignmentCandidate = regexp.MustCompile(`(?i)([a-z0-9_-]+(?: +[a-z0-9_-]+)+)("? *[:=])`)

// safePresentationText removes terminal syntax and invisible key-splitting
// characters before credential redaction. The returned string is the only
// human-readable text boundary exposed to the OpenTUI sidecar.
func safePresentationText(value string) string {
	value = ansi.Strip(value)
	var normalized strings.Builder
	for _, character := range value {
		if unicode.In(character, unicode.Cf) {
			continue
		}
		if unicode.IsSpace(character) {
			normalized.WriteByte(' ')
			continue
		}
		if unicode.IsControl(character) {
			continue
		}
		normalized.WriteRune(character)
	}
	// Scrub ordinary assignments before compacting only a whitespace-split key.
	// This preserves unrelated word boundaries while still closing key-splitting
	// bypasses such as "to ken=secret".
	spacedSafe := secrets.Scrub(normalized.String())
	splitSafe := secrets.Scrub(compactSplitCredentialKeys(spacedSafe))
	return strings.Join(strings.Fields(splitSafe), " ")
}

func compactSplitCredentialKeys(value string) string {
	return spacedAssignmentCandidate.ReplaceAllStringFunc(value, func(match string) string {
		parts := spacedAssignmentCandidate.FindStringSubmatch(match)
		words, delimiter := parts[1], parts[2]
		for start := len(words) - 1; start >= 0; start-- {
			if words[start] == ' ' || start > 0 && words[start-1] != ' ' {
				continue
			}
			candidateText := words[start:]
			candidate := strings.ReplaceAll(candidateText, " ", "")
			probe := " " + candidate + "=probe"
			if secrets.Scrub(probe) == probe {
				continue
			}
			if !strings.Contains(candidateText, " ") {
				return match
			}
			return words[:start] + candidate + delimiter
		}
		return match
	})
}
