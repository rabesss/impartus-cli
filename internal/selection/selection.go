// Package selection owns the canonical downloader selection vocabulary shared
// by configuration validation and artifact identity generation.
package selection

import "strings"

// View is a canonical camera-view selection.
type View string

const (
	// ViewLeft selects the first/left camera output.
	ViewLeft View = "left"
	// ViewRight selects the second/right camera output.
	ViewRight View = "right"
	// ViewBoth selects both camera outputs.
	ViewBoth View = "both"
)

// NormalizeView maps supported aliases to their canonical downloader names.
func NormalizeView(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "first":
		return string(ViewLeft)
	case "second":
		return string(ViewRight)
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

// ParseView validates and canonicalizes a camera-view selection.
func ParseView(value string) (View, bool) {
	view := View(NormalizeView(value))
	switch view {
	case ViewLeft, ViewRight, ViewBoth:
		return view, true
	default:
		return "", false
	}
}

// Includes reports whether a selected view set contains an output view.
func (view View) Includes(output View) bool {
	return view == ViewBoth || view == output
}

// ValidQuality reports whether value is a supported video quality.
func ValidQuality(value string) bool {
	switch value {
	case "144", "450", "720":
		return true
	default:
		return false
	}
}

// ValidAudioFormat reports whether value is a supported audio-only format.
func ValidAudioFormat(value string) bool {
	switch value {
	case "mp3", "m4a", "aac", "opus":
		return true
	default:
		return false
	}
}
