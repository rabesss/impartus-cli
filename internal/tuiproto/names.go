package tuiproto

import (
	"strings"
	"unicode"
)

// goInitialisms mirrors the repository revive var-naming configuration so
// generated Go identifiers pass the project linter without exemptions.
var goInitialisms = map[string]string{
	"api":  "API",
	"http": "HTTP",
	"id":   "ID",
	"json": "JSON",
	"ok":   "OK",
	"ttid": "TTID",
	"url":  "URL",
}

// goExportedName converts a schema property name to an exported Go identifier.
func goExportedName(name string) string {
	var builder strings.Builder
	for _, word := range splitWords(name) {
		builder.WriteString(capitalizeWord(word))
	}
	return builder.String()
}

// goConstName converts an enum member to an exported Go constant suffix.
func goConstName(value string) string {
	return goExportedName(strings.ReplaceAll(value, ".", "-"))
}

// tsConstName converts a schema header key to a TypeScript constant name.
func tsConstName(name string) string {
	words := splitWords(name)
	upper := make([]string, 0, len(words))
	for _, word := range words {
		upper = append(upper, strings.ToUpper(word))
	}
	return strings.Join(upper, "_")
}

func capitalizeWord(word string) string {
	if word == "" {
		return ""
	}
	if initialism, ok := goInitialisms[strings.ToLower(word)]; ok {
		return initialism
	}
	runes := []rune(strings.ToLower(word))
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// splitWords breaks camelCase, kebab-case, and snake_case names into words.
func splitWords(name string) []string {
	words := make([]string, 0, 4)
	current := make([]rune, 0, len(name))
	flush := func() {
		if len(current) > 0 {
			words = append(words, string(current))
			current = current[:0]
		}
	}
	for _, symbol := range name {
		switch {
		case symbol == '_' || symbol == '-' || symbol == ' ':
			flush()
		case unicode.IsUpper(symbol):
			flush()
			current = append(current, symbol)
		default:
			current = append(current, symbol)
		}
	}
	flush()
	return words
}

// docComment renders a description as wrapped comment lines with the prefix.
// A non-empty subject is prepended so generated Go type and constant comments
// begin with the identifier they document, as revive requires.
func docComment(prefix, subject, description string) []string {
	text := strings.TrimSpace(description)
	if text == "" {
		return nil
	}
	if subject != "" {
		text = subject + " " + text
	}
	return wrapComment(prefix, text, 76)
}

func wrapComment(prefix, text string, width int) []string {
	words := strings.Fields(text)
	lines := make([]string, 0, 4)
	line := prefix
	for _, word := range words {
		candidate := line + " " + word
		if line != prefix && len(candidate) > width {
			lines = append(lines, line)
			line = prefix + " " + word
			continue
		}
		line = candidate
	}
	if line != prefix {
		lines = append(lines, line)
	}
	return lines
}
