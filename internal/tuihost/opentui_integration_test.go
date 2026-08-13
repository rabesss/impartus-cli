package tuihost_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/tuihost"
	"github.com/rabesss/impartus-cli/internal/tuisession"
)

type integrationCatalog struct{}

func (integrationCatalog) Courses(context.Context) (client.Courses, error) {
	return client.Courses{{
		InstituteID:   1,
		ProfessorName: "Dr. Rao",
		SessionID:     2,
		SessionName:   "Monsoon",
		SubjectID:     3,
		SubjectName:   "Distributed Systems",
		VideoCount:    4,
	}}, nil
}

func TestCompiledOpenTUICompletesPrivateSessionSelfTest(t *testing.T) {
	executable := os.Getenv("IMPARTUS_UI_TEST_BINARY")
	if executable == "" {
		t.Skip("set IMPARTUS_UI_TEST_BINARY to the compiled OpenTUI sidecar")
	}
	session, err := tuisession.Start(t.Context(), tuisession.Options{
		Catalog:  integrationCatalog{},
		SelfTest: tuisession.SelfTestOptions{Steps: 3},
		Version:  "integration-test",
	})
	if err != nil {
		t.Fatalf("start private TUI session: %v", err)
	}
	cleanupIntegrationSession(t, session)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := tuihost.Run(t.Context(), tuihost.Options{
		Session:    session,
		Executable: executable,
		Arguments:  []string{"--noninteractive-self-test"},
		Stdout:     &stdout,
		Stderr:     &stderr,
	}); err != nil {
		t.Fatalf("run compiled OpenTUI self-test: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}
	var result struct {
		Courses int    `json:"courses"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode self-test result %q: %v", stdout.String(), err)
	}
	if result.Courses != 1 || result.Status != "ok" {
		t.Fatalf("self-test result = %+v", result)
	}
	if stderr.Len() != 0 {
		t.Fatalf("self-test stderr = %q", stderr.String())
	}
}

func cleanupIntegrationSession(t *testing.T, session *tuisession.Session) {
	t.Helper()
	t.Cleanup(func() {
		if err := session.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
}
