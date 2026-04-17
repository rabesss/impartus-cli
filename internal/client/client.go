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
	"strings"

	"github.com/rabesss/impartus-cli/internal/config"
)

var invalidFileNameRe = regexp.MustCompile(`[<>:"/\\|?*\n\r]`)
var uriValueRe = regexp.MustCompile(`URI="([^"]+)"`)

type Client struct {
	httpClient        *http.Client
	UserAgentProvider func() string
	token             string
}

func New(httpClient *http.Client, userAgentProvider func() string) *Client {
	if httpClient == nil {
		httpClient = NewHTTPClient(0)
	}
	if userAgentProvider == nil {
		userAgentProvider = func() string { return "impartus-downloader" }
	}

	return &Client{
		httpClient:        httpClient,
		UserAgentProvider: userAgentProvider,
	}
}

func (c *Client) initialize() {
	if c.httpClient == nil {
		c.httpClient = NewHTTPClient(0)
	}
	if c.UserAgentProvider == nil {
		c.UserAgentProvider = func() string { return "impartus-downloader" }
	}
}

func (c *Client) randomUserAgent() string {
	c.initialize()
	return c.UserAgentProvider()
}

func (c *Client) Token() string {
	if c == nil {
		return ""
	}
	return c.token
}

func (c *Client) SetToken(token string) {
	if c == nil {
		return
	}
	c.token = token
}

func (c *Client) GetAuthorizedWithToken(ctx context.Context, url string, token string) (*http.Response, error) {
	cli := c
	if cli == nil {
		cli = New(nil, nil)
	}
	cli.initialize()

	return cli.doRequestWithToken(ctx, http.MethodGet, url, nil, token)
}

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
		token = c.Token()
	}
	if token == "" {
		return "", errors.New("token is not set")
	}
	return token, nil
}

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
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
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
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
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
		streamInfos, err := c.GetStreamInfos(ctx, cfg.BaseURL, token, lecture)
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
			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr != nil {
				return parsedPlaylists, fmt.Errorf("playlist request failed with status %d and unreadable body: %w", resp.StatusCode, readErr)
			}
			return parsedPlaylists, fmt.Errorf("playlist request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}

		scanner := bufio.NewScanner(resp.Body)
		parsedPlaylists = append(parsedPlaylists, ParsePlaylist(scanner, lecture.TTID, lecture.Topic, lecture.SeqNo))
		resp.Body.Close()
	}

	return parsedPlaylists, nil
}

func (c *Client) GetStreamInfos(ctx context.Context, baseURL, token string, lecture Lecture) ([]StreamInfo, error) {
	uri := fmt.Sprintf("%s/fetchvideo?ttid=%d&token=%s&type=index.m3u8", baseURL, lecture.TTID, token)
	resp, err := c.GetAuthorizedWithToken(ctx, uri, token)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, fmt.Errorf("stream info request failed with status %d and unreadable body: %w", resp.StatusCode, readErr)
		}
		return nil, fmt.Errorf("stream info request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return ParseStreamInfosFromBody(body)
}

func ParsePlaylist(scanner *bufio.Scanner, id int, title string, seqNo int) ParsedPlaylist {
	parsedOutput := ParsedPlaylist{
		ID:    id,
		Title: title,
		SeqNo: seqNo,
	}

	isFirstView := true
	firstViewURLs := make([]string, 0)
	secondViewURLs := make([]string, 0)

	for scanner.Scan() {
		line := scanner.Text()
		if parsedOutput.KeyURL == "" && strings.HasPrefix(line, "#EXT-X-KEY") {
			match := uriValueRe.FindStringSubmatch(line)
			if len(match) == 2 {
				parsedOutput.KeyURL = match[1]
			}
		} else if strings.HasPrefix(line, "#EXT-X-DISCONTINUITY") {
			isFirstView = false
		} else if !strings.HasPrefix(line, "#EXT") {
			if isFirstView {
				firstViewURLs = append(firstViewURLs, line)
			} else {
				secondViewURLs = append(secondViewURLs, line)
			}
		}
	}

	parsedOutput.FirstViewURLs = firstViewURLs
	if !isFirstView {
		parsedOutput.HasMultipleViews = true
		parsedOutput.SecondViewURLs = secondViewURLs
	}

	return parsedOutput
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
	req.Header.Set("User-Agent", c.randomUserAgent())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

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
	c.SetToken(token)
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
	defer response.Body.Close()
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
	req.Header.Set("User-Agent", c.randomUserAgent())
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
	c.SetToken(token)
	if err := os.WriteFile(".token", []byte(token), 0o600); err != nil {
		return fmt.Errorf("failed to persist token: %w", err)
	}
	return nil
}
