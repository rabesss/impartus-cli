package tuisession

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/rabesss/impartus-cli/internal/secrets"
)

var spacedAssignmentCandidate = regexp.MustCompile(`(?i)([a-z0-9_-]+(?: +[a-z0-9_-]+)+)( *["']? *[:=])`)
var spacedSchemeCandidate = regexp.MustCompile(`(?i)([a-z0-9_-]+)( *["']? *[:=] *)([a-z0-9_-]+(?: +[a-z0-9_-]+)+)`)
var spacedURLAssignmentCandidate = regexp.MustCompile(`(?i)([?&]) *([a-z0-9_-]+(?: +[a-z0-9_-]+)*) *= *`)

// safePresentationText removes terminal syntax and invisible key-splitting
// characters before credential redaction. The returned string is the only
// human-readable text boundary exposed to the OpenTUI sidecar.
func safePresentationText(value string) string {
	stripped := ansi.Strip(value)
	safe, baselineEvidence := sanitizePresentationText(stripped)
	detectionValue := presentationDetectionView(value)
	if stripped != value || detectionValue != value {
		// ANSI parsers legitimately consume the final byte of a short escape
		// sequence, while OSC and related strings can hide printable payload.
		// Those bytes can be credential syntax (for example a character inside
		// "token"). Re-run detection with raw printable bytes and both literal and
		// credential-shaped terminal payloads preserved. If a value recognized in
		// any view remains visible in the ordinary stripped output, discard the
		// adversarial message. Checking the actual output occurrence prevents a
		// clean decoy with the same value from explaining a separate leak.
		_, directEvidence := secrets.ScrubWithEvidence(detectionValue)
		_, rawEvidence := sanitizePresentationText(detectionValue)
		views := terminalSequenceDetectionViews(detectionValue)
		_, payloadEvidence := sanitizePresentationText(views.payload)
		_, compactEvidence := sanitizePresentationText(views.compact)
		for _, candidate := range []secrets.RedactionEvidence{
			directEvidence,
			rawEvidence,
			payloadEvidence,
			compactEvidence,
		} {
			if candidate.Count() > baselineEvidence.Count() {
				return "REDACTED"
			}
		}
		candidateEvidence := directEvidence.
			Combined(rawEvidence).
			Combined(payloadEvidence).
			Combined(compactEvidence)
		if candidateEvidence.HasVisibleValueIn(safe, normalizePresentationEvidence) {
			return "REDACTED"
		}
	}
	return safe
}

func presentationDetectionView(value string) string {
	var compact strings.Builder
	for len(value) > 0 {
		character, size := utf8.DecodeRuneInString(value)
		if character == utf8.RuneError && size == 1 {
			compact.WriteByte(value[0])
		} else if character >= 0x80 && character <= 0x9f {
			// JSON and other Unicode transports encode C1 terminal controls as
			// UTF-8. Canonicalize them to the byte form expected by the terminal
			// decoder so detection matches the equivalent raw input.
			compact.WriteByte(byte(character))
		} else if !unicode.In(character, unicode.Mn, unicode.Mc, unicode.Me) {
			compact.WriteString(value[:size])
		}
		value = value[size:]
	}
	return compact.String()
}

func normalizePresentationEvidence(value string) string {
	return strings.Join(strings.Fields(ansi.Strip(value)), " ")
}

type terminalDetectionViews struct {
	payload string
	compact string
}

func terminalSequenceDetectionViews(value string) terminalDetectionViews {
	var payload strings.Builder
	var compact strings.Builder
	state := byte(ansi.NormalState)
	for len(value) > 0 {
		sequence, width, consumed, nextState := ansi.DecodeSequence(value, state, nil)
		if consumed <= 0 {
			payload.WriteString(value)
			compact.WriteString(value)
			break
		}
		state = nextState
		if width > 0 || !isTerminalSequence(sequence) {
			payload.WriteString(sequence)
			compact.WriteString(sequence)
		} else {
			payload.WriteString(terminalSequencePayload(sequence))
			compact.WriteString(terminalSequenceCredentialPayload(sequence))
		}
		value = value[consumed:]
	}
	return terminalDetectionViews{payload: payload.String(), compact: compact.String()}
}

func isTerminalSequence(sequence string) bool {
	return ansi.HasEscPrefix(sequence) || ansi.HasCsiPrefix(sequence) || isTerminalStringSequence(sequence)
}

func terminalSequencePayload(sequence string) string {
	if ansi.HasCsiPrefix(sequence) {
		return csiCredentialPayload(sequence)
	}
	if isTerminalStringSequence(sequence) {
		return terminalStringPayload(sequence)
	}
	if ansi.HasEscPrefix(sequence) && len(sequence) > 1 {
		return sequence[len(sequence)-1:]
	}
	return ""
}

func terminalSequenceCredentialPayload(sequence string) string {
	if isTerminalStringSequence(sequence) {
		return compactCredentialSyntax(terminalStringBody(sequence))
	}
	return terminalSequencePayload(sequence)
}

func isTerminalStringSequence(sequence string) bool {
	return ansi.HasOscPrefix(sequence) || ansi.HasDcsPrefix(sequence) || ansi.HasApcPrefix(sequence) ||
		ansi.HasSosPrefix(sequence) || ansi.HasPmPrefix(sequence)
}

func csiCredentialPayload(sequence string) string {
	// CSI parameter syntax can consume credential characters beyond the final
	// command byte. Preserve key/delimiter-shaped parameter bytes and always
	// preserve the final byte while dropping the introducer and numeric/style
	// parameters. That reconstructs both `ESC[k` inside a key, `ESC[0=a`
	// across a delimiter, and `ESC[/@` across a URL userinfo delimiter.
	start := 1
	if ansi.HasEscPrefix(sequence) {
		start = 2
	}
	body := sequence[start:]
	if body == "" {
		return ""
	}
	final := body[len(body)-1]
	if final < 0x40 || final > 0x7e {
		return compactCredentialSyntax(body)
	}
	return compactCredentialSyntax(body[:len(body)-1]) + string(final)
}

func compactCredentialSyntax(value string) string {
	var payload strings.Builder
	for _, character := range value {
		if unicode.IsLetter(character) || strings.ContainsRune("_-:=\"'", character) {
			payload.WriteRune(character)
		}
	}
	return payload.String()
}

func terminalStringPayload(sequence string) string {
	body := terminalStringBody(sequence)
	if ansi.HasOscPrefix(sequence) {
		if separator := strings.IndexByte(body, ';'); separator >= 0 {
			body = body[separator+1:]
		}
	}
	return body
}

func terminalStringBody(sequence string) string {
	prefix := 1
	if ansi.HasEscPrefix(sequence) {
		prefix = 2
	}
	end := len(sequence)
	if end > prefix && (sequence[end-1] == ansi.BEL || sequence[end-1] == ansi.ST) {
		end--
	} else if end >= prefix+2 && sequence[end-2:] == "\x1b\\" {
		end -= 2
	}
	return sequence[prefix:end]
}

func sanitizePresentationText(value string) (string, secrets.RedactionEvidence) {
	marker := presentationSpaceMarker(value)
	markedValue := normalizePresentationRunes(value, true)
	// Preserve URL tokenization through the URL-only scrub. Compact a sensitive
	// query key and an obfuscated HTTP scheme before re-marking ordinary spaces,
	// then restore those word boundaries for free-form assignment handling.
	spaced := strings.ReplaceAll(markedValue, marker, " ")
	markedURLs := compactSensitiveURLAssignments(compactSplitCredentialKeys(spaced))
	markedURLs = strings.ReplaceAll(markedURLs, " ", marker)
	markedURLs = compactMarkedHTTPSchemes(markedURLs, marker)
	safeURLs, urlEvidence := secrets.ScrubCredentialURLsWithEvidence(markedURLs)
	safeURLs = strings.ReplaceAll(safeURLs, url.QueryEscape(marker), marker)
	spaced = strings.ReplaceAll(safeURLs, marker, " ")
	compacted := compactSplitCredentialSchemes(compactSplitCredentialKeys(spaced))
	splitSafe, assignmentEvidence := secrets.ScrubWithEvidence(compacted)
	return strings.Join(strings.Fields(splitSafe), " "), urlEvidence.Combined(assignmentEvidence)
}

func normalizePresentationRunes(value string, markSpaces bool) string {
	marker := " "
	if markSpaces {
		marker = presentationSpaceMarker(value)
	}
	var normalized strings.Builder
	for _, character := range value {
		if unicode.In(character, unicode.Cf) {
			continue
		}
		if unicode.IsSpace(character) {
			normalized.WriteString(marker)
			continue
		}
		if unicode.IsControl(character) {
			continue
		}
		normalized.WriteRune(character)
	}
	return normalized.String()
}

func compactSensitiveURLAssignments(value string) string {
	return spacedURLAssignmentCandidate.ReplaceAllStringFunc(value, func(match string) string {
		parts := spacedURLAssignmentCandidate.FindStringSubmatch(match)
		separator, key := parts[1], strings.ReplaceAll(parts[2], " ", "")
		probe := " " + key + "=probe"
		if secrets.Scrub(probe) == probe {
			return match
		}
		return separator + key + "="
	})
}

func compactMarkedHTTPSchemes(value, marker string) string {
	var compacted strings.Builder
	for index := 0; index < len(value); {
		matched := false
		for _, scheme := range []string{"https://", "http://"} {
			end, ok := markedHTTPSchemeEnd(value, index, marker, scheme)
			if !ok {
				continue
			}
			compacted.WriteString(scheme)
			index = end
			matched = true
			break
		}
		if matched {
			continue
		}
		compacted.WriteByte(value[index])
		index++
	}
	return compacted.String()
}

func markedHTTPSchemeEnd(value string, start int, marker, scheme string) (int, bool) {
	position := start
	for index := 0; index < len(scheme); index++ {
		if position >= len(value) || strings.ToLower(value[position:position+1]) != scheme[index:index+1] {
			return start, false
		}
		position++
		if index == len(scheme)-1 {
			continue
		}
		for strings.HasPrefix(value[position:], marker) {
			position += len(marker)
		}
	}
	return position, true
}

func presentationSpaceMarker(value string) string {
	marker := "\uE000"
	for strings.Contains(value, marker) || strings.Contains(value, url.QueryEscape(marker)) {
		marker += "\uE001"
	}
	return marker
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

func compactSplitCredentialSchemes(value string) string {
	return spacedSchemeCandidate.ReplaceAllStringFunc(value, func(match string) string {
		parts := spacedSchemeCandidate.FindStringSubmatch(match)
		key, delimiter, words := parts[1], parts[2], parts[3]
		probe := " " + key + "=probe"
		if secrets.Scrub(probe) == probe {
			return match
		}
		fields := strings.Fields(words)
		for end := 2; end <= len(fields); end++ {
			scheme := strings.Join(fields[:end], "")
			if !secrets.IsCredentialScheme(scheme) {
				continue
			}
			remainder := strings.Join(fields[end:], " ")
			if remainder != "" {
				remainder = " " + remainder
			}
			return key + delimiter + scheme + remainder
		}
		return match
	})
}
