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
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/rabesss/impartus-cli/internal/config"
)

var invalidFileNameRe = regexp.MustCompile(`[<>:"/\\|?*\n\r]`)
var uriValueRe = regexp.MustCompile(`URI="([^"]+)"`)

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

// NewLoggedIn creates a Client and authenticates it against the Impartus API
// using the provided config. It is the shared bootstrap for the CLI's
// initClient and the server's default upstream login, replacing duplicated
// New + LoginAndSetToken sequences.
func NewLoggedIn(ctx context.Context, cfg *config.Config) (*Client, error) {
	c := New(nil, nil)
	if err := c.LoginAndSetToken(ctx, cfg); err != nil {
		return nil, err
	}
	return c, nil
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

func (c *Client) tokenValue() string {
	return c.token
}

func (c *Client) setToken(token string) {
	c.token = token
}

// GetAuthorizedWithToken performs an authenticated GET request with the given token.
func (c *Client) GetAuthorizedWithToken(ctx context.Context, url, token string) (*http.Response, error) {
	c.initialize()
	return c.doRequestWithToken(ctx, http.MethodGet, url, nil, token)
}

// LoginAndSetToken authenticates with the Impartus API and stores the resulting token.
func (c *Client) LoginAndSetToken(ctx context.Context, cfg *config.Config) error {
	cli, baseURL, err := c.prepareLogin(cfg)
	if err != nil {
		return err
	}
	if cli.tryStoredToken(ctx, cfg, baseURL) {
		return nil
	}
	token, err := cli.login(ctx, cfg, baseURL)
	if err != nil {
		return err
	}
	return cli.storeToken(cfg, token)
}

// resolveToken returns the token from config, falling back to the client's stored token.
func (c *Client) resolveToken(cfg *config.Config) (string, error) {
	token := cfg.Token
	if token == "" {
		token = c.tokenValue()
	}
	if token == "" {
		return "", errors.New("token is not set")
	}
	return token, nil
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
		parsed, parseErr := ParsePlaylist(scanner, lecture.TTID, lecture.Topic, lecture.SeqNo)
		_ = resp.Body.Close() //nolint:errcheck
		if parseErr != nil {
			return parsedPlaylists, fmt.Errorf("parse playlist for lecture %d (%s): %w", lecture.TTID, lecture.Topic, parseErr)
		}
		parsedPlaylists = append(parsedPlaylists, parsed)
	}

	return parsedPlaylists, nil
}

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

// ParsePlaylist parses an HLS playlist from the given scanner, extracting chunk URLs and key information.
func ParsePlaylist(scanner *bufio.Scanner, id int, title string, seqNo int) (ParsedPlaylist, error) {
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
		line := scanner.Text()
		if parsedOutput.KeyURL == "" && strings.HasPrefix(line, "#EXT-X-KEY") {
			match := uriValueRe.FindStringSubmatch(line)
			if len(match) == 2 {
				parsedOutput.KeyURL = match[1]
			}
		} else if strings.HasPrefix(line, "#EXTINF:") {
			pendingDuration = parseEXTINFDuration(line)
		} else if strings.HasPrefix(line, "#EXT-X-DISCONTINUITY") {
			isFirstView = false
		} else if !strings.HasPrefix(line, "#EXT") {
			if isFirstView {
				firstViewURLs = append(firstViewURLs, line)
				firstDurations = append(firstDurations, pendingDuration)
			} else {
				secondViewURLs = append(secondViewURLs, line)
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

func (c *Client) readStoredToken() (string, bool) {
	tokenBytes, err := os.ReadFile(".token")
	if err != nil {
		return "", false
	}
	token := strings.TrimSpace(string(tokenBytes))
	if token == "" {
		return "", false
	}
	return token, true
}

func (c *Client) validateStoredToken(ctx context.Context, baseURL, token string) (bool, error) {
	profileURL := fmt.Sprintf("%s/user/profile", baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, profileURL, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("User-Agent", c.userAgent())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck

	return resp.StatusCode == http.StatusOK, nil
}

func (c *Client) prepareLogin(cfg *config.Config) (*Client, string, error) {
	if cfg == nil {
		return nil, "", errors.New("config is required")
	}
	cli := c
	if cli == nil {
		cli = New(nil, nil)
	}
	cli.initialize()
	if cfg.BaseURL == "" {
		return nil, "", errors.New("baseUrl is required")
	}
	return cli, cfg.BaseURL, nil
}

func (c *Client) tryStoredToken(ctx context.Context, cfg *config.Config, baseURL string) bool {
	token, ok := c.readStoredToken()
	if !ok {
		return false
	}
	valid, err := c.validateStoredToken(ctx, baseURL, token)
	if err != nil || !valid {
		return false
	}
	cfg.Token = token
	c.setToken(token)
	return true
}

func (c *Client) login(ctx context.Context, cfg *config.Config, baseURL string) (string, error) {
	req, err := c.newLoginRequest(ctx, cfg, baseURL)
	if err != nil {
		return "", err
	}
	response, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("login failed: %w", err)
	}
	defer func() { _ = response.Body.Close() }() //nolint:errcheck
	if err := validateLoginResponse(response); err != nil {
		return "", err
	}
	var loginResponse LoginResponse
	if err := json.NewDecoder(response.Body).Decode(&loginResponse); err != nil {
		return "", fmt.Errorf("failed to decode login response: %w", err)
	}
	if loginResponse.Token == "" {
		return "", errors.New("empty token in login response")
	}
	return loginResponse.Token, nil
}

func (c *Client) newLoginRequest(ctx context.Context, cfg *config.Config, baseURL string) (*http.Request, error) {
	requestBody, err := json.Marshal(map[string]string{"username": cfg.Username, "password": cfg.Password})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal login body: %w", err)
	}
	loginURL := fmt.Sprintf("%s/auth/signin", baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginURL, bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Referer", "https://bitshyd.impartus.com/login/")
	req.Header.Set("User-Agent", c.userAgent())
	return req, nil
}

func validateLoginResponse(response *http.Response) error {
	if response.StatusCode == http.StatusUnauthorized {
		return errors.New("wrong credentials please retry")
	}
	if response.StatusCode == http.StatusOK {
		return nil
	}
	body, readErr := io.ReadAll(response.Body)
	if readErr != nil {
		return fmt.Errorf("login failed with status %d and unreadable body: %w", response.StatusCode, readErr)
	}
	return fmt.Errorf("login failed with status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
}

func (c *Client) storeToken(cfg *config.Config, token string) error {
	cfg.Token = token
	c.setToken(token)
	if err := os.WriteFile(".token", []byte(token), 0o600); err != nil {
		return fmt.Errorf("failed to persist token: %w", err)
	}
	if err := os.Chmod(".token", 0o600); err != nil {
		return fmt.Errorf("failed to enforce .token permissions: %w", err)
	}
	return nil
}
