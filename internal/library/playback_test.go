package library_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/library"
)

func TestPlaybackKeepsNewestCoalescedResumeState(t *testing.T) {
	store := openTestStore(t)
	manifest := buildTestManifest(t, filepath.Join(t.TempDir(), "lecture.mp4"), "playback")
	if err := store.RecordManifest(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	newer := time.Date(2026, time.August, 9, 9, 0, 0, 500_000_000, time.UTC)
	if err := store.RecordPlayback(context.Background(), library.PlaybackState{
		ArtifactID:      manifest.ArtifactID,
		PositionSeconds: 42.5,
		DurationSeconds: 100,
		LastPlayedAt:    newer,
	}); err != nil {
		t.Fatalf("RecordPlayback() error = %v", err)
	}
	if err := store.RecordPlayback(context.Background(), library.PlaybackState{
		ArtifactID:      manifest.ArtifactID,
		PositionSeconds: 5,
		DurationSeconds: 100,
		LastPlayedAt:    newer.Truncate(time.Second),
	}); err != nil {
		t.Fatalf("RecordPlayback() stale error = %v", err)
	}

	state, found, err := store.Playback(context.Background(), manifest.ArtifactID)
	if err != nil {
		t.Fatalf("Playback() error = %v", err)
	}
	if !found || state.PositionSeconds != 42.5 || state.DurationSeconds != 100 || !state.LastPlayedAt.Equal(newer) {
		t.Fatalf("Playback() = (%+v, %t), want newest state", state, found)
	}
}

func TestPlaybackEqualTimestampCannotRegressAndCompletionIsSticky(t *testing.T) {
	store := openTestStore(t)
	manifest := buildTestManifest(t, filepath.Join(t.TempDir(), "lecture.mp4"), "playback-merge")
	if err := store.RecordManifest(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	playedAt := time.Now().UTC().Add(-time.Minute)
	states := []library.PlaybackState{
		{ArtifactID: manifest.ArtifactID, PositionSeconds: 80, DurationSeconds: 100, Completed: true, LastPlayedAt: playedAt},
		{ArtifactID: manifest.ArtifactID, PositionSeconds: 20, DurationSeconds: 90, Completed: false, LastPlayedAt: playedAt},
	}
	for _, state := range states {
		if err := store.RecordPlayback(context.Background(), state); err != nil {
			t.Fatalf("RecordPlayback(%+v) error = %v", state, err)
		}
	}
	state, found, err := store.Playback(context.Background(), manifest.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	if !found || state.PositionSeconds != 80 || state.DurationSeconds != 100 || !state.Completed || !state.LastPlayedAt.Equal(playedAt) {
		t.Fatalf("equal-time playback = (%+v, %t), want non-regressing merge", state, found)
	}
	if recordErr := store.RecordPlayback(context.Background(), library.PlaybackState{
		ArtifactID: manifest.ArtifactID, PositionSeconds: 10, DurationSeconds: 100,
		Completed: false, LastPlayedAt: playedAt.Add(time.Second),
	}); recordErr != nil {
		t.Fatalf("RecordPlayback(newer incomplete) error = %v", recordErr)
	}
	state, found, err = store.Playback(context.Background(), manifest.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	if !found || state.PositionSeconds != 10 || state.DurationSeconds != 100 || !state.Completed || !state.LastPlayedAt.Equal(playedAt.Add(time.Second)) {
		t.Fatalf("merged playback = (%+v, %t), want newer position with sticky completion", state, found)
	}
}

func TestPlaybackRejectsFarFutureTimestamp(t *testing.T) {
	store := openTestStore(t)
	manifest := buildTestManifest(t, filepath.Join(t.TempDir(), "lecture.mp4"), "playback-future")
	if err := store.RecordManifest(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	err := store.RecordPlayback(context.Background(), library.PlaybackState{
		ArtifactID:      manifest.ArtifactID,
		PositionSeconds: 5,
		DurationSeconds: 100,
		LastPlayedAt:    time.Now().UTC().Add(24 * time.Hour),
	})
	if err == nil || !strings.Contains(err.Error(), "future") {
		t.Fatalf("RecordPlayback(future) error = %v, want future timestamp rejection", err)
	}
	if _, found, readErr := store.Playback(context.Background(), manifest.ArtifactID); readErr != nil || found {
		t.Fatalf("rejected future playback persisted state: found=%t err=%v", found, readErr)
	}
}

func TestPlaybackEqualTimestampMergePreservesPositionDurationInvariant(t *testing.T) {
	store := openTestStore(t)
	manifest := buildTestManifest(t, filepath.Join(t.TempDir(), "lecture.mp4"), "playback-duration-merge")
	if err := store.RecordManifest(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	playedAt := time.Now().UTC().Add(-time.Minute)
	states := []library.PlaybackState{
		{ArtifactID: manifest.ArtifactID, PositionSeconds: 100, DurationSeconds: 0, LastPlayedAt: playedAt},
		{ArtifactID: manifest.ArtifactID, PositionSeconds: 0, DurationSeconds: 50, LastPlayedAt: playedAt},
	}
	for _, state := range states {
		if err := store.RecordPlayback(context.Background(), state); err != nil {
			t.Fatalf("RecordPlayback(%+v) error = %v", state, err)
		}
	}
	merged, found, err := store.Playback(context.Background(), manifest.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	if !found || merged.PositionSeconds != 100 || merged.DurationSeconds != 0 {
		t.Fatalf("merged playback = (%+v, %t), want position 100 with unknown duration", merged, found)
	}
}
