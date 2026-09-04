package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/secrets"
)

const maxLoginResponseSize int64 = 1 * 1024 * 1024

// NewLoggedIn creates a Client and authenticates it against the Impartus API
// using the provided config. It is the shared bootstrap for the CLI's
// initClient and the server's default upstream login, replacing duplicated
// New + LoginAndSetToken sequences.
func NewLoggedIn(ctx context.Context, cfg *config.Config) (*Client, error) {
	c, err := newClientFromConfig(cfg)
	if err != nil {
		return nil, err
	}
	if err := c.LoginAndSetToken(ctx, cfg); err != nil {
		return nil, err
	}
	return c, nil
}

func newClientFromConfig(cfg *config.Config) (*Client, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}

	var timeout time.Duration
	if cfg.HTTPTimeout != "" {
		parsedTimeout, err := time.ParseDuration(cfg.HTTPTimeout)
		if err != nil {
			return nil, fmt.Errorf("invalid httpTimeout: %w", err)
		}
		timeout = parsedTimeout
	}

	return New(NewHTTPClient(timeout), nil), nil
}

func (c *Client) tokenValue() string {
	if c == nil {
		return ""
	}
	c.tokenMu.RLock()
	defer c.tokenMu.RUnlock()
	return c.token
}

func (c *Client) setToken(token string) {
	if c == nil {
		return
	}
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	c.token = token
}

// LoginAndSetToken authenticates with the Impartus API and stores the resulting
// token. It also updates cfg.Token for compatibility; cfg remains caller-owned
// and must not be read or mutated concurrently by code that bypasses Client's
// synchronized token access.
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

// resolveToken returns the token from config, falling back to the client's
// synchronized stored token. A config token is treated as an immutable caller
// value while requests are in flight.
func (c *Client) resolveToken(cfg *config.Config) (string, error) {
	c.tokenMu.RLock()
	token := cfg.Token
	if token == "" {
		token = c.token
	}
	c.tokenMu.RUnlock()
	if token == "" {
		return "", errors.New("token is not set")
	}
	return token, nil
}

func (c *Client) setActiveToken(cfg *config.Config, token string) {
	if c == nil {
		return
	}
	c.tokenMu.Lock()
	c.token = token
	if cfg != nil {
		cfg.Token = token
	}
	c.tokenMu.Unlock()
}

func (c *Client) readStoredToken() (string, bool) {
	return c.readStoredTokenAt(config.DefaultTokenCachePath)
}

func (c *Client) readStoredTokenAt(path string) (string, bool) {
	tokenBytes, err := readTokenCache(path)
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
	resp, err := c.GetAuthorizedWithTokenForOrigins(ctx, profileURL, token, baseURL)
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
	baseURL, err := validateAndCanonicalizeBaseURL(cfg.BaseURL)
	if err != nil {
		return nil, "", err
	}
	if _, err := newMediaOriginPolicy(append([]string{baseURL}, cfg.MediaOrigins...)...); err != nil {
		return nil, "", err
	}
	return cli, baseURL, nil
}

// validateAndCanonicalizeBaseURL applies the upstream URL boundary that
// Config.Validate cannot guarantee for callers that invoke client methods
// directly. API and login URLs must never carry query or fragment credentials,
// and rejecting them before request construction keeps unsafe values out of
// HTTP errors and redirect metadata.
func validateAndCanonicalizeBaseURL(rawBaseURL string) (string, error) {
	parsed, err := url.Parse(rawBaseURL)
	if err != nil {
		return "", newMediaOriginError(ErrInvalidMediaURL)
	}
	if err := validateMediaURL(parsed, true); err != nil {
		return "", err
	}
	if parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawFragment != "" {
		return "", newMediaOriginError(ErrInvalidMediaURL)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	return parsed.String(), nil
}

func newBaseURLPolicy(rawBaseURL string, additionalOrigins ...string) (string, mediaOriginPolicy, error) {
	if rawBaseURL == "" {
		return "", mediaOriginPolicy{}, errors.New("baseUrl is required")
	}
	baseURL, err := validateAndCanonicalizeBaseURL(rawBaseURL)
	if err != nil {
		return "", mediaOriginPolicy{}, err
	}
	origins := append([]string{baseURL}, additionalOrigins...)
	policy, err := newMediaOriginPolicy(origins...)
	if err != nil {
		return "", mediaOriginPolicy{}, err
	}
	return baseURL, policy, nil
}

func (c *Client) tryStoredToken(ctx context.Context, cfg *config.Config, baseURL string) bool {
	token, ok := c.readStoredTokenAt(resolvedTokenCachePath(cfg))
	if !ok {
		return false
	}
	valid, err := c.validateStoredToken(ctx, baseURL, token)
	if err != nil || !valid {
		return false
	}
	c.setActiveToken(cfg, token)
	return true
}

func (c *Client) login(ctx context.Context, cfg *config.Config, baseURL string) (string, error) {
	c.initialize()
	req, err := c.newLoginRequest(ctx, cfg, baseURL)
	if err != nil {
		return "", err
	}
	requestClient := *c.httpClient
	requestClient.Jar = nil
	requestClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		// Login requests carry the username and password in the body. Never
		// replay that body to a redirected origin or across an HTTPS downgrade.
		return http.ErrUseLastResponse
	}
	response, err := requestClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("login failed: %w", secrets.SanitizeError(err))
	}
	defer func() { _ = response.Body.Close() }() //nolint:errcheck
	if responseErr := validateLoginResponse(response); responseErr != nil {
		return "", responseErr
	}
	body, readErr := readResponseBodyWithLimit(response.Body, maxLoginResponseSize)
	if readErr != nil {
		if errors.Is(readErr, errResponseSizeLimit) {
			return "", fmt.Errorf("login response exceeds max size %d bytes", maxLoginResponseSize)
		}
		return "", fmt.Errorf("failed to read login response: %w", secrets.SanitizeError(readErr))
	}
	var loginResponse LoginResponse
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&loginResponse); err != nil {
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
		return &AuthenticationError{Operation: "login", StatusCode: response.StatusCode}
	}
	if response.StatusCode == http.StatusOK {
		return nil
	}
	return fmt.Errorf("login failed with status %d", response.StatusCode)
}

func (c *Client) storeToken(cfg *config.Config, token string) error {
	c.setActiveToken(cfg, token)
	path := resolvedTokenCachePath(cfg)
	if err := writeTokenCache(path, []byte(token)); err != nil {
		return fmt.Errorf("failed to persist token cache: %w", err)
	}
	return nil
}
