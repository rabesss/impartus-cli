package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rabesss/impartus-cli/internal/secrets"
)

const defaultHTTPTimeout = 10 * time.Minute

// NewHTTPClient creates a new HTTP client with sensible defaults and the given timeout.
func NewHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = defaultHTTPTimeout
	}

	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
			DisableCompression:  false,
		},
	}
}

func (c *Client) doRequestWithToken(ctx context.Context, method, url string, body io.Reader, token string) (*http.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		// Redact the URL and sanitize the error: malformed/tokenized URLs can
		// surface in the parse error, which may carry query tokens.
		return nil, fmt.Errorf("failed to create http request for %s %s: %w", method, secrets.RedactURL(url), secrets.SanitizeError(err))
	}

	if token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	}
	req.Header.Set("User-Agent", c.userAgent())
	req.Header.Set("Accept", "application/json, text/plain, */*")
	if body != nil {
		req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	}

	response, err := c.httpClient.Do(req)
	if err != nil {
		// http.Client.Do returns a *url.Error whose Error() embeds the full
		// request URL (including query tokens). Sanitize it before wrapping so
		// the token can never reach logs via %w/%v on this error.
		return nil, fmt.Errorf("request failed with error %w for %s %s", secrets.SanitizeError(err), method, secrets.RedactURL(url))
	}

	return response, nil
}
