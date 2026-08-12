package downloader

import (
	"path/filepath"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

func TestPlanJoinResultMatchesPublishedOutputNames(t *testing.T) {
	t.Parallel()

	location := t.TempDir()
	playlist := client.ParsedPlaylist{
		ID: 9, SeqNo: 7, Title: "Consensus",
		FirstViewURLs: []string{"left.ts"}, SecondViewURLs: []string{"right.ts"},
	}
	tests := []struct {
		name string
		cfg  config.Config
		want JoinResult
	}{
		{
			name: "video left",
			cfg:  config.Config{Views: "left"},
			want: JoinResult{LeftOutput: filepath.Join(location, "LEC 007 Consensus LEFT VIEW.mp4"), LeftContainer: "mp4"},
		},
		{
			name: "video right",
			cfg:  config.Config{Views: "right"},
			want: JoinResult{RightOutput: filepath.Join(location, "LEC 007 Consensus RIGHT VIEW.mp4"), RightContainer: "mp4"},
		},
		{
			name: "video both",
			cfg:  config.Config{Views: "both"},
			want: JoinResult{
				LeftOutput: filepath.Join(location, "LEC 007 Consensus LEFT VIEW.mp4"), LeftContainer: "mp4",
				RightOutput: filepath.Join(location, "LEC 007 Consensus RIGHT VIEW.mp4"), RightContainer: "mp4",
				BothOutput: filepath.Join(location, "LEC 007 Consensus BOTH.mkv"), BothContainer: "mkv",
			},
		},
		{
			name: "audio mp3 both",
			cfg:  config.Config{Views: "both", AudioOnly: true, AudioFormat: "mp3"},
			want: JoinResult{
				LeftOutput: filepath.Join(location, "LEC 007 Consensus LEFT VIEW.mp3"), LeftContainer: "mp3",
				RightOutput: filepath.Join(location, "LEC 007 Consensus RIGHT VIEW.mp3"), RightContainer: "mp3",
				BothOutput: filepath.Join(location, "LEC 007 Consensus BOTH.mp3"), BothContainer: "mp3",
			},
		},
		{
			name: "audio aac publishes m4a",
			cfg:  config.Config{Views: "both", AudioOnly: true, AudioFormat: "aac"},
			want: JoinResult{
				LeftOutput: filepath.Join(location, "LEC 007 Consensus LEFT VIEW.m4a"), LeftContainer: "m4a",
				RightOutput: filepath.Join(location, "LEC 007 Consensus RIGHT VIEW.m4a"), RightContainer: "m4a",
				BothOutput: filepath.Join(location, "LEC 007 Consensus BOTH.m4a"), BothContainer: "m4a",
			},
		},
		{
			name: "audio m4a both",
			cfg:  config.Config{Views: "both", AudioOnly: true, AudioFormat: "m4a"},
			want: JoinResult{
				LeftOutput: filepath.Join(location, "LEC 007 Consensus LEFT VIEW.m4a"), LeftContainer: "m4a",
				RightOutput: filepath.Join(location, "LEC 007 Consensus RIGHT VIEW.m4a"), RightContainer: "m4a",
				BothOutput: filepath.Join(location, "LEC 007 Consensus BOTH.m4a"), BothContainer: "m4a",
			},
		},
		{
			name: "audio opus both",
			cfg:  config.Config{Views: "both", AudioOnly: true, AudioFormat: "opus"},
			want: JoinResult{
				LeftOutput: filepath.Join(location, "LEC 007 Consensus LEFT VIEW.opus"), LeftContainer: "opus",
				RightOutput: filepath.Join(location, "LEC 007 Consensus RIGHT VIEW.opus"), RightContainer: "opus",
				BothOutput: filepath.Join(location, "LEC 007 Consensus BOTH.opus"), BothContainer: "opus",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.cfg.DownloadLocation = location
			got, err := PlanJoinResult(&test.cfg, playlist)
			if err != nil {
				t.Fatalf("PlanJoinResult() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("PlanJoinResult() = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestPlanJoinResultRejectsNoncanonicalAudioFormat(t *testing.T) {
	t.Parallel()

	for _, format := range []string{"AAC", " aac "} {
		_, err := PlanJoinResult(&config.Config{
			DownloadLocation: t.TempDir(),
			Views:            "left",
			AudioOnly:        true,
			AudioFormat:      format,
		}, client.ParsedPlaylist{ID: 9, FirstViewURLs: []string{"left.ts"}})
		if err == nil {
			t.Fatalf("PlanJoinResult(AudioFormat=%q) error = nil", format)
		}
	}
}

func TestPlanJoinResultBothAcceptsSingleCameraLecture(t *testing.T) {
	result, err := PlanJoinResult(&config.Config{
		DownloadLocation: t.TempDir(),
		Views:            "both",
	}, client.ParsedPlaylist{
		ID:            9,
		SeqNo:         1,
		Title:         "Single camera",
		FirstViewURLs: []string{"segment.ts"},
	})
	if err != nil {
		t.Fatalf("PlanJoinResult() error = %v", err)
	}
	if result.LeftOutput == "" || result.RightOutput != "" || result.BothOutput != "" {
		t.Fatalf("PlanJoinResult() = %+v, want left-only output", result)
	}
}
