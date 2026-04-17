package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewTokenStoreReturnsEmptyStore(t *testing.T) {
	ts := NewTokenStore()
	if ts == nil {
		t.Fatal("expected non-nil TokenStore")
	}
	if ts.tokens == nil {
		t.Error("expected tokens map to be initialized")
	}
}

func TestTokenStoreStoreAndIsValid(t *testing.T) {
	ts := NewTokenStore()
	info := TokenInfo{
		Username:  "testuser",
		Expiry:    time.Now().Add(1 * time.Hour),
		CreatedAt: time.Now(),
	}

	token := "testval-xyz"
	ts.Store(token, info)

	if !ts.IsValid(token) {
		t.Error("expected stored token to be valid")
	}
}

func TestTokenStoreIsValidReturnsFalseForNonexistentToken(t *testing.T) {
	ts := NewTokenStore()

	if ts.IsValid("nonexistent-token") {
		t.Error("expected nonexistent token to be invalid")
	}
}

func TestTokenStoreIsValidReturnsFalseForExpiredToken(t *testing.T) {
	ts := NewTokenStore()
	info := TokenInfo{
		Username:  "testuser",
		Expiry:    time.Now().Add(-1 * time.Hour), // Already expired
		CreatedAt: time.Now().Add(-2 * time.Hour),
	}

	token := "expired-token"
	ts.Store(token, info)

	if ts.IsValid(token) {
		t.Error("expected expired token to be invalid")
	}

	// Token should have been deleted from store
	if _, ok := ts.tokens[token]; ok {
		t.Error("expected expired token to be deleted from store")
	}
}

func TestTokenStoreDelete(t *testing.T) {
	ts := NewTokenStore()
	info := TokenInfo{
		Username:  "testuser",
		Expiry:    time.Now().Add(1 * time.Hour),
		CreatedAt: time.Now(),
	}

	token := "token-to-delete"
	ts.Store(token, info)

	ts.Delete(token)

	if ts.IsValid(token) {
		t.Error("expected deleted token to be invalid")
	}
}

func TestTokenStoreCleanupExpired(t *testing.T) {
	ts := NewTokenStore()

	// Add expired token
	expiredInfo := TokenInfo{
		Username:  "expired-user",
		Expiry:    time.Now().Add(-1 * time.Hour),
		CreatedAt: time.Now().Add(-2 * time.Hour),
	}
	ts.Store("expired-token", expiredInfo)

	// Add valid token
	validInfo := TokenInfo{
		Username:  "valid-user",
		Expiry:    time.Now().Add(1 * time.Hour),
		CreatedAt: time.Now(),
	}
	ts.Store("valid-token", validInfo)

	ts.CleanupExpired()

	if ts.IsValid("expired-token") {
		t.Error("expected expired token to be removed after cleanup")
	}
	if !ts.IsValid("valid-token") {
		t.Error("expected valid token to remain after cleanup")
	}
}

func TestTokenStoreConcurrentAccess(t *testing.T) {
	ts := NewTokenStore()
	info := TokenInfo{
		Username:  "testuser",
		Expiry:    time.Now().Add(1 * time.Hour),
		CreatedAt: time.Now(),
	}

	// Store some tokens
	for i := 0; i < 100; i++ {
		token := strings.Repeat("token", 8) + strings.TrimLeft(strings.Repeat("x", i), "x")
		if token == "" {
			token = "token"
		}
		ts.Store(token, info)
	}

	// Concurrent reads and writes
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				ts.IsValid("token")
				ts.Store("new-token", info)
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestGenerateTokenProducesNonEmptyToken(t *testing.T) {
	token, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}
	if token == "" {
		t.Error("expected non-empty token")
	}
}

func TestGenerateTokenProducesUniqueTokens(t *testing.T) {
	tokens := make(map[string]bool)
	for i := 0; i < 100; i++ {
		token, err := GenerateToken()
		if err != nil {
			t.Fatalf("GenerateToken returned error: %v", err)
		}
		if tokens[token] {
			t.Errorf("duplicate token generated: %s", token)
		}
		tokens[token] = true
	}
}

func TestGenerateTokenLength(t *testing.T) {
	token, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}
	// 32 bytes base64 encoded should produce a string of length >= 43
	if len(token) < 40 {
		t.Errorf("expected token length >= 40, got %d", len(token))
	}
}

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

func TestTokenInfoStruct(t *testing.T) {
	now := time.Now()
	info := TokenInfo{
		Username:  "testuser",
		Expiry:    now.Add(24 * time.Hour),
		CreatedAt: now,
	}

	if info.Username != "testuser" {
		t.Errorf("expected username 'testuser', got %s", info.Username)
	}
	if info.Expiry.Before(now) {
		t.Error("expected expiry to be in the future")
	}
	if info.CreatedAt != now {
		t.Error("expected createdAt to match")
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

			// For 4xx errors with nil hint, details should not have retryable field
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

	// When hint is provided, it should be in details
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
