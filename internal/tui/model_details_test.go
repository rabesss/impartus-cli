package tui_test

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/tui"
)

func TestDependencyDiagnosticsStayVisibleWithoutBlockingCatalogUse(t *testing.T) {
	backend := &fakeBackend{courses: client.Courses{{SubjectName: "Available course"}}}
	model := tui.NewWithOptions(context.Background(), backend, tui.Options{Diagnostics: []tui.Diagnostic{
		{Name: "mpv", Status: "pass", Detail: "/usr/bin/mpv"},
		{Name: "ffmpeg", Status: "fail", Detail: "ffmpeg is not available on PATH"},
	}})
	model = applyCommand(t, model, model.Init())
	model, _ = update(t, model, key('!', "!"))
	if got := model.View().Content; !strings.Contains(got, "Diagnostics") || !strings.Contains(got, "ffmpeg is not available") {
		t.Fatalf("diagnostics view =\n%s", got)
	}
	model, _ = update(t, model, key(tea.KeyEscape, ""))
	if got := model.View().Content; !strings.Contains(got, "Available course") {
		t.Fatalf("diagnostics blocked catalog return =\n%s", got)
	}
}

func TestLectureDetailsAreReadableBeforePlayback(t *testing.T) {
	backend := &fakeBackend{
		courses: client.Courses{{SubjectName: "Course", SubjectID: 11, SessionID: 22}},
		lectures: client.Lectures{{
			Topic: "Consensus", ProfessorName: "Leslie Lamport", ClassroomName: "AB-5",
			ActualDuration: 3661, StartTime: "2026-08-01T09:00:00Z", TTID: 101,
		}},
	}
	model := tui.New(context.Background(), backend)
	model = applyCommand(t, model, model.Init())
	model, command := update(t, model, key(tea.KeyEnter, ""))
	model = applyCommand(t, model, command)
	model, _ = update(t, model, key('i', "i"))
	if got := model.View().Content; !strings.Contains(got, "Leslie Lamport") || !strings.Contains(got, "01:01:01") || !strings.Contains(got, "AB-5") {
		t.Fatalf("details view =\n%s", got)
	}
}
