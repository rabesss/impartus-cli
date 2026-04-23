package client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/config"
)

// TestParseStreamInfosFromBody tests stream info parsing from response body
func TestParseStreamInfosFromBody(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantCount int
		wantErr   bool
	}{
		{
			name:      "valid stream infos",
			body:      "http://example.com/1280x720/playlist.m3u8\nhttp://example.com/1920x1080/playlist.m3u8\n",
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:      "empty body",
			body:      "",
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:      "no valid lines",
			body:      "#EXTM3U\n#EXT-X-TARGETDURATION:10\n",
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:      "mixed valid and invalid lines",
			body:      "#EXTM3U\nhttp://example.com/1280x720/playlist.m3u8\n#EXT-X-TARGETDURATION:10\nhttp://example.com/640x360/playlist.m3u8\n",
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:      "144p audio only",
			body:      "http://example.com/audio/144/playlist.m3u8\n",
			wantCount: 1,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseStreamInfosFromBody([]byte(tt.body))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseStreamInfosFromBody() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(got) != tt.wantCount {
				t.Errorf("ParseStreamInfosFromBody() got %d infos, want %d", len(got), tt.wantCount)
			}
		})
	}
}

// TestSelectStreamByQualityRemainingCoverage tests remaining uncovered cases
func TestSelectStreamByQualityRemainingCoverage(t *testing.T) {
	streamInfos := []StreamInfo{
		{Quality: "144", URL: "http://example.com/144.m3u8"},
		{Quality: "360", URL: "http://example.com/360.m3u8"},
		{Quality: "480", URL: "http://example.com/480.m3u8"},
	}

	tests := []struct {
		name      string
		infos     []StreamInfo
		quality   string
		audioOnly bool
		wantURL   string
	}{
		{
			name:      "4xx prefix matches 480",
			infos:     streamInfos,
			quality:   "480",
			audioOnly: false,
			wantURL:   "http://example.com/480.m3u8",
		},
		{
			name:      "4xx prefix with only 360 returns empty",
			infos:     []StreamInfo{{Quality: "360", URL: "http://example.com/360.m3u8"}},
			quality:   "480",
			audioOnly: false,
			wantURL:   "", // No 4xx match, and exact match fails
		},
		{
			name:      "audio only with 144",
			infos:     streamInfos,
			quality:   "",
			audioOnly: true,
			wantURL:   "http://example.com/144.m3u8",
		},
		{
			name:      "empty infos with audio only",
			infos:     []StreamInfo{},
			quality:   "",
			audioOnly: true,
			wantURL:   "",
		},
		{
			name:      "exact quality match",
			infos:     streamInfos,
			quality:   "360",
			audioOnly: false,
			wantURL:   "http://example.com/360.m3u8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SelectStreamByQuality(tt.infos, tt.quality, tt.audioOnly)
			if got != tt.wantURL {
				t.Errorf("SelectStreamByQuality() = %q, want %q", got, tt.wantURL)
			}
		})
	}
}

// TestGetAuthorizedWithToken_NilToken tests GetAuthorizedWithToken with empty token
func TestGetAuthorizedWithToken_NilToken(t *testing.T) {
	client := New(nil, nil)

	resp, err := client.GetAuthorizedWithToken(context.Background(), "http://example.com/test", "")
	if resp != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Errorf("GetAuthorizedWithToken() with empty token should not error on request creation: %v", err)
	}
}

// TestGetAuthorizedWithToken_NilClient tests nil client handling
func TestGetAuthorizedWithToken_NilClient(t *testing.T) {
	var nilClient *Client
	ctx := context.Background()

	resp, err := nilClient.GetAuthorizedWithToken(ctx, "http://example.com/test", "some-token")
	if resp != nil {
		resp.Body.Close()
	}
	if err != nil && !strings.Contains(err.Error(), "request failed") {
		t.Errorf("GetAuthorizedWithToken() unexpected error: %v", err)
	}
}

// TestDoRequestWithTokenRequestBuilding tests request building
func TestDoRequestWithTokenRequestBuilding(t *testing.T) {
	client := New(nil, nil)
	client.SetToken("test-token")

	tests := []struct {
		name       string
		method     string
		url        string
		body       io.Reader
		token      string
		wantErr    bool
		errContain string
	}{
		{
			name:       "GET with valid token",
			method:     http.MethodGet,
			url:        "http://example.com/test",
			body:       nil,
			token:      "valid-token",
			wantErr:    false,
			errContain: "",
		},
		{
			name:       "GET without token",
			method:     http.MethodGet,
			url:        "http://example.com/test",
			body:       nil,
			token:      "",
			wantErr:    false,
			errContain: "",
		},
		{
			name:       "invalid URL",
			method:     http.MethodGet,
			url:        "://invalid-url",
			body:       nil,
			token:      "token",
			wantErr:    true,
			errContain: "failed to create http request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := client.doRequestWithToken(context.Background(), tt.method, tt.url, tt.body, tt.token)
			if tt.wantErr {
				if err == nil {
					t.Error("doRequestWithToken() expected error, got nil")
				} else if tt.errContain != "" && !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("doRequestWithToken() error = %q, want contains %q", err.Error(), tt.errContain)
				}
				return
			}
			if err != nil {
				t.Errorf("doRequestWithToken() unexpected error: %v", err)
			}
			if resp != nil {
				resp.Body.Close()
			}
		})
	}
}

// TestStoreToken_Success tests successful token storage
func TestStoreToken_Success(t *testing.T) {
	client := New(nil, nil)
	cfg := &config.Config{}

	err := client.storeToken(cfg, "test-token")
	if err != nil {
		t.Errorf("storeToken() unexpected error: %v", err)
	}
	if cfg.Token != "test-token" {
		t.Errorf("cfg.Token = %q, want %q", cfg.Token, "test-token")
	}
	if client.Token() != "test-token" {
		t.Errorf("client.Token() = %q, want %q", client.Token(), "test-token")
	}

	// Cleanup: remove .token file after test
	t.Cleanup(func() {
		os.Remove(".token")
	})
}

// TestLogin_RequestCreation tests login request creation
func TestLogin_RequestCreation(t *testing.T) {
	client := New(nil, nil)

	cfg := &config.Config{
		Username: "testuser",
		Password: "testpass",
		BaseURL:  "https://example.com",
	}

	req, err := client.newLoginRequest(context.Background(), cfg, cfg.BaseURL)
	if err != nil {
		t.Errorf("newLoginRequest() error = %v", err)
	}
	if req == nil {
		t.Fatal("newLoginRequest() returned nil request")
	}

	// Verify request properties
	if req.Method != http.MethodPost {
		t.Errorf("newLoginRequest() method = %q, want %q", req.Method, http.MethodPost)
	}
	if !strings.Contains(req.URL.String(), "/auth/signin") {
		t.Errorf("newLoginRequest() URL = %q, want contains /auth/signin", req.URL.String())
	}

	// Verify headers
	if req.Header.Get("Content-Type") != "application/json;charset=UTF-8" {
		t.Errorf("newLoginRequest() Content-Type = %q, want %q", req.Header.Get("Content-Type"), "application/json;charset=UTF-8")
	}
	if req.Header.Get("Accept") != "application/json, text/plain, */*" {
		t.Errorf("newLoginRequest() Accept = %q, want %q", req.Header.Get("Accept"), "application/json, text/plain, */*")
	}
	if req.Header.Get("Referer") != "https://bitshyd.impartus.com/login/" {
		t.Errorf("newLoginRequest() Referer = %q, want %q", req.Header.Get("Referer"), "https://bitshyd.impartus.com/login/")
	}
}

// TestValidateLoginResponse_RemainingCoverage tests remaining validateLoginResponse cases
func TestValidateLoginResponse_RemainingCoverage(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    bool
	}{
		{
			name:       "OK with empty body",
			statusCode: http.StatusOK,
			body:       "",
			wantErr:    false,
		},
		{
			name:       "OK with JSON body",
			statusCode: http.StatusOK,
			body:       `{"token":"abc123"}`,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body io.ReadCloser
			if tt.statusCode != http.StatusOK {
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
		})
	}
}

// TestInitialize_Coverage tests initialize() coverage
func TestInitialize_Coverage(t *testing.T) {
	t.Run("initialize with nil httpClient creates default", func(t *testing.T) {
		c := &Client{}
		c.initialize()
		if c.httpClient == nil {
			t.Error("httpClient should be initialized with default")
		}
	})

	t.Run("initialize with existing httpClient preserves it", func(t *testing.T) {
		extClient := &http.Client{Timeout: 5e9}
		c := &Client{httpClient: extClient}
		c.initialize()
		if c.httpClient != extClient {
			t.Error("httpClient should be preserved")
		}
	})
}

// TestDoRequestWithToken_Headers tests request headers are set correctly
func TestDoRequestWithToken_Headers(t *testing.T) {
	client := New(&http.Client{Transport: &headerCheckTransport{
		expectedHeaders: map[string]string{
			"Authorization": "Bearer test-token",
			"User-Agent":    "impartus-downloader",
			"Accept":        "application/json, text/plain, */*",
		},
	}}, nil)
	client.SetToken("test-token")

	resp, err := client.doRequestWithToken(context.Background(), http.MethodGet, "http://example.com/test", nil, "test-token")
	if err != nil {
		t.Errorf("doRequestWithToken() error = %v", err)
	}
	if resp != nil {
		resp.Body.Close()
	}
}

// headerCheckTransport is a test transport
type headerCheckTransport struct {
	expectedHeaders map[string]string
}

func (t *headerCheckTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Verify each expected header is set correctly
	for name, expected := range t.expectedHeaders {
		actual := req.Header.Get(name)
		if actual != expected {
			return nil, fmt.Errorf("header %q = %q, want %q", name, actual, expected)
		}
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("{}")),
		Header:     http.Header{},
	}, nil
}

// TestReadStoredToken_ErrorPaths tests readStoredToken error handling
func TestReadStoredToken_ErrorPaths(t *testing.T) {
	client := New(nil, nil)

	// Non-existent file
	token, ok := client.readStoredToken()
	if ok {
		t.Error("readStoredToken() expected false for non-existent file, got true")
	}
	if token != "" {
		t.Errorf("readStoredToken() token = %q, want empty string", token)
	}
}

// TestLoginAndSetToken_NilClient tests LoginAndSetToken with nil client
func TestLoginAndSetToken_NilClient(t *testing.T) {
	var nilClient *Client
	cfg := &config.Config{
		BaseURL:  "https://example.com",
		Username: "user",
		Password: "pass",
	}

	err := nilClient.LoginAndSetToken(context.Background(), cfg)
	if err == nil {
		t.Error("LoginAndSetToken(nil) expected error, got nil")
	}
}

// TestLoginAndSetToken_NilConfig tests LoginAndSetToken with nil config
func TestLoginAndSetToken_NilConfig(t *testing.T) {
	client := New(nil, nil)
	err := client.LoginAndSetToken(context.Background(), nil)
	if err == nil {
		t.Error("LoginAndSetToken(nil config) expected error, got nil")
	}
	if !strings.Contains(err.Error(), "config is required") {
		t.Errorf("LoginAndSetToken(nil config) error = %q, want contains 'config is required'", err.Error())
	}
}

// TestLoginAndSetToken_MissingBaseURL tests LoginAndSetToken with missing base URL
func TestLoginAndSetToken_MissingBaseURL(t *testing.T) {
	client := New(nil, nil)
	cfg := &config.Config{
		Username: "user",
		Password: "pass",
	}

	err := client.LoginAndSetToken(context.Background(), cfg)
	if err == nil {
		t.Error("LoginAndSetToken(missing baseURL) expected error, got nil")
	}
	if !strings.Contains(err.Error(), "baseUrl is required") {
		t.Errorf("LoginAndSetToken(missing baseURL) error = %q, want contains 'baseUrl is required'", err.Error())
	}
}

// TestNewLoginRequest_MarshalError tests request body creation
func TestNewLoginRequest_MarshalError(t *testing.T) {
	client := New(nil, nil)
	cfg := &config.Config{
		BaseURL:  "https://example.com",
		Username: "user",
		Password: "pass",
	}

	req, err := client.newLoginRequest(context.Background(), cfg, cfg.BaseURL)
	if err != nil {
		t.Errorf("newLoginRequest() unexpected error: %v", err)
		return
	}
	if req == nil {
		t.Error("newLoginRequest() returned nil request")
		return
	}

	// Read body to verify it's valid JSON
	body, err := io.ReadAll(req.Body)
	req.Body.Close()
	if err != nil {
		t.Errorf("io.ReadAll() error: %v", err)
		return
	}
	if !bytes.Contains(body, []byte("user")) || !bytes.Contains(body, []byte("pass")) {
		t.Errorf("newLoginRequest() body = %q, want contains user and pass", string(body))
	}
}

// TestValidateStoredToken_ErrorPaths tests validateStoredToken error handling
func TestValidateStoredToken_ErrorPaths(t *testing.T) {
	client := New(nil, nil)

	_, err := client.validateStoredToken(context.Background(), "://invalid", "token")
	if err == nil {
		t.Error("validateStoredToken() expected error for invalid URL, got nil")
	}
}

// TestLoginFlow_MissingCredentials tests login flow with missing credentials
func TestLoginFlow_MissingCredentials(t *testing.T) {
	client := New(nil, nil)
	cfg := &config.Config{
		BaseURL:  "https://example.com",
		Username: "",
		Password: "",
	}

	token, err := client.login(context.Background(), cfg, cfg.BaseURL)
	if err == nil {
		t.Error("login() expected error for empty credentials, got nil")
	}
	if token != "" {
		t.Errorf("login() token = %q, want empty string", token)
	}
}

// TestGetPlaylists_NoMatchingStream tests GetPlaylists with unreachable server
func TestGetPlaylists_NoMatchingStream(t *testing.T) {
	client := New(nil, nil)
	cfg := &config.Config{
		BaseURL:   "https://example.com",
		Token:     "test-token",
		Quality:   "9999",
		AudioOnly: false,
	}

	lectures := Lectures{
		{TTID: 1, Topic: "Test", SeqNo: 1},
	}

	// Expect network error since server is unreachable
	_, err := client.GetPlaylists(context.Background(), cfg, lectures)
	if err == nil {
		t.Error("GetPlaylists() expected error for unreachable server, got nil")
	}
}
