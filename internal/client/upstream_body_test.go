package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/config"
)

const credentialBearingUpstreamBody = "Authorization: Bearer body-bearer-secret\n" +
	"URL: https://media.example.test/chunk.ts?token=body-query-secret\n" +
	"URL: https://user:body-userinfo-secret@media.example.test/chunk.ts\n" +
	"Cookie: session=body-cookie-secret\n"

func TestNonUnauthorizedUpstreamBodiesAreSanitized(t *testing.T) {
	tests := []struct {
		name string
		call func(*Client) error
	}{
		{
			name: "courses",
			call: func(c *Client) error {
				_, err := c.GetCourses(context.Background(), &config.Config{
					BaseURL: "https://api.example.test",
					Token:   "request-token",
				})
				return err
			},
		},
		{
			name: "lectures",
			call: func(c *Client) error {
				_, err := c.GetLectures(context.Background(), &config.Config{
					BaseURL: "https://api.example.test",
					Token:   "request-token",
				}, Course{SubjectID: 7, SessionID: 9})
				return err
			},
		},
		{
			name: "stream info",
			call: func(c *Client) error {
				_, err := c.getStreamInfos(context.Background(), "https://api.example.test", "request-token", Lecture{TTID: 42})
				return err
			},
		},
		{
			name: "playlist",
			call: func(c *Client) error {
				_, err := c.getPlaylist(context.Background(), "https://media.example.test/playlist.m3u8", "request-token", Lecture{TTID: 42})
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := New(&http.Client{Transport: &statusBodyTransport{
				statusCode: http.StatusBadGateway,
				body:       credentialBearingUpstreamBody,
			}}, nil)
			err := test.call(c)
			if err == nil {
				t.Fatal("call error = nil, want non-401 upstream failure")
			}
			for _, secret := range []string{
				"body-bearer-secret",
				"body-query-secret",
				"body-userinfo-secret",
				"body-cookie-secret",
			} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("call error leaked %q: %v", secret, err)
				}
			}
			if !strings.Contains(err.Error(), "REDACTED") {
				t.Fatalf("call error = %v, want sanitized body marker", err)
			}
		})
	}
}

func TestGetAuthorizedWithTokenRedactsUnknownQueryTokenFromNetworkError(t *testing.T) {
	const token = "network-secret+a&b"
	rawURL := "https://api.example.test/segment?unknown=" + url.QueryEscape("Bearer "+token)
	transportError := errors.New("dial failed for " + rawURL)
	c := New(&http.Client{Transport: &errorTransport{err: transportError}}, nil)

	resp, err := c.GetAuthorizedWithToken(context.Background(), rawURL, token)
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if err == nil {
		t.Fatal("GetAuthorizedWithToken() error = nil, want network failure")
	}
	for _, secret := range []string{token, strings.TrimPrefix(token, "Bearer "), url.QueryEscape(token), url.QueryEscape("Bearer " + token)} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("network error leaked %q: %v", secret, err)
		}
	}
	if !strings.Contains(err.Error(), "REDACTED") {
		t.Fatalf("network error = %v, want redaction marker", err)
	}
}

func TestSuccessfulUpstreamBodyReadErrorsAreSanitized(t *testing.T) {
	const token = "successful-read-secret"
	tests := []struct {
		name string
		call func(*Client) error
	}{
		{
			name: "courses",
			call: func(c *Client) error {
				_, err := c.GetCourses(context.Background(), &config.Config{BaseURL: "https://api.example.test", Token: token})
				return err
			},
		},
		{
			name: "lectures",
			call: func(c *Client) error {
				_, err := c.GetLectures(context.Background(), &config.Config{BaseURL: "https://api.example.test", Token: token}, Course{SubjectID: 7, SessionID: 9})
				return err
			},
		},
		{
			name: "stream info",
			call: func(c *Client) error {
				_, err := c.getStreamInfos(context.Background(), "https://api.example.test", token, Lecture{TTID: 42})
				return err
			},
		},
		{
			name: "playlist",
			call: func(c *Client) error {
				_, err := c.getPlaylist(context.Background(), "https://media.example.test/playlist.m3u8", token, Lecture{TTID: 42})
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := New(&http.Client{Transport: &readErrorTransport{
				err: errors.New("read failed unknown=" + token),
			}}, nil)
			err := test.call(c)
			if err == nil {
				t.Fatal("call error = nil, want read failure")
			}
			if strings.Contains(err.Error(), token) {
				t.Fatalf("call error leaked token: %v", err)
			}
			if !strings.Contains(err.Error(), "REDACTED") {
				t.Fatalf("call error = %v, want redaction marker", err)
			}
		})
	}
}

type statusBodyTransport struct {
	statusCode int
	body       string
}

type errorTransport struct {
	err error
}

func (t *errorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, t.err
}

type readErrorTransport struct {
	err error
}

func (t *readErrorTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(errorReader{err: t.err}),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

type errorReader struct {
	err error
}

func (r errorReader) Read([]byte) (int, error) {
	return 0, r.err
}

func (t *statusBodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: t.statusCode,
		Body:       io.NopCloser(strings.NewReader(t.body)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}
