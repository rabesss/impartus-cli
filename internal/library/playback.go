package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

const playbackFutureTolerance = 5 * time.Minute

// PlaybackState is the durable resume point for one logical artifact.
type PlaybackState struct {
	ArtifactID      string    `json:"artifactId"`
	PositionSeconds float64   `json:"positionSeconds"`
	DurationSeconds float64   `json:"durationSeconds"`
	Completed       bool      `json:"completed"`
	LastPlayedAt    time.Time `json:"lastPlayedAt"`
}

// RecordPlayback persists one coalesced playback checkpoint. Older checkpoints
// are accepted as no-ops so concurrent event delivery cannot regress resume.
func (store *Store) RecordPlayback(ctx context.Context, state PlaybackState) error {
	if store == nil || store.database == nil {
		return errors.New("library store is closed")
	}
	state.ArtifactID = strings.TrimSpace(state.ArtifactID)
	if state.ArtifactID == "" {
		return errors.New("playback artifact ID is required")
	}
	if invalidSeconds(state.PositionSeconds) || invalidSeconds(state.DurationSeconds) {
		return errors.New("playback position and duration must be finite and non-negative")
	}
	if state.DurationSeconds > 0 && state.PositionSeconds > state.DurationSeconds {
		return errors.New("playback position exceeds duration")
	}
	if state.LastPlayedAt.IsZero() {
		return errors.New("playback lastPlayedAt is required")
	}
	if state.LastPlayedAt.After(time.Now().Add(playbackFutureTolerance)) {
		return errors.New("playback lastPlayedAt is too far in the future")
	}
	playedAt := formatDatabaseTime(state.LastPlayedAt)
	updatedAt := formatDatabaseTime(time.Now())
	_, err := store.database.ExecContext(ctx, `
		INSERT INTO playback (
			artifact_id, position_seconds, duration_seconds, completed, last_played_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(artifact_id) DO UPDATE SET
			position_seconds = CASE
				WHEN excluded.last_played_at = playback.last_played_at
				THEN max(playback.position_seconds, excluded.position_seconds)
				ELSE excluded.position_seconds
			END,
			duration_seconds = CASE
				WHEN excluded.last_played_at = playback.last_played_at
				THEN CASE
					WHEN max(playback.duration_seconds, excluded.duration_seconds) = 0
						OR max(playback.position_seconds, excluded.position_seconds)
							<= max(playback.duration_seconds, excluded.duration_seconds)
					THEN max(playback.duration_seconds, excluded.duration_seconds)
					ELSE 0
				END
				ELSE excluded.duration_seconds
			END,
			completed = CASE
				WHEN playback.completed = 1 OR excluded.completed = 1 THEN 1
				ELSE 0
			END,
			last_played_at = excluded.last_played_at,
			updated_at = excluded.updated_at
		WHERE excluded.last_played_at >= playback.last_played_at`,
		state.ArtifactID,
		state.PositionSeconds,
		state.DurationSeconds,
		state.Completed,
		playedAt,
		updatedAt,
	)
	if err != nil {
		return fmt.Errorf("record playback for %s: %w", state.ArtifactID, err)
	}
	return nil
}

// Playback returns the current resume state when one has been recorded.
func (store *Store) Playback(ctx context.Context, artifactID string) (PlaybackState, bool, error) {
	if store == nil || store.database == nil {
		return PlaybackState{}, false, errors.New("library store is closed")
	}
	artifactID = strings.TrimSpace(artifactID)
	if artifactID == "" {
		return PlaybackState{}, false, errors.New("playback artifact ID is required")
	}
	var state PlaybackState
	var completed int
	var playedAt string
	err := store.database.QueryRowContext(ctx, `
		SELECT artifact_id, position_seconds, duration_seconds, completed, last_played_at
		FROM playback WHERE artifact_id = ?`, artifactID).Scan(
		&state.ArtifactID,
		&state.PositionSeconds,
		&state.DurationSeconds,
		&completed,
		&playedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return PlaybackState{}, false, nil
	}
	if err != nil {
		return PlaybackState{}, false, fmt.Errorf("read playback for %s: %w", artifactID, err)
	}
	state.Completed = completed == 1
	state.LastPlayedAt, err = parseDatabaseTime(playedAt)
	if err != nil {
		return PlaybackState{}, false, fmt.Errorf("decode playback time for %s: %w", artifactID, err)
	}
	return state, true, nil
}

func invalidSeconds(value float64) bool {
	return value < 0 || math.IsNaN(value) || math.IsInf(value, 0)
}
