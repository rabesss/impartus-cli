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

type oversizedKeyTransport struct {
	body string
}

func (t *oversizedKeyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(t.body)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}
