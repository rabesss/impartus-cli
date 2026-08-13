package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rabesss/impartus-cli/internal/client"
)

func TestWorkspaceLayoutBreakpointsAndRectangles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		width         int
		height        int
		wantMode      layoutMode
		wantNav       int
		wantInspector int
	}{
		{name: "compact", width: 40, height: 10, wantMode: layoutCompact},
		{name: "medium", width: 80, height: 24, wantMode: layoutMedium, wantInspector: 28},
		{name: "wide", width: 140, height: 32, wantMode: layoutWide, wantNav: 22, wantInspector: 36},
		{name: "wide needs height", width: 140, height: 18, wantMode: layoutMedium, wantInspector: 47},
		{name: "short is compact", width: 80, height: 15, wantMode: layoutCompact},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			layout := calculateLayout(test.width, test.height, true)
			if layout.mode != test.wantMode || layout.navigation.width != test.wantNav || layout.inspector.width != test.wantInspector {
				t.Fatalf("layout = %+v, want mode=%s nav=%d inspector=%d", layout, test.wantMode, test.wantNav, test.wantInspector)
			}
			if layout.activity.height != 3 {
				t.Fatalf("activity height = %d, want 3", layout.activity.height)
			}
			assertValidRectangles(t, layout, test.width, test.height)
		})
	}
}

func TestWorkspaceLayoutNeverReturnsNegativeRectangles(t *testing.T) {
	t.Parallel()
	for width := 1; width <= 140; width++ {
		for height := 1; height <= 32; height++ {
			assertValidRectangles(t, calculateLayout(width, height, true), width, height)
		}
	}
}

func assertValidRectangles(t *testing.T, layout workspaceLayout, width, height int) {
	t.Helper()
	for name, rect := range map[string]rectangle{
		"header": layout.header, "navigation": layout.navigation, "collection": layout.collection,
		"inspector": layout.inspector, "activity": layout.activity, "footer": layout.footer,
	} {
		if rect.x < 0 || rect.y < 0 || rect.width < 0 || rect.height < 0 {
			t.Fatalf("%dx%d %s rectangle is negative: %+v", width, height, name, rect)
		}
		if rect.x+rect.width > max(1, width) || rect.y+rect.height > max(1, height) {
			t.Fatalf("%dx%d %s rectangle exceeds screen: %+v", width, height, name, rect)
		}
	}
}

func TestPaneFocusAndOverlayRestoration(t *testing.T) {
	t.Parallel()
	model := workspaceTestModel(140, 32)
	if model.effectiveFocus() != paneCollection {
		t.Fatalf("initial focus = %s", model.effectiveFocus())
	}
	model, _ = applyWorkspaceMessage(t, model, keyMessage(tea.KeyTab, ""))
	if model.focus != paneInspector {
		t.Fatalf("tab focus = %s, want inspector", model.focus)
	}
	model, _ = applyWorkspaceMessage(t, model, keyMessage('?', "?"))
	if overlay, open := model.topOverlay(); !open || overlay.kind != overlayHelp {
		t.Fatalf("help overlay = %+v, open=%t", overlay, open)
	}
	model, _ = applyWorkspaceMessage(t, model, keyMessage(tea.KeyEscape, ""))
	if model.focus != paneInspector {
		t.Fatalf("focus after help = %s, want inspector", model.focus)
	}

	model, _ = applyWorkspaceMessage(t, model, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	model, _ = applyWorkspaceMessage(t, model, tea.WindowSizeMsg{Width: 40, Height: 10})
	model, _ = applyWorkspaceMessage(t, model, keyMessage(tea.KeyEscape, ""))
	if model.focus != paneCollection {
		t.Fatalf("hidden inspector restored as %s, want collection", model.focus)
	}
}

func TestPaletteSearchAndRegistryDispatch(t *testing.T) {
	t.Parallel()
	model := workspaceTestModel(80, 24)
	model, _ = applyWorkspaceMessage(t, model, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	for _, character := range "library" {
		model, _ = applyWorkspaceMessage(t, model, keyMessage(character, string(character)))
	}
	rendered := ansi.Strip(model.View().Content)
	if !strings.Contains(rendered, "Open library") || strings.Contains(rendered, "Open courses") {
		t.Fatalf("filtered palette =\n%s", rendered)
	}
	model, command := applyWorkspaceMessage(t, model, keyMessage(tea.KeyEnter, ""))
	if command == nil || !model.loading {
		t.Fatalf("palette library action command=%v loading=%t", command, model.loading)
	}
	if _, open := model.topOverlay(); open {
		t.Fatal("palette did not close before dispatch")
	}
}

func TestCollectionStateIsIndependentByDomain(t *testing.T) {
	t.Parallel()
	model := workspaceTestModel(80, 24)
	model.courses = client.Courses{{SubjectName: "Course A"}, {SubjectName: "Course B"}}
	model.cursor = 1
	model.filter.SetValue("course")
	model.lectures = client.Lectures{{Topic: "Raft intro"}, {Topic: "Raft details"}}
	model = model.transitionScreen(screenLectures, true)
	model.cursor = 1
	model.filter.SetValue("raft")
	model = model.transitionScreen(screenLibrary, true)
	model = model.transitionScreen(screenLectures, false)
	if model.cursor != 1 || model.filter.Value() != "raft" {
		t.Fatalf("lecture state = cursor %d filter %q", model.cursor, model.filter.Value())
	}
	model = model.transitionScreen(screenCourses, false)
	if model.cursor != 1 || model.filter.Value() != "course" {
		t.Fatalf("course state = cursor %d filter %q", model.cursor, model.filter.Value())
	}
}

func TestLateLectureResultCannotReplaceUnrelatedPane(t *testing.T) {
	t.Parallel()
	model := workspaceTestModel(80, 24)
	model.screen = screenDiagnostics
	model.loading = true
	model.course = client.Course{InstituteID: 1, SubjectID: 2, SessionID: 3}
	updated, _ := model.updateLecturesLoaded(lecturesLoadedMsg{
		course: model.course, lectures: client.Lectures{{Topic: "Late result"}},
	})
	next, ok := updated.(Model)
	if !ok {
		t.Fatalf("updateLecturesLoaded returned %T", updated)
	}
	if next.screen != screenDiagnostics || len(next.lectures) != 0 || !next.loading {
		t.Fatalf("late result mutated unrelated state: screen=%d lectures=%v loading=%t", next.screen, next.lectures, next.loading)
	}
}

func TestNoColorUnicodeAndNarrowRenderingAreCellSafe(t *testing.T) {
	model := workspaceTestModel(40, 10)
	model.noColor = true
	model.styles = newStyleSet(true)
	model.courses = client.Courses{
		{SubjectName: "資料 e\u0301 👩🏽‍💻\x1b[2J", ProfessorName: "Dr. λ", VideoCount: 12},
		{SubjectName: "Second course"},
	}
	rendered := model.View().Content
	plain := ansi.Strip(rendered)
	if strings.Contains(rendered, "\x1b[38") || strings.Contains(rendered, "\x1b[48") {
		t.Fatalf("NO_COLOR rendering contains color SGR: %q", rendered)
	}
	if !strings.Contains(plain, "[ACTIVE]") || !strings.Contains(plain, "> 資料 é 👩🏽‍💻") || strings.Contains(plain, "\x1b[2J") {
		t.Fatalf("accessible Unicode rendering =\n%s", plain)
	}
	assertScreenCells(t, rendered, 40, 10)

	for _, size := range []struct{ width, height int }{{1, 1}, {2, 2}, {10, 3}, {39, 9}, {75, 15}} {
		model.width, model.height = size.width, size.height
		assertScreenCells(t, model.View().Content, size.width, size.height)
	}
}

func TestLoadingEmptyErrorAndRedactedStatusStates(t *testing.T) {
	model := workspaceTestModel(40, 10)
	model.courses = nil
	model.loading = true
	if got := ansi.Strip(model.View().Content); !strings.Contains(got, "LOADING") || !strings.Contains(got, "Loading") {
		t.Fatalf("loading state =\n%s", got)
	}
	model.loading = false
	if got := ansi.Strip(model.View().Content); !strings.Contains(got, "EMPTY: No courses") {
		t.Fatalf("empty state =\n%s", got)
	}
	model.err = errors.New("request failed auth=secret-token")
	if got := ansi.Strip(model.View().Content); !strings.Contains(got, "ERROR") || strings.Contains(got, "secret-token") || !strings.Contains(got, "REDACTED") {
		t.Fatalf("error state =\n%s", got)
	}
	model.err = nil
	model.status = "warning auth=status-secret"
	if got := ansi.Strip(model.View().Content); strings.Contains(got, "status-secret") || !strings.Contains(got, "REDACTED") {
		t.Fatalf("status redaction =\n%s", got)
	}
}

func TestCollectionRenderingIsBoundedByVisibleRows(t *testing.T) {
	t.Parallel()
	model := workspaceTestModel(40, 10)
	model.courses = make(client.Courses, 10_000)
	for index := range model.courses {
		model.courses[index].SubjectName = fmt.Sprintf("Course %05d", index)
	}
	plain := ansi.Strip(model.View().Content)
	if count := strings.Count(plain, "Course 0"); count > 6 {
		t.Fatalf("rendered %d catalog rows for a six-row viewport", count)
	}
	if strings.Contains(plain, "Course 09999") {
		t.Fatal("rendered an off-screen catalog row")
	}
}

func workspaceTestModel(width, height int) Model {
	model := New(context.Background(), nil)
	model.loading = false
	model.width = width
	model.height = height
	model.focus = paneCollection
	model.courses = client.Courses{{SubjectName: "Course", VideoCount: 1}}
	return model
}

func applyWorkspaceMessage(t *testing.T, model Model, message tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	updated, command := model.Update(message)
	next, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update returned %T", updated)
	}
	return next, command
}

func keyMessage(code rune, text string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Text: text}
}

func assertScreenCells(t *testing.T, rendered string, width, height int) {
	t.Helper()
	lines := strings.Split(rendered, "\n")
	if len(lines) != height {
		t.Fatalf("line count = %d, want %d: %q", len(lines), height, rendered)
	}
	for index, line := range lines {
		if got := ansi.StringWidth(line); got != width {
			t.Fatalf("line %d width = %d, want %d: %q", index, got, width, line)
		}
	}
}
