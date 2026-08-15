//go:build !windows

package player

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestRealMPVSmoke(t *testing.T) {
	if os.Getenv("IMPARTUS_MPV_SMOKE") != "1" {
		t.Skip("set IMPARTUS_MPV_SMOKE=1 to run the real mpv integration smoke")
	}
	if err := CheckBinary("mpv"); err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Fatal("real mpv smoke requires ffmpeg on PATH")
	}

	root := t.TempDir()
	segmentPath := filepath.Join(root, "segment.ts")
	generateCtx, cancelGenerate := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelGenerate()
	command := exec.CommandContext(generateCtx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=880:duration=2", "-c:a", "aac", "-f", "mpegts", segmentPath) // #nosec G204 -- fixed local smoke command
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generate smoke media: %v: %s", err, output)
	}
	segment, err := os.ReadFile(segmentPath)
	if err != nil {
		t.Fatalf("read smoke segment: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			if _, writeErr := fmt.Fprint(w, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:2\n#EXTINF:2.000,\n/segment.ts\n#EXT-X-ENDLIST\n"); writeErr != nil {
				return
			}
		case "/segment.ts":
			w.Header().Set("Content-Type", "video/MP2T")
			if _, writeErr := w.Write(segment); writeErr != nil {
				return
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	session, err := Start(ctx, Options{Binary: "mpv", VideoOutput: "null", AudioOutput: "null"})
	if err != nil {
		t.Fatalf("start real mpv: %v", err)
	}
	pid := session.ProcessID()
	cleanupSession(t, session)
	if err := session.Load(ctx, server.URL+"/master.m3u8"); err != nil {
		t.Fatalf("load real smoke playlist: %v", err)
	}
	observed := false
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for !observed {
		select {
		case event, open := <-session.Events():
			if !open {
				t.Fatal("real mpv event stream closed before loaded state was observed")
			}
			if event.Name == "property-change" && event.Property == "duration" {
				var duration float64
				observed = json.Unmarshal(event.Data, &duration) == nil && duration > 0
			}
		case <-deadline.C:
			t.Fatal("timed out observing loaded real mpv state")
		}
	}
	if err := session.Pause(ctx, true); err != nil {
		t.Fatalf("pause real mpv: %v", err)
	}
	if err := session.SeekRelative(ctx, 0.1); err != nil {
		t.Fatalf("seek real mpv: %v", err)
	}
	if err := session.Pause(ctx, false); err != nil {
		t.Fatalf("resume real mpv: %v", err)
	}

	if err := session.WaitForEnd(ctx); err != nil {
		t.Fatalf("wait for real media end: %v", err)
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("close real mpv: %v", err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("real mpv child was not reaped: %v", err)
	}
}
