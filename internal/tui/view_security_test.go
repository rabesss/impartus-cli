package tui_test

import (
	"context"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/tui"
)

func TestRenderSanitizesRemoteMetadataAndErrors(t *testing.T) {
	t.Parallel()

	model := tui.New(context.Background(), &fakeBackend{
		courses: client.Courses{{
			SubjectName:   "Course\x1b]52;c;Y2xpcGJvYXJk\a\nInjected",
			ProfessorName: "Professor\x1b[2J\r\tName\u0085End",
		}},
	})
	model = applyCommand(t, model, model.Init())

	rendered := model.View().Content
	for _, forbidden := range []string{"\x1b]52", "\x1b[2J", "\a", "Course\nInjected", "\r", "\t", "\u0085"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("rendered terminal text contains %q:\n%s", forbidden, rendered)
		}
	}
	if !strings.Contains(rendered, "Course Injected") || !strings.Contains(rendered, "Professor Name End") {
		t.Fatalf("rendered terminal text lost safe metadata:\n%s", rendered)
	}
}
