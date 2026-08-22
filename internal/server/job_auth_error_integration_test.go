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

	created := createIssue170Job(t, s, header)

	waitForBackgroundJobWork(t, s)
	flushTestStore(t, s.jobStore)

	event, rawEvent := readSingleIssue170FailedEvent(t, conn)
	if event.JobID != created.ID || event.Status != StatusFailed || event.Error != wantSummary {
		t.Fatalf("unexpected failed event: %+v", event)
	}
	assertNoIssue170Markers(t, string(rawEvent), localAPIToken, usernameMarker, passwordMarker, baseURLMarker, bodyMarker, tokenMarker)

	got, getBody := getIssue170Job(t, s, header, created.ID)
	if got.Status != StatusFailed || got.Progress != 0 || got.Error != wantSummary {
		t.Fatalf("job state = status:%q progress:%v error:%q", got.Status, got.Progress, got.Error)
	}
	assertNoIssue170Markers(t, getBody, localAPIToken, usernameMarker, passwordMarker, baseURLMarker, bodyMarker, tokenMarker)

	persistedBytes, err := os.ReadFile(persistencePath)
	if err != nil {
		t.Fatalf("read persisted jobs: %v", err)
	}
	assertNoIssue170Markers(t, string(persistedBytes), localAPIToken, usernameMarker, passwordMarker, baseURLMarker, bodyMarker, tokenMarker)

	restarted := newTestPersistentStore(t, persistencePath)
	reloaded, ok := restarted.GetJob(created.ID)
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

func TestTypedNonLoginFailureReachesSafeJobEventAndAPI(t *testing.T) {
	const (
		bodyMarker  = "issue170-data-response-body-marker"
		tokenMarker = "issue170-data-token-marker"
		wantSummary = "upstream authentication failed"
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/subjects/1/lectures/1" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		if _, err := w.Write([]byte(bodyMarker)); err != nil {
			t.Errorf("write upstream 401 body: %v", err)
		}
	}))
	defer upstream.Close()

	taskDir := t.TempDir()
	serverConfig := &config.Config{
		Username:         "local-user",
		Password:         "local-password",
		BaseURL:          upstream.URL,
		TokenCachePath:   filepath.Join(taskDir, "auth-cache"),
		DownloadLocation: filepath.Join(taskDir, "downloads"),
		TempDirLocation:  filepath.Join(taskDir, "temporary"),
		Quality:          "450",
	}
	login := func(_ context.Context, cfg *config.Config) (*client.Client, *config.Config, error) {
		loginCfg := cloneConfig(cfg)
		loginCfg.Token = tokenMarker
		return client.New(upstream.Client(), nil), loginCfg, nil
	}
	s := newAPIServerFull("8080", serverConfig, login, "", false)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.jobStore.Close(ctx); err != nil {
			t.Errorf("close in-memory job store: %v", err)
		}
	})

	wsURL, header := startWebSocketTestServer(t, s)
	conn := dialWebSocket(t, wsURL, header)
	waitForWebSocketClients(t, s.wsHub, 1)
	localAPIToken := strings.TrimPrefix(header.Get("Authorization"), "Bearer ")
	created := createIssue170Job(t, s, header)
	waitForBackgroundJobWork(t, s)

	event, rawEvent := readSingleIssue170FailedEvent(t, conn)
	if event.JobID != created.ID || event.Status != StatusFailed || event.Error != wantSummary {
		t.Fatalf("unexpected non-login failed event: %+v", event)
	}
	assertNoIssue170Markers(t, string(rawEvent), localAPIToken, bodyMarker, tokenMarker, upstream.URL)

	got, rawJob := getIssue170Job(t, s, header, created.ID)
	if got.Status != StatusFailed || got.Progress != 0 || got.Error != wantSummary {
		t.Fatalf("non-login job state = status:%q progress:%v error:%q", got.Status, got.Progress, got.Error)
	}
	assertNoIssue170Markers(t, rawJob, localAPIToken, bodyMarker, tokenMarker, upstream.URL)
}

func createIssue170Job(t *testing.T, s *APIServer, header http.Header) Job {
	t.Helper()
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/api/v1/jobs",
		strings.NewReader(`{"subjectId":1,"sessionId":1,"startIndex":1,"endIndex":1}`),
	)
	request.Header.Set("Authorization", header.Get("Authorization"))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	s.router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create job status = %d, body = %s", response.Code, response.Body)
	}
	var created struct {
		Data Job `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatalf("decode created job: %v", err)
	}
	return created.Data
}

func getIssue170Job(t *testing.T, s *APIServer, header http.Header, jobID string) (Job, string) {
	t.Helper()
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/jobs/"+jobID, nil)
	request.Header.Set("Authorization", header.Get("Authorization"))
	response := httptest.NewRecorder()
	s.router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("get job status = %d, body = %s", response.Code, response.Body)
	}
	raw := response.Body.String()
	var got struct {
		Data Job `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode get job response: %v", err)
	}
	return got.Data, raw
}

func readSingleIssue170FailedEvent(t *testing.T, conn websocketReader) (wsEvent, []byte) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline() failed: %v", err)
	}
	for {
		_, rawEvent, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("ReadMessage() before job.failed: %v", err)
		}
		var event wsEvent
		if err := json.Unmarshal(rawEvent, &event); err != nil {
			t.Fatalf("decode websocket event: %v", err)
		}
		if event.Type != "job.failed" {
			continue
		}

		if err := conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
			t.Fatalf("SetReadDeadline() for duplicate drain failed: %v", err)
		}
		for {
			_, extraRaw, readErr := conn.ReadMessage()
			if readErr != nil {
				var netErr net.Error
				if errors.As(readErr, &netErr) && netErr.Timeout() {
					return event, rawEvent
				}
				t.Fatalf("ReadMessage() while checking duplicate events: %v", readErr)
			}
			var extra wsEvent
			if err := json.Unmarshal(extraRaw, &extra); err != nil {
				t.Fatalf("decode additional websocket event: %v", err)
			}
			if extra.Type == "job.failed" {
				t.Fatalf("received duplicate job.failed event: %+v", extra)
			}
		}
	}
}

type websocketReader interface {
	SetReadDeadline(time.Time) error
	ReadMessage() (messageType int, p []byte, err error)
}

func assertNoIssue170Markers(t *testing.T, text string, markers ...string) {
	t.Helper()
	for _, marker := range markers {
		if marker != "" && strings.Contains(text, marker) {
			t.Fatalf("public job data leaked marker %q: %s", marker, text)
		}
	}
}
