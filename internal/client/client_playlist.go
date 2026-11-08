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
	"time"

	"github.com/rabesss/impartus-cli/internal/retry"
	"github.com/rabesss/impartus-cli/internal/secrets"
)

var invalidFileNameRe = regexp.MustCompile(`[<>:"/\\|?*\n\r]`)

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
			return nil, retry.NewStatusError(resp, time.Now(), fmt.Errorf("stream info request failed with status %d and unreadable body: %w", resp.StatusCode, readErr))
		}
		return nil, retry.NewStatusError(resp, time.Now(), fmt.Errorf("stream info request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body))))
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
	currentKeyURL := ""

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-KEY:") {
			keyURL, err := parsePlaylistKey(baseURL, line)
			if err != nil {
				return ParsedPlaylist{}, err
			}
			currentKeyURL = keyURL
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
			// The key in force for the first segment decides how the whole
			// playlist is decrypted, so every segment must agree on whether it
			// is encrypted at all.
			if segmentCount == 0 {
				parsedOutput.KeyURL = currentKeyURL
			} else if (currentKeyURL == "") != (parsedOutput.KeyURL == "") {
				return ParsedPlaylist{}, errors.New("playlist mixes clear and encrypted segments")
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
	if segmentCount == 0 {
		parsedOutput.KeyURL = currentKeyURL
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

// parsePlaylistKey returns the resolved key URI an #EXT-X-KEY line puts in
// force, or "" when it switches encryption off. Segments are decrypted whole
// with AES-CBC, so only AES-128 is accepted, together with the non-standard
// ENCRYPTION spelling some upstreams send and a missing METHOD (the legacy
// behaviour). Every other method is refused rather than mangled.
func parsePlaylistKey(baseURL *url.URL, line string) (string, error) {
	attributes, err := parseHLSAttributes(strings.TrimPrefix(line, "#EXT-X-KEY:"))
	if err != nil {
		return "", fmt.Errorf("invalid playlist key: %w", err)
	}
	method, hasMethod := attributes["METHOD"]
	switch method = strings.ToUpper(strings.TrimSpace(method)); {
	case method == "NONE":
		return "", nil
	case !hasMethod, method == "AES-128", method == "ENCRYPTION":
	default:
		// The METHOD value is upstream text, so it is not echoed: a crafted
		// playlist could put credential material in it.
		return "", errors.New("unsupported HLS encryption method: only AES-128 is supported")
	}
	uri, ok := attributes["URI"]
	if !ok {
		return "", errors.New("invalid playlist key: encrypted key has no URI")
	}
	keyURL, err := resolveMediaReference(baseURL, uri)
	if err != nil {
		return "", fmt.Errorf("invalid playlist key URI: %w", err)
	}
	return keyURL, nil
}

// parseHLSAttributes parses an HLS attribute list (NAME=value pairs separated
// by commas, where a value may be a quoted string containing commas and '=').
// Names are upper-cased. A repeated name, a pair with no '=', or an
// unterminated quoted string is an error, so an ambiguous key line cannot be
// read two ways.
func parseHLSAttributes(list string) (map[string]string, error) {
	attributes := make(map[string]string)
	for i := 0; i < len(list); {
		for i < len(list) && (list[i] == ' ' || list[i] == '\t') {
			i++
		}
		if i == len(list) {
			break
		}
		eq := strings.IndexByte(list[i:], '=')
		if eq < 0 {
			return nil, errors.New("malformed attribute list")
		}
		name := strings.ToUpper(strings.TrimSpace(list[i : i+eq]))
		if name == "" || strings.ContainsAny(name, ",\"") {
			return nil, errors.New("malformed attribute list")
		}
		i += eq + 1
		var value string
		if i < len(list) && list[i] == '"' {
			closing := strings.IndexByte(list[i+1:], '"')
			if closing < 0 {
				return nil, errors.New("unterminated quoted attribute")
			}
			value = list[i+1 : i+1+closing]
			i += closing + 2
			for i < len(list) && (list[i] == ' ' || list[i] == '\t') {
				i++
			}
			if i < len(list) && list[i] != ',' {
				return nil, errors.New("malformed attribute list")
			}
		} else {
			comma := strings.IndexByte(list[i:], ',')
			if comma < 0 {
				comma = len(list) - i
			}
			value = strings.TrimSpace(list[i : i+comma])
			i += comma
		}
		if _, dup := attributes[name]; dup {
			return nil, errors.New("repeated attribute")
		}
		attributes[name] = value
		if i < len(list) && list[i] == ',' {
			i++
		}
	}
	return attributes, nil
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
	_, err := parseMediaQuery(parsedURL.RawQuery)
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
