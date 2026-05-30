package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRespondWithErrorWritesJSONResponse(t *testing.T) {
	rec := httptest.NewRecorder()
	respondWithError(rec, http.StatusBadRequest, "TEST_ERROR", "Test error message", "testCommand", nil)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	contentType := rec.Header().Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Errorf("expected Content-Type application/json, got %s", contentType)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if success, ok := resp["success"].(bool); !ok || success {
		t.Error("expected success to be false")
	}

	errorObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error object in response")
	}
	if errorObj["code"] != "TEST_ERROR" {
		t.Errorf("expected code TEST_ERROR, got %v", errorObj["code"])
	}
	if errorObj["message"] != "Test error message" {
		t.Errorf("expected message 'Test error message', got %v", errorObj["message"])
	}

	metaObj, ok := resp["meta"].(map[string]any)
	if !ok {
		t.Fatal("expected meta object in response")
	}
	if metaObj["command"] != "testCommand" {
		t.Errorf("expected meta.command 'testCommand', got %v", metaObj["command"])
	}
	if metaObj["mode"] != "api" {
		t.Errorf("expected meta.mode 'api', got %v", metaObj["mode"])
	}
}

func TestRespondWithErrorWithDetails(t *testing.T) {
	rec := httptest.NewRecorder()
	details := map[string]string{"field": "username"}
	respondWithError(rec, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Validation failed", "testCommand", nil, details)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected status %d, got %d", http.StatusUnprocessableEntity, rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	errorObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error object in response")
	}
	if errorObj["details"] == nil {
		t.Error("expected details in error response")
	}
}

func TestRespondWithSuccessWritesJSONResponse(t *testing.T) {
	rec := httptest.NewRecorder()
	data := map[string]any{
		"token":   "test-token",
		"expires": time.Now().Add(24 * time.Hour),
	}
	respondWithSuccess(rec, "testCommand", data)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	contentType := rec.Header().Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Errorf("expected Content-Type application/json, got %s", contentType)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if success, ok := resp["success"].(bool); !ok || !success {
		t.Error("expected success to be true")
	}

	dataObj, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatal("expected data object in response")
	}
	if dataObj["token"] != "test-token" {
		t.Errorf("expected token 'test-token', got %v", dataObj["token"])
	}
}

func TestRespondWithErrorRetryHintForUpstreamErrors(t *testing.T) {
	tests := []struct {
		name       string
		code       string
		status     int
		retryable  bool
		retryAfter float64
	}{
		{"LOGIN_FAILED is retryable", "LOGIN_FAILED", http.StatusBadGateway, true, 30},
		{"COURSES_FETCH_FAILED is retryable", "COURSES_FETCH_FAILED", http.StatusBadGateway, true, 30},
		{"LECTURES_FETCH_FAILED is retryable", "LECTURES_FETCH_FAILED", http.StatusBadGateway, true, 30},
		{"CANCEL_FAILED is retryable", "CANCEL_FAILED", http.StatusInternalServerError, true, 10},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			hint := &retryHint{Retryable: tc.retryable, RetryAfter: int(tc.retryAfter)}
			respondWithError(rec, tc.status, tc.code, "test error", "testCommand", hint)

			if rec.Code != tc.status {
				t.Errorf("expected status %d, got %d", tc.status, rec.Code)
			}

			var resp map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}

			errorObj, ok := resp["error"].(map[string]any)
			if !ok {
				t.Fatal("expected error object in response")
			}

			details, ok := errorObj["details"].(map[string]any)
			if !ok {
				t.Fatal("expected details in error response")
			}

			if details["retryable"] != tc.retryable {
				t.Errorf("expected retryable=%v, got %v", tc.retryable, details["retryable"])
			}
			if details["retryAfter"] != tc.retryAfter {
				t.Errorf("expected retryAfter=%v, got %v", tc.retryAfter, details["retryAfter"])
			}
		})
	}
}

func TestRespondWithErrorNoRetryHintForClientErrors(t *testing.T) {
	tests := []struct {
		name   string
		code   string
		status int
	}{
		{"INVALID_REQUEST is not retryable", "INVALID_REQUEST", http.StatusBadRequest},
		{"MISSING_PARAMETER is not retryable", "MISSING_PARAMETER", http.StatusBadRequest},
		{"INVALID_JOB_CONFIG is not retryable", "INVALID_JOB_CONFIG", http.StatusBadRequest},
		{"JOB_CANNOT_CANCEL is not retryable", "JOB_CANNOT_CANCEL", http.StatusBadRequest},
		{"AUTH_FAILED is not retryable", "AUTH_FAILED", http.StatusUnauthorized},
		{"MISSING_TOKEN is not retryable", "MISSING_TOKEN", http.StatusUnauthorized},
		{"INVALID_TOKEN_FORMAT is not retryable", "INVALID_TOKEN_FORMAT", http.StatusUnauthorized},
		{"INVALID_TOKEN is not retryable", "INVALID_TOKEN", http.StatusUnauthorized},
		{"JOB_NOT_FOUND is not retryable", "JOB_NOT_FOUND", http.StatusNotFound},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			respondWithError(rec, tc.status, tc.code, "test error", "testCommand", nil)

			if rec.Code != tc.status {
				t.Errorf("expected status %d, got %d", tc.status, rec.Code)
			}

			var resp map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}

			errorObj, ok := resp["error"].(map[string]any)
			if !ok {
				t.Fatal("expected error object in response")
			}

			if errorObj["details"] != nil {
				details, ok := errorObj["details"].(map[string]any)
				if ok {
					if _, hasRetryable := details["retryable"]; hasRetryable {
						t.Error("expected no retryable field for client errors")
					}
				}
			}
		})
	}
}

func TestRespondWithErrorWithRetryHintAndDetails(t *testing.T) {
	rec := httptest.NewRecorder()
	hint := &retryHint{Retryable: true, RetryAfter: 30}
	details := map[string]string{"status": "completed"}
	respondWithError(rec, http.StatusBadRequest, "JOB_CANNOT_CANCEL", "Cannot cancel job in terminal state", "cancelJob", hint, details)

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	errorObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error object in response")
	}

	detailsResp, ok := errorObj["details"].(map[string]any)
	if !ok {
		t.Fatal("expected details in error response")
	}

	if detailsResp["retryable"] != true {
		t.Errorf("expected retryable=true, got %v", detailsResp["retryable"])
	}
	if detailsResp["retryAfter"] != float64(30) {
		t.Errorf("expected retryAfter=30, got %v", detailsResp["retryAfter"])
	}
}
