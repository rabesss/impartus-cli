// Package client implements the Impartus API client for fetching courses, lectures, and playlists.
package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/secrets"
)

// Client is the HTTP client for interacting with the Impartus API.
type Client struct {
	initOnce          sync.Once
	httpClient        *http.Client
	UserAgentProvider func() string
	tokenMu           sync.RWMutex
	token             string
}

const defaultUserAgent = "impartus-downloader"

const maxCatalogResponseSize int64 = 10 * 1024 * 1024

const maxUpstreamErrorBodySize int64 = 512

const upstreamErrorBodyOmitted = "upstream response body omitted"

var errResponseSizeLimit = errors.New("response exceeds max size")

func readResponseBodyWithLimit(body io.Reader, limit int64) ([]byte, error) {
	contents, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(contents)) > limit {
		return nil, fmt.Errorf("%w %d bytes", errResponseSizeLimit, limit)
	}
	return contents, nil
}

func readSanitizedErrorBody(body io.Reader, token string) (string, error) {
	if int64(len(strings.TrimSpace(token))) > maxUpstreamErrorBodySize {
		return upstreamErrorBodyOmitted, nil
	}
	contents, err := io.ReadAll(io.LimitReader(body, maxUpstreamErrorBodySize+1))
	if err != nil {
		return "", secrets.SanitizeErrorWithToken(err, token)
	}
	if int64(len(contents)) > maxUpstreamErrorBodySize {
		// Do not expose a prefix of an oversized body: an unknown credential may
		// begin before the cap and continue beyond it, defeating token redaction.
		return upstreamErrorBodyOmitted, nil
	}
	return secrets.RedactWithToken(strings.TrimSpace(string(contents)), token), nil
}

// New creates a new Impartus API client with the given HTTP client and user agent
// provider. Nil arguments fall back to sensible defaults.
func New(httpClient *http.Client, userAgentProvider func() string) *Client {
	c := &Client{httpClient: httpClient, UserAgentProvider: userAgentProvider}
	c.initialize()
	return c
}

// initialize fills in default dependencies for any nil fields so that a
// zero-value Client (e.g. &Client{}) is still safe to use.
func (c *Client) initialize() {
	c.initOnce.Do(func() {
		if c.httpClient == nil {
			c.httpClient = NewHTTPClient(0)
		}
		if c.UserAgentProvider == nil {
			c.UserAgentProvider = func() string { return defaultUserAgent }
		}
	})
}

func (c *Client) userAgent() string {
	c.initialize()
	return c.UserAgentProvider()
}

// GetAuthorizedWithToken performs an authenticated GET request with the given
// token. For compatibility, the caller-provided URL's exact origin is the
// trusted initial origin; cross-origin redirects remain blocked. Internal
// media paths must use GetAuthorizedWithTokenForOrigins with their explicit
// allowlist instead of this compatibility method.
func (c *Client) GetAuthorizedWithToken(ctx context.Context, rawURL, token string) (*http.Response, error) {
	if token == "" {
		return c.getAuthorizedWithToken(ctx, rawURL, token, mediaOriginPolicy{})
	}
	policy, err := newMediaOriginPolicy(rawURL)
	if err != nil {
		return nil, err
	}
	return c.getAuthorizedWithToken(ctx, rawURL, token, policy)
}

// GetAuthorizedWithTokenForOrigins performs an authenticated GET request with
// an immutable, request-scoped exact-origin policy. The policy is rebuilt for
// every call so concurrent clients cannot overwrite one another's allowlist.
func (c *Client) GetAuthorizedWithTokenForOrigins(ctx context.Context, rawURL, token string, origins ...string) (*http.Response, error) {
	policy, err := newMediaOriginPolicy(origins...)
	if err != nil {
		return nil, err
	}
	return c.getAuthorizedWithToken(ctx, rawURL, token, policy)
}

func (c *Client) getAuthorizedWithToken(ctx context.Context, rawURL, token string, policy mediaOriginPolicy) (*http.Response, error) {
	c.initialize()
	redactionToken := token
	parsedURL, err := parseRequestURL(rawURL)
	if err != nil {
		return nil, err
	}
	// Config and media-origin validation accept scheme casing, while
	// transports dispatch on canonical lower-case schemes. Normalize before
	// handing the URL to net/http.
	rawURL = parsedURL.String()
	if validateErr := validateMediaURL(parsedURL, token != ""); validateErr != nil {
		return nil, validateErr
	}

	if token == "" && policy.allows(parsedURL) && hasCredentialQuery(parsedURL) {
		// A tokenless call to a configured origin must not forward a
		// credential-bearing query alias supplied by an upstream playlist or
		// caller. An unconfigured origin has no bearer to protect, so its URL
		// (for example a signed CDN URL) is sent as given. The media redirect
		// wrapper below still removes credential headers across origins.
		strippedURL, stripErr := stripBearerTokenQuery(rawURL, "")
		if stripErr != nil {
			return nil, stripErr
		}
		rawURL = strippedURL
		parsedURL, err = parseRequestURL(rawURL)
		if err != nil {
			return nil, err
		}
	}

	if token != "" && !policy.allows(parsedURL) {
		// A playlist or media endpoint may be public, but an unconfigured
		// origin must never receive the Impartus bearer token. Remove only the
		// query parameters carrying the token, so unrelated CDN signing
		// parameters survive, then fail closed if the token is still present
		// in any URL component (for example a path or hostname). There is no
		// safe way to rewrite those components while preserving the requested
		// destination.
		strippedURL, stripErr := stripBearerQueryParams(rawURL, token)
		if stripErr != nil {
			return nil, stripErr
		}
		parsedStrippedURL, parseErr := parseRequestURL(strippedURL)
		if parseErr != nil {
			return nil, parseErr
		}
		if boundaryErr := validateRedirectCredentialBoundary(parsedStrippedURL, token, false); boundaryErr != nil {
			return nil, boundaryErr
		}
		rawURL = parsedStrippedURL.String()
		parsedURL = parsedStrippedURL
		token = ""
	}

	// Every media request goes through a request-local copy of the HTTP
	// client. This keeps redirect policy, the default redirect bound, cookie
	// isolation, and credential-header stripping active even for public
	// tokenless URLs. The compatibility entry point still permits its exact
	// initial origin and loopback HTTP; the wrapper only broadens the policy
	// for an explicitly configured origin.
	// Keep the original bearer as redirect redaction/boundary context even
	// when the request token was cleared for an unconfigured media origin.
	// The request client never uses this value to attach Authorization; it is
	// only used to sanitize hook views and reject credential reintroduction.
	requestClient := c.httpClientForMediaRequest(parsedURL, redactionToken, policy)
	return c.doRequestWithTokenClient(ctx, http.MethodGet, rawURL, nil, token, requestClient, redactionToken)
}

// GetCourses fetches the list of courses for the authenticated user.
func (c *Client) GetCourses(ctx context.Context, cfg *config.Config) (Courses, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}

	baseURL, policy, err := newBaseURLPolicy(cfg.BaseURL)
	if err != nil {
		return nil, err
	}

	token, err := c.resolveToken(cfg)
	if err != nil {
		return nil, err
	}

	url, err := baseURLPath(baseURL, "subjects")
	if err != nil {
		return nil, err
	}
	resp, err := c.getAuthorizedWithToken(ctx, url, token, policy)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("subjects request failed with status %d: %w", resp.StatusCode, &AuthenticationError{
			Operation:  "subjects",
			StatusCode: resp.StatusCode,
		})
	}
	if resp.StatusCode != http.StatusOK {
		body, readErr := readSanitizedErrorBody(resp.Body, token)
		if readErr != nil {
			return nil, fmt.Errorf("subjects request failed with status %d and unreadable body: %w", resp.StatusCode, readErr)
		}
		return nil, fmt.Errorf("subjects request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := readResponseBodyWithLimit(resp.Body, maxCatalogResponseSize)
	if err != nil {
		return nil, fmt.Errorf("failed to read courses response: %w", secrets.SanitizeErrorWithToken(err, token))
	}
	var courses Courses
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&courses); err != nil {
		return nil, fmt.Errorf("failed to decode courses response: %w", err)
	}

	for i := range courses {
		courses[i].SubjectName = sanitizeFileName(courses[i].SubjectName)
	}

	return courses, nil
}

// GetLectures fetches the list of lectures for a given course.
func (c *Client) GetLectures(ctx context.Context, cfg *config.Config, course Course) (Lectures, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}

	baseURL, policy, err := newBaseURLPolicy(cfg.BaseURL)
	if err != nil {
		return nil, err
	}

	token, err := c.resolveToken(cfg)
	if err != nil {
		return nil, err
	}

	url, err := baseURLPath(baseURL, fmt.Sprintf("subjects/%d/lectures/%d", course.SubjectID, course.SessionID))
	if err != nil {
		return nil, err
	}
	resp, err := c.getAuthorizedWithToken(ctx, url, token, policy)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("lectures request failed with status %d: %w", resp.StatusCode, &AuthenticationError{
			Operation:  "lectures",
			StatusCode: resp.StatusCode,
		})
	}
	if resp.StatusCode != http.StatusOK {
		body, readErr := readSanitizedErrorBody(resp.Body, token)
		if readErr != nil {
			return nil, fmt.Errorf("lectures request failed with status %d and unreadable body: %w", resp.StatusCode, readErr)
		}
		return nil, fmt.Errorf("lectures request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := readResponseBodyWithLimit(resp.Body, maxCatalogResponseSize)
	if err != nil {
		return nil, fmt.Errorf("failed to read lectures response: %w", secrets.SanitizeErrorWithToken(err, token))
	}
	var lectures Lectures
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&lectures); err != nil {
		return nil, fmt.Errorf("failed to decode lectures response: %w", err)
	}

	for i := range lectures {
		if lectures[i].InstituteID == 0 {
			lectures[i].InstituteID = course.InstituteID
		}
		if lectures[i].SubjectID == 0 {
			lectures[i].SubjectID = course.SubjectID
		}
		if lectures[i].SessionID == 0 {
			lectures[i].SessionID = course.SessionID
		}
		lectures[i].Topic = sanitizeFileName(lectures[i].Topic)
		lectures[i].SubjectName = sanitizeFileName(lectures[i].SubjectName)
	}

	return lectures, nil
}

// GetPlaylists fetches and parses HLS playlists for the given lectures.
func (c *Client) GetPlaylists(ctx context.Context, cfg *config.Config, lectures Lectures) ([]ParsedPlaylist, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}

	baseURL, policy, err := newBaseURLPolicy(cfg.BaseURL, cfg.MediaOrigins...)
	if err != nil {
		return nil, err
	}

	token, err := c.resolveToken(cfg)
	if err != nil {
		return nil, err
	}

	parsedPlaylists := make([]ParsedPlaylist, 0, len(lectures))
	unavailableQualities := make(map[string]struct{})
	for _, lecture := range lectures {
		streamInfos, err := c.getStreamInfosWithPolicy(ctx, baseURL, token, lecture, policy)
		if err != nil {
			return parsedPlaylists, err
		}

		streamURL := SelectStreamByQuality(streamInfos, cfg.Quality, cfg.AudioOnly)
		if streamURL == "" {
			for _, streamInfo := range streamInfos {
				recordDiagnosticQuality(unavailableQualities, streamInfo.Quality)
			}
			continue
		}

		parsed, err := c.getPlaylistWithPolicy(ctx, streamURL, token, lecture, policy)
		if err != nil {
			return parsedPlaylists, err
		}
		parsedPlaylists = append(parsedPlaylists, parsed)
	}
	if len(parsedPlaylists) == 0 && len(unavailableQualities) > 0 {
		qualities := make([]string, 0, len(unavailableQualities))
		for quality := range unavailableQualities {
			qualities = append(qualities, quality)
		}
		sort.Strings(qualities)
		return nil, newQualityUnavailableError(cfg.Quality, qualities)
	}

	return parsedPlaylists, nil
}

func (c *Client) getPlaylist(ctx context.Context, streamURL, token string, lecture Lecture) (ParsedPlaylist, error) {
	policy, err := newMediaOriginPolicy(streamURL)
	if err != nil {
		return ParsedPlaylist{}, err
	}
	return c.getPlaylistWithPolicy(ctx, streamURL, token, lecture, policy)
}

func (c *Client) getPlaylistWithPolicy(ctx context.Context, streamURL, token string, lecture Lecture, policy mediaOriginPolicy) (ParsedPlaylist, error) {
	resp, err := c.getAuthorizedWithToken(ctx, streamURL, token, policy)
	if err != nil {
		return ParsedPlaylist{}, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		_ = resp.Body.Close() //nolint:errcheck
		return ParsedPlaylist{}, fmt.Errorf("playlist request failed with status %d: %w", resp.StatusCode, &AuthenticationError{
			Operation:  "playlist",
			StatusCode: resp.StatusCode,
		})
	}
	if resp.StatusCode != http.StatusOK {
		body, readErr := readSanitizedErrorBody(resp.Body, token)
		_ = resp.Body.Close() //nolint:errcheck
		if readErr != nil {
			return ParsedPlaylist{}, fmt.Errorf("playlist request failed with status %d and unreadable body: %w", resp.StatusCode, readErr)
		}
		return ParsedPlaylist{}, fmt.Errorf("playlist request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, readErr := readResponseBodyWithLimit(resp.Body, maxPlaylistResponseSize)
	if readErr != nil {
		_ = resp.Body.Close() //nolint:errcheck
		if errors.Is(readErr, errResponseSizeLimit) {
			return ParsedPlaylist{}, fmt.Errorf("playlist response exceeds max size %d bytes", maxPlaylistResponseSize)
		}
		return ParsedPlaylist{}, fmt.Errorf("read playlist response: %w", secrets.SanitizeErrorWithToken(readErr, token))
	}
	scanner := bufio.NewScanner(bytes.NewReader(body))
	// Scanner's maximum token size includes the line delimiter. Keep the
	// public payload limit at 1 MiB while allowing either LF or CRLF.
	scanner.Buffer(make([]byte, 64*1024), maxPlaylistLineSize+2)
	scanner.Split(scanPlaylistLines)
	playlistBaseURL := streamURL
	if resp.Request != nil && resp.Request.URL != nil {
		playlistBaseURL = resp.Request.URL.String()
	}
	parsed, parseErr := parsePlaylist(scanner, playlistBaseURL, lecture.TTID, lecture.Topic, lecture.SeqNo)
	_ = resp.Body.Close() //nolint:errcheck
	if parseErr != nil {
		return ParsedPlaylist{}, fmt.Errorf("parse playlist for lecture %d (%s): %w", lecture.TTID, lecture.Topic, parseErr)
	}
	parsed.InstituteID = lecture.InstituteID
	parsed.SubjectID = lecture.SubjectID
	parsed.SessionID = lecture.SessionID
	return parsed, nil
}
