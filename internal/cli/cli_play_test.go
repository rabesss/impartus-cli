package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/config"
)

func TestValidateFlagOverrides(t *testing.T) {
	cases := []struct {
		name    string
		cfg     *config.Config
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid quality 144",
			cfg:     &config.Config{Quality: "144"},
			wantErr: false,
		},
		{
			name:    "valid quality 450",
			cfg:     &config.Config{Quality: "450"},
			wantErr: false,
		},
		{
			name:    "valid quality 720",
			cfg:     &config.Config{Quality: "720"},
			wantErr: false,
		},
		{
			name:    "invalid quality 1080",
			cfg:     &config.Config{Quality: "1080"},
			wantErr: true,
			errMsg:  "invalid quality value \"1080\"",
		},
		{
			name:    "invalid quality hd",
			cfg:     &config.Config{Quality: "hd"},
			wantErr: true,
			errMsg:  "invalid quality value \"hd\"",
		},
		{
			name:    "valid views first",
			cfg:     &config.Config{Views: "first"},
			wantErr: false,
		},
		{
			name:    "valid views second",
			cfg:     &config.Config{Views: "second"},
			wantErr: false,
		},
		{
			name:    "valid views both",
			cfg:     &config.Config{Views: "both"},
			wantErr: false,
		},
		{
			name:    "valid views left (legacy)",
			cfg:     &config.Config{Views: "left"},
			wantErr: false,
		},
		{
			name:    "valid views right (legacy)",
			cfg:     &config.Config{Views: "right"},
			wantErr: false,
		},
		{
			name:    "invalid views sideways",
			cfg:     &config.Config{Views: "sideways"},
			wantErr: true,
			errMsg:  "invalid views value \"sideways\"",
		},
		{
			name:    "valid audio format mp3",
			cfg:     &config.Config{AudioOnly: true, AudioFormat: "mp3"},
			wantErr: false,
		},
		{
			name:    "valid audio format m4a",
			cfg:     &config.Config{AudioOnly: true, AudioFormat: "m4a"},
			wantErr: false,
		},
		{
			name:    "valid audio format aac",
			cfg:     &config.Config{AudioOnly: true, AudioFormat: "aac"},
			wantErr: false,
		},
		{
			name:    "valid audio format opus",
			cfg:     &config.Config{AudioOnly: true, AudioFormat: "opus"},
			wantErr: false,
		},
		{
			name:    "invalid audio format wav",
			cfg:     &config.Config{AudioOnly: true, AudioFormat: "wav"},
			wantErr: true,
			errMsg:  "invalid audioFormat value \"wav\"",
		},
		{
			name:    "empty config is valid",
			cfg:     &config.Config{},
			wantErr: false,
		},
		{
			name:    "empty quality is valid",
			cfg:     &config.Config{Quality: ""},
			wantErr: false,
		},
		{
			name:    "empty views is valid",
			cfg:     &config.Config{Views: ""},
			wantErr: false,
		},
		{
			name:    "empty audio format is valid when audio-only",
			cfg:     &config.Config{AudioOnly: true, AudioFormat: ""},
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateFlagOverrides(tc.cfg)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error but got nil")
				}
				if !strings.Contains(err.Error(), tc.errMsg) {
					t.Fatalf("expected error containing %q, got %q", tc.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestExecutePlayDelegates(t *testing.T) {
	restoreCLIState(t)
	called := false
	runPlayFn = func(args []string) error {
		called = true
		if len(args) != 2 || args[0] != "-s" || args[1] != "1" {
			t.Errorf("unexpected args passed to play: %v", args)
		}
		return nil
	}
	os.Args = []string{"impartus", "play", "-s", "1"}

	if err := Execute("dev", ""); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !called {
		t.Fatal("expected play subcommand to be executed")
	}
}

func TestValidatePlayFlagsRejectsPartialDirectSelection(t *testing.T) {
	tests := []struct {
		name string
		in   playFlags
		want string
	}{
		{
			name: "subject without session",
			in:   playFlags{subject: 1},
			want: "requires both",
		},
		{
			name: "session without subject",
			in:   playFlags{session: 2},
			want: "requires both",
		},
		{
			name: "lecture range without direct course",
			in:   playFlags{start: 1, end: 2},
			want: "range flags require",
		},
		{
			name: "negative subject",
			in:   playFlags{subject: -1, session: 2},
			want: "positive",
		},
		{
			name: "negative lecture",
			in:   playFlags{lecture: -1},
			want: "selection values must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePlayFlags(tt.in)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestParsePlayFlags(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantErr       bool
		wantSubject   int
		wantSession   int
		wantStart     int
		wantEnd       int
		wantQuality   string
		wantViews     string
		wantSkipNA    bool
		wantIncludeNA bool
	}{
		{
			name:    "empty args gives all defaults",
			args:    []string{},
			wantErr: false,
		},
		{
			name:        "-s 1 -S 2 sets subject and session",
			args:        []string{"-s", "1", "-S", "2"},
			wantSubject: 1,
			wantSession: 2,
		},
		{
			name:      "-l 3 sets start and end to same value",
			args:      []string{"-l", "3"},
			wantStart: 3,
			wantEnd:   3,
		},
		{
			name:      "--start 2 --end 5 sets range",
			args:      []string{"--start", "2", "--end", "5"},
			wantStart: 2,
			wantEnd:   5,
		},
		{
			name:        "--quality 720 --views left",
			args:        []string{"--quality", "720", "--views", "left"},
			wantQuality: "720",
			wantViews:   "left",
		},
		{
			name:       "--skip-no-audio sets flag",
			args:       []string{"--skip-no-audio"},
			wantSkipNA: true,
		},
		{
			name:          "--include-noaudio sets flag",
			args:          []string{"--include-noaudio"},
			wantIncludeNA: true,
		},
		{
			name:    "unknown flag returns error",
			args:    []string{"--unknown"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := parsePlayFlags(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error but got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if f.subject != tt.wantSubject {
				t.Errorf("subject = %d, want %d", f.subject, tt.wantSubject)
			}
			if f.session != tt.wantSession {
				t.Errorf("session = %d, want %d", f.session, tt.wantSession)
			}
			if f.start != tt.wantStart {
				t.Errorf("start = %d, want %d", f.start, tt.wantStart)
			}
			if f.end != tt.wantEnd {
				t.Errorf("end = %d, want %d", f.end, tt.wantEnd)
			}
			if f.quality != tt.wantQuality {
				t.Errorf("quality = %q, want %q", f.quality, tt.wantQuality)
			}
			if f.views != tt.wantViews {
				t.Errorf("views = %q, want %q", f.views, tt.wantViews)
			}
			if f.skipNoAudio != tt.wantSkipNA {
				t.Errorf("skipNoAudio = %v, want %v", f.skipNoAudio, tt.wantSkipNA)
			}
			if f.includeNoAudio != tt.wantIncludeNA {
				t.Errorf("includeNoAudio = %v, want %v", f.includeNoAudio, tt.wantIncludeNA)
			}
		})
	}
}
