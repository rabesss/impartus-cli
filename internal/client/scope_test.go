package client_test

import (
	"context"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

func TestResolveLectureScopeRejectsConflictingSelectedCourse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		lectures client.Lectures
		want     string
	}{
		{name: "subject", lectures: client.Lectures{{TTID: 1, InstituteID: 9, SubjectID: 68, SessionID: 8}}, want: "subject scope mismatch"},
		{name: "session", lectures: client.Lectures{{TTID: 1, InstituteID: 9, SubjectID: 67, SessionID: 9}}, want: "session scope mismatch"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := client.ResolveLectureScope(context.Background(), &config.Config{}, nil, test.lectures, 67, 8)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ResolveLectureScope() error = %v, want %q", err, test.want)
			}
		})
	}
}
