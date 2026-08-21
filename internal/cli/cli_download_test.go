package cli

import (
	"strings"
	"testing"
)

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
		if f.quality != "720" || f.views != "both" || !f.audioOnly || !f.audioOnlySet || f.format != "mp3" || f.output != "/tmp/out" || !f.skipNoAudio {
			t.Errorf("flag values mismatch: %+v", f)
		}
	})

	t.Run("tracks an explicit false audio-only override", func(t *testing.T) {
		f, err := parseDownloadFlags([]string{"-s", "1", "-S", "2", "--audio-only=false"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if f.audioOnly || !f.audioOnlySet {
			t.Fatalf("audio-only flags = %+v, want explicit false override", f)
		}
	})

	t.Run("accepts exact TTID without a range", func(t *testing.T) {
		f, err := parseDownloadFlags([]string{"-s", "1", "-S", "2", "--ttid", "987"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if f.ttid != 987 || !f.ttidSet || f.startSet || f.endSet {
			t.Fatalf("TTID flags = %+v, want exact-only selection", f)
		}
	})

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "rejects non-positive TTID", args: []string{"-s", "1", "-S", "2", "--ttid", "0"}, want: "--ttid must be positive"},
		{name: "rejects TTID with start", args: []string{"-s", "1", "-S", "2", "--ttid", "9", "--start", "1"}, want: "cannot be combined"},
		{name: "rejects TTID with end", args: []string{"-s", "1", "-S", "2", "--ttid", "9", "--end", "1"}, want: "cannot be combined"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseDownloadFlags(tc.args); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("parseDownloadFlags() error = %v, want %q", err, tc.want)
			}
		})
	}

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
