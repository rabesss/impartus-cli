package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// TokenInfo holds metadata for an authenticated API token.
type TokenInfo struct {
	Username  string
	Expiry    time.Time
	CreatedAt time.Time
}

// TokenStore manages API authentication tokens with thread-safe access.
type TokenStore struct {
	tokens map[string]TokenInfo
	mu     sync.RWMutex
}

// NewTokenStore creates a new empty token store.
func NewTokenStore() *TokenStore {
	return &TokenStore{
		tokens: make(map[string]TokenInfo),
	}
}

// Store adds a token with its metadata to the store.
func (ts *TokenStore) Store(token string, info TokenInfo) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.tokens[token] = info
}

// IsValid checks whether a token is present and not expired, removing it if expired.
func (ts *TokenStore) IsValid(token string) bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	info, ok := ts.tokens[token]
	if !ok {
		return false
	}

	if time.Now().After(info.Expiry) {
		delete(ts.tokens, token)
		return false
	}

	return true
}

// Delete removes a token from the store.
func (ts *TokenStore) Delete(token string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	delete(ts.tokens, token)
}

// CleanupExpired removes all expired tokens from the store.
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

// StartTokenCleanup starts a background goroutine that periodically removes expired tokens.
// It returns a stop function that should be called on shutdown.
func StartTokenCleanup(tokenStore *TokenStore) func() {
	ticker := time.NewTicker(1 * time.Hour)
	stop := make(chan struct{})
	var stopOnce sync.Once

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				tokenStore.CleanupExpired()
			case <-stop:
				return
			}
		}
	}()

	return func() {
		stopOnce.Do(func() {
			close(stop)
		})
	}
}

// GenerateToken creates a cryptographically secure random token encoded in URL-safe base64.
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func constantTimeStringEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func (s *APIServer) loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Extract client IP for rate limiting
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	clientIP := host

	if !s.loginLimiter.allow(clientIP) {
		respondWithError(w, http.StatusTooManyRequests, "RATE_LIMITED", "Too many login attempts. Please try again later.", "login", &retryHint{Retryable: true, RetryAfter: 60})
		return
	}

	var req loginRequest
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB limit
	if err = json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body", "login", nil)
		return
	}

	if s.cfg == nil || !constantTimeStringEqual(s.cfg.Username, req.Username) || !constantTimeStringEqual(s.cfg.Password, req.Password) {
		respondWithError(w, http.StatusUnauthorized, "AUTH_FAILED", "Invalid username or password", "login", nil)
		return
	}

	// Reset rate limiter on successful login
	s.loginLimiter.reset(clientIP)

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

	respondWithSuccess(w, "login", loginResponse{Token: token, Expires: expires})
}

func (s *APIServer) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			respondWithError(w, http.StatusUnauthorized, "MISSING_TOKEN", "Authorization header required", "validateAuth", nil)
			return
		}

		if !strings.HasPrefix(authHeader, "Bearer ") {
			respondWithError(w, http.StatusUnauthorized, "INVALID_TOKEN_FORMAT", "Expected 'Bearer <token>'", "validateAuth", nil)
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if !s.tokenStore.IsValid(token) {
			respondWithError(w, http.StatusUnauthorized, "INVALID_TOKEN", "Token is invalid or expired", "validateAuth", nil)
			return
		}

		next.ServeHTTP(w, r)
	})
}
