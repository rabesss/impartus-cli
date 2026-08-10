package tui_test

import (
	"context"
	"errors"
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

func TestRenderRedactsCredentialsFromBackendErrors(t *testing.T) {
	t.Parallel()

	model := tui.New(context.Background(), &fakeBackend{coursesErr: errors.New(
		"catalog failed https://example.test/list?token=url-secret auth=body-secret signature=signed-secret",
	)})
	model = applyCommand(t, model, model.Init())
	rendered := model.View().Content
	for _, secret := range []string{"url-secret", "body-secret", "signed-secret"} {
		if strings.Contains(rendered, secret) {
			t.Fatalf("rendered backend error leaked %q:\n%s", secret, rendered)
		}
	}
	if !strings.Contains(rendered, "REDACTED") {
		t.Fatalf("rendered backend error omitted redaction marker:\n%s", rendered)
	}
}
