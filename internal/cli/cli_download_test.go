package cli

import "testing"

func TestParseDownloadFlags(t *testing.T) {
	t.Run("valid full flags", func(t *testing.T) {
		f, err := parseDownloadFlags([]string{
			"-s", "1", "-S", "2", "--start", "1", "--end", "3",
			"--quality", "720", "--views", "both", "--audio-only",
			"--format", "mp3", "-o", "/tmp/out", "--skip-no-audio",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if f.subject != 1 || f.session != 2 || f.start != 1 || f.end != 3 {
			t.Errorf("ids/range mismatch: %+v", f)
		}
		if f.quality != "720" || f.views != "both" || !f.audioOnly || f.format != "mp3" || f.output != "/tmp/out" || !f.skipNoAudio {
			t.Errorf("flag values mismatch: %+v", f)
		}
	})

	t.Run("requires subject and session", func(t *testing.T) {
		if _, err := parseDownloadFlags([]string{"--start", "1"}); err == nil {
			t.Fatal("expected error for missing subject/session")
		}
	})

	t.Run("rejects positional arguments", func(t *testing.T) {
		if _, err := parseDownloadFlags([]string{"-s", "1", "-S", "2", "extra"}); err == nil {
			t.Fatal("expected error for positional argument")
		}
	})

	t.Run("rejects unknown flag", func(t *testing.T) {
		if _, err := parseDownloadFlags([]string{"--nope"}); err == nil {
			t.Fatal("expected error for unknown flag")
		}
	})
}
