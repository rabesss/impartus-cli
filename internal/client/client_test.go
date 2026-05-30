package client

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/config"
)

// TestNew tests Client construction with various inputs
func TestNew(t *testing.T) {
	tests := []struct {
		name              string
		httpClient        *http.Client
		userAgentProvider func() string
		wantHTTPClientNil bool
		wantUserAgent     string
	}{
		{
			name:              "nil httpClient uses default",
			httpClient:        nil,
			userAgentProvider: nil,
			wantHTTPClientNil: false,
			wantUserAgent:     "impartus-downloader",
		},
		{
			name:              "with valid httpClient",
			httpClient:        &http.Client{},
			userAgentProvider: nil,
			wantHTTPClientNil: false,
			wantUserAgent:     "impartus-downloader",
		},
		{
			name:       "with custom userAgentProvider",
			httpClient: nil,
			userAgentProvider: func() string {
				return "custom-agent"
			},
			wantHTTPClientNil: false,
			wantUserAgent:     "custom-agent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := New(tt.httpClient, tt.userAgentProvider)
			if c == nil {
				t.Fatal("New returned nil")
			}
			if c.httpClient == nil && !tt.wantHTTPClientNil {
				t.Error("httpClient is nil but didn't expect it")
			}
			if c.httpClient != nil && tt.wantHTTPClientNil {
				t.Error("httpClient is not nil but expected it")
			}
			if got := c.userAgent(); got != tt.wantUserAgent {
				t.Errorf("userAgent() = %q, want %q", got, tt.wantUserAgent)
			}
		})
	}
}

// TestClient_tokenValue tests the Token getter with nil and valid receivers
func TestClient_tokenValue(t *testing.T) {
	tests := []struct {
		name   string
		client *Client
		want   string
	}{
		{
			name:   "empty token",
			client: &Client{},
			want:   "",
		},
		{
			name:   "with token",
			client: &Client{token: "testval-xyz"},
			want:   "testval-xyz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.client.tokenValue(); got != tt.want {
				t.Errorf("tokenValue() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestClient_setToken tests the SetToken setter with nil and valid receivers
func TestClient_setToken(t *testing.T) {
	tests := []struct {
		name   string
		client *Client
		token  string
	}{
		{
			name:   "valid client sets token",
			client: &Client{},
			token:  "new-token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.client.setToken(tt.token)
			if tt.client != nil && tt.client.token != tt.token {
				t.Errorf("setToken() token = %q, want %q", tt.client.token, tt.token)
			}
		})
	}
}

// TestClient_ensure tests the ensure method
func TestClient_ensure(t *testing.T) {
	t.Run("client with nil httpClient", func(t *testing.T) {
		c := &Client{}
		c.initialize()
		if c.httpClient == nil {
			t.Error("httpClient should be initialized after ensure()")
		}
		if c.httpClient == nil {
			t.Error("HTTPClient should be initialized after ensure()")
		}
	})

	t.Run("client with nil UserAgentProvider", func(t *testing.T) {
		c := &Client{
			httpClient: NewHTTPClient(0),
		}
		c.initialize()
		if c.UserAgentProvider == nil {
			t.Error("UserAgentProvider should be initialized after ensure()")
		}
		ua := c.userAgent()
		if ua != "impartus-downloader" {
			t.Errorf("userAgent() = %q, want %q", ua, "impartus-downloader")
		}
	})
}

// TestClient_userAgent tests user agent generation
func TestClient_userAgent(t *testing.T) {
	customUA := "my-custom-agent/1.0"
	c := &Client{
		UserAgentProvider: func() string {
			return customUA
		},
		httpClient: NewHTTPClient(0),
	}

	ua := c.userAgent()
	if ua != customUA {
		t.Errorf("userAgent() = %q, want %q", ua, customUA)
	}
}

// TestSanitiseFileName tests the filename sanitization function
func TestSanitiseFileName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"normal filename", "normal filename"},
		{"filename<>with|invalid:chars", "filename__with_invalid_chars"},
		{"  spaces  ", "spaces"},
		{".hidden.", "hidden"},
		{"new\nline", "new_line"},
		{"tab\there", "tab\there"},
		{`<>/\|*`, "______"},
		{"nochange", "nochange"},
		{"", ""},
		{"   ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeFileName(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeFileName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// TestParsePlaylist tests the playlist parser function
func TestParsePlaylist(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		wantID         int
		wantTitle      string
		wantSeqNo      int
		wantFirstViews int
		wantHasMulti   bool
	}{
		{
			name:           "simple playlist",
			input:          "#EXTM3U\n#EXT-X-KEY:METHOD=ENCRYPTION,URI=\"key.bin\"\nsegment1.ts\nsegment2.ts\n",
			wantID:         42,
			wantTitle:      "Test Title",
			wantSeqNo:      1,
			wantFirstViews: 2,
			wantHasMulti:   false,
		},
		{
			name:           "playlist with discontinuity",
			input:          "#EXTM3U\nsegment1.ts\n#EXT-X-DISCONTINUITY\nsegment2.ts\n",
			wantID:         42,
			wantTitle:      "Multi View",
			wantSeqNo:      2,
			wantFirstViews: 1,
			wantHasMulti:   true,
		},
		{
			name:           "empty playlist",
			input:          "#EXTM3U\n",
			wantID:         0,
			wantTitle:      "",
			wantSeqNo:      0,
			wantFirstViews: 0,
			wantHasMulti:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scanner := bufio.NewScanner(strings.NewReader(tt.input))
			got, err := ParsePlaylist(scanner, tt.wantID, tt.wantTitle, tt.wantSeqNo)
			if err != nil {
				t.Fatalf("ParsePlaylist() unexpected error: %v", err)
			}
			if got.ID != tt.wantID {
				t.Errorf("ID = %d, want %d", got.ID, tt.wantID)
			}
			if got.Title != tt.wantTitle {
				t.Errorf("Title = %q, want %q", got.Title, tt.wantTitle)
			}
			if got.SeqNo != tt.wantSeqNo {
				t.Errorf("SeqNo = %d, want %d", got.SeqNo, tt.wantSeqNo)
			}
			if len(got.FirstViewURLs) != tt.wantFirstViews {
				t.Errorf("FirstViewURLs len = %d, want %d", len(got.FirstViewURLs), tt.wantFirstViews)
			}
			if len(got.FirstDurations) != len(got.FirstViewURLs) {
				t.Errorf("FirstDurations len = %d, want %d", len(got.FirstDurations), len(got.FirstViewURLs))
			}
			if tt.wantHasMulti && !got.HasMultipleViews {
				t.Error("HasMultipleViews = false, want true")
			}
		})
	}
}

func TestParsePlaylistPreservesExtinfDurations(t *testing.T) {
	input := "#EXTM3U\n#EXTINF:4.25,\nleft0.ts\n#EXTINF:7.1,\nleft1.ts\n#EXT-X-DISCONTINUITY\n#EXTINF:9.5,\nright0.ts\n"
	scanner := bufio.NewScanner(strings.NewReader(input))

	got, err := ParsePlaylist(scanner, 42, "Durations", 1)
	if err != nil {
		t.Fatalf("ParsePlaylist() unexpected error: %v", err)
	}

	if len(got.FirstDurations) != 2 || got.FirstDurations[0] != 4.25 || got.FirstDurations[1] != 7.1 {
		t.Fatalf("unexpected first durations: %+v", got.FirstDurations)
	}
	if len(got.SecondDurations) != 1 || got.SecondDurations[0] != 9.5 {
		t.Fatalf("unexpected second durations: %+v", got.SecondDurations)
	}
}

type errOnceReader struct {
	remaining []byte
	err       error
}

func (r *errOnceReader) Read(p []byte) (int, error) {
	if len(r.remaining) > 0 {
		n := copy(p, r.remaining)
		r.remaining = r.remaining[n:]
		if len(r.remaining) == 0 {
			return n, r.err
		}
		return n, nil
	}
	return 0, r.err
}

func TestParsePlaylistReturnsScannerError(t *testing.T) {
	scanner := bufio.NewScanner(&errOnceReader{
		remaining: []byte("#EXTM3U\nsegment1.ts\n"),
		err:       errors.New("scanner boom"),
	})

	_, err := ParsePlaylist(scanner, 42, "Broken", 3)
	if err == nil {
		t.Fatal("expected scanner error")
	}
	if !strings.Contains(err.Error(), "scan playlist") {
		t.Fatalf("expected wrapped scanner error, got %v", err)
	}
}

// TestSelectStreamByQuality tests the stream URL selection logic
func TestSelectStreamByQuality(t *testing.T) {
	streamInfos := []StreamInfo{
		{Quality: "144", URL: "http://example.com/144.m3u8"},
		{Quality: "360", URL: "http://example.com/360.m3u8"},
		{Quality: "480", URL: "http://example.com/480.m3u8"},
		{Quality: "720", URL: "http://example.com/720.m3u8"},
		{Quality: "1080", URL: "http://example.com/1080.m3u8"},
	}

	tests := []struct {
		name    string
		cfg     *config.Config
		wantURL string
	}{
		{
			name:    "audio only returns 144",
			cfg:     &config.Config{AudioOnly: true},
			wantURL: "http://example.com/144.m3u8",
		},
		{
			name:    "exact quality match",
			cfg:     &config.Config{Quality: "720"},
			wantURL: "http://example.com/720.m3u8",
		},
		{
			name:    "quality 480 when preferred 4xx",
			cfg:     &config.Config{Quality: "480"},
			wantURL: "http://example.com/480.m3u8",
		},
		{
			name:    "no match returns empty",
			cfg:     &config.Config{Quality: "999"},
			wantURL: "",
		},
		{
			name:    "no stream infos returns empty",
			cfg:     &config.Config{Quality: "720"},
			wantURL: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var infos []StreamInfo
			if tt.name == "no stream infos returns empty" {
				infos = nil // empty slice
			} else {
				infos = streamInfos
			}
			got := SelectStreamByQuality(infos, tt.cfg.Quality, tt.cfg.AudioOnly)
			if got != tt.wantURL {
				t.Errorf("SelectStreamByQuality() = %q, want %q", got, tt.wantURL)
			}
		})
	}
}

// TestValidateLoginResponse tests login response validation
func TestValidateLoginResponse(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "OK status",
			statusCode: http.StatusOK,
			body:       `{"token":"abc"}`,
			wantErr:    false,
		},
		{
			name:       "Unauthorized",
			statusCode: http.StatusUnauthorized,
			body:       "",
			wantErr:    true,
			errMsg:     "wrong credentials",
		},
		{
			name:       "Other error with body",
			statusCode: http.StatusInternalServerError,
			body:       "server error",
			wantErr:    true,
		},
		{
			name:       "Other error unreadable body",
			statusCode: http.StatusBadGateway,
			body:       "",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body io.ReadCloser
			if tt.body != "" || (tt.statusCode != http.StatusOK && tt.statusCode != http.StatusUnauthorized) {
				body = io.NopCloser(strings.NewReader(tt.body))
			}
			resp := &http.Response{
				StatusCode: tt.statusCode,
				Body:       body,
			}
			err := validateLoginResponse(resp)
			if tt.wantErr && err == nil {
				t.Error("validateLoginResponse() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("validateLoginResponse() unexpected error: %v", err)
			}
			if tt.errMsg != "" && err != nil && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("validateLoginResponse() error = %q, want contains %q", err.Error(), tt.errMsg)
			}
		})
	}
}

// TestPrepareLogin tests login preparation with various configs
func TestPrepareLogin(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.Config
		wantErr bool
		errType error
	}{
		{
			name:    "nil config",
			cfg:     nil,
			wantErr: true,
			errType: errors.New("config is required"),
		},
		{
			name:    "empty base URL",
			cfg:     &config.Config{},
			wantErr: true,
			errType: errors.New("baseUrl is required"),
		},
		{
			name:    "with BaseURL",
			cfg:     &config.Config{BaseURL: "https://example.com"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := New(nil, nil)
			cli, baseURL, err := c.prepareLogin(tt.cfg)
			if tt.wantErr {
				if err == nil {
					t.Error("prepareLogin() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("prepareLogin() unexpected error: %v", err)
				return
			}
			if cli == nil {
				t.Error("prepareLogin() returned nil client")
			}
			if baseURL == "" {
				t.Error("prepareLogin() returned empty baseURL")
			}
		})
	}
}

// TestPrepareLogin_NilReceiver tests prepareLogin with nil client receiver
func TestPrepareLogin_NilReceiver(t *testing.T) {
	cfg := &config.Config{BaseURL: "https://example.com"}
	cli, baseURL, err := (*Client)(nil).prepareLogin(cfg)
	if err != nil {
		t.Errorf("nil receiver prepareLogin() error: %v", err)
	}
	if cli == nil {
		t.Error("nil receiver prepareLogin() returned nil client")
	}
	if baseURL != "https://example.com" {
		t.Errorf("baseURL = %q, want %q", baseURL, "https://example.com")
	}
}

// TestGetCourses_GetLectures_InputValidation tests input validation without network
func TestGetCourses_InputValidation(t *testing.T) {
	c := New(nil, nil)

	// Test nil config
	_, err := c.GetCourses(context.Background(), nil)
	if err == nil {
		t.Error("GetCourses(nil) expected error, got nil")
	}
	if !strings.Contains(err.Error(), "config is required") {
		t.Errorf("GetCourses(nil) error = %q, want contains %q", err.Error(), "config is required")
	}

	// Test empty baseUrl
	_, err = c.GetCourses(context.Background(), &config.Config{})
	if err == nil {
		t.Error("GetCourses(empty config) expected error, got nil")
	}
	if !strings.Contains(err.Error(), "baseUrl is required") {
		t.Errorf("GetCourses(empty config) error = %q, want contains %q", err.Error(), "baseUrl is required")
	}

	// Test missing token
	_, err = c.GetCourses(context.Background(), &config.Config{BaseURL: "https://example.com"})
	if err == nil {
		t.Error("GetCourses(no token) expected error, got nil")
	}
	if !strings.Contains(err.Error(), "token is not set") {
		t.Errorf("GetCourses(no token) error = %q, want contains %q", err.Error(), "token is not set")
	}
}

// TestGetLectures_InputValidation tests input validation without network
func TestGetLectures_InputValidation(t *testing.T) {
	c := New(nil, nil)

	// Test nil config
	_, err := c.GetLectures(context.Background(), nil, Course{})
	if err == nil {
		t.Error("GetLectures(nil) expected error, got nil")
	}
	if !strings.Contains(err.Error(), "config is required") {
		t.Errorf("GetLectures(nil) error = %q, want contains %q", err.Error(), "config is required")
	}

	// Test empty baseUrl
	_, err = c.GetLectures(context.Background(), &config.Config{}, Course{})
	if err == nil {
		t.Error("GetLectures(empty config) expected error, got nil")
	}
	if !strings.Contains(err.Error(), "baseUrl is required") {
		t.Errorf("GetLectures(empty config) error = %q, want contains %q", err.Error(), "baseUrl is required")
	}

	// Test missing token
	_, err = c.GetLectures(context.Background(), &config.Config{BaseURL: "https://example.com"}, Course{})
	if err == nil {
		t.Error("GetLectures(no token) expected error, got nil")
	}
	if !strings.Contains(err.Error(), "token is not set") {
		t.Errorf("GetLectures(no token) error = %q, want contains %q", err.Error(), "token is not set")
	}
}

// TestGetPlaylists_InputValidation tests input validation without network
func TestGetPlaylists_InputValidation(t *testing.T) {
	c := New(nil, nil)

	// Test nil config
	_, err := c.GetPlaylists(context.Background(), nil, Lectures{})
	if err == nil {
		t.Error("GetPlaylists(nil) expected error, got nil")
	}
	if !strings.Contains(err.Error(), "config is required") {
		t.Errorf("GetPlaylists(nil) error = %q, want contains %q", err.Error(), "config is required")
	}

	// Test empty baseUrl
	_, err = c.GetPlaylists(context.Background(), &config.Config{}, Lectures{})
	if err == nil {
		t.Error("GetPlaylists(empty config) expected error, got nil")
	}
	if !strings.Contains(err.Error(), "baseUrl is required") {
		t.Errorf("GetPlaylists(empty config) error = %q, want contains %q", err.Error(), "baseUrl is required")
	}
}

func TestLectures_SelectRange(t *testing.T) {
	lectures := Lectures{
		{TTID: 1, Topic: "Lecture 1"},
		{TTID: 2, Topic: "Lecture 2"},
		{TTID: 3, Topic: "Lecture 3"},
		{TTID: 4, Topic: "Lecture 4"},
		{TTID: 5, Topic: "Lecture 5"},
	}

	tests := []struct {
		name    string
		start   int
		end     int
		wantIDs []int
		wantErr bool
	}{
		{"full range default", 0, 0, []int{5, 4, 3, 2, 1}, false},
		{"first lecture", 1, 1, []int{5}, false},
		{"last lecture", 5, 5, []int{1}, false},
		{"middle range", 2, 4, []int{4, 3, 2}, false},
		{"start defaults to 1", 0, 2, []int{5, 4}, false},
		{"end defaults to len", 3, 0, []int{3, 2, 1}, false},
		{"invalid start > end", 3, 2, nil, true},
		{"start out of range", 6, 6, nil, true},
		{"end out of range", 1, 6, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := lectures.SelectRange(tt.start, tt.end)
			if (err != nil) != tt.wantErr {
				t.Errorf("SelectRange(%d, %d) error = %v, wantErr %v", tt.start, tt.end, err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if len(got) != len(tt.wantIDs) {
				t.Fatalf("SelectRange(%d, %d) returned %d lectures, want %d", tt.start, tt.end, len(got), len(tt.wantIDs))
			}
			for i, l := range got {
				if l.TTID != tt.wantIDs[i] {
					t.Errorf("lecture[%d].TTID = %d, want %d", i, l.TTID, tt.wantIDs[i])
				}
			}
		})
	}
}

func TestLectures_SelectRange_Empty(t *testing.T) {
	lectures := Lectures{}
	_, err := lectures.SelectRange(1, 5)
	if err == nil {
		t.Error("expected error for empty lectures")
	}
}

func TestLectures_SelectRange_DoesNotMutateOriginal(t *testing.T) {
	lectures := Lectures{
		{TTID: 1},
		{TTID: 2},
		{TTID: 3},
	}
	_, err := lectures.SelectRange(1, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lectures) != 3 {
		t.Errorf("original slice mutated: len = %d, want 3", len(lectures))
	}
	if lectures[0].TTID != 1 {
		t.Errorf("original slice[0] TTID = %d, want 1", lectures[0].TTID)
	}
}

func TestPrecompiledRegexes(t *testing.T) {
	if !uriValueRe.MatchString(`URI="https://example.com/key"`) {
		t.Error("uriValueRe should match URI= pattern")
	}
	if uriValueRe.MatchString(`not a uri pattern`) {
		t.Error("uriValueRe should not match non-URI pattern")
	}
	if !resolutionRe.MatchString("1920x1080") {
		t.Error("resolutionRe should match resolution pattern")
	}
	if resolutionRe.MatchString("no resolution here") {
		t.Error("resolutionRe should not match non-resolution text")
	}
}

func TestFilterNoAudio(t *testing.T) {
	lectures := Lectures{
		{TTID: 1, Topic: "Lecture 1", NoAudio: 0},
		{TTID: 2, Topic: "Lecture 2", NoAudio: 1},
		{TTID: 3, Topic: "Lecture 3", NoAudio: 0},
	}

	filtered := lectures.FilterNoAudio()
	if len(filtered) != 2 {
		t.Fatalf("expected 2 lectures, got %d", len(filtered))
	}
	if filtered[0].TTID != 1 {
		t.Errorf("first lecture TTID = %d, want 1", filtered[0].TTID)
	}
	if filtered[1].TTID != 3 {
		t.Errorf("second lecture TTID = %d, want 3", filtered[1].TTID)
	}
}

func TestFilterNoAudio_AllNoAudio(t *testing.T) {
	lectures := Lectures{
		{TTID: 1, NoAudio: 1},
		{TTID: 2, NoAudio: 1},
	}
	filtered := lectures.FilterNoAudio()
	if len(filtered) != 0 {
		t.Errorf("expected 0 lectures, got %d", len(filtered))
	}
}

func TestFilterNoAudio_NoneNoAudio(t *testing.T) {
	lectures := Lectures{
		{TTID: 1, NoAudio: 0},
	}
	filtered := lectures.FilterNoAudio()
	if len(filtered) != 1 {
		t.Errorf("expected 1 lecture, got %d", len(filtered))
	}
}

func TestFilterNoAudio_Empty(t *testing.T) {
	filtered := Lectures{}.FilterNoAudio()
	if len(filtered) != 0 {
		t.Errorf("expected 0 lectures, got %d", len(filtered))
	}
}
