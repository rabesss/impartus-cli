package downloader

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

func TestPlayServerWorkflow(t *testing.T) {
	// 1. Define test key and plaintext
	testKey := []byte("0123456789abcdef")                   // 16 bytes AES key
	plaintext := []byte("hello world 1234hello world 1234") // 32 bytes (multiple of block size)

	// Pad with PKCS7 (16 bytes of value 16)
	paddedPlaintext := make([]byte, len(plaintext)+16)
	copy(paddedPlaintext, plaintext)
	for i := 0; i < 16; i++ {
		paddedPlaintext[len(plaintext)+i] = 16
	}

	// Encrypt using AES-CBC with zero IV
	block, err := aes.NewCipher(testKey)
	if err != nil {
		t.Fatalf("failed to create cipher: %v", err)
	}
	iv := make([]byte, 16)
	encrypter := cipher.NewCBCEncrypter(block, iv)
	ciphertext := make([]byte, len(paddedPlaintext))
	encrypter.CryptBlocks(ciphertext, paddedPlaintext)

	// 2. Setup mock HTTP server for the Impartus API
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "key") {
			// Return transformed key (2 header bytes + reversed key bytes)
			rev := make([]byte, len(testKey))
			for i := range testKey {
				rev[i] = testKey[len(testKey)-1-i]
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(append([]byte{0, 0}, rev...)) //nolint:errcheck
			return
		}
		if strings.Contains(r.URL.Path, "segment") {
			w.Header().Set("Content-Type", "video/MP2T")
			_, _ = w.Write(ciphertext) //nolint:errcheck
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	// 3. Create downloader with configured mock client
	cfg := &config.Config{
		Views: "both",
	}
	cfg.ApplyDefaults()

	apiClient := client.New(nil, nil)
	d := New(cfg, apiClient)

	// 4. Setup mock playlist structure pointing to the mock server URLs
	playlist := client.ParsedPlaylist{
		KeyURL:           ts.URL + "/key",
		FirstViewURLs:    []string{ts.URL + "/segment/left/0"},
		SecondViewURLs:   []string{ts.URL + "/segment/right/0"},
		FirstDurations:   []float64{6.5},
		SecondDurations:  []float64{6.5},
		HasMultipleViews: true,
	}

	ctx := context.Background()
	playURL, cleanup, err := d.StartPlayServer(ctx, playlist)
	if err != nil {
		t.Fatalf("StartPlayServer failed: %v", err)
	}
	defer cleanup()

	// 5. Test master.m3u8 endpoint
	resp, err := http.Get(playURL) //nolint:noctx // test client GET ok without ctx
	if err != nil {
		t.Fatalf("failed to fetch master playlist: %v", err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	masterContent, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read master: %v", err)
	}
	if !strings.Contains(string(masterContent), "left.m3u8") || !strings.Contains(string(masterContent), "right.m3u8") {
		t.Fatalf("master.m3u8 content invalid: %s", string(masterContent))
	}

	// 6. Test left.m3u8 endpoint
	// Extract left.m3u8 URL from master playlist
	lines := strings.Split(string(masterContent), "\n")
	var leftM3U8URL string
	for _, line := range lines {
		if strings.Contains(line, "left.m3u8") {
			leftM3U8URL = line
			break
		}
	}
	if leftM3U8URL == "" {
		t.Fatal("left.m3u8 URL not found in master playlist")
	}

	respLeft, err := http.Get(leftM3U8URL) //nolint:noctx // test client GET ok without ctx
	if err != nil {
		t.Fatalf("failed to fetch left playlist: %v", err)
	}
	defer func() { _ = respLeft.Body.Close() }() //nolint:errcheck

	leftContent, err := io.ReadAll(respLeft.Body)
	if err != nil {
		t.Fatalf("failed to read left playlist: %v", err)
	}
	if !strings.Contains(string(leftContent), "/segment/left/0") {
		t.Fatalf("left.m3u8 content invalid: %s", string(leftContent))
	}

	// 7. Test segment endpoint (requests segment and asserts decrypted output)
	linesLeft := strings.Split(string(leftContent), "\n")
	var segmentURL string
	for _, line := range linesLeft {
		if strings.Contains(line, "/segment/left/0") {
			segmentURL = line
			break
		}
	}
	if segmentURL == "" {
		t.Fatal("segment URL not found in left playlist")
	}

	respSeg, err := http.Get(segmentURL) //nolint:noctx // test client GET ok without ctx
	if err != nil {
		t.Fatalf("failed to fetch segment: %v", err)
	}
	defer func() { _ = respSeg.Body.Close() }() //nolint:errcheck

	if respSeg.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 for segment, got %d", respSeg.StatusCode)
	}

	segContent, err := io.ReadAll(respSeg.Body)
	if err != nil {
		t.Fatalf("failed to read segment: %v", err)
	}

	if string(segContent) != string(plaintext) {
		t.Fatalf("decrypted segment content mismatch. Got %q, want %q", string(segContent), string(plaintext))
	}
}

func TestBuildLocalM3U8(t *testing.T) {
	tests := []struct {
		name         string
		view         string
		urls         []string
		wantSegments int
	}{
		{
			name:         "zero URLs produces valid M3U8 with no segments",
			view:         "left",
			urls:         []string{},
			wantSegments: 0,
		},
		{
			name:         "single URL produces one segment entry",
			view:         "left",
			urls:         []string{"https://example.com/chunk0.ts"},
			wantSegments: 1,
		},
		{
			name:         "multiple URLs produce correct segment count",
			view:         "right",
			urls:         []string{"https://a.test/0", "https://a.test/1", "https://a.test/2"},
			wantSegments: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildLocalM3U8(tt.view, tt.urls, nil, 9999, "test-token")

			// Verify required HLS headers are always present
			for _, header := range []string{
				"#EXTM3U",
				"#EXT-X-VERSION:3",
				"#EXT-X-TARGETDURATION:11",
				"#EXT-X-KEY:METHOD=NONE",
				"#EXT-X-ENDLIST",
			} {
				if !strings.Contains(result, header) {
					t.Errorf("missing required header %q in output:\n%s", header, result)
				}
			}

			// Count #EXTINF entries
			gotSegments := strings.Count(result, "#EXTINF:")
			if gotSegments != tt.wantSegments {
				t.Errorf("segment count = %d, want %d", gotSegments, tt.wantSegments)
			}

			// Verify URL format for each segment
			for i := range tt.urls {
				wantURL := fmt.Sprintf("http://127.0.0.1:9999/test-token/segment/%s/%d", tt.view, i)
				if !strings.Contains(result, wantURL) {
					t.Errorf("missing expected segment URL %q in output:\n%s", wantURL, result)
				}
			}
		})
	}
}

func TestBuildLocalM3U8UsesSegmentDurations(t *testing.T) {
	result := buildLocalM3U8("left", []string{"a.ts", "b.ts"}, []float64{4.25, 7.1}, 9999, "test-token")

	if !strings.Contains(result, "#EXT-X-TARGETDURATION:8") {
		t.Fatalf("expected target duration rounded from segment durations, got:\n%s", result)
	}
	if !strings.Contains(result, "#EXTINF:4.250,") || !strings.Contains(result, "#EXTINF:7.100,") {
		t.Fatalf("expected EXTINF durations to be preserved, got:\n%s", result)
	}
}

func TestHandleSegmentErrorPaths(t *testing.T) {
	// Create a Downloader with a working rate limiter via New() + ApplyDefaults()
	d := New(&config.Config{Views: "both"}, client.New(nil, nil))

	playlist := client.ParsedPlaylist{
		FirstViewURLs:    []string{"https://example.com/seg0", "https://example.com/seg1"},
		SecondViewURLs:   []string{"https://example.com/rseg0"},
		HasMultipleViews: true,
	}
	key := []byte("0123456789abcdef") // 16-byte AES key

	handler := d.handleSegment(playlist, key)

	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "too few path parts",
			path:       "/token/segment",
			wantStatus: http.StatusBadRequest,
			wantBody:   "invalid segment path",
		},
		{
			name:       "too many path parts",
			path:       "/token/segment/left/0/extra",
			wantStatus: http.StatusBadRequest,
			wantBody:   "invalid segment path",
		},
		{
			name:       "non-numeric segment index",
			path:       "/token/segment/left/abc",
			wantStatus: http.StatusBadRequest,
			wantBody:   "invalid segment index",
		},
		{
			name:       "invalid view name",
			path:       "/token/segment/center/0",
			wantStatus: http.StatusBadRequest,
			wantBody:   "invalid view name",
		},
		{
			name:       "negative segment index",
			path:       "/token/segment/left/-1",
			wantStatus: http.StatusNotFound,
			wantBody:   "segment index out of range",
		},
		{
			name:       "segment index out of range for first view",
			path:       "/token/segment/left/99",
			wantStatus: http.StatusNotFound,
			wantBody:   "segment index out of range",
		},
		{
			name:       "segment index out of range for second view",
			path:       "/token/segment/right/1",
			wantStatus: http.StatusNotFound,
			wantBody:   "segment index out of range",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://localhost"+tt.path, nil) //nolint:noctx // test handler invocation
			req.URL.Path = tt.path
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Errorf("body = %q, want substring %q", rec.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestHandleMasterViewConfigurations(t *testing.T) {
	tests := []struct {
		name             string
		views            string
		hasMultipleViews bool
		firstViewURLs    []string
		secondViewURLs   []string
		wantLeft         bool
		wantRight        bool
		wantStatus       int
	}{
		{
			name:             "both views with multiple views available",
			views:            "both",
			hasMultipleViews: true,
			firstViewURLs:    []string{"https://a.test/0"},
			secondViewURLs:   []string{"https://b.test/0"},
			wantLeft:         true,
			wantRight:        true,
			wantStatus:       http.StatusOK,
		},
		{
			name:             "left view only",
			views:            "left",
			hasMultipleViews: true,
			firstViewURLs:    []string{"https://a.test/0"},
			secondViewURLs:   []string{"https://b.test/0"},
			wantLeft:         true,
			wantRight:        false,
			wantStatus:       http.StatusOK,
		},
		{
			name:             "right view only with multiple views",
			views:            "right",
			hasMultipleViews: true,
			firstViewURLs:    []string{"https://a.test/0"},
			secondViewURLs:   []string{"https://b.test/0"},
			wantLeft:         false,
			wantRight:        true,
			wantStatus:       http.StatusOK,
		},
		{
			name:             "HasMultipleViews false shows only left",
			views:            "both",
			hasMultipleViews: false,
			firstViewURLs:    []string{"https://a.test/0"},
			secondViewURLs:   []string{"https://b.test/0"},
			wantLeft:         true,
			wantRight:        false,
			wantStatus:       http.StatusOK,
		},
		{
			name:             "empty URLs returns not found instead of broken stream",
			views:            "both",
			hasMultipleViews: true,
			firstViewURLs:    []string{},
			secondViewURLs:   []string{},
			wantLeft:         false,
			wantRight:        false,
			wantStatus:       http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{Views: tt.views}
			cfg.ApplyDefaults()
			d := New(cfg, client.New(nil, nil))

			playlist := client.ParsedPlaylist{
				FirstViewURLs:    tt.firstViewURLs,
				SecondViewURLs:   tt.secondViewURLs,
				HasMultipleViews: tt.hasMultipleViews,
			}

			handler := d.handleMaster(playlist, 8888, "test-token")

			req := httptest.NewRequest(http.MethodGet, "http://localhost/test-token/master.m3u8", nil) //nolint:noctx // test handler invocation
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			body := rec.Body.String()
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body:\n%s", rec.Code, tt.wantStatus, body)
			}

			hasLeft := strings.Contains(body, "left.m3u8")
			hasRight := strings.Contains(body, "right.m3u8")

			if hasLeft != tt.wantLeft {
				t.Errorf("left.m3u8 present = %v, want %v; body:\n%s", hasLeft, tt.wantLeft, body)
			}
			if hasRight != tt.wantRight {
				t.Errorf("right.m3u8 present = %v, want %v; body:\n%s", hasRight, tt.wantRight, body)
			}

			if tt.wantStatus == http.StatusOK && !strings.Contains(body, "RESOLUTION=") {
				t.Errorf("expected master playlist metadata to include resolution, body:\n%s", body)
			}
			if tt.wantStatus == http.StatusOK && !strings.Contains(body, "BANDWIDTH=") {
				t.Errorf("expected master playlist metadata to include bandwidth, body:\n%s", body)
			}
			if ct := rec.Header().Get("Content-Type"); tt.wantStatus == http.StatusOK && ct != "application/vnd.apple.mpegurl" {
				t.Errorf("Content-Type = %q, want %q", ct, "application/vnd.apple.mpegurl")
			}
		})
	}
}

func TestStartPlayServerRejectsPlaylistWithNoPlayableViews(t *testing.T) {
	d := New(&config.Config{Views: "both"}, client.New(nil, nil))
	_, cleanup, err := d.StartPlayServer(context.Background(), client.ParsedPlaylist{SeqNo: 9})
	if err == nil {
		t.Fatal("expected no playable views error")
	}
	if cleanup != nil {
		t.Fatal("cleanup should be nil when server does not start")
	}
	if !strings.Contains(err.Error(), "no playable views") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadSegmentBytesDetectsOverflow(t *testing.T) {
	got, err := readSegmentBytes(strings.NewReader("12345"), 5)
	if err != nil {
		t.Fatalf("unexpected exact-limit error: %v", err)
	}
	if string(got) != "12345" {
		t.Fatalf("got %q, want exact payload", string(got))
	}

	_, err = readSegmentBytes(strings.NewReader("123456"), 5)
	if err == nil {
		t.Fatal("expected overflow error")
	}
	if !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Fatalf("unexpected overflow error: %v", err)
	}
}

func TestHlsVariantMetadata(t *testing.T) {
	tests := []struct {
		quality      string
		wantBandwidth int
		wantRes       string
	}{
		{"144", 256000, "256x144"},
		{"450", 800000, "800x450"},
		{"720", 1500000, "1280x720"},
		{"", 1500000, "1280x720"},
		{"  450  ", 800000, "800x450"},
		{"unknown", 1500000, "1280x720"},
	}
	for _, tt := range tests {
		t.Run(tt.quality, func(t *testing.T) {
			bw, res := hlsVariantMetadata(tt.quality)
			if bw != tt.wantBandwidth {
				t.Errorf("bandwidth = %d, want %d", bw, tt.wantBandwidth)
			}
			if res != tt.wantRes {
				t.Errorf("resolution = %q, want %q", res, tt.wantRes)
			}
		})
	}
}

func TestSegmentDuration(t *testing.T) {
	durations := []float64{5.0, 0, 8.5}
	tests := []struct {
		name  string
		index int
		want  float64
	}{
		{"valid index 0", 0, 5.0},
		{"zero duration falls back to 11", 1, 11.0},
		{"valid index 2", 2, 8.5},
		{"out of range falls back to 11", 3, 11.0},
		{"negative index falls back to 11", -1, 11.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := segmentDuration(durations, tt.index); got != tt.want {
				t.Errorf("segmentDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTargetDuration(t *testing.T) {
	tests := []struct {
		name          string
		durations     []float64
		segmentCount  int
		wantCeiling   int
	}{
		{"rounds up max duration", []float64{5.0, 7.1, 3.0}, 3, 8},
		{"empty durations defaults to 11", nil, 3, 11},
		{"all zeros defaults to 11", []float64{0, 0}, 2, 11},
		{"single segment", []float64{10.0}, 1, 10},
		{"exact integer needs no rounding", []float64{11.0}, 1, 11},
		{"fractional rounds up", []float64{10.1}, 1, 11},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := targetDuration(tt.durations, tt.segmentCount); got != tt.wantCeiling {
				t.Errorf("targetDuration() = %d, want %d", got, tt.wantCeiling)
			}
		})
	}
}
