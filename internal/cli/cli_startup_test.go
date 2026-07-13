package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

func TestLoadConfig(t *testing.T) {
	t.Run("load failure", func(t *testing.T) {
		restoreCLIState(t)
		wantErr := errors.New("load config")
		loadResolvedFn = func(path string) (*config.Config, error) {
			if path != "" {
				t.Fatalf("LoadResolved path = %q, want default path", path)
			}
			return nil, wantErr
		}

		if _, err := loadConfig(); !errors.Is(err, wantErr) {
			t.Fatalf("loadConfig() error = %v, want %v", err, wantErr)
		}
	})

	t.Run("normalizes views", func(t *testing.T) {
		restoreCLIState(t)
		want := &config.Config{Views: " First "}
		loadResolvedFn = func(string) (*config.Config, error) { return want, nil }

		got, err := loadConfig()
		if err != nil {
			t.Fatalf("loadConfig() unexpected error: %v", err)
		}
		if got != want {
			t.Fatal("loadConfig() returned a different config pointer")
		}
		if got.Views != "left" {
			t.Fatalf("loadConfig() Views = %q, want left", got.Views)
		}
	})
}

func TestInitClient(t *testing.T) {
	t.Run("does not construct client after config failure", func(t *testing.T) {
		restoreCLIState(t)
		wantErr := errors.New("load config")
		loadResolvedFn = func(string) (*config.Config, error) { return nil, wantErr }
		newLoggedInFn = func(context.Context, *config.Config) (*client.Client, error) {
			t.Fatal("NewLoggedIn called after config failure")
			return nil, nil
		}

		cfg, apiClient, err := initClient(context.Background())
		if cfg != nil || apiClient != nil || !errors.Is(err, wantErr) {
			t.Fatalf("initClient() = (%v, %v, %v), want (nil, nil, %v)", cfg, apiClient, err, wantErr)
		}
	})

	t.Run("returns authentication failure", func(t *testing.T) {
		restoreCLIState(t)
		wantCfg := &config.Config{Views: "both"}
		wantErr := errors.New("authenticate")
		loadResolvedFn = func(string) (*config.Config, error) { return wantCfg, nil }
		newLoggedInFn = func(ctx context.Context, cfg *config.Config) (*client.Client, error) {
			if ctx == nil {
				t.Fatal("NewLoggedIn context is nil")
			}
			if cfg != wantCfg {
				t.Fatal("NewLoggedIn received a different config")
			}
			return nil, wantErr
		}

		cfg, apiClient, err := initClient(context.Background())
		if cfg != nil || apiClient != nil || !errors.Is(err, wantErr) {
			t.Fatalf("initClient() = (%v, %v, %v), want (nil, nil, %v)", cfg, apiClient, err, wantErr)
		}
	})

	t.Run("returns initialized dependencies", func(t *testing.T) {
		restoreCLIState(t)
		wantCfg := &config.Config{Views: "second"}
		wantClient := client.New(nil, nil)
		loadResolvedFn = func(string) (*config.Config, error) { return wantCfg, nil }
		newLoggedInFn = func(context.Context, *config.Config) (*client.Client, error) {
			return wantClient, nil
		}

		gotCfg, gotClient, err := initClient(context.Background())
		if err != nil {
			t.Fatalf("initClient() unexpected error: %v", err)
		}
		if gotCfg != wantCfg || gotClient != wantClient {
			t.Fatalf("initClient() = (%p, %p), want (%p, %p)", gotCfg, gotClient, wantCfg, wantClient)
		}
		if gotCfg.Views != "right" {
			t.Fatalf("initClient() normalized Views = %q, want right", gotCfg.Views)
		}
	})
}

func TestRunServeStartup(t *testing.T) {
	t.Run("rejects invalid arguments before loading config", func(t *testing.T) {
		restoreCLIState(t)
		loadResolvedFn = func(string) (*config.Config, error) {
			t.Fatal("LoadResolved called for invalid arguments")
			return nil, nil
		}

		if err := runServe([]string{"--port", "70000"}, "test"); err == nil {
			t.Fatal("runServe() error = nil, want invalid port error")
		}
	})

	t.Run("returns config failure", func(t *testing.T) {
		restoreCLIState(t)
		wantErr := errors.New("load config")
		loadResolvedFn = func(string) (*config.Config, error) { return nil, wantErr }
		startAPIServerFn = func(context.Context, string, *config.Config) error {
			t.Fatal("server started after config failure")
			return nil
		}

		if err := runServe(nil, "test"); !errors.Is(err, wantErr) {
			t.Fatalf("runServe() error = %v, want %v", err, wantErr)
		}
	})

	for _, tc := range []struct {
		name     string
		startErr error
	}{
		{name: "starts server"},
		{name: "returns server failure", startErr: errors.New("listen")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			restoreCLIState(t)
			wantCfg := &config.Config{Views: "first"}
			loadResolvedFn = func(string) (*config.Config, error) { return wantCfg, nil }
			called := false
			startAPIServerFn = func(ctx context.Context, port string, cfg *config.Config) error {
				called = true
				if ctx == nil {
					t.Fatal("server start context is nil")
				}
				if port != "9090" {
					t.Fatalf("server port = %q, want 9090", port)
				}
				if cfg != wantCfg {
					t.Fatal("server received a different config")
				}
				return tc.startErr
			}

			err := runServe([]string{"--port", "9090"}, "test")
			if !called {
				t.Fatal("server start was not called")
			}
			if !errors.Is(err, tc.startErr) {
				t.Fatalf("runServe() error = %v, want %v", err, tc.startErr)
			}
		})
	}
}
