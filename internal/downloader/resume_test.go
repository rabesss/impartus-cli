package downloader

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/lockfile"
)

// resumeServer serves a key and encrypted chunks /c0.ts, /c1.ts, ... and
// counts requests per path. A path in fail answers with that status.
type resumeServer struct {
	t        *testing.T
	mu       sync.Mutex
	key      []byte
	chunks   map[string][]byte // path -> plaintext
	fail     map[string]int
	requests map[string]int
	server   *httptest.Server
}

func newResumeServer(t *testing.T, chunkCount int) *resumeServer {
	t.Helper()
	s := &resumeServer{t: t, key: []byte("1234567890123456"), chunks: map[string][]byte{}, fail: map[string]int{}, requests: map[string]int{}}
	for i := range chunkCount {
		s.chunks[fmt.Sprintf("/c%d.ts", i)] = bytes.Repeat([]byte(fmt.Sprintf("chunk-%d;", i)), 10+i)
	}
	s.server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.server.Close)
	return s
}

func (s *resumeServer) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests[r.URL.Path]++
	status, failing := s.fail[r.URL.Path]
	key := append([]byte(nil), s.key...)
	plaintext, known := s.chunks[r.URL.Path]
	s.mu.Unlock()
	switch {
	case failing:
		http.Error(w, "unavailable", status)
	case r.URL.Path == "/key":
		_, _ = w.Write(fakeKeyResponse(key)) //nolint:errcheck
	case known:
		_, _ = w.Write(encryptCBCZeroIV(s.t, plaintext, key)) //nolint:errcheck
	default:
		http.NotFound(w, r)
	}
}

func (s *resumeServer) resetCounts() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = map[string]int{}
}

func (s *resumeServer) count(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests[path]
}

func (s *resumeServer) playlist(paths ...string) client.ParsedPlaylist {
	urls := make([]string, len(paths))
	for i, path := range paths {
		urls[i] = s.server.URL + path
	}
	return client.ParsedPlaylist{KeyURL: s.server.URL + "/key", Title: "Resume", ID: 42, SeqNo: 1, FirstViewURLs: urls}
}

func (s *resumeServer) plaintext(paths ...string) []byte {
	var out []byte
	for _, path := range paths {
		out = append(out, s.chunks[path]...)
	}
	return out
}

// writeConcatFFmpegScript is a fake ffmpeg that concatenates the files the
// input playlist lists, so output bytes reflect exactly which chunks joined.
func writeConcatFFmpegScript(t *testing.T) string {
	t.Helper()
	scriptPath := filepath.Join(t.TempDir(), "ffmpeg")
	script := `#!/usr/bin/env bash
set -eu
input=""
previous=""
last=""
for arg in "$@"; do
  if [[ "$previous" == "-i" && -z "$input" ]]; then
    input="$arg"
  fi
  previous="$arg"
  last="$arg"
done
: > "$last"
dir="$(dirname "$input")"
while IFS= read -r line; do
  [[ -z "$line" || "$line" == \#* ]] && continue
  if [[ "$line" == /* ]]; then cat "$line" >> "$last"; else cat "$dir/$line" >> "$last"; fi
done < "$input"
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil { // #nosec G306 -- test executable
		t.Fatalf("WriteFile(fake ffmpeg) failed: %v", err)
	}
	return scriptPath
}

type resumeHarness struct {
	server   *resumeServer
	tempBase string
	d        *Downloader
}

func newResumeHarness(t *testing.T, pipeline bool, chunkCount int) *resumeHarness {
	t.Helper()
	server := newResumeServer(t, chunkCount)
	tempBase := t.TempDir()
	writeWorkspaceSentinel(t, tempBase)
	d := workspaceTestDownloader(tempBase, t.TempDir(), server.server, writeConcatFFmpegScript(t), pipeline)
	return &resumeHarness{server: server, tempBase: tempBase, d: d}
}

func (h *resumeHarness) run(t *testing.T, playlist client.ParsedPlaylist, tracker *ProgressTracker) (JoinResult, error) {
	t.Helper()
	return h.d.DownloadAndJoinPlaylist(context.Background(), playlist, nil, tracker)
}

func readOutput(t *testing.T, result JoinResult) []byte {
	t.Helper()
	data, err := os.ReadFile(result.LeftOutput)
	if err != nil {
		t.Fatalf("ReadFile(output) failed: %v", err)
	}
	return data
}

func TestResumeFetchesOnlyMissingChunks(t *testing.T) {
	forEachDownloadPath(t, func(t *testing.T, pipeline bool) {
		paths := []string{"/c0.ts", "/c1.ts", "/c2.ts"}
		h := newResumeHarness(t, pipeline, 3)
		playlist := h.server.playlist(paths...)

		h.server.fail["/c1.ts"] = http.StatusNotFound
		if _, err := h.run(t, playlist, nil); err == nil {
			t.Fatal("first run error = nil, want chunk 1 failure")
		}
		view := "left"
		if pipeline {
			view = "first"
		}
		workspace := onlyResumeWorkspace(t, h.tempBase, 42)
		want := []string{".lock", "42_" + view + "_0000.ts", "42_" + view + "_0002.ts", "resume.json"}
		if got := dirNames(t, workspace); !slicesEqual(got, want) {
			t.Fatalf("kept workspace = %v, want %v", got, want)
		}

		delete(h.server.fail, "/c1.ts")
		h.server.resetCounts()
		result, err := h.run(t, playlist, nil)
		if err != nil {
			t.Fatalf("second run error = %v", err)
		}
		for path, wantCount := range map[string]int{"/key": 1, "/c0.ts": 0, "/c1.ts": 1, "/c2.ts": 0} {
			if got := h.server.count(path); got != wantCount {
				t.Fatalf("second run %s requests = %d, want %d", path, got, wantCount)
			}
		}

		clean := newResumeHarness(t, pipeline, 3)
		cleanResult, err := clean.run(t, clean.server.playlist(paths...), nil)
		if err != nil {
			t.Fatalf("clean run error = %v", err)
		}
		if got, want := readOutput(t, result), readOutput(t, cleanResult); !bytes.Equal(got, want) || !bytes.Equal(got, h.server.plaintext(paths...)) {
			t.Fatalf("resumed output = %q, want clean output %q", got, want)
		}
		assertOnlyWorkspaceSentinel(t, h.tempBase)
	})
}

func TestResumeRestartsWhenPlaylistChanges(t *testing.T) {
	h := newResumeHarness(t, false, 3)
	h.server.chunks["/c2b.ts"] = []byte("replacement chunk")
	h.server.fail["/c1.ts"] = http.StatusNotFound
	if _, err := h.run(t, h.server.playlist("/c0.ts", "/c1.ts", "/c2.ts"), nil); err == nil {
		t.Fatal("first run error = nil, want failure")
	}

	delete(h.server.fail, "/c1.ts")
	h.server.resetCounts()
	changed := []string{"/c0.ts", "/c1.ts", "/c2b.ts"}
	result, err := h.run(t, h.server.playlist(changed...), nil)
	if err != nil {
		t.Fatalf("second run error = %v", err)
	}
	for _, path := range changed {
		if got := h.server.count(path); got != 1 {
			t.Fatalf("%s requests = %d, want 1 (full re-download)", path, got)
		}
	}
	if got := readOutput(t, result); !bytes.Equal(got, h.server.plaintext(changed...)) {
		t.Fatalf("output = %q, want %q", got, h.server.plaintext(changed...))
	}
	assertOnlyWorkspaceSentinel(t, h.tempBase) // the old playlist's workspace was swept
}

func TestResumeRestartsWhenKeyChanges(t *testing.T) {
	paths := []string{"/c0.ts", "/c1.ts", "/c2.ts"}
	h := newResumeHarness(t, false, 3)
	h.server.fail["/c1.ts"] = http.StatusNotFound
	if _, err := h.run(t, h.server.playlist(paths...), nil); err == nil {
		t.Fatal("first run error = nil, want failure")
	}

	delete(h.server.fail, "/c1.ts")
	h.server.mu.Lock()
	h.server.key = []byte("6543210987654321")
	h.server.mu.Unlock()
	h.server.resetCounts()
	result, err := h.run(t, h.server.playlist(paths...), nil)
	if err != nil {
		t.Fatalf("second run error = %v", err)
	}
	for _, path := range paths {
		if got := h.server.count(path); got != 1 {
			t.Fatalf("%s requests = %d, want 1 after a key change", path, got)
		}
	}
	if got := readOutput(t, result); !bytes.Equal(got, h.server.plaintext(paths...)) {
		t.Fatalf("output = %q, want %q", got, h.server.plaintext(paths...))
	}
}

func TestResumeDoesNotReuseAcrossDownloadModes(t *testing.T) {
	paths := []string{"/c0.ts", "/c1.ts", "/c2.ts"}
	h := newResumeHarness(t, false, 3)
	h.server.fail["/c1.ts"] = http.StatusNotFound
	if _, err := h.run(t, h.server.playlist(paths...), nil); err == nil {
		t.Fatal("serial run error = nil, want failure")
	}

	delete(h.server.fail, "/c1.ts")
	h.server.resetCounts()
	h.d.config.EnablePipeline = true
	if _, err := h.run(t, h.server.playlist(paths...), nil); err != nil {
		t.Fatalf("pipelined run error = %v", err)
	}
	for _, path := range paths {
		if got := h.server.count(path); got != 1 {
			t.Fatalf("%s requests = %d, want 1: serial chunks must not feed a pipelined join", path, got)
		}
	}
	assertOnlyWorkspaceSentinel(t, h.tempBase)
}

func TestResumeIgnoresPartialAndEmptyChunks(t *testing.T) {
	paths := []string{"/c0.ts", "/c1.ts", "/c2.ts"}
	h := newResumeHarness(t, false, 3)
	h.server.fail["/c1.ts"] = http.StatusNotFound
	if _, err := h.run(t, h.server.playlist(paths...), nil); err == nil {
		t.Fatal("first run error = nil, want failure")
	}
	workspace := onlyResumeWorkspace(t, h.tempBase, 42)
	for name, data := range map[string]string{"42_left_0001.ts": "", "42_left_0001.ts.part": "trunc", "42_left_0001.ts.temp": "junk"} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	delete(h.server.fail, "/c1.ts")
	h.server.resetCounts()
	result, err := h.run(t, h.server.playlist(paths...), nil)
	if err != nil {
		t.Fatalf("second run error = %v", err)
	}
	if got := h.server.count("/c1.ts"); got != 1 {
		t.Fatalf("/c1.ts requests = %d, want 1: empty and partial files are not complete", got)
	}
	if got := readOutput(t, result); !bytes.Equal(got, h.server.plaintext(paths...)) {
		t.Fatalf("output = %q, want %q", got, h.server.plaintext(paths...))
	}
}

func TestResumedChunksReportRealSizes(t *testing.T) {
	forEachDownloadPath(t, func(t *testing.T, pipeline bool) {
		paths := []string{"/c0.ts", "/c1.ts", "/c2.ts"}
		h := newResumeHarness(t, pipeline, 3)
		h.server.fail["/c1.ts"] = http.StatusNotFound
		if _, err := h.run(t, h.server.playlist(paths...), nil); err == nil {
			t.Fatal("first run error = nil, want failure")
		}

		delete(h.server.fail, "/c1.ts")
		tracker := NewProgressTrackerWithOptions(1, 3, nil, ProgressTrackerOptions{})
		defer tracker.Stop()
		if _, err := h.run(t, h.server.playlist(paths...), tracker); err != nil {
			t.Fatalf("second run error = %v", err)
		}
		wantResumed := int64(len(h.server.plaintext("/c0.ts", "/c2.ts")))
		if got := atomic.LoadInt64(&tracker.resumedBytes); got != wantResumed {
			t.Fatalf("resumed bytes = %d, want %d", got, wantResumed)
		}
		if got := atomic.LoadInt32(&tracker.completedChunks); got != 3 {
			t.Fatalf("completed chunks = %d, want 3", got)
		}
		if got := atomic.LoadInt64(&tracker.downloadedBytes); got <= wantResumed {
			t.Fatalf("downloaded bytes = %d, want resumed %d plus chunk 1", got, wantResumed)
		}
	})
}

func TestResumeRefusesSymlinkedWorkspace(t *testing.T) {
	h := newResumeHarness(t, false, 1)
	playlist := h.server.playlist("/c0.ts")
	name := fmt.Sprintf("lecture-42-resume-%s", h.d.resumeFingerprint(playlist)[:16])
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(h.tempBase, name)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, err := h.run(t, playlist, nil); err != nil {
		t.Fatalf("run error = %v, want success in a one-off workspace", err)
	}
	if got := dirNames(t, target); len(got) != 0 {
		t.Fatalf("symlink target entries = %v, want untouched", got)
	}
	if info, err := os.Lstat(filepath.Join(h.tempBase, name)); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink was removed or replaced (err=%v)", err)
	}
}

func TestSweepResumeWorkspaces(t *testing.T) {
	base := t.TempDir()
	now := time.Now()
	makeWorkspace := func(name, fingerprint string, age time.Duration) string {
		t.Helper()
		dir := filepath.Join(base, name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if fingerprint != "" {
			meta := filepath.Join(dir, resumeMetaName)
			data := fmt.Sprintf(`{"version":1,"fingerprint":%q}`, fingerprint)
			if err := os.WriteFile(meta, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(meta, now.Add(-age), now.Add(-age)); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	const old, recent = 8 * 24 * time.Hour, 6 * 24 * time.Hour
	makeWorkspace("lecture-1-resume-0000000000000001", "fp1", old)           // stale: removed
	makeWorkspace("lecture-2-resume-0000000000000002", "fp2", recent)        // fresh: kept
	locked := makeWorkspace("lecture-3-resume-0000000000000003", "fp3", old) // stale but in use: kept
	makeWorkspace("lecture-4-resume-0000000000000004", "", old)              // no metadata: kept
	makeWorkspace(".lecture-5-trash-00000000000000aa", "", 0)                // trash: removed
	makeWorkspace("lecture-7-resume-0000000000000007", "fp7", old)           // current: kept
	makeWorkspace("lecture-9-resume-0000000000000009", "fp9-old", recent)    // same lecture, old playlist: removed
	makeWorkspace("lecture-10-resume-000000000000000a", "fp10", recent)      // other lecture: kept
	makeWorkspace("unrelated", "fpx", old)                                   // not ours: kept
	symlinkTarget := makeWorkspace(filepath.Join("..", filepath.Base(t.TempDir()), "target"), "fp6", old)
	if err := os.Symlink(symlinkTarget, filepath.Join(base, "lecture-6-resume-0000000000000006")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	lock, err := lockfile.TryAcquire(filepath.Join(locked, resumeLockName))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }() //nolint:errcheck

	var logs strings.Builder
	d := NewWithDiagnosticWriter(nil, nil, &logs)
	d.sweepResumeWorkspaces(base, 9, "fp9-new", "lecture-7-resume-0000000000000007", now)

	want := []string{
		"lecture-10-resume-000000000000000a",
		"lecture-2-resume-0000000000000002",
		"lecture-3-resume-0000000000000003",
		"lecture-4-resume-0000000000000004",
		"lecture-6-resume-0000000000000006",
		"lecture-7-resume-0000000000000007",
		"unrelated",
	}
	if got := dirNames(t, base); !slicesEqual(got, want) {
		t.Fatalf("after sweep = %v, want %v (logs: %s)", got, want, logs.String())
	}
	if _, err := os.Stat(filepath.Join(symlinkTarget, resumeMetaName)); err != nil {
		t.Fatalf("symlink target was touched: %v", err)
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
