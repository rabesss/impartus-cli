package server

import (
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

func TestConstantTimeStringEqual(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{name: "equal", a: "secret", b: "secret", want: true},
		{name: "different value", a: "secret", b: "Secret", want: false},
		{name: "different length", a: "secret", b: "secrets", want: false},
		{name: "both empty", a: "", b: "", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := constantTimeStringEqual(tt.a, tt.b); got != tt.want {
				t.Fatalf("constantTimeStringEqual(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestStartTokenCleanupStopIsIdempotent(t *testing.T) {
	stop := StartTokenCleanup(NewTokenStore())
	stop()
	stop()
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
