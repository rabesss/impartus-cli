package tuisession

import "testing"

func TestPlaybackTelemetryEventIsAnImmutableSnapshot(t *testing.T) {
	telemetry := playbackTelemetry{
		position: 12,
		duration: 60,
		volume:   75,
		speed:    1.25,
		paused:   true,
		muted:    true,
	}
	event := telemetry.event("operation-1")

	telemetry.position = 24
	telemetry.duration = 120
	telemetry.volume = 50
	telemetry.speed = 2
	telemetry.paused = false
	telemetry.muted = false

	if event.PositionSeconds == nil || *event.PositionSeconds != 12 {
		t.Fatalf("position snapshot = %v, want 12", event.PositionSeconds)
	}
	if event.DurationSeconds == nil || *event.DurationSeconds != 60 {
		t.Fatalf("duration snapshot = %v, want 60", event.DurationSeconds)
	}
	if event.Volume == nil || *event.Volume != 75 {
		t.Fatalf("volume snapshot = %v, want 75", event.Volume)
	}
	if event.Speed == nil || *event.Speed != 1.25 {
		t.Fatalf("speed snapshot = %v, want 1.25", event.Speed)
	}
	if event.Paused == nil || !*event.Paused {
		t.Fatalf("paused snapshot = %v, want true", event.Paused)
	}
	if event.Muted == nil || !*event.Muted {
		t.Fatalf("muted snapshot = %v, want true", event.Muted)
	}
}
