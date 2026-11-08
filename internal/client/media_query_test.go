package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/rabesss/impartus-cli/internal/config"
)

func TestParseStreamInfosAcceptsSemicolonQuery(t *testing.T) {
	infos, err := ParseStreamInfosFromBody([]byte("https://media.example.test/hls/1280x720/index.m3u8?v=2;x=1\n"))
	if err != nil {
		t.Fatalf("ParseStreamInfosFromBody() error = %v", err)
	}
	if len(infos) != 1 || infos[0].URL != "https://media.example.test/hls/1280x720/index.m3u8?v=2;x=1" {
		t.Fatalf("ParseStreamInfosFromBody() = %+v, want the ';' stream kept byte-for-byte", infos)
	}
}

func TestParseStreamInfosRejectsMalformedSemicolonQuery(t *testing.T) {
	infos, err := ParseStreamInfosFromBody([]byte("https://media.example.test/hls/1280x720/index.m3u8?v=2;x=%zz\n"))
	if !errors.Is(err, ErrNoStreams) {
		t.Fatalf("ParseStreamInfosFromBody() error = %v, want ErrNoStreams", err)
	}
	if len(infos) != 0 {
		t.Fatalf("ParseStreamInfosFromBody() = %+v, want a malformed escape rejected", infos)
	}
}

func TestGetPlaylistsKeepsSemicolonStreamURL(t *testing.T) {
	var (
		mu         sync.Mutex
		seenQuery  string
		serverURL  string
		playlistOK bool
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fetchvideo":
			_, _ = io.WriteString(w, serverURL+"/1280x720/index.m3u8?v=2;x=1\n") //nolint:errcheck
		case "/1280x720/index.m3u8":
			mu.Lock()
			seenQuery = r.URL.RawQuery
			playlistOK = true
			mu.Unlock()
			_, _ = io.WriteString(w, "#EXTM3U\n#EXTINF:10.0,\nseg0.ts\n#EXT-X-ENDLIST\n") //nolint:errcheck
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	playlists, err := New(server.Client(), nil).GetPlaylists(
		context.Background(),
		&config.Config{BaseURL: server.URL, Token: "request-token", Quality: "720"},
		Lectures{{TTID: 42, Topic: "Semicolon", SeqNo: 1}},
	)
	if err != nil {
		t.Fatalf("GetPlaylists() error = %v", err)
	}
	if len(playlists) != 1 {
		t.Fatalf("GetPlaylists() = %d playlists, want 1", len(playlists))
	}
	mu.Lock()
	defer mu.Unlock()
	if !playlistOK || seenQuery != "v=2;x=1" {
		t.Fatalf("playlist request query = %q (sent %v), want %q", seenQuery, playlistOK, "v=2;x=1")
	}
}

func TestGetAuthorizedForwardsSemicolonQuery(t *testing.T) {
	const token = "secret-abc-bearer"
	for _, configured := range []bool{false, true} {
		name := "unconfigured origin"
		if configured {
			name = "configured origin"
		}
		t.Run(name, func(t *testing.T) {
			var seen *http.Request
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seen = r.Clone(context.Background())
			}))
			defer server.Close()
			origins := []string{"https://api.example.test"}
			if configured {
				origins = append(origins, server.URL)
			}

			resp, err := New(server.Client(), nil).GetAuthorizedWithTokenForOrigins(context.Background(), server.URL+"/seg.ts?v=2;x=1", token, origins...)
			if err != nil {
				t.Fatalf("GetAuthorizedWithTokenForOrigins() error = %v", err)
			}
			_ = resp.Body.Close() //nolint:errcheck
			if seen == nil || seen.URL.RawQuery != "v=2;x=1" {
				t.Fatalf("server saw %v, want query %q forwarded unchanged", seen, "v=2;x=1")
			}
			if got := seen.Header.Get("Authorization") != ""; got != configured {
				t.Fatalf("Authorization sent = %v, want %v", got, configured)
			}
		})
	}
}

func TestGetAuthorizedStripsSemicolonSeparatedBearer(t *testing.T) {
	const token = "secret-abc-bearer"
	var seen *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Clone(context.Background())
	}))
	defer server.Close()

	for _, query := range []string{"token=" + token + ";v=2", "v=2;token=" + token, "v=2;sig=abc&token=" + token} {
		t.Run(query, func(t *testing.T) {
			seen = nil
			resp, err := New(server.Client(), nil).GetAuthorizedWithTokenForOrigins(context.Background(), server.URL+"/seg.ts?"+query, token, "https://api.example.test")
			if err != nil {
				t.Fatalf("GetAuthorizedWithTokenForOrigins() error = %v", err)
			}
			_ = resp.Body.Close() //nolint:errcheck
			if seen == nil {
				t.Fatal("request was not sent")
			}
			if strings.Contains(seen.URL.RawQuery, token) || seen.Header.Get("Authorization") != "" {
				t.Fatalf("unconfigured origin received the bearer: query %q", seen.URL.RawQuery)
			}
			if !strings.Contains(seen.URL.RawQuery, "v=2") {
				t.Fatalf("unrelated parameter was dropped: query %q", seen.URL.RawQuery)
			}
		})
	}
}

func TestGetAuthorizedRejectsMalformedSemicolonQuery(t *testing.T) {
	transport := &mediaOriginCaptureTransport{}
	resp, err := New(&http.Client{Transport: transport}, nil).GetAuthorizedWithTokenForOrigins(
		context.Background(), "https://media.example.test/seg.ts?v=2;x=%zz", "secret-abc-bearer")
	if resp != nil {
		_ = resp.Body.Close() //nolint:errcheck
	}
	if err == nil || !errors.Is(err, ErrMediaOrigin) {
		t.Fatalf("GetAuthorizedWithTokenForOrigins() error = %v, want ErrMediaOrigin", err)
	}
	if transport.request != nil {
		t.Fatal("malformed query was sent")
	}
}

func TestHasCredentialQueryTreatsSemicolonAsSeparator(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://media.example.test/seg.ts?v=2;x=1":     false,
		"https://media.example.test/seg.ts?v=2;token=x": true,
		"https://media.example.test/seg.ts?v=2;x=%zz":   true,
	} {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := hasCredentialQuery(parsed); got != want {
			t.Errorf("hasCredentialQuery(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestStripBearerQueryParamsKeepsSeparators(t *testing.T) {
	const token = "secret-abc-bearer"
	tests := map[string]string{
		"v=2;x=1":                     "v=2;x=1",
		"token=" + token + ";v=2;x=1": "v=2;x=1",
		"v=2;token=" + token + "&x=1": "v=2&x=1",
		"v=2&x=1;token=" + token:      "v=2&x=1",
		"sig=a%3Bb;token=" + token:    "sig=a%3Bb",
	}
	for query, want := range tests {
		got, err := stripBearerQueryParams("https://media.example.test/seg.ts?"+query, token)
		if err != nil {
			t.Fatalf("stripBearerQueryParams(%q) error = %v", query, err)
		}
		if got != "https://media.example.test/seg.ts?"+want {
			t.Errorf("stripBearerQueryParams(%q) = %q, want query %q", query, got, want)
		}
	}
}
