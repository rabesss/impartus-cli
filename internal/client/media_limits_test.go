package client

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/config"
)

func TestGetCoursesRejectsOversizedCatalogResponse(t *testing.T) {
	transport := &fixedResponseTransport{body: strings.Repeat("x", int(maxCatalogResponseSize)+1)}
	c := New(&http.Client{Transport: transport}, nil)

	_, err := c.GetCourses(context.Background(), &config.Config{
		BaseURL: "https://api.example.test",
		Token:   "bearer-secret",
	})
	if err == nil || !strings.Contains(err.Error(), "response exceeds max size") {
		t.Fatalf("GetCourses() error = %v, want catalog response size limit", err)
	}
}

func TestGetPlaylistRejectsOversizedResponse(t *testing.T) {
	transport := &fixedResponseTransport{body: "#EXTM3U\n" + strings.Repeat("#EXT-X-VERSION:3\n", int(maxPlaylistResponseSize/16)+1)}
	c := New(&http.Client{Transport: transport}, nil)

	_, err := c.getPlaylist(context.Background(), "https://cdn.example.test/playlist.m3u8", "bearer-secret", Lecture{TTID: 42})
	if err == nil || !strings.Contains(err.Error(), "playlist response exceeds max size") {
		t.Fatalf("getPlaylist() error = %v, want playlist response size limit", err)
	}
}

type fixedResponseTransport struct {
	body string
}

func (t *fixedResponseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(t.body)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}
