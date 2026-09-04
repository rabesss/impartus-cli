package client

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/config"
)

const (
	responseLimitCatalogBytes  = 10 * 1024 * 1024
	responseLimitStreamBytes   = 1 * 1024 * 1024
	responseLimitPlaylistBytes = 10 * 1024 * 1024
	responseLimitLoginBytes    = 1 * 1024 * 1024
)

func TestCatalogResponsesRejectOversizedCoursesAndLectures(t *testing.T) {
	for _, test := range []struct {
		name string
		call func(*Client) error
	}{
		{
			name: "courses",
			call: func(c *Client) error {
				_, err := c.GetCourses(context.Background(), &config.Config{
					BaseURL: "https://api.example.test",
					Token:   "catalog-token",
				})
				return err
			},
		},
		{
			name: "lectures",
			call: func(c *Client) error {
				_, err := c.GetLectures(context.Background(), &config.Config{
					BaseURL: "https://api.example.test",
					Token:   "catalog-token",
				}, Course{SubjectID: 7, SessionID: 9})
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := New(&http.Client{Transport: &limitResponseTransport{
				body: strings.Repeat("x", responseLimitCatalogBytes+1),
			}}, nil)
			err := test.call(c)
			if err == nil || !strings.Contains(err.Error(), "response exceeds max size") {
				t.Fatalf("%s error = %v, want catalog response size limit", test.name, err)
			}
		})
	}
}

func TestCatalogResponseKeepsDecoderTrailingDataCompatibility(t *testing.T) {
	c := New(&http.Client{Transport: &limitResponseTransport{
		body: `[{"subjectName":"Course"}] trailing bytes`,
	}}, nil)

	courses, err := c.GetCourses(context.Background(), &config.Config{
		BaseURL: "https://api.example.test",
		Token:   "catalog-token",
	})
	if err != nil {
		t.Fatalf("GetCourses() error = %v, want prior decoder compatibility", err)
	}
	if len(courses) != 1 || courses[0].SubjectName != "Course" {
		t.Fatalf("GetCourses() = %+v, want one decoded course", courses)
	}
}

func TestStreamInfoResponseRejectsOversizedBody(t *testing.T) {
	c := New(&http.Client{Transport: &limitResponseTransport{
		body: strings.Repeat("x", responseLimitStreamBytes+1),
	}}, nil)

	_, err := c.getStreamInfos(context.Background(), "https://api.example.test", "stream-token", Lecture{TTID: 42})
	if err == nil || !strings.Contains(err.Error(), "response exceeds max size") {
		t.Fatalf("getStreamInfos() error = %v, want stream-info response size limit", err)
	}
}

func TestLoginResponseRejectsOversizedBody(t *testing.T) {
	c := New(&http.Client{Transport: &limitResponseTransport{
		body: strings.Repeat("x", responseLimitLoginBytes+1),
	}}, nil)

	token, err := c.login(context.Background(), &config.Config{
		Username: "user",
		Password: "password",
	}, "https://api.example.test")
	if token != "" {
		t.Fatalf("login token = %q, want empty on oversized response", token)
	}
	if err == nil || !strings.Contains(err.Error(), "login response exceeds max size") {
		t.Fatalf("login error = %v, want login response size limit", err)
	}
}

func TestLoginResponseKeepsDecoderTrailingDataCompatibility(t *testing.T) {
	c := New(&http.Client{Transport: &limitResponseTransport{
		body: `{"token":"login-token"} trailing bytes`,
	}}, nil)

	token, err := c.login(context.Background(), &config.Config{
		Username: "user",
		Password: "password",
	}, "https://api.example.test")
	if err != nil {
		t.Fatalf("login() error = %v, want prior decoder compatibility", err)
	}
	if token != "login-token" {
		t.Fatalf("login token = %q, want login-token", token)
	}
}

func TestPlaylistResponseRejectsOversizedBody(t *testing.T) {
	c := New(&http.Client{Transport: &limitResponseTransport{
		body: "#EXTM3U\n" + strings.Repeat("#EXT-X-VERSION:3\n", responseLimitPlaylistBytes/16+1),
	}}, nil)

	_, err := c.getPlaylist(context.Background(), "https://media.example.test/playlist.m3u8", "playlist-token", Lecture{TTID: 42})
	if err == nil || !strings.Contains(err.Error(), "playlist response exceeds max size") {
		t.Fatalf("getPlaylist() error = %v, want playlist response size limit", err)
	}
}

func TestPlaylistResponseAcceptsExactTotalLimit(t *testing.T) {
	body := strings.Repeat("#\n", responseLimitPlaylistBytes/2)
	if len(body) != responseLimitPlaylistBytes {
		t.Fatalf("test playlist length = %d, want %d", len(body), responseLimitPlaylistBytes)
	}
	c := New(&http.Client{Transport: &limitResponseTransport{body: body}}, nil)

	if _, err := c.getPlaylist(context.Background(), "https://media.example.test/playlist.m3u8", "playlist-token", Lecture{TTID: 42}); err != nil {
		t.Fatalf("getPlaylist() error = %v, want exact total-limit response to succeed", err)
	}
}

func TestPlaylistLineBelowNewLimitRemainsSupported(t *testing.T) {
	longURL := "https://media.example.test/" + strings.Repeat("a", 100*1024)
	c := New(&http.Client{Transport: &limitResponseTransport{
		body: "#EXTM3U\n" + longURL + "\n",
	}}, nil)

	playlist, err := c.getPlaylist(context.Background(), "https://media.example.test/playlist.m3u8", "playlist-token", Lecture{TTID: 42})
	if err != nil {
		t.Fatalf("getPlaylist() error = %v, want line below 1 MiB to remain supported", err)
	}
	if len(playlist.FirstViewURLs) != 1 || playlist.FirstViewURLs[0] != longURL {
		t.Fatalf("playlist segments = %d, want one preserved long URL", len(playlist.FirstViewURLs))
	}
}

func TestPlaylistLineLimitCountsPayloadBytes(t *testing.T) {
	prefix := "https://media.example.test/"
	for _, test := range []struct {
		name      string
		lineBytes int
		delimiter string
		wantErr   bool
	}{
		{name: "exact payload limit with LF", lineBytes: maxPlaylistLineSize, delimiter: "\n", wantErr: false},
		{name: "one byte over payload limit with LF", lineBytes: maxPlaylistLineSize + 1, delimiter: "\n", wantErr: true},
		{name: "exact payload limit with CRLF", lineBytes: maxPlaylistLineSize, delimiter: "\r\n", wantErr: false},
		{name: "one byte over payload limit with CRLF", lineBytes: maxPlaylistLineSize + 1, delimiter: "\r\n", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			line := prefix + strings.Repeat("a", test.lineBytes-len(prefix))
			c := New(&http.Client{Transport: &limitResponseTransport{
				body: "#EXTM3U\n" + line + test.delimiter,
			}}, nil)

			playlist, err := c.getPlaylist(context.Background(), "https://media.example.test/playlist.m3u8", "playlist-token", Lecture{TTID: 42})
			if test.wantErr {
				if err == nil || (!strings.Contains(err.Error(), "token too long") && !strings.Contains(err.Error(), "line exceeds max size")) {
					t.Fatalf("getPlaylist() error = %v, want line-size rejection", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("getPlaylist() error = %v, want exact payload-limit line to succeed", err)
			}
			if len(playlist.FirstViewURLs) != 1 || playlist.FirstViewURLs[0] != line {
				t.Fatalf("playlist segments = %d, want exact-limit line preserved", len(playlist.FirstViewURLs))
			}
		})
	}
}

func TestPlaylistParserRejectsExcessiveSegmentCount(t *testing.T) {
	var body strings.Builder
	body.WriteString("#EXTM3U\n")
	for i := 0; i < 10001; i++ {
		body.WriteString("segment.ts\n")
	}

	scanner := bufio.NewScanner(strings.NewReader(body.String()))
	_, err := parsePlaylist(scanner, "https://media.example.test/playlist.m3u8", 1, "Lecture", 1)
	if err == nil || !strings.Contains(err.Error(), "too many media segments") {
		t.Fatalf("parsePlaylist() error = %v, want segment-count limit", err)
	}
}

func TestPlaylistParserAcceptsExactSegmentCount(t *testing.T) {
	var body strings.Builder
	for i := 0; i < maxPlaylistSegments; i++ {
		body.WriteString("segment.ts\n")
	}

	scanner := bufio.NewScanner(strings.NewReader(body.String()))
	playlist, err := parsePlaylist(scanner, "https://media.example.test/playlist.m3u8", 1, "Lecture", 1)
	if err != nil {
		t.Fatalf("parsePlaylist() error = %v, want exact segment-count limit to succeed", err)
	}
	if len(playlist.FirstViewURLs) != maxPlaylistSegments {
		t.Fatalf("playlist segment count = %d, want %d", len(playlist.FirstViewURLs), maxPlaylistSegments)
	}
}

type limitResponseTransport struct {
	body string
}

func (t *limitResponseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(t.body)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}
