package downloader

import (
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

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
