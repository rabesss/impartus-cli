package app

import (
	"context"
	"errors"

	"github.com/rabesss/impartus-cli/internal/library"
)

// Resume returns the latest durable checkpoint for an artifact.
func (service *Service) Resume(ctx context.Context, artifactID string) (library.PlaybackState, bool, error) {
	if service == nil || service.history == nil {
		return library.PlaybackState{}, false, errors.New("application playback history is not configured")
	}
	return service.history.Playback(ctx, artifactID)
}

// RecordPlayback stores one coalesced durable playback checkpoint.
func (service *Service) RecordPlayback(ctx context.Context, state library.PlaybackState) error {
	if service == nil || service.history == nil {
		return errors.New("application playback history is not configured")
	}
	return service.history.RecordPlayback(ctx, state)
}
