package buildinfo

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// UnknownDate is the explicit fallback for binaries built without a timestamp.
const UnknownDate = "unknown"

// NormalizeDate prevents an empty linker value from reaching human or JSON output.
func NormalizeDate(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return UnknownDate
	}
	return value
}

// ResolveDate selects and validates the timestamp used by stamped builds.
func ResolveDate(explicit, sourceDateEpoch string, now time.Time) (string, error) {
	if value := strings.TrimSpace(explicit); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return "", fmt.Errorf("BUILD_DATE must be RFC3339: %w", err)
		}
		resolved := parsed.UTC()
		if resolved.Year() < 0 || resolved.Year() > 9999 {
			return "", fmt.Errorf("BUILD_DATE is outside the RFC3339 range")
		}
		return resolved.Format(time.RFC3339), nil
	}

	if value := strings.TrimSpace(sourceDateEpoch); value != "" {
		seconds, err := strconv.ParseInt(value, 10, 64)
		if err != nil || seconds < 0 {
			return "", fmt.Errorf("SOURCE_DATE_EPOCH must be a non-negative integer")
		}
		resolved := time.Unix(seconds, 0).UTC()
		if resolved.Year() < 0 || resolved.Year() > 9999 {
			return "", fmt.Errorf("SOURCE_DATE_EPOCH is outside the RFC3339 range")
		}
		return resolved.Format(time.RFC3339), nil
	}

	return now.UTC().Format(time.RFC3339), nil
}
