//go:build !windows

package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/rabesss/impartus-cli/internal/config"
)

func TestReadTokenCacheFileRejectsFIFOWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token-fifo")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("create token FIFO: %v", err)
	}

	result := make(chan error, 1)
	go func() {
		_, err := readTokenCacheFile(path)
		result <- err
	}()
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "regular file") {
			t.Fatalf("readTokenCacheFile(FIFO) error = %v, want regular-file rejection", err)
		}
	case <-time.After(time.Second):
		t.Fatal("readTokenCacheFile(FIFO) blocked")
	}
}

func TestReadTokenCacheFileDirectlyRejectsFinalSymlink(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("must-not-be-read"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "token-link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if content, err := readTokenCacheFile(link); err == nil {
		t.Fatalf("readTokenCacheFile() followed symlink and returned %q", content)
	}
}

func TestValidatePublishedTokenCacheRejectsWrongModeAndSpecialFile(t *testing.T) {
	wrongMode := filepath.Join(t.TempDir(), "wrong-mode")
	if err := os.WriteFile(wrongMode, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(wrongMode, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := validatePublishedTokenCache(wrongMode); err == nil || !strings.Contains(err.Error(), "want 0600") {
		t.Fatalf("validatePublishedTokenCache(wrong mode) error = %v", err)
	}

	fifo := filepath.Join(t.TempDir(), "token-fifo")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validatePublishedTokenCache(fifo); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("validatePublishedTokenCache(FIFO) error = %v", err)
	}
	if err := writeTokenCache(fifo, []byte("secret")); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("writeTokenCache(FIFO) error = %v", err)
	}
}

func TestPublishTokenCacheFileRejectsInsecureCandidateBeforeReplace(t *testing.T) {
	parent := t.TempDir()
	candidate := filepath.Join(parent, ".token.tmp-insecure")
	destination := filepath.Join(parent, ".token")
	if err := os.WriteFile(destination, []byte("existing-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(candidate, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(candidate, 0o666); err != nil {
		t.Fatal(err)
	}

	err := publishTokenCacheFile(candidate, destination)
	if err == nil || !strings.Contains(err.Error(), "want 0600") {
		t.Fatalf("publishTokenCacheFile(insecure candidate) error = %v, want mode rejection", err)
	}
	if content, readErr := os.ReadFile(destination); readErr != nil || string(content) != "existing-token" {
		t.Fatalf("destination after rejected publish = %q, error = %v; want existing-token", content, readErr)
	}
	if content, readErr := os.ReadFile(candidate); readErr != nil || string(content) != "secret" {
		t.Fatalf("candidate after rejected publish = %q, error = %v; want preserved secret", content, readErr)
	}
}

func TestNewLoggedInRejectsWorldReadableUnixCacheAndRewritesItPrivately(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(cachePath, []byte("unsafe-cached-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cachePath, 0o644); err != nil {
		t.Fatal(err)
	}

	var signIns atomic.Int32
	var profiles atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/auth/signin":
			signIns.Add(1)
			writer.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(writer).Encode(map[string]string{"token": "replacement-token"}); err != nil {
				t.Errorf("encode login response: %v", err)
			}
		case "/user/profile":
			profiles.Add(1)
			if got := request.Header.Get("Authorization"); got != "Bearer replacement-token" {
				t.Errorf("profile used rejected cache token: %q", got)
			}
			writer.WriteHeader(http.StatusOK)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	newConfig := func() *config.Config {
		return &config.Config{
			Username:       "user",
			Password:       "pass",
			BaseURL:        server.URL,
			TokenCachePath: cachePath,
		}
	}
	if _, err := NewLoggedIn(context.Background(), newConfig()); err != nil {
		t.Fatalf("first NewLoggedIn() error = %v", err)
	}
	if signIns.Load() != 1 || profiles.Load() != 0 {
		t.Fatalf("first login calls = sign-in:%d profile:%d, want 1/0", signIns.Load(), profiles.Load())
	}
	content, err := os.ReadFile(cachePath)
	if err != nil || string(content) != "replacement-token" {
		t.Fatalf("rewritten cache = %q, error = %v", content, err)
	}
	info, err := os.Stat(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("rewritten cache mode = %04o, want 0600", info.Mode().Perm())
	}

	if _, err := NewLoggedIn(context.Background(), newConfig()); err != nil {
		t.Fatalf("second NewLoggedIn() error = %v", err)
	}
	if signIns.Load() != 1 || profiles.Load() != 1 {
		t.Fatalf("reuse calls = sign-in:%d profile:%d, want 1/1", signIns.Load(), profiles.Load())
	}
}
