package events

import (
	"encoding/json"

	"github.com/rabesss/impartus-cli/internal/secrets"
)

// marshalScrubbedEvent uses encoding/json's existing contract for arbitrary
// Details values, including custom marshalers and cycle/unsupported-type errors.
// It then replaces only credential-bearing JSON strings, including object keys.
// Keeping the remaining bytes preserves numeric precision, nulls, field order,
// and even duplicate keys without a recursive walk or a lossy map round trip.
func marshalScrubbedEvent(event Event) (json.RawMessage, error) {
	encoded, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	var scrubbed []byte
	unchanged := 0
	for start := 0; start < len(encoded); start++ {
		if encoded[start] != '"' {
			continue
		}
		end := start + 1
		// Marshal has validated the JSON, so every string and escape terminates.
		// Skip escaped bytes to distinguish literal quotes from delimiters.
		for encoded[end] != '"' {
			if encoded[end] == '\\' {
				end++
			}
			end++
		}
		var value string
		if err := json.Unmarshal(encoded[start:end+1], &value); err != nil {
			return nil, err
		}
		if safe := secrets.Scrub(value); safe != value {
			replacement, err := json.Marshal(safe)
			if err != nil {
				return nil, err
			}
			scrubbed = append(scrubbed, encoded[unchanged:start]...)
			scrubbed = append(scrubbed, replacement...)
			unchanged = end + 1
		}
		start = end
	}
	if unchanged == 0 {
		return encoded, nil
	}
	return append(scrubbed, encoded[unchanged:]...), nil
}
