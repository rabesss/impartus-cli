package client

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var invalidFileNameRe = regexp.MustCompile(`[<>:"/\\|?*\n\r]`)
var uriValueRe = regexp.MustCompile(`URI="([^"]+)"`)

// getStreamInfos fetches stream information for a given lecture.
func (c *Client) getStreamInfos(ctx context.Context, baseURL, token string, lecture Lecture) ([]StreamInfo, error) {
	uri := fmt.Sprintf("%s/fetchvideo?ttid=%d&token=%s&type=index.m3u8", baseURL, lecture.TTID, token)
	resp, err := c.GetAuthorizedWithToken(ctx, uri, token)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 512))
		if readErr != nil {
			return nil, fmt.Errorf("stream info request failed with status %d and unreadable body: %w", resp.StatusCode, readErr)
		}
		return nil, fmt.Errorf("stream info request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}

	return ParseStreamInfosFromBody(body)
}

// ParsePlaylist parses an HLS playlist without a base URL. It is retained for
// package compatibility; production ingestion uses parsePlaylist so relative
// media references are resolved before they leave the client package.
func ParsePlaylist(scanner *bufio.Scanner, id int, title string, seqNo int) (ParsedPlaylist, error) {
	return parsePlaylistWithBase(scanner, nil, id, title, seqNo)
}

func parsePlaylist(scanner *bufio.Scanner, playlistURL string, id int, title string, seqNo int) (ParsedPlaylist, error) {
	baseURL, err := url.Parse(playlistURL)
	if err != nil || !validHTTPURL(baseURL) {
		return ParsedPlaylist{}, errors.New("invalid playlist base URL")
	}
	return parsePlaylistWithBase(scanner, baseURL, id, title, seqNo)
}

func parsePlaylistWithBase(scanner *bufio.Scanner, baseURL *url.URL, id int, title string, seqNo int) (ParsedPlaylist, error) {
	parsedOutput := ParsedPlaylist{
		ID:    id,
		Title: title,
		SeqNo: seqNo,
	}

	isFirstView := true
	firstViewURLs := make([]string, 0)
	secondViewURLs := make([]string, 0)
	firstDurations := make([]float64, 0)
	secondDurations := make([]float64, 0)
	pendingDuration := 0.0

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if parsedOutput.KeyURL == "" && strings.HasPrefix(line, "#EXT-X-KEY") {
			match := uriValueRe.FindStringSubmatch(line)
			if len(match) == 2 {
				keyURL, err := resolveMediaReference(baseURL, match[1])
				if err != nil {
					return ParsedPlaylist{}, fmt.Errorf("invalid playlist key URI: %w", err)
				}
				parsedOutput.KeyURL = keyURL
			}
		} else if strings.HasPrefix(line, "#EXTINF:") {
			pendingDuration = parseEXTINFDuration(line)
		} else if line == "#EXT-X-DISCONTINUITY" {
			isFirstView = false
		} else if !strings.HasPrefix(line, "#") {
			segmentURL, err := resolveMediaReference(baseURL, line)
			if err != nil {
				return ParsedPlaylist{}, fmt.Errorf("invalid playlist segment URI: %w", err)
			}
			if isFirstView {
				firstViewURLs = append(firstViewURLs, segmentURL)
				firstDurations = append(firstDurations, pendingDuration)
			} else {
				secondViewURLs = append(secondViewURLs, segmentURL)
				secondDurations = append(secondDurations, pendingDuration)
			}
			pendingDuration = 0
		}
	}

	if err := scanner.Err(); err != nil {
		return ParsedPlaylist{}, fmt.Errorf("scan playlist: %w", err)
	}

	parsedOutput.FirstViewURLs = firstViewURLs
	parsedOutput.FirstDurations = firstDurations
	if !isFirstView {
		parsedOutput.HasMultipleViews = true
		parsedOutput.SecondViewURLs = secondViewURLs
		parsedOutput.SecondDurations = secondDurations
	}

	return parsedOutput, nil
}

func resolveMediaReference(baseURL *url.URL, rawReference string) (string, error) {
	reference := strings.TrimSpace(rawReference)
	if reference == "" {
		return "", errors.New("empty URI")
	}
	parsedReference, err := url.Parse(reference)
	if err != nil {
		return "", errors.New("malformed URI")
	}
	if baseURL == nil {
		if parsedReference.IsAbs() && !validHTTPURL(parsedReference) {
			return "", errors.New("URI must use HTTP or HTTPS")
		}
		return reference, nil
	}
	resolved := baseURL.ResolveReference(parsedReference)
	if !validHTTPURL(resolved) {
		return "", errors.New("resolved URI must use HTTP or HTTPS with a host")
	}
	return resolved.String(), nil
}

func validHTTPURL(parsedURL *url.URL) bool {
	return parsedURL != nil && (parsedURL.Scheme == "http" || parsedURL.Scheme == "https") && parsedURL.Host != ""
}

func parseEXTINFDuration(line string) float64 {
	durationText := strings.TrimPrefix(line, "#EXTINF:")
	if comma := strings.Index(durationText, ","); comma >= 0 {
		durationText = durationText[:comma]
	}
	duration, err := strconv.ParseFloat(strings.TrimSpace(durationText), 64)
	if err != nil || duration <= 0 {
		return 0
	}
	return duration
}

func sanitizeFileName(name string) string {
	name = invalidFileNameRe.ReplaceAllString(name, "_")
	name = strings.TrimSpace(name)
	return strings.Trim(name, ".")
}
