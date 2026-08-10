package cli

import (
	"fmt"
	"time"

	"github.com/vbauerster/mpb/v8"

	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/downloader"
)

func newDownloadProgress(cfg *config.Config, presentation downloadPresentationOptions, totalLectures, totalChunks int) (*mpb.Progress, *downloader.ProgressTracker, error) {
	if !presentation.showProgress || !cfg.ProgressTracking.Enabled {
		return nil, nil, nil
	}

	progressOptions := []mpb.ContainerOption{mpb.WithWidth(70)}
	if presentation.progressOutput != nil {
		progressOptions = append(progressOptions, mpb.WithOutput(presentation.progressOutput))
	}
	p := mpb.New(progressOptions...)

	var updateInterval time.Duration
	if cfg.ProgressTracking.UpdateInterval != "" {
		var err error
		updateInterval, err = time.ParseDuration(cfg.ProgressTracking.UpdateInterval)
		if err != nil {
			p.Shutdown()
			return nil, nil, fmt.Errorf("invalid progressTracking.updateInterval: %w", err)
		}
	}

	tracker := downloader.NewProgressTrackerWithOptions(totalLectures, totalChunks, p, downloader.ProgressTrackerOptions{
		ShowSpeed:       cfg.ProgressTracking.ShowSpeed,
		ShowETA:         cfg.ProgressTracking.ShowETA,
		SampleInterval:  updateInterval,
		SpeedWindowSize: cfg.ProgressTracking.SpeedWindowSize,
	})
	return p, tracker, nil
}
