package client

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/rabesss/impartus-cli/internal/secrets"
)

var invalidFileNameRe = regexp.MustCompile(`[<>:"/\\|?*\n\r]`)
var uriValueRe = regexp.MustCompile(`URI="([^"]+)"`)

const (
	maxPlaylistResponseSize int64 = 10 * 1024 * 1024
	maxPlaylistLineSize     int   = 1 * 1024 * 1024
	// maxPlaylistSegments counts the segments of both views of a dual-view
	// lecture together. The response size limit already bounds the playlist,
	// so this only needs to stop a flood of tiny segment lines.
	maxPlaylistSegments             = 50000
	maxStreamInfoResponseSize int64 = 1 * 1024 * 1024
)

func scanPlaylistLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	advance, token, err = bufio.ScanLines(data, atEOF)
	if err != nil {
		return advance, token, err
	}
	if len(token) > maxPlaylistLineSize {
		return 0, nil, errors.New("playlist line exceeds max size")
	}
	return advance, token, nil
}

// getStreamInfos fetches stream information for a given lecture.
func (c *Client) getStreamInfos(ctx context.Context, baseURL, token string, lecture Lecture) ([]StreamInfo, error) { //nolint:unparam // retained compatibility helper for direct stream-info callers
	policy, err := newMediaOriginPolicy(baseURL)
	if err != nil {
		return nil, err
	}
	return c.getStreamInfosWithPolicy(ctx, baseURL, token, lecture, policy)
}

func (c *Client) getStreamInfosWithPolicy(ctx context.Context, baseURL, token string, lecture Lecture, policy mediaOriginPolicy) ([]StreamInfo, error) {
	uri, err := baseURLPath(baseURL, "fetchvideo")
	if err != nil {
		return nil, err
	}
	parsedURI, err := url.Parse(uri)
	if err != nil {
		return nil, errInvalidBaseURL
	}
	query := url.Values{}
	query.Set("ttid", strconv.Itoa(lecture.TTID))
	query.Set("token", normalizeBearerToken(token))
	query.Set("type", "index.m3u8")
	parsedURI.RawQuery = query.Encode()
	uri = parsedURI.String()
	resp, err := c.getAuthorizedWithToken(ctx, uri, token, policy)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("stream info request failed with status %d: %w", resp.StatusCode, &AuthenticationError{
			Operation:  "stream info",
			StatusCode: resp.StatusCode,
		})
	}
	if resp.StatusCode != http.StatusOK {
		body, readErr := readSanitizedErrorBody(resp.Body, token)
		if readErr != nil {
			return nil, fmt.Errorf("stream info request failed with status %d and unreadable body: %w", resp.StatusCode, readErr)
		}
		return nil, fmt.Errorf("stream info request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := readResponseBodyWithLimit(resp.Body, maxStreamInfoResponseSize)
	if err != nil {
		return nil, secrets.SanitizeErrorWithToken(err, token)
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
	if err != nil {
		return ParsedPlaylist{}, errors.New("invalid playlist base URL")
	}
	if err := validateMediaURL(baseURL, false); err != nil {
		return ParsedPlaylist{}, fmt.Errorf("invalid playlist base URL: %w", err)
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
	segmentCount := 0

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
			if segmentCount >= maxPlaylistSegments {
				return ParsedPlaylist{}, errors.New("playlist contains too many media segments")
			}
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
			segmentCount++
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
	if parsedReference.User != nil {
		return "", ErrMediaURLUserinfo
	}
	if baseURL == nil {
		if parsedReference.IsAbs() {
			if err := validateMediaURL(parsedReference, false); err != nil {
				return "", err
			}
		}
		return reference, nil
	}
	resolved := baseURL.ResolveReference(parsedReference)
	if err := validateMediaURL(resolved, false); err != nil {
		return "", fmt.Errorf("resolved URI must use HTTP or HTTPS with a host: %w", err)
	}
	return resolved.String(), nil
}

func validHTTPURL(parsedURL *url.URL) bool {
	if parsedURL == nil || parsedURL.Opaque != "" || parsedURL.User != nil || parsedURL.Host == "" {
		return false
	}
	if !strings.EqualFold(parsedURL.Scheme, "http") && !strings.EqualFold(parsedURL.Scheme, "https") {
		return false
	}
	hostname := parsedURL.Hostname()
	if hostname == "" || strings.Contains(hostname, "%") || strings.HasSuffix(parsedURL.Host, ":") {
		return false
	}
	if port := parsedURL.Port(); port != "" {
		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber < 0 || portNumber > 65535 {
			return false
		}
	}
	_, err := url.ParseQuery(parsedURL.RawQuery)
	return err == nil
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
