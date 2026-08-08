package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/downloader"
)

func TestDownloadArtifactsAllowTTIDCollisionAcrossScopes(t *testing.T) {
	outputDir := t.TempDir()
	results := materializeJoinResults(t, outputDir, []downloader.JoinResult{
		{LeftOutput: "first.mp4"},
		{LeftOutput: "second.mp4"},
	})
	runner := &fakeLectureDownloadRunner{
		playlists: []client.ParsedPlaylist{
			{InstituteID: 10, SubjectID: 20, SessionID: 30, ID: 9},
			{InstituteID: 1, SubjectID: 2, SessionID: 3, ID: 9},
		},
		results: results,
	}
	lectures := client.Lectures{
		{InstituteID: 1, SubjectID: 2, SessionID: 3, TTID: 9},
		{InstituteID: 10, SubjectID: 20, SessionID: 30, TTID: 9},
	}

	result, err := downloadLecturesWithRunner(context.Background(), &config.Config{
		DownloadLocation: outputDir,
		Views:            "left",
		Quality:          "720",
	}, runner, lectures, quietDownloadPresentation())
	if err != nil {
		t.Fatalf("downloadLecturesWithRunner() error = %v", err)
	}
	if len(result.Artifacts) != 2 {
		t.Fatalf("len(Artifacts) = %d, want 2", len(result.Artifacts))
	}
	if result.Artifacts[0].ArtifactID == result.Artifacts[1].ArtifactID {
		t.Fatalf("cross-scope artifacts shared ID %q", result.Artifacts[0].ArtifactID)
	}
	if result.Artifacts[0].Lecture.InstituteID != 10 || result.Artifacts[1].Lecture.InstituteID != 1 {
		t.Fatalf("artifacts followed lecture FIFO instead of playlist scope: %+v", result.Artifacts)
	}
}

func TestDownloadArtifactsValidateIdentityBeforeMediaFetch(t *testing.T) {
	outputDir := t.TempDir()
	runner := &fakeLectureDownloadRunner{
		playlists: []client.ParsedPlaylist{{InstituteID: 0, SubjectID: 2, SessionID: 3, ID: 9}},
		results:   materializeJoinResults(t, outputDir, []downloader.JoinResult{{LeftOutput: "lecture.mp4"}}),
	}
	_, err := downloadLecturesWithRunner(context.Background(), &config.Config{
		DownloadLocation: outputDir,
		Views:            "left",
		Quality:          "720",
	}, runner, client.Lectures{{InstituteID: 0, SubjectID: 2, SessionID: 3, TTID: 9}}, quietDownloadPresentation())
	if err == nil {
		t.Fatal("downloadLecturesWithRunner() error = nil, want invalid identity error")
	}
	if runner.fetches != 0 || runner.downloads != 0 {
		t.Fatalf("invalid identity performed media work: fetches=%d downloads=%d", runner.fetches, runner.downloads)
	}
}

func TestDownloadArtifactsRejectDuplicateScopedLecture(t *testing.T) {
	lecture := client.Lecture{InstituteID: 1, SubjectID: 2, SessionID: 3, TTID: 9}
	runner := &fakeLectureDownloadRunner{playlists: []client.ParsedPlaylist{{ID: 9}, {ID: 9}}}
	_, err := downloadLecturesWithRunner(context.Background(), &config.Config{
		DownloadLocation: t.TempDir(),
		Views:            "left",
		Quality:          "720",
	}, runner, client.Lectures{lecture, lecture}, quietDownloadPresentation())
	if err == nil {
		t.Fatal("downloadLecturesWithRunner() error = nil, want duplicate scoped identity error")
	}
}

func TestDownloadArtifactsRejectPlaylistWithoutSelectedLecture(t *testing.T) {
	runner := &fakeLectureDownloadRunner{playlists: []client.ParsedPlaylist{{ID: 99}}}
	_, err := downloadLecturesWithRunner(context.Background(), &config.Config{
		DownloadLocation: t.TempDir(),
		Views:            "left",
		Quality:          "720",
	}, runner, client.Lectures{{InstituteID: 1, SubjectID: 2, SessionID: 3, TTID: 9}}, quietDownloadPresentation())
	if err == nil {
		t.Fatal("downloadLecturesWithRunner() error = nil, want missing lecture error")
	}
}

func TestBuildDownloadArtifactUsesTypedOutputMetadata(t *testing.T) {
	lecture := client.Lecture{InstituteID: 1, SubjectID: 2, SessionID: 3, TTID: 4}
	producedAt := time.Date(2026, time.August, 8, 5, 6, 7, 0, time.UTC)

	t.Run("video views", func(t *testing.T) {
		outputDir := t.TempDir()
		result := materializeJoinResults(t, outputDir, []downloader.JoinResult{{
			LeftOutput:     "opaque-left.mp4",
			LeftContainer:  "mp4",
			RightOutput:    "opaque-right.mp4",
			RightContainer: "mp4",
			BothOutput:     "opaque-both.mkv",
			BothContainer:  "mkv",
		}})[0]
		manifest, err := buildDownloadArtifact(lecture, &config.Config{Views: "both", Quality: "720"}, result, producedAt)
		if err != nil {
			t.Fatalf("buildDownloadArtifact() error = %v", err)
		}
		wantContainers := []string{"mp4", "mp4", "mkv"}
		wantViews := []string{"left", "right", "both"}
		for i := range manifest.Files {
			if manifest.Files[i].Role != "video" || manifest.Files[i].View != wantViews[i] || manifest.Files[i].Container != wantContainers[i] {
				t.Fatalf("Files[%d] = %+v", i, manifest.Files[i])
			}
		}
	})

	for _, test := range []struct {
		format        string
		wantContainer string
	}{
		{format: "mp3", wantContainer: "mp3"},
		{format: "m4a", wantContainer: "m4a"},
		{format: "aac", wantContainer: "m4a"},
		{format: "opus", wantContainer: "opus"},
	} {
		t.Run("audio "+test.format, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "opaque-output")
			if err := os.WriteFile(path, []byte("audio"), 0o600); err != nil {
				t.Fatal(err)
			}
			manifest, err := buildDownloadArtifact(lecture, &config.Config{
				Views:       "left",
				Quality:     "450",
				AudioOnly:   true,
				AudioFormat: test.format,
			}, downloader.JoinResult{LeftOutput: path, LeftContainer: test.wantContainer}, producedAt)
			if err != nil {
				t.Fatalf("buildDownloadArtifact() error = %v", err)
			}
			if len(manifest.Files) != 1 || manifest.Files[0].Role != "audio" || manifest.Files[0].Container != test.wantContainer {
				t.Fatalf("Files = %+v, want audio/%s", manifest.Files, test.wantContainer)
			}
			if manifest.Selection.AudioFormat != test.format {
				t.Fatalf("selection audioFormat = %q, want %q", manifest.Selection.AudioFormat, test.format)
			}
		})
	}
}
