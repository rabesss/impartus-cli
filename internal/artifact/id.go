// Package artifact defines the stable boundary between Impartus downloads and
// tools that consume the resulting local media.
package artifact

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/rabesss/impartus-cli/internal/selection"
)

const artifactIDPrefix = "impartus:v1:"

var canonicalDomain = []byte("impartus-artifact-v1\x00")

// Identity contains the scoped lecture identity and normalized output
// selection that make one logical Impartus artifact distinct from another.
// It intentionally does not include local paths or file contents.
type Identity struct {
	InstituteID int
	SubjectID   int
	SessionID   int
	TTID        int
	AudioOnly   bool
	Views       string
	Quality     string
	AudioFormat string
}

// CanonicalBytes returns the version-1 binary identity representation used to
// derive artifact IDs. The format is independent of Go's JSON and struct
// encoders so future serialization changes cannot alter an existing identity.
func CanonicalBytes(identity Identity) ([]byte, error) {
	normalized, err := normalizeIdentity(identity)
	if err != nil {
		return nil, err
	}

	result := make([]byte, 0, len(canonicalDomain)+4*8+1+2*3+len(normalized.Views)+len(normalized.Quality)+len(normalized.AudioFormat))
	result = append(result, canonicalDomain...)
	for _, value := range []int{normalized.InstituteID, normalized.SubjectID, normalized.SessionID, normalized.TTID} {
		var encoded [8]byte
		// Values are validated as positive above, and every Go int is representable
		// as uint64 after that check on every supported architecture.
		binary.BigEndian.PutUint64(encoded[:], uint64(value)) // #nosec G115
		result = append(result, encoded[:]...)
	}
	if normalized.AudioOnly {
		result = append(result, 1)
	} else {
		result = append(result, 0)
	}
	result = appendCanonicalText(result, normalized.Views)
	result = appendCanonicalText(result, normalized.Quality)
	result = appendCanonicalText(result, normalized.AudioFormat)
	return result, nil
}

// NewID derives a stable, URL-safe ID for a logical artifact.
func NewID(identity Identity) (string, error) {
	canonical, err := CanonicalBytes(identity)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return artifactIDPrefix + base64.RawURLEncoding.EncodeToString(digest[:]), nil
}

func normalizeIdentity(identity Identity) (Identity, error) {
	for _, id := range []struct {
		name  string
		value int
	}{
		{name: "instituteId", value: identity.InstituteID},
		{name: "subjectId", value: identity.SubjectID},
		{name: "sessionId", value: identity.SessionID},
		{name: "ttid", value: identity.TTID},
	} {
		if id.value <= 0 {
			return Identity{}, fmt.Errorf("%s must be positive", id.name)
		}
	}

	view, ok := selection.ParseView(identity.Views)
	if !ok {
		return Identity{}, fmt.Errorf("unsupported views %q", identity.Views)
	}
	identity.Views = string(view)

	identity.Quality = strings.ToLower(strings.TrimSpace(identity.Quality))
	if !selection.ValidQuality(identity.Quality) {
		return Identity{}, fmt.Errorf("unsupported quality %q", identity.Quality)
	}

	identity.AudioFormat = strings.ToLower(strings.TrimSpace(identity.AudioFormat))
	if !identity.AudioOnly {
		identity.AudioFormat = ""
		return identity, nil
	}
	if selection.ValidAudioFormat(identity.AudioFormat) {
		return identity, nil
	}
	if identity.AudioFormat == "" {
		return Identity{}, errors.New("audioFormat is required for audio-only artifacts")
	}
	return Identity{}, fmt.Errorf("unsupported audioFormat %q", identity.AudioFormat)
}

func appendCanonicalText(destination []byte, value string) []byte {
	var length [2]byte
	// Callers pass only the validated v1 enum values, whose longest encoding is
	// five bytes and therefore cannot overflow uint16.
	binary.BigEndian.PutUint16(length[:], uint16(len(value))) // #nosec G115
	destination = append(destination, length[:]...)
	return append(destination, value...)
}
