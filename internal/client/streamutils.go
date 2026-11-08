package client

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/rabesss/impartus-cli/internal/selection"
)

// \d+x\d+ requires at least one digit on each side of "x" so that empty
// resolution strings (e.g. a bare "x" in a URL path) are not accepted.
var resolutionRe = regexp.MustCompile(`\d+x\d+`)

// ErrNoStreams reports that the stream info for the selected lectures listed
// no usable stream at all.
var ErrNoStreams = errors.New("no streams available for the selected lectures")

// noUsableStreamsError reports a stream info response that has lines but no
// usable stream. It counts the malformed lines instead of quoting them, since
// they are upstream text.
type noUsableStreamsError struct {
	malformed int
}

func (e *noUsableStreamsError) Error() string {
	return fmt.Sprintf("stream info response has no usable streams (%d malformed lines)", e.malformed)
}

func (e *noUsableStreamsError) Unwrap() error { return ErrNoStreams }

// ParseStreamInfosFromBody extracts stream information from response body.
// Each line is checked for URL pattern and resolution in format "WIDTHxHEIGHT".
// Blank lines and HLS tags or comments (lines starting with '#') are skipped.
// Any other line that is not an HTTP(S) URL with a resolution is malformed.
// When at least one stream parses, malformed lines are ignored, since an
// upstream may list extra lines; when none does, a response with malformed
// lines is an error wrapping ErrNoStreams. An empty list is returned without
// an error, and the caller decides what that means.
func ParseStreamInfosFromBody(body []byte) ([]StreamInfo, error) {
	lines := strings.Split(string(body), "\n")

	streamInfos := make([]StreamInfo, 0)
	malformed := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if streamInfo, ok := parseStreamInfoLine(line); ok {
			streamInfos = append(streamInfos, streamInfo)
			continue
		}
		malformed++
	}
	if len(streamInfos) == 0 && malformed > 0 {
		return nil, &noUsableStreamsError{malformed: malformed}
	}
	return streamInfos, nil
}

func parseStreamInfoLine(line string) (StreamInfo, bool) {
	parsedURL, err := url.Parse(line)
	if err != nil || !validHTTPURL(parsedURL) {
		return StreamInfo{}, false
	}
	match := resolutionRe.FindStringSubmatch(line)
	if len(match) == 0 {
		return StreamInfo{}, false
	}
	resolution := strings.Split(match[0], "x")
	if len(resolution) != 2 {
		return StreamInfo{}, false
	}
	return StreamInfo{Quality: resolution[1], URL: line}, true
}

// SelectStreamByQuality selects the best stream URL based on quality preferences.
// If audioOnly is true, prefers audio-only streams (quality "144").
// Otherwise prefers 450/480p if quality preference starts with "4",
// or exact quality match.
func SelectStreamByQuality(streamInfos []StreamInfo, quality string, audioOnly bool) string {
	if audioOnly {
		for _, streamInfo := range streamInfos {
			if streamInfo.Quality == "144" {
				return streamInfo.URL
			}
		}
		if len(streamInfos) > 0 {
			return streamInfos[0].URL
		}
		return ""
	}

	for _, streamInfo := range streamInfos {
		if (streamInfo.Quality == "450" || streamInfo.Quality == "480") && strings.HasPrefix(quality, "4") {
			return streamInfo.URL
		}
		if streamInfo.Quality == quality {
			return streamInfo.URL
		}
	}
	return ""
}

func acceptedQualityLabel(upstreamQuality string) string {
	if upstreamQuality == "480" {
		upstreamQuality = "450"
	}
	if selection.ValidQuality(upstreamQuality) {
		return upstreamQuality
	}
	return ""
}

func recordDiagnosticQuality(qualities map[string]struct{}, upstreamQuality string) {
	quality := acceptedQualityLabel(upstreamQuality)
	if quality == "" {
		quality = unsupportedQualityLabel
	}
	qualities[quality] = struct{}{}
}
