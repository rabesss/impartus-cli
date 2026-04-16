package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

type TokenInfo struct {
	Username  string
	Expiry    time.Time
	CreatedAt time.Time
}

type TokenStore struct {
	tokens map[string]TokenInfo
	mu     sync.RWMutex
}

func NewTokenStore() *TokenStore {
	return &TokenStore{
		tokens: make(map[string]TokenInfo),
	}
}

func (ts *TokenStore) Store(token string, info TokenInfo) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.tokens[token] = info
}

func (ts *TokenStore) IsValid(token string) bool {
	ts.mu.RLock()
	info, ok := ts.tokens[token]
	ts.mu.RUnlock()
	if !ok {
		return false
	}

	if time.Now().After(info.Expiry) {
		ts.Delete(token)
		return false
	}

	return true
}

func (ts *TokenStore) Delete(token string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	delete(ts.tokens, token)
}

func (ts *TokenStore) CleanupExpired() {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	now := time.Now()
	for token, info := range ts.tokens {
		if now.After(info.Expiry) {
			delete(ts.tokens, token)
		}
	}
}

func StartTokenCleanup(tokenStore *TokenStore) {
	ticker := time.NewTicker(1 * time.Hour)
	go func() {
		for range ticker.C {
			tokenStore.CleanupExpired()
		}
	}()
}

func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// responseMeta represents the meta field in API responses
type responseMeta struct {
	Command string `json:"command"`
	Mode    string `json:"mode"`
}

// retryHint indicates whether an error is retryable and how long to wait before retrying
type retryHint struct {
	Retryable  bool `json:"retryable"`
	RetryAfter int  `json:"retryAfter"`
}

func respondWithEnvelope(w http.ResponseWriter, status int, command string, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	envelope := map[string]any{
		"success": true,
		"data":    data,
		"error":   nil,
		"meta": responseMeta{
			Command: command,
			Mode:    "api",
		},
	}

	if err := json.NewEncoder(w).Encode(envelope); err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

func respondWithError(w http.ResponseWriter, status int, code, message, command string, hint *retryHint, details ...any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	errorResp := map[string]any{
		"success": false,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
		"meta": responseMeta{
			Command: command,
			Mode:    "api",
		},
	}

	errorData := errorResp["error"].(map[string]any)
	if hint != nil {
		errorData["details"] = hint
	} else if len(details) > 0 {
		errorData["details"] = details[0]
	}

	if err := json.NewEncoder(w).Encode(errorResp); err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

func respondWithSuccess(w http.ResponseWriter, command string, data map[string]any) {
	respondWithEnvelope(w, http.StatusOK, command, data)
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *APIServer) loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body", "login", nil)
		return
	}

	if s.cfg == nil || s.cfg.Username != req.Username || s.cfg.Password != req.Password {
		respondWithError(w, http.StatusUnauthorized, "AUTH_FAILED", "Invalid username or password", "login", nil)
		return
	}

	token, err := GenerateToken()
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "TOKEN_GENERATION_FAILED", "Failed to generate token", "login", nil)
		return
	}

	expires := time.Now().Add(24 * time.Hour)
	s.tokenStore.Store(token, TokenInfo{
		Username:  req.Username,
		Expiry:    expires,
		CreatedAt: time.Now(),
	})

	respondWithSuccess(w, "login", map[string]any{
		"token":   token,
		"expires": expires,
	})
}

func (s *APIServer) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			respondWithError(w, http.StatusUnauthorized, "MISSING_TOKEN", "Authorization header required", "auth", nil)
			return
		}

		if !strings.HasPrefix(authHeader, "Bearer ") {
			respondWithError(w, http.StatusUnauthorized, "INVALID_TOKEN_FORMAT", "Expected 'Bearer <token>'", "auth", nil)
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if !s.tokenStore.IsValid(token) {
			respondWithError(w, http.StatusUnauthorized, "INVALID_TOKEN", "Token is invalid or expired", "auth", nil)
			return
		}

		next.ServeHTTP(w, r)
	})
}
