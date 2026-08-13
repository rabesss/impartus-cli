package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/player"
)

func BenchmarkView80x24(b *testing.B) {
	benchmarkView(b, 80, 24, 200)
}

func BenchmarkView140x32(b *testing.B) {
	benchmarkView(b, 140, 32, 200)
}

func BenchmarkViewLargeCatalogue(b *testing.B) {
	benchmarkView(b, 140, 32, 100_000)
}

func benchmarkView(b *testing.B, width, height, count int) {
	b.Helper()
	model := New(context.Background(), nil)
	model.loading = false
	model.width = width
	model.height = height
	model.courses = make(client.Courses, count)
	for index := range model.courses {
		model.courses[index] = client.Course{
			SubjectName: "Course " + fmt.Sprintf("%06d", index), ProfessorName: "Professor", VideoCount: 24,
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = model.View().Content
	}
}

func BenchmarkPlaybackUpdates(b *testing.B) {
	model := New(context.Background(), nil)
	model.loading = false
	model.screen = screenPlayback
	model.lecture = client.Lecture{Topic: "Playback benchmark"}
	model.duration = 3600
	model.width = 140
	model.height = 32
	event := player.Event{Name: "property-change", Property: "time-pos", Data: json.RawMessage("120.5")}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		model, _, _ = model.applyPlaybackEvent(event)
		_ = model.View().Content
	}
}
