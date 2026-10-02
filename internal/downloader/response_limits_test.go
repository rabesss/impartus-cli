package downloader

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
)

func TestFetchDecryptionKeyRejectsOversizedResponse(t *testing.T) {
	const keyResponseLimitForTest = 4 * 1024
	apiClient := client.New(&http.Client{Transport: &oversizedKeyTransport{
		body: strings.Repeat("k", keyResponseLimitForTest+1),
	}}, nil)
	d := testLimitDownloader(t.TempDir(), apiClient)

	key, err := d.fetchDecryptionKey(t.Context(), "https://media.example.test/key")
	if key != nil {
		t.Fatalf("fetchDecryptionKey() key length = %d, want nil on oversized response", len(key))
	}
	if err == nil || !strings.Contains(err.Error(), "decryption key response exceeds max size") {
		t.Fatalf("fetchDecryptionKey() error = %v, want response size limit", err)
	}
	if errors.Is(err, errDownloadSizeLimit) {
		t.Fatalf("fetchDecryptionKey() error = %v, want key-response limit classification", err)
	}
}

func TestFetchDecryptionKeyReadErrorsSanitizeToken(t *testing.T) {
	const token = "key-read-secret"
	apiClient := client.New(&http.Client{Transport: &keyReadErrorTransport{
		err: errors.New("key read failed unknown=" + token),
	}}, nil)
	d := testLimitDownloader(t.TempDir(), apiClient)
	d.config.Token = token

	key, err := d.fetchDecryptionKey(t.Context(), "https://media.example.test/key")
	if key != nil {
		t.Fatalf("fetchDecryptionKey() key = %v, want nil on read failure", key)
	}
	if err == nil || strings.Contains(err.Error(), token) || !strings.Contains(err.Error(), "REDACTED") {
		t.Fatalf("fetchDecryptionKey() error = %v, want sanitized read failure", err)
	}
}

type oversizedKeyTransport struct {
	body string
}

type keyReadErrorTransport struct {
	err error
}

func (t *keyReadErrorTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(keyErrorReader{err: t.err}),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

type keyErrorReader struct {
	err error
}

func (r keyErrorReader) Read([]byte) (int, error) {
	return 0, r.err
}

func (t *oversizedKeyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(t.body)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}
