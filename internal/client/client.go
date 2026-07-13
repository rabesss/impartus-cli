// Package client implements the Impartus API client for fetching courses, lectures, and playlists.
package client

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/rabesss/impartus-cli/internal/config"
)

// Client is the HTTP client for interacting with the Impartus API.
type Client struct {
	httpClient        *http.Client
	UserAgentProvider func() string
	token             string
}

const defaultUserAgent = "impartus-downloader"

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
	if c.httpClient == nil {
		c.httpClient = NewHTTPClient(0)
	}
	if c.UserAgentProvider == nil {
		c.UserAgentProvider = func() string { return defaultUserAgent }
	}
}

func (c *Client) userAgent() string {
	c.initialize()
	return c.UserAgentProvider()
}

// GetAuthorizedWithToken performs an authenticated GET request with the given token.
func (c *Client) GetAuthorizedWithToken(ctx context.Context, url, token string) (*http.Response, error) {
	c.initialize()
	return c.doRequestWithToken(ctx, http.MethodGet, url, nil, token)
}

// GetCourses fetches the list of courses for the authenticated user.
func (c *Client) GetCourses(ctx context.Context, cfg *config.Config) (Courses, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}

	if cfg.BaseURL == "" {
		return nil, errors.New("baseUrl is required")
	}

	token, err := c.resolveToken(cfg)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/subjects", cfg.BaseURL)
	resp, err := c.GetAuthorizedWithToken(ctx, url, token)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 512))
		if readErr != nil {
			return nil, fmt.Errorf("subjects request failed with status %d and unreadable body: %w", resp.StatusCode, readErr)
		}
		return nil, fmt.Errorf("subjects request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var courses Courses
	if err := json.NewDecoder(resp.Body).Decode(&courses); err != nil {
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

	if cfg.BaseURL == "" {
		return nil, errors.New("baseUrl is required")
	}

	token, err := c.resolveToken(cfg)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/subjects/%d/lectures/%d", cfg.BaseURL, course.SubjectID, course.SessionID)
	resp, err := c.GetAuthorizedWithToken(ctx, url, token)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 512))
		if readErr != nil {
			return nil, fmt.Errorf("lectures request failed with status %d and unreadable body: %w", resp.StatusCode, readErr)
		}
		return nil, fmt.Errorf("lectures request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var lectures Lectures
	if err := json.NewDecoder(resp.Body).Decode(&lectures); err != nil {
		return nil, fmt.Errorf("failed to decode lectures response: %w", err)
	}

	for i := range lectures {
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

	if cfg.BaseURL == "" {
		return nil, errors.New("baseUrl is required")
	}

	token, err := c.resolveToken(cfg)
	if err != nil {
		return nil, err
	}

	parsedPlaylists := make([]ParsedPlaylist, 0, len(lectures))
	for _, lecture := range lectures {
		streamInfos, err := c.getStreamInfos(ctx, cfg.BaseURL, token, lecture)
		if err != nil {
			return parsedPlaylists, err
		}

		streamURL := SelectStreamByQuality(streamInfos, cfg.Quality, cfg.AudioOnly)
		if streamURL == "" {
			continue
		}

		resp, err := c.GetAuthorizedWithToken(ctx, streamURL, token)
		if err != nil {
			return parsedPlaylists, err
		}
		if resp.StatusCode != http.StatusOK {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 512))
			_ = resp.Body.Close() //nolint:errcheck
			if readErr != nil {
				return parsedPlaylists, fmt.Errorf("playlist request failed with status %d and unreadable body: %w", resp.StatusCode, readErr)
			}
			return parsedPlaylists, fmt.Errorf("playlist request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}

		scanner := bufio.NewScanner(resp.Body)
		parsed, parseErr := parsePlaylist(scanner, streamURL, lecture.TTID, lecture.Topic, lecture.SeqNo)
		_ = resp.Body.Close() //nolint:errcheck
		if parseErr != nil {
			return parsedPlaylists, fmt.Errorf("parse playlist for lecture %d (%s): %w", lecture.TTID, lecture.Topic, parseErr)
		}
		parsedPlaylists = append(parsedPlaylists, parsed)
	}

	return parsedPlaylists, nil
}
