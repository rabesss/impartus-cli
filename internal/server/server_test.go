package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewAPIServerWithPersistenceRestoresJobsAcrossRestart(t *testing.T) {
	tmpDir := t.TempDir()
	persistencePath := filepath.Join(tmpDir, ".jobs.json")

	s1 := NewAPIServerWithPersistence("8080", validServerConfig(), persistencePath)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s1.jobStore.Close(ctx); err != nil {
			t.Errorf("close first server store: %v", err)
		}
	})
	job := s1.jobStore.CreateJob(123, 456, 1, 3, validServerConfig())
	if err := s1.jobStore.CompleteJob(job.ID, []string{"lecture.mp4"}); err != nil {
		t.Fatalf("complete job: %v", err)
	}

	s2 := NewAPIServerWithPersistence("8080", validServerConfig(), persistencePath)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s2.jobStore.Close(ctx); err != nil {
			t.Errorf("close second server store: %v", err)
		}
	})
	restored, ok := s2.jobStore.GetJob(job.ID)
	if !ok {
		t.Fatal("expected persisted job to be restored")
	}
	if restored.Status != "completed" {
		t.Fatalf("expected restored status completed, got %s", restored.Status)
	}
	if len(restored.Outputs) != 1 || restored.Outputs[0] != "lecture.mp4" {
		t.Fatalf("expected restored outputs to round-trip, got %+v", restored.Outputs)
	}
}

func TestServerPersistenceClosesAndFlushesWhenServeReturns(t *testing.T) {
	t.Chdir(t.TempDir())
	persistencePath := filepath.Join(t.TempDir(), ".jobs.json")
	s := NewAPIServerWithPersistence("not-a-port", validServerConfig(), persistencePath)
	job := s.jobStore.CreateJob(1, 1, 1, 5, validServerConfig())
	s.jobStore.SetLectureProgress(job.ID, 3, 5)

	if err := s.Start(); err == nil {
		t.Fatal("expected invalid listen port to fail")
	}

	restarted := newTestPersistentStore(t, persistencePath)
	restored, ok := restarted.CopyJob(job.ID)
	if !ok {
		t.Fatal("job missing after server shutdown flush")
	}
	if restored.CompletedLectures != 3 || restored.TotalLectures != 5 {
		t.Fatalf("restored lecture progress = %d/%d, want 3/5", restored.CompletedLectures, restored.TotalLectures)
	}
}

func TestWebSocketRouteRequiresAuth(t *testing.T) {
	s := newAPIServer(validServerConfig())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/ws", nil)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated websocket request, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "MISSING_TOKEN") {
		t.Fatalf("expected MISSING_TOKEN error, got body: %s", rec.Body.String())
	}
}

func TestRequestIDMiddlewareAddsHeader(t *testing.T) {
	// Test that middleware generates a request ID when none provided
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := requestIDFrom(r)
		if requestID == "" {
			t.Error("expected request ID in context")
		}
		w.WriteHeader(http.StatusOK)
	})

	middleware := requestIDMiddleware(handler)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)

	// Check response header contains X-Request-ID
	requestID := rec.Header().Get("X-Request-ID")
	if requestID == "" {
		t.Error("expected X-Request-ID header in response")
	}
}

func TestRequestIDMiddlewarePropagatesExistingID(t *testing.T) {
	existingID := "existing-request-id-12345"

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := requestIDFrom(r)
		if requestID != existingID {
			t.Errorf("expected request ID %q, got %q", existingID, requestID)
		}
		w.WriteHeader(http.StatusOK)
	})

	middleware := requestIDMiddleware(handler)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/test", nil)
	req.Header.Set("X-Request-ID", existingID)
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)

	// Check response header contains the propagated ID
	requestID := rec.Header().Get("X-Request-ID")
	if requestID != existingID {
		t.Errorf("expected X-Request-ID %q, got %q", existingID, requestID)
	}
}
