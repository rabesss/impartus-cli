package downloader

import (
	"context"
	"os"
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

func TestPlanJoinResultMatchesSelectedFinalOutputs(t *testing.T) {
	t.Parallel()

	playlist := client.ParsedPlaylist{
		ID: 40, SeqNo: 7, Title: "Path/Traversal",
		FirstViewURLs: []string{"left"}, SecondViewURLs: []string{"right"},
	}
	tests := []struct {
		name      string
		cfg       *config.Config
		wantPaths []string
		wantViews []string
		wantTypes []string
	}{
		{
			name:      "watch audio left",
			cfg:       &config.Config{DownloadLocation: "/downloads", AudioOnly: true, AudioFormat: "mp3", Views: "left"},
			wantPaths: []string{filepath.Join("/downloads", "LEC 007 Path_Traversal LEFT VIEW.mp3")},
			wantViews: []string{"left"}, wantTypes: []string{"mp3"},
		},
		{
			name: "aac both",
			cfg:  &config.Config{DownloadLocation: "/downloads", AudioOnly: true, AudioFormat: "aac", Views: "both"},
			wantPaths: []string{
				filepath.Join("/downloads", "LEC 007 Path_Traversal LEFT VIEW.m4a"),
				filepath.Join("/downloads", "LEC 007 Path_Traversal RIGHT VIEW.m4a"),
				filepath.Join("/downloads", "LEC 007 Path_Traversal BOTH.m4a"),
			},
			wantViews: []string{"left", "right", "both"}, wantTypes: []string{"m4a", "m4a", "m4a"},
		},
		{
			name:      "video right",
			cfg:       &config.Config{DownloadLocation: "/downloads", Views: "right"},
			wantPaths: []string{filepath.Join("/downloads", "LEC 007 Path_Traversal RIGHT VIEW.mp4")},
			wantViews: []string{"right"}, wantTypes: []string{"mp4"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan, err := PlanJoinResult(test.cfg, playlist)
			if err != nil {
				t.Fatalf("PlanJoinResult() error = %v", err)
			}
			if got := plan.OutputPaths(); !equalStrings(got, test.wantPaths) {
				t.Fatalf("OutputPaths() = %q, want %q", got, test.wantPaths)
			}
			gotViews, gotTypes := joinResultMetadata(plan)
			if !equalStrings(gotViews, test.wantViews) || !equalStrings(gotTypes, test.wantTypes) {
				t.Fatalf("metadata = views:%q containers:%q, want views:%q containers:%q", gotViews, gotTypes, test.wantViews, test.wantTypes)
			}
		})
	}
}

func TestPlanJoinResultMatchesPublishedJoinPaths(t *testing.T) {
	t.Parallel()

	directory := filepath.Join(t.TempDir(), " output with surrounding spaces ")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	left := filepath.Join(directory, "left.m3u8")
	right := filepath.Join(directory, "right.m3u8")
	for _, path := range []string{left, right} {
		if err := os.WriteFile(path, []byte("#EXTM3U"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{DownloadLocation: directory, Views: "both", AudioOnly: true, AudioFormat: "aac"}
	playlist := client.ParsedPlaylist{
		ID: 40, SeqNo: 7, Title: "Stable", FirstViewURLs: []string{"left"}, SecondViewURLs: []string{"right"},
	}
	plan, err := PlanJoinResult(cfg, playlist)
	if err != nil {
		t.Fatal(err)
	}
	download := &Downloader{config: cfg, ffmpegPath: writeFakeFFmpegScript(t, filepath.Join(directory, "ffmpeg.log"), "media")}
	joined, err := download.joinAudioOutput(context.Background(), M3U8File{FirstViewFile: left, SecondViewFile: right, Playlist: playlist})
	if err != nil {
		t.Fatalf("joinAudioOutput() error = %v", err)
	}
	if !equalStrings(joined.OutputPaths(), plan.OutputPaths()) {
		t.Fatalf("published paths = %q, planned paths = %q", joined.OutputPaths(), plan.OutputPaths())
	}
}

func TestPlanJoinResultRejectsUnavailableSelectedView(t *testing.T) {
	t.Parallel()

	_, err := PlanJoinResult(&config.Config{DownloadLocation: t.TempDir(), Views: "left"}, client.ParsedPlaylist{
		ID: 1, SeqNo: 1, Title: "No left", SecondViewURLs: []string{"right"},
	})
	if err == nil {
		t.Fatal("PlanJoinResult() error = nil, want unavailable-view error")
	}
}

func joinResultMetadata(result JoinResult) ([]string, []string) {
	views, containers := make([]string, 0, 3), make([]string, 0, 3)
	for _, output := range []struct{ path, view, container string }{
		{result.LeftOutput, "left", result.LeftContainer},
		{result.RightOutput, "right", result.RightContainer},
		{result.BothOutput, "both", result.BothContainer},
	} {
		if output.path != "" {
			views = append(views, output.view)
			containers = append(containers, output.container)
		}
	}
	return views, containers
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
