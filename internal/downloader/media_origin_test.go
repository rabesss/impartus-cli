package downloader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

type mediaOriginRequestTransport struct {
	request *http.Request
}

func (t *mediaOriginRequestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.request = req.Clone(req.Context())
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("encrypted chunk")),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func TestDownloaderDoesNotAttachTokenToUnconfiguredChunkOrigin(t *testing.T) {
	transport := &mediaOriginRequestTransport{}
	apiClient := client.New(&http.Client{Transport: transport}, nil)
	d := New(&config.Config{
		BaseURL:                   "https://api.example.test",
		Token:                     "bearer-secret",
		TempDirLocation:           t.TempDir(),
		Views:                     "left",
		RateLimit:                 100,
		APIRateLimit:              20,
		EnablePipeline:            false,
		DownloadWorkersPerLecture: 1,
	}, apiClient)

	_, data, _, err := d.doDownloadChunkWithLimit(context.Background(), "https://evil.example.test/chunk.ts?token=bearer-secret", 1, 0, "left", true, 64)
	if err != nil {
		t.Fatalf("doDownloadChunkWithLimit() error = %v", err)
	}
	if string(data) != "encrypted chunk" {
		t.Fatalf("chunk data = %q, want response body", data)
	}
	if transport.request == nil {
		t.Fatal("chunk request was not sent")
	}
	if got := transport.request.Header.Get("Authorization"); got != "" {
		t.Fatalf("chunk Authorization = %q, want no bearer token", got)
	}
	if got := transport.request.URL.RawQuery; got != "" {
		t.Fatalf("chunk query = %q, want bearer token removed", got)
	}
}

func TestDownloaderAttachesTokenToConfiguredCDNChunkOrigin(t *testing.T) {
	transport := &mediaOriginRequestTransport{}
	apiClient := client.New(&http.Client{Transport: transport}, nil)
	d := New(&config.Config{
		BaseURL:                   "https://api.example.test",
		MediaOrigins:              []string{"https://cdn.example.test"},
		Token:                     "bearer-secret",
		TempDirLocation:           t.TempDir(),
		Views:                     "left",
		RateLimit:                 100,
		APIRateLimit:              20,
		EnablePipeline:            false,
		DownloadWorkersPerLecture: 1,
	}, apiClient)

	_, data, _, err := d.doDownloadChunkWithLimit(context.Background(), "https://cdn.example.test/chunk.ts", 1, 0, "left", true, 64)
	if err != nil {
		t.Fatalf("doDownloadChunkWithLimit() error = %v", err)
	}
	if string(data) != "encrypted chunk" {
		t.Fatalf("chunk data = %q, want response body", data)
	}
	if transport.request == nil {
		t.Fatal("chunk request was not sent")
	}
	if got := transport.request.Header.Get("Authorization"); got != "Bearer bearer-secret" {
		t.Fatalf("chunk Authorization = %q, want configured bearer token", got)
	}
}

func TestDownloaderRejectsOversizedDecryptionKeyResponse(t *testing.T) {
	transport := &decryptionKeyResponseTransport{body: strings.Repeat("k", int(maxDecryptionKeyResponseSize)+1)}
	apiClient := client.New(&http.Client{Transport: transport}, nil)
	d := New(&config.Config{
		BaseURL:                   "https://api.example.test",
		Token:                     "bearer-secret",
		TempDirLocation:           t.TempDir(),
		Views:                     "left",
		RateLimit:                 100,
		APIRateLimit:              20,
		DownloadWorkersPerLecture: 1,
		DecryptWorkersPerLecture:  1,
	}, apiClient)

	_, err := d.fetchDecryptionKey(context.Background(), "https://api.example.test/key")
	if err == nil || !strings.Contains(err.Error(), "decryption key response exceeds max size") {
		t.Fatalf("fetchDecryptionKey() error = %v, want response size limit", err)
	}
}

func TestDownloaderSanitizesSuccessfulBodyReadErrors(t *testing.T) {
	const token = "downloader-read-secret"
	newClient := func() *client.Client {
		return client.New(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(tokenErrorReader{token: token}),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		})}, nil)
	}

	d := New(&config.Config{
		BaseURL:                   "https://api.example.test",
		Token:                     token,
		TempDirLocation:           t.TempDir(),
		Views:                     "left",
		RateLimit:                 100,
		APIRateLimit:              20,
		DownloadWorkersPerLecture: 1,
		DecryptWorkersPerLecture:  1,
	}, newClient())
	_, err := d.fetchDecryptionKey(context.Background(), "https://api.example.test/key")
	assertSanitizedDownloaderError(t, err, token)

	d = testLimitDownloader(t.TempDir(), newClient())
	d.config.Token = token
	chunkPath, chunkData, bytesWritten, err := d.doDownloadChunkWithLimit(context.Background(), "https://api.example.test/chunk.ts", 1, 0, "left", true, 64)
	if chunkPath != "" || chunkData != nil || bytesWritten != 0 {
		t.Fatalf("failed chunk returned path=%q data=%v bytes=%d", chunkPath, chunkData, bytesWritten)
	}
	assertSanitizedDownloaderError(t, err, token)
}

func assertSanitizedDownloaderError(t *testing.T, err error, token string) {
	t.Helper()
	if err == nil {
		t.Fatal("operation error = nil, want body read failure")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("operation error leaked token: %v", err)
	}
	if !strings.Contains(err.Error(), "REDACTED") {
		t.Fatalf("operation error = %v, want redaction marker", err)
	}
}

type decryptionKeyResponseTransport struct {
	body string
}

type tokenErrorReader struct {
	token string
}

func (r tokenErrorReader) Read([]byte) (int, error) {
	return 0, fmt.Errorf("read failed unknown=%s", r.token)
}

func (t *decryptionKeyResponseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(t.body)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}
