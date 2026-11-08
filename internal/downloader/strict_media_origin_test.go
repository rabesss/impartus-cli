package downloader

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/retry"
)

const (
	strictPathMarker  = "private-strict-path"
	strictQueryMarker = "signed-strict-query"
)

func newStrictCountingServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

func strictMediaURL(origin string) string {
	return origin + "/" + strictPathMarker + "/media.ts?sig=" + strictQueryMarker
}

// assertStrictDownloaderRefusal checks a strictMediaOrigins refusal: the typed
// origin error, the policy identity, and no trace of the refused URL beyond
// its origin.
func assertStrictDownloaderRefusal(t *testing.T, err error, origin string) {
	t.Helper()
	assertDownloaderOriginRefusal(t, err, origin)
	var originErr *client.UnconfiguredMediaOriginError
	if !errors.As(err, &originErr) || !originErr.Strict {
		t.Fatalf("error = %q, want a strict refusal", err)
	}
	if !errors.Is(err, client.ErrMediaOrigin) {
		t.Fatalf("error = %q, want errors.Is(ErrMediaOrigin)", err)
	}
	for _, leaked := range []string{strictPathMarker, strictQueryMarker} {
		if strings.Contains(err.Error(), leaked) {
			t.Fatalf("error = %q, leaks refused URL part %q", err, leaked)
		}
	}
}

func TestDownloadChunkStrictMediaOriginsRefusesUnconfiguredOrigin(t *testing.T) {
	server, hits := newStrictCountingServer(t)

	d := testLimitDownloader(t.TempDir(), client.New(server.Client(), nil))
	d.config.StrictMediaOrigins = true
	path, data, written, err := d.doDownloadChunkWithLimit(t.Context(), strictMediaURL(server.URL), 1, 0, "left", true, 8)
	if path != "" || data != nil || written != 0 {
		t.Fatalf("refused chunk returned path=%q data=%v written=%d", path, data, written)
	}
	assertStrictDownloaderRefusal(t, err, server.URL)
	if got := hits.Load(); got != 0 {
		t.Fatalf("unconfigured origin hits = %d, want 0", got)
	}
}

func TestFetchDecryptionKeyStrictMediaOriginsRefusesUnconfiguredOrigin(t *testing.T) {
	server, hits := newStrictCountingServer(t)

	d := New(&config.Config{Token: "test-token", StrictMediaOrigins: true}, client.New(server.Client(), nil))
	d.retry = retry.Policy{}
	key, err := d.fetchDecryptionKey(t.Context(), strictMediaURL(server.URL))
	if key != nil {
		t.Fatalf("refused key fetch returned %d bytes", len(key))
	}
	assertStrictDownloaderRefusal(t, err, server.URL)
	if got := hits.Load(); got != 0 {
		t.Fatalf("unconfigured origin hits = %d, want 0", got)
	}
}

func TestDownloadPlaylistStrictMediaOriginsReportsRefusedChunkOrigin(t *testing.T) {
	for _, enablePipeline := range []bool{false, true} {
		t.Run(fmt.Sprintf("pipeline=%t", enablePipeline), func(t *testing.T) {
			key := []byte("0123456789abcdef")
			configured := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/key" {
					writeDownloadSpeedResponse(w, fakeKeyResponse(key))
					return
				}
				http.NotFound(w, r)
			}))
			defer configured.Close()
			cdn, cdnHits := newStrictCountingServer(t)

			cfg := &config.Config{
				BaseURL:                   configured.URL,
				Token:                     "test-token",
				StrictMediaOrigins:        true,
				TempDirLocation:           t.TempDir(),
				Views:                     "left",
				EnablePipeline:            enablePipeline,
				DownloadWorkersPerLecture: 1,
				DecryptWorkersPerLecture:  1,
				RateLimit:                 100,
				APIRateLimit:              20,
			}
			d := New(cfg, client.New(configured.Client(), nil))
			d.retry = retry.Policy{}

			_, err := d.DownloadPlaylist(t.Context(), client.ParsedPlaylist{
				ID:            42,
				SeqNo:         1,
				KeyURL:        configured.URL + "/key",
				FirstViewURLs: []string{strictMediaURL(cdn.URL)},
			}, nil, nil)
			assertStrictDownloaderRefusal(t, err, cdn.URL)
			if got := cdnHits.Load(); got != 0 {
				t.Fatalf("unconfigured chunk origin hits = %d, want 0", got)
			}
		})
	}
}

func TestStartPlaybackStreamStrictMediaOriginsKeepsKeyRefusalType(t *testing.T) {
	server, hits := newStrictCountingServer(t)

	d := New(&config.Config{Views: "left", Token: "test-token", StrictMediaOrigins: true}, client.New(nil, nil))
	d.retry = retry.Policy{}
	stream, err := d.StartPlaybackStream(context.Background(), client.ParsedPlaylist{
		KeyURL:        strictMediaURL(server.URL),
		FirstViewURLs: []string{server.URL + "/segment"},
	})
	if err == nil {
		stream.Cleanup()
		t.Fatal("StartPlaybackStream() error = nil, want strict key refusal")
	}
	assertStrictDownloaderRefusal(t, err, server.URL)
	if got := hits.Load(); got != 0 {
		t.Fatalf("unconfigured key origin hits = %d, want 0", got)
	}
}

func TestPlaybackStreamStrictMediaOriginsReportsRefusedSegmentOrigin(t *testing.T) {
	key := []byte("0123456789abcdef")
	configured := newPlayTestUpstream(t, key, http.StatusOK)
	defer configured.Close()
	cdn, cdnHits := newStrictCountingServer(t)

	d := New(&config.Config{BaseURL: configured.URL, Views: "left", Token: "test-token", StrictMediaOrigins: true}, client.New(nil, nil))
	stream, err := d.StartPlaybackStream(context.Background(), client.ParsedPlaylist{
		KeyURL:        configured.URL + "/key",
		FirstViewURLs: []string{strictMediaURL(cdn.URL)},
	})
	if err != nil {
		t.Fatalf("StartPlaybackStream() error = %v", err)
	}
	defer stream.Cleanup()

	segmentURL := strings.TrimSuffix(stream.URL, "master.m3u8") + "segment/left/0"
	resp, err := http.Get(segmentURL) //nolint:noctx // test-only local request
	if err != nil {
		t.Fatalf("request segment: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}

	select {
	case failure := <-stream.Failures:
		assertStrictDownloaderRefusal(t, failure, cdn.URL)
	default:
		t.Fatal("playback stream did not report the strict origin refusal")
	}
	if got := cdnHits.Load(); got != 0 {
		t.Fatalf("unconfigured segment origin hits = %d, want 0", got)
	}
}
