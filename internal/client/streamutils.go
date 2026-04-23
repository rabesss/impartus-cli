package client

import (
	"regexp"
	"strings"
)

var resolutionRe = regexp.MustCompile(`\d*x\d*`)

// ParseStreamInfosFromBody extracts stream information from response body.
// Each line is checked for URL pattern and resolution in format "WIDTHxHEIGHT".
func ParseStreamInfosFromBody(body []byte) ([]StreamInfo, error) {
	lines := strings.Split(string(body), "\n")

	streamInfos := make([]StreamInfo, 0)
	for _, line := range lines {
		if strings.HasPrefix(line, "http") || strings.HasPrefix(line, "https") {
			match := resolutionRe.FindStringSubmatch(line)
			if len(match) > 0 {
				resolution := strings.Split(match[0], "x")
				if len(resolution) == 2 {
					streamInfos = append(streamInfos, StreamInfo{Quality: resolution[1], URL: line})
				}
			}
		}
	}

	return streamInfos, nil
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
