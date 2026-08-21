package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rabesss/impartus-cli/internal/config"
)

func TestNewLoggedInUsesExplicitTokenCachePathAndReusesIt(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "nested", "impartus.token")
	var signIns atomic.Int32
	var profiles atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/signin":
			signIns.Add(1)
			w.Header().Set("Content-Type", "application/json")
			if encodeErr := json.NewEncoder(w).Encode(map[string]string{"token": "explicit-cache-token"}); encodeErr != nil {
				t.Errorf("encode login response: %v", encodeErr)
			}
		case "/user/profile":
			profiles.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer explicit-cache-token" {
				t.Errorf("profile authorization = %q, want explicit cache token", got)
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	firstCfg := &config.Config{
		Username:       "user",
		Password:       "pass",
		BaseURL:        server.URL,
		TokenCachePath: cachePath,
	}
	if _, err := NewLoggedIn(context.Background(), firstCfg); err != nil {
		t.Fatalf("first NewLoggedIn() error = %v", err)
	}
	if got, err := os.ReadFile(cachePath); err != nil || string(got) != "explicit-cache-token" {
		t.Fatalf("explicit token cache = %q, read error = %v", got, err)
	}
	info, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("stat explicit token cache: %v", err)
	}
	if got := info.Mode().Perm(); runtime.GOOS != "windows" && got != 0o600 {
		t.Fatalf("explicit token cache mode = %04o, want 0600", got)
	}

	secondCfg := &config.Config{
		Username:       "user",
		Password:       "pass",
		BaseURL:        server.URL,
		TokenCachePath: cachePath,
	}
	if _, err := NewLoggedIn(context.Background(), secondCfg); err != nil {
		t.Fatalf("second NewLoggedIn() error = %v", err)
	}
	if signIns.Load() != 1 || profiles.Load() != 1 {
		t.Fatalf("login/profile calls = sign-in:%d profile:%d, want one each", signIns.Load(), profiles.Load())
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(cachePath), ".token")); !os.IsNotExist(err) {
		t.Fatalf("legacy cache unexpectedly created beside explicit cache: %v", err)
	}
}

func TestWriteTokenCacheAtomicallyReplacesRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".token")
	if err := writeTokenCache(path, []byte("old-token")); err != nil {
		t.Fatalf("initial writeTokenCache() error = %v", err)
	}
	if err := writeTokenCache(path, []byte("new-token")); err != nil {
		t.Fatalf("replacement writeTokenCache() error = %v", err)
	}
	got, ok := (&Client{}).readStoredTokenAt(path)
	if !ok || got != "new-token" {
		t.Fatalf("readStoredTokenAt() = (%q, %t), want (new-token, true)", got, ok)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat replacement cache: %v", err)
	}
	if got := info.Mode().Perm(); runtime.GOOS != "windows" && got != 0o600 {
		t.Fatalf("replacement cache mode = %04o, want 0600", got)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".token.tmp-*"))
	if err != nil {
		t.Fatalf("glob temporary token files: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary token files remain after atomic replacement: %v", matches)
	}
}

func TestTokenCacheRejectsSymlinkedParentAndTarget(t *testing.T) {
	parentTarget := t.TempDir()
	parentLink := filepath.Join(t.TempDir(), "cache-parent")
	if err := os.Symlink(parentTarget, parentLink); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	parentErr := writeTokenCache(filepath.Join(parentLink, "token"), []byte("secret"))
	if parentErr == nil || !strings.Contains(parentErr.Error(), "token cache parent") {
		t.Fatalf("writeTokenCache(symlink parent) error = %v, want parent rejection", parentErr)
	}
	if _, err := os.Stat(filepath.Join(parentTarget, "token")); !os.IsNotExist(err) {
		t.Fatalf("symlink parent target was written: %v", err)
	}

	target := filepath.Join(t.TempDir(), "real-token")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatalf("write real target: %v", err)
	}
	link := filepath.Join(t.TempDir(), "token")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("target symlinks unavailable: %v", err)
	}
	if err := writeTokenCache(link, []byte("new")); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("writeTokenCache(symlink target) error = %v, want target rejection", err)
	}
	got, ok := (&Client{}).readStoredTokenAt(link)
	if ok || got != "" {
		t.Fatalf("readStoredTokenAt(symlink target) = (%q, %t), want empty false", got, ok)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "old" {
		t.Fatalf("symlink target changed: %q, read error = %v", got, err)
	}
}

func TestTokenCacheAllowsSymlinkedAncestorWithRealImmediateParent(t *testing.T) {
	realRoot := t.TempDir()
	linkedRoot := filepath.Join(t.TempDir(), "cache-root")
	if err := os.Symlink(realRoot, linkedRoot); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	path := filepath.Join(linkedRoot, "private", "token")
	if err := writeTokenCache(path, []byte("secret")); err != nil {
		t.Fatalf("writeTokenCache(safe ancestor symlink) error = %v", err)
	}
	got, ok := (&Client{}).readStoredTokenAt(path)
	if !ok || got != "secret" {
		t.Fatalf("readStoredTokenAt(safe ancestor symlink) = (%q, %t), want secret true", got, ok)
	}
}

func TestLegacyTokenCacheDefaultRemainsDotToken(t *testing.T) {
	root := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) }) //nolint:errcheck

	if err := (&Client{}).storeToken(&config.Config{}, "legacy-token"); err != nil {
		t.Fatalf("storeToken() error = %v", err)
	}
	if got, err := os.ReadFile(config.DefaultTokenCachePath); err != nil || string(got) != "legacy-token" {
		t.Fatalf("legacy token cache = %q, read error = %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(root, "token")); !os.IsNotExist(err) {
		t.Fatalf("unexpected alternate token path: %v", err)
	}
}

func TestLegacyTokenCacheWorksFromLogicalSymlinkWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	realDirectory := filepath.Join(root, "release")
	logicalDirectory := filepath.Join(root, "current")
	if err := os.Mkdir(realDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realDirectory, logicalDirectory); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	previousDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	previousPWD, hadPWD := os.LookupEnv("PWD")
	t.Cleanup(func() {
		_ = os.Chdir(previousDirectory) //nolint:errcheck
		if hadPWD {
			_ = os.Setenv("PWD", previousPWD) //nolint:errcheck
		} else {
			_ = os.Unsetenv("PWD") //nolint:errcheck
		}
	})
	if err := os.Chdir(logicalDirectory); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("PWD", logicalDirectory); err != nil {
		t.Fatal(err)
	}

	if err := (&Client{}).storeToken(&config.Config{}, "logical-cwd-token"); err != nil {
		t.Fatalf("storeToken() from logical symlink cwd error = %v", err)
	}
	if token, ok := (&Client{}).readStoredToken(); !ok || token != "logical-cwd-token" {
		t.Fatalf("readStoredToken() = (%q, %t), want logical-cwd-token true", token, ok)
	}
}
