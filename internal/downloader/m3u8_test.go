package downloader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

func TestWriteM3U8FileContent(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "out.m3u8")
	chunks := []string{filepath.Join(dir, "chunk0.ts"), filepath.Join(dir, "chunk1.ts")}

	if err := writeM3U8File(manifest, chunks); err != nil {
		t.Fatalf("writeM3U8File: %v", err)
	}

	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	content := string(data)

	for _, want := range []string{"#EXTM3U", "#EXT-X-VERSION:3", "#EXT-X-KEY:METHOD=NONE", "#EXT-X-ENDLIST"} {
		if !strings.Contains(content, want) {
			t.Errorf("manifest missing %q\n%s", want, content)
		}
	}
	if got := strings.Count(content, "#EXTINF:1"); got != len(chunks) {
		t.Errorf("#EXTINF count = %d, want %d", got, len(chunks))
	}
	for _, c := range chunks {
		if !strings.Contains(content, c) {
			t.Errorf("manifest missing absolute chunk path %q", c)
		}
	}
}

func TestWriteM3U8FileRelativeChunkPath(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "sub", "out.m3u8")
	if err := os.MkdirAll(filepath.Dir(manifest), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Absolute chunk in the parent directory should be rewritten relative to the manifest dir.
	chunk := filepath.Join(dir, "chunk0.ts")
	if err := writeM3U8File(manifest, []string{chunk}); err != nil {
		t.Fatalf("writeM3U8File: %v", err)
	}
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if !strings.Contains(string(data), "chunk0.ts") {
		t.Errorf("manifest missing chunk reference:\n%s", data)
	}
}

func TestCreateTempM3U8FileNaming(t *testing.T) {
	dir := t.TempDir()
	d := &Downloader{config: &config.Config{TempDirLocation: dir}}

	playlist := DownloadedPlaylist{
		FirstViewChunks:  []string{filepath.Join(dir, "a.ts")},
		SecondViewChunks: []string{filepath.Join(dir, "b.ts")},
		Playlist:         client.ParsedPlaylist{ID: 42},
	}

	m3u8File, err := d.CreateTempM3U8File(playlist)
	if err != nil {
		t.Fatalf("CreateTempM3U8File: %v", err)
	}

	for _, f := range []string{m3u8File.FirstViewFile, m3u8File.SecondViewFile} {
		if f == "" {
			t.Fatal("expected a non-empty view file path")
		}
		if _, statErr := os.Stat(f); statErr != nil {
			t.Errorf("expected manifest %q to exist: %v", f, statErr)
		}
	}
	if !strings.HasSuffix(m3u8File.FirstViewFile, "42_first.m3u8") {
		t.Errorf("unexpected first view file name %q", m3u8File.FirstViewFile)
	}
	if !strings.HasSuffix(m3u8File.SecondViewFile, "42_second.m3u8") {
		t.Errorf("unexpected second view file name %q", m3u8File.SecondViewFile)
	}
}
