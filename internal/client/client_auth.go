package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rabesss/impartus-cli/internal/config"
)

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
	return c.token
}

func (c *Client) setToken(token string) {
	c.token = token
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
