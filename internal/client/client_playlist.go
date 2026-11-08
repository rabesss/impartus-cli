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
		if strings.HasPrefix(line, "#EXT-X-KEY") {
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
			if err := applySegmentKey(&parsedOutput, segmentCount, currentKeyURL); err != nil {
				return ParsedPlaylist{}, err
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
	// A playlist with no segments needs no key: leaving KeyURL empty keeps
	// callers from fetching one for nothing.

	parsedOutput.FirstViewURLs = firstViewURLs
	parsedOutput.FirstDurations = firstDurations
	if !isFirstView {
		parsedOutput.HasMultipleViews = true
		parsedOutput.SecondViewURLs = secondViewURLs
		parsedOutput.SecondDurations = secondDurations
	}

	return parsedOutput, nil
}

// applySegmentKey records the key in force for a playlist's segment. The key
// for the first segment decides how the whole playlist is decrypted, so every
// later segment must use the same key or none. A playlist that rotates to a
// different key URI is refused: decrypting its later segments with the first
// key would silently produce garbage.
func applySegmentKey(playlist *ParsedPlaylist, segmentIndex int, keyURL string) error {
	if segmentIndex == 0 {
		playlist.KeyURL = keyURL
		return nil
	}
	if (keyURL == "") != (playlist.KeyURL == "") {
		return errors.New("playlist mixes clear and encrypted segments")
	}
	if keyURL != playlist.KeyURL {
		return errors.New("playlist rotates encryption keys: only one key per playlist is supported")
	}
	return nil
}

// parsePlaylistKey returns the resolved key URI an #EXT-X-KEY line puts in
// force, or "" when it switches encryption off. Segments are decrypted whole
// with AES-CBC, so only AES-128 is accepted, together with the non-standard
// ENCRYPTION spelling some upstreams send and a missing METHOD (the legacy
// behavior). Every other method is refused rather than mangled.
func parsePlaylistKey(baseURL *url.URL, line string) (string, error) {
	list, ok := strings.CutPrefix(line, "#EXT-X-KEY:")
	if !ok {
		// A bare or misspelled key tag must not read as a clear playlist.
		return "", errors.New("invalid playlist key: malformed EXT-X-KEY tag")
	}
	attributes, err := parseHLSAttributes(list)
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
	if iv, hasIV := attributes["IV"]; hasIV {
		if err := validatePlaylistKeyIV(iv); err != nil {
			return "", err
		}
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

// maxPlaylistKeyIVDigits is the number of hex digits in a 128-bit IV.
const maxPlaylistKeyIVDigits = 32

// validatePlaylistKeyIV accepts an explicit IV only when it is zero, which is
// the IV every segment is decrypted with. An HLS IV is a hexadecimal integer
// with a 0x or 0X prefix and at most 128 bits. A non-zero IV would decrypt to
// garbage, so it is refused, as is a malformed one. The IV is upstream text,
// so it is never echoed.
func validatePlaylistKeyIV(iv string) error {
	digits, ok := strings.CutPrefix(strings.TrimSpace(iv), "0x")
	if !ok {
		digits, ok = strings.CutPrefix(strings.TrimSpace(iv), "0X")
	}
	if !ok || digits == "" || len(digits) > maxPlaylistKeyIVDigits {
		return errors.New("invalid playlist key: malformed IV")
	}
	zero := true
	for i := 0; i < len(digits); i++ {
		if !isHexDigit(digits[i]) {
			return errors.New("invalid playlist key: malformed IV")
		}
		if digits[i] != '0' {
			zero = false
		}
	}
	if !zero {
		return errors.New("unsupported HLS key IV: only a zero IV is supported")
	}
	return nil
}

// parseHLSAttributes parses an HLS attribute list (NAME=value pairs separated
// by commas, where a value may be a quoted string containing commas and '=').
// Names are upper-cased. A repeated name, a pair with no '=', or an
// unterminated quoted string is an error, so an ambiguous key line cannot be
// read two ways.
func parseHLSAttributes(list string) (map[string]string, error) {
	attributes := make(map[string]string)
	for i := skipHLSBlanks(list, 0); i < len(list); i = skipHLSBlanks(list, i) {
		eq := strings.IndexByte(list[i:], '=')
		if eq < 0 {
			return nil, errors.New("malformed attribute list")
		}
		name := strings.ToUpper(strings.TrimSpace(list[i : i+eq]))
		if name == "" || strings.ContainsAny(name, ",\"") {
			return nil, errors.New("malformed attribute list")
		}
		value, next, err := parseHLSAttributeValue(list, i+eq+1)
		if err != nil {
			return nil, err
		}
		if _, dup := attributes[name]; dup {
			return nil, errors.New("repeated attribute")
		}
		attributes[name] = value
		i = next
		if i < len(list) && list[i] == ',' {
			i++
		}
	}
	return attributes, nil
}

// parseHLSAttributeValue parses the attribute value that starts at list[i]
// and returns it with the index just past it, which is the separating comma
// or the end of list.
func parseHLSAttributeValue(list string, i int) (string, int, error) {
	if start := skipHLSBlanks(list, i); start < len(list) && list[start] == '"' {
		return parseHLSQuotedValue(list, start)
	}
	comma := strings.IndexByte(list[i:], ',')
	if comma < 0 {
		comma = len(list) - i
	}
	value := strings.TrimSpace(list[i : i+comma])
	if strings.Contains(value, `"`) {
		// A stray quote means the line was meant to hold a quoted string
		// that never closes; reading on would split it into attributes.
		return "", 0, errors.New("malformed attribute list")
	}
	return value, i + comma, nil
}

// parseHLSQuotedValue parses the quoted string whose opening quote is at
// list[i].
func parseHLSQuotedValue(list string, i int) (string, int, error) {
	closing := strings.IndexByte(list[i+1:], '"')
	if closing < 0 {
		return "", 0, errors.New("unterminated quoted attribute")
	}
	value := list[i+1 : i+1+closing]
	next := skipHLSBlanks(list, i+closing+2)
	if next < len(list) && list[next] != ',' {
		return "", 0, errors.New("malformed attribute list")
	}
	return value, next, nil
}

func skipHLSBlanks(list string, i int) int {
	for i < len(list) && (list[i] == ' ' || list[i] == '\t') {
		i++
	}
	return i
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
	if !validHTTPURLParts(parsedURL) {
		return false
	}
	_, err := parseMediaQuery(parsedURL.RawQuery)
	return err == nil
}

// validHTTPURLParts checks everything validHTTPURL does except the query.
func validHTTPURLParts(parsedURL *url.URL) bool {
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
	return true
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
