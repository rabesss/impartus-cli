package downloader

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/selection"
)

// PlanJoinResult returns the final paths and containers that
// DownloadAndJoinPlaylist will publish for a parsed playlist. Durable callers
// use it before starting media work so crash recovery knows the exact finals.
func PlanJoinResult(cfg *config.Config, playlist client.ParsedPlaylist) (JoinResult, error) {
	location, views, inputErr := normalizedPlanInput(cfg)
	if inputErr != nil {
		return JoinResult{}, inputErr
	}
	container, extension, mediaErr := plannedMedia(cfg)
	if mediaErr != nil {
		return JoinResult{}, mediaErr
	}
	base := lectureOutputBase(playlist)
	result := JoinResult{}
	planSelectedViews(&result, location, base, extension, container, views, cfg.AudioOnly, playlist)
	if len(result.OutputPaths()) == 0 {
		return JoinResult{}, fmt.Errorf("%w: selected %s view is unavailable for lecture %d", ErrNoSelectedMedia, views, playlist.ID)
	}
	return result, nil
}

func normalizedPlanInput(cfg *config.Config) (string, selection.View, error) {
	if cfg == nil {
		return "", "", errors.New("download configuration is required")
	}
	location := cfg.DownloadLocation
	if strings.TrimSpace(location) == "" {
		return "", "", errors.New("download location is required")
	}
	views, ok := selection.ParseView(cfg.Views)
	if !ok {
		return "", "", fmt.Errorf("unsupported views %q", cfg.Views)
	}
	return location, views, nil
}

func plannedMedia(cfg *config.Config) (string, string, error) {
	if !cfg.AudioOnly {
		return "mp4", "mp4", nil
	}
	if !selection.ValidAudioFormat(cfg.AudioFormat) {
		return "", "", fmt.Errorf("unsupported audio format %q", cfg.AudioFormat)
	}
	container := audioContainer(cfg.AudioFormat)
	return container, container, nil
}

func lectureOutputBase(playlist client.ParsedPlaylist) string {
	return fmt.Sprintf("LEC %03d %s", playlist.SeqNo, sanitizeFilename(playlist.Title))
}

func outputFilename(base string, view selection.View, container string) string {
	suffix := ""
	switch view {
	case selection.ViewLeft:
		suffix = " LEFT VIEW"
	case selection.ViewRight:
		suffix = " RIGHT VIEW"
	case selection.ViewBoth:
		suffix = " BOTH"
	}
	return fmt.Sprintf("%s%s.%s", base, suffix, container)
}

func planSelectedViews(
	result *JoinResult,
	location, base, extension, container string,
	views selection.View,
	audioOnly bool,
	playlist client.ParsedPlaylist,
) {
	if views.Includes(selection.ViewLeft) && len(playlist.FirstViewURLs) > 0 {
		result.LeftOutput = filepath.Join(location, outputFilename(base, selection.ViewLeft, extension))
		result.LeftContainer = container
	}
	if views.Includes(selection.ViewRight) && len(playlist.SecondViewURLs) > 0 {
		result.RightOutput = filepath.Join(location, outputFilename(base, selection.ViewRight, extension))
		result.RightContainer = container
	}
	if views == selection.ViewBoth && result.LeftOutput != "" && result.RightOutput != "" {
		bothContainer := "mkv"
		if audioOnly {
			bothContainer = container
		}
		result.BothOutput = filepath.Join(location, outputFilename(base, selection.ViewBoth, bothContainer))
		result.BothContainer = bothContainer
	}
}
