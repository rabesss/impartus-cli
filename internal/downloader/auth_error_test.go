package downloader

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
)

func TestDownloadChunkUnauthorizedWrapsTypedAuthenticationError(t *testing.T) {
	const bodyMarker = "chunk-response-body-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		if _, err := w.Write([]byte(bodyMarker)); err != nil {
			t.Errorf("write chunk 401 body: %v", err)
		}
	}))
	defer server.Close()

	d := testLimitDownloader(t.TempDir(), client.New(server.Client(), nil))
	path, data, written, err := d.doDownloadChunkWithLimit(t.Context(), server.URL+"/chunk.ts?token=query-secret", 1, 0, "left", true, 8)
	if path != "" || data != nil || written != 0 {
		t.Fatalf("failed chunk returned path=%q data=%v written=%d", path, data, written)
	}
	assertDownloaderAuthenticationError(t, err, "chunk", bodyMarker, "query-secret")
}

func TestFetchDecryptionKeyUnauthorizedWrapsTypedAuthenticationError(t *testing.T) {
	const bodyMarker = "key-response-body-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		if _, err := w.Write([]byte(bodyMarker)); err != nil {
			t.Errorf("write key 401 body: %v", err)
		}
	}))
	defer server.Close()

	d := testLimitDownloader(t.TempDir(), client.New(server.Client(), nil))
	_, err := d.fetchDecryptionKey(t.Context(), server.URL+"/key?token=query-secret")
	assertDownloaderAuthenticationError(t, err, "decryption key", bodyMarker, "query-secret")
}

func assertDownloaderAuthenticationError(t *testing.T, err error, operation string, secretMarkers ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("error = nil, want typed authentication error")
	}
	if !errors.Is(err, client.ErrAuthentication) {
		t.Fatalf("error = %T %q, want ErrAuthentication identity", err, err)
	}
	var authErr *client.AuthenticationError
	if !errors.As(err, &authErr) {
		t.Fatalf("error = %T %q, want *AuthenticationError", err, err)
	}
	if authErr.Operation != operation || authErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("authentication metadata = (%q, %d), want (%q, 401)", authErr.Operation, authErr.StatusCode, operation)
	}
	for _, marker := range secretMarkers {
		if strings.Contains(err.Error(), marker) {
			t.Fatalf("typed authentication error leaked %q: %v", marker, err)
		}
	}
}
