package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
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
		{name: "rejects zero start", args: []string{"-s", "1", "-S", "2", "--start", "0"}, want: "--start must be a positive 1-based index"},
		{name: "rejects negative start", args: []string{"-s", "1", "-S", "2", "--start", "-1"}, want: "--start must be a positive 1-based index"},
		{name: "rejects zero end", args: []string{"-s", "1", "-S", "2", "--end", "0"}, want: "--end must be a positive 1-based index"},
		{name: "rejects negative end", args: []string{"-s", "1", "-S", "2", "--end", "-1"}, want: "--end must be a positive 1-based index"},
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

func TestInvalidExplicitDownloadRangeStopsBeforePreflight(t *testing.T) {
	for _, args := range [][]string{
		{"-s", "1", "-S", "2", "--start", "0"},
		{"-s", "1", "-S", "2", "--end", "0"},
	} {
		t.Run(strings.Join(args[len(args)-2:], "_"), func(t *testing.T) {
			_, err := executeDownloadWithDependencies(args, quietDownloadPresentation(), downloadExecutionDependencies{
				ensureFFmpeg: func() error {
					t.Fatal("invalid range reached FFmpeg preflight")
					return nil
				},
				initClient: func(context.Context) (*config.Config, *client.Client, error) {
					t.Fatal("invalid range reached client login")
					return nil, nil, nil
				},
			})
			if err == nil || !strings.Contains(err.Error(), "positive 1-based index") {
				t.Fatalf("executeDownloadWithDependencies() error = %v, want range rejection", err)
			}
		})
	}
}
