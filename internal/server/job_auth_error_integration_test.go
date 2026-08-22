package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

func TestTypedLoginFailureIsSafeAcrossJobEventAPIAndPersistence(t *testing.T) {
	const (
		usernameMarker = "issue170-user-marker"
		passwordMarker = "issue170-password-marker"
		baseURLMarker  = "issue170-upstream-url-marker"
		bodyMarker     = "issue170-response-body-marker"
		tokenMarker    = "issue170-upstream-token-marker"
		wantSummary    = "upstream authentication failed"
	)
	taskDir := t.TempDir()
	persistencePath := filepath.Join(taskDir, "jobs-state.json")
	var loginCalls atomic.Int32
	login := func(_ context.Context, _ *config.Config) (*client.Client, *config.Config, error) {
		loginCalls.Add(1)
		typed := &client.AuthenticationError{Operation: "login", StatusCode: http.StatusUnauthorized}
		return nil, nil, fmt.Errorf(
			"upstream=%s username=%s password=%s token=%s body=%s: %w",
			baseURLMarker,
			usernameMarker,
			passwordMarker,
			tokenMarker,
			bodyMarker,
			typed,
		)
	}
	s := newAPIServerFull("8080", &config.Config{
		Username:         usernameMarker,
		Password:         passwordMarker,
		BaseURL:          "https://" + baseURLMarker + ".invalid",
		Token:            tokenMarker,
		TokenCachePath:   filepath.Join(taskDir, "auth-cache"),
		DownloadLocation: filepath.Join(taskDir, "downloads"),
		TempDirLocation:  filepath.Join(taskDir, "temporary"),
		Quality:          "450",
	}, login, persistencePath, true)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.jobStore.Close(ctx); err != nil {
			t.Errorf("close persistent job store: %v", err)
		}
	})

	wsURL, header := startWebSocketTestServer(t, s)
	conn := dialWebSocket(t, wsURL, header)
	waitForWebSocketClients(t, s.wsHub, 1)
	localAPIToken := strings.TrimPrefix(header.Get("Authorization"), "Bearer ")

	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/api/v1/jobs",
		strings.NewReader(`{"subjectId":1,"sessionId":1,"startIndex":1,"endIndex":1}`),
	)
	request.Header.Set("Authorization", header.Get("Authorization"))
	request.Header.Set("Content-Type", "application/json")
	createdResponse := httptest.NewRecorder()
	s.router.ServeHTTP(createdResponse, request)
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("create job status = %d, body = %s", createdResponse.Code, createdResponse.Body)
	}
	var created struct {
		Data Job `json:"data"`
	}
	if err := json.NewDecoder(createdResponse.Body).Decode(&created); err != nil {
		t.Fatalf("decode created job: %v", err)
	}

	waitForBackgroundJobWork(t, s)
	flushTestStore(t, s.jobStore)

	if err := conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline() failed: %v", err)
	}
	failedEvents := 0
	for {
		_, rawEvent, err := conn.ReadMessage()
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				break
			}
			t.Fatalf("ReadMessage() failed: %v", err)
		}
		var event wsEvent
		if err := json.Unmarshal(rawEvent, &event); err != nil {
			t.Fatalf("decode websocket event: %v", err)
		}
		if event.Type != "job.failed" {
			continue
		}
		failedEvents++
		if event.JobID != created.Data.ID || event.Status != StatusFailed || event.Error != wantSummary {
			t.Fatalf("unexpected failed event: %+v", event)
		}
		assertNoIssue170Markers(t, string(rawEvent), localAPIToken, usernameMarker, passwordMarker, baseURLMarker, bodyMarker, tokenMarker)
	}
	if failedEvents != 1 {
		t.Fatalf("job.failed event count = %d, want exactly 1", failedEvents)
	}

	getRequest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/jobs/"+created.Data.ID, nil)
	getRequest.Header.Set("Authorization", header.Get("Authorization"))
	getResponse := httptest.NewRecorder()
	s.router.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("get job status = %d, body = %s", getResponse.Code, getResponse.Body)
	}
	getBody := getResponse.Body.String()
	var got struct {
		Data Job `json:"data"`
	}
	if err := json.Unmarshal([]byte(getBody), &got); err != nil {
		t.Fatalf("decode get job response: %v", err)
	}
	if got.Data.Status != StatusFailed || got.Data.Progress != 0 || got.Data.Error != wantSummary {
		t.Fatalf("job state = status:%q progress:%v error:%q", got.Data.Status, got.Data.Progress, got.Data.Error)
	}
	assertNoIssue170Markers(t, getBody, localAPIToken, usernameMarker, passwordMarker, baseURLMarker, bodyMarker, tokenMarker)

	persistedBytes, err := os.ReadFile(persistencePath)
	if err != nil {
		t.Fatalf("read persisted jobs: %v", err)
	}
	assertNoIssue170Markers(t, string(persistedBytes), localAPIToken, usernameMarker, passwordMarker, baseURLMarker, bodyMarker, tokenMarker)

	restarted := newTestPersistentStore(t, persistencePath)
	reloaded, ok := restarted.GetJob(created.Data.ID)
	if !ok {
		t.Fatal("failed job missing after persistence reload")
	}
	if reloaded.Status != StatusFailed || reloaded.Progress != 0 || reloaded.Error != wantSummary {
		t.Fatalf("reloaded job = status:%q progress:%v error:%q", reloaded.Status, reloaded.Progress, reloaded.Error)
	}
	if loginCalls.Load() != 1 {
		t.Fatalf("upstream login calls = %d, want 1", loginCalls.Load())
	}
	s.upstreamCacheMu.RLock()
	cached := s.upstreamCache
	s.upstreamCacheMu.RUnlock()
	if cached != nil {
		t.Fatal("typed login failure poisoned the upstream client cache")
	}
}

func assertNoIssue170Markers(t *testing.T, text string, markers ...string) {
	t.Helper()
	for _, marker := range markers {
		if marker != "" && strings.Contains(text, marker) {
			t.Fatalf("public job data leaked marker %q: %s", marker, text)
		}
	}
}
